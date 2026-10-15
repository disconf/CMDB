package system

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type ReadinessCheck struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
	Detail   string `json:"detail"`
}

type ReadinessReport struct {
	Status      string           `json:"status"`
	Score       int              `json:"score"`
	Passed      int              `json:"passed"`
	Warning     int              `json:"warning"`
	Failed      int              `json:"failed"`
	GeneratedAt string           `json:"generatedAt"`
	Checks      []ReadinessCheck `json:"checks"`
}

func (s *Service) ProductionReadiness(ctx context.Context) ReadinessReport {
	checks := []ReadinessCheck{}
	if s.db != nil {
		if err := s.db.PingContext(ctx); err == nil {
			checks = append(checks, readinessPass("postgres", "PostgreSQL 主库", "数据", true, "连接与查询正常"))
		} else {
			checks = append(checks, readinessFail("postgres", "PostgreSQL 主库", "数据", true, err.Error()))
		}
		extensions := 0
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM pg_extension WHERE extname='vector'`).Scan(&extensions); err == nil && extensions > 0 {
			checks = append(checks, readinessPass("pgvector", "pgvector 向量扩展", "AI", true, "扩展已启用"))
		} else {
			checks = append(checks, readinessFail("pgvector", "pgvector 向量扩展", "AI", true, "扩展未启用"))
		}
	} else {
		checks = append(checks, readinessFail("postgres", "PostgreSQL 主库", "数据", true, "DATABASE_URL 未配置"))
	}
	checks = append(checks, readinessAnyEnv("redis", "Redis 缓存", "数据", true, "REDIS_URL", "REDIS_CLUSTER_ADDRS", "REMOTE_TERMINAL_REDIS_URL"))
	checks = append(checks, readinessEnv("kafka", "Kafka 事件总线", "数据", true, "KAFKA_BROKERS"))
	checks = append(checks, readinessAnyEnv("keycloak", "Keycloak 认证", "安全", true, "KEYCLOAK_ISSUER_URL", "KEYCLOAK_BASE_URL", "KEYCLOAK_ADMIN_URL"))
	checks = append(checks, readinessVault(ctx, envValue("VAULT_ADDR"), envValue("VAULT_TOKEN")))
	checks = append(checks, readinessEnv("object_storage", "MinIO/S3 对象存储", "存储", true, "S3_ENDPOINT_URL", "S3_BUCKET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY"))
	checks = append(checks, readinessHTTP(ctx, "loki", "Loki 日志", "可观测性", false, envValue("LOKI_URL"), "/ready"))
	checks = append(checks, readinessHTTP(ctx, "neo4j", "Neo4j 图数据库", "拓扑", false, envValue("NEO4J_URL"), "/"))
	checks = append(checks, readinessHTTP(ctx, "opensearch", "OpenSearch 搜索", "检索", false, envValue("OPENSEARCH_URL"), "/_cluster/health"))
	checks = append(checks, readinessOptionalEnv("temporal", "Temporal 工作流", "流程", "TEMPORAL_ADDRESS"))
	checks = append(checks, readinessHTTP(ctx, "thanos", "Thanos 长期存储", "可观测性", false, envValue("THANOS_URL"), "/-/ready"))
	checks = append(checks, readinessHTTP(ctx, "tempo", "Tempo 链路追踪", "可观测性", false, envValue("TEMPO_URL"), "/ready"))
	checks = append(checks, readinessHTTP(ctx, "otel", "OpenTelemetry 链路导出", "可观测性", false, envValue("OTEL_EXPORTER_OTLP_ENDPOINT"), "/"))
	checks = append(checks, readinessHTTP(ctx, "prometheus", "Prometheus", "监控", true, envValue("PROMETHEUS_URL"), "/-/healthy"))
	checks = append(checks, readinessHTTP(ctx, "alertmanager", "Alertmanager", "监控", true, envValue("ALERTMANAGER_URL"), "/-/healthy"))
	checks = append(checks, readinessHTTP(ctx, "argocd", "Argo CD", "发布", false, envValue("CICD_ARGOCD_URL"), "/healthz"))
	checks = append(checks, readinessHTTP(ctx, "rancher", "Rancher", "平台", false, envValue("RANCHER_URL"), "/ping"))
	report := ReadinessReport{Status: "passed", GeneratedAt: time.Now().Format("2006-01-02 15:04:05"), Checks: checks}
	for _, item := range checks {
		switch item.Status {
		case "passed":
			report.Passed++
		case "warning":
			report.Warning++
		case "failed":
			report.Failed++
		}
	}
	report.Score = 100
	if len(checks) > 0 {
		report.Score = report.Passed * 100 / len(checks)
	}
	if report.Failed > 0 {
		report.Status = "blocked"
	} else if report.Warning > 0 {
		report.Status = "warning"
	}
	return report
}

func readinessPass(key, name, category string, required bool, detail string) ReadinessCheck {
	return ReadinessCheck{Key: key, Name: name, Category: category, Status: "passed", Required: required, Detail: detail}
}
func readinessFail(key, name, category string, required bool, detail string) ReadinessCheck {
	return ReadinessCheck{Key: key, Name: name, Category: category, Status: "failed", Required: required, Detail: detail}
}
func readinessWarning(key, name, category string, required bool, detail string) ReadinessCheck {
	return ReadinessCheck{Key: key, Name: name, Category: category, Status: "warning", Required: required, Detail: detail}
}
func readinessEnv(key, name, category string, required bool, names ...string) ReadinessCheck {
	missing := []string{}
	for _, env := range names {
		if envValue(env) == "" {
			missing = append(missing, env)
		}
	}
	if len(missing) > 0 {
		if required {
			return readinessFail(key, name, category, true, "缺少配置："+strings.Join(missing, ", "))
		}
		return readinessWarning(key, name, category, false, "缺少配置："+strings.Join(missing, ", "))
	}
	return readinessPass(key, name, category, required, "配置完整")
}
func readinessAnyEnv(key, name, category string, required bool, names ...string) ReadinessCheck {
	for _, env := range names {
		if envValue(env) != "" {
			return readinessPass(key, name, category, required, env+" 已配置")
		}
	}
	return readinessFail(key, name, category, required, "未配置："+strings.Join(names, " / "))
}
func readinessOptionalEnv(key, name, category, env string) ReadinessCheck {
	if envValue(env) == "" {
		return readinessWarning(key, name, category, false, env+" 未配置")
	}
	return readinessPass(key, name, category, false, "配置完整")
}
// readinessVault verifies that Vault answers and that the configured token is
// still valid. A presence-only check hides expired tokens, which silently break
// the credential centre and every task that needs a stored SSH login.
func readinessVault(ctx context.Context, addr, token string) ReadinessCheck {
	if strings.TrimSpace(addr) == "" {
		return readinessFail("vault", "Vault 凭据中心", "安全", true, "VAULT_ADDR 未配置")
	}
	if strings.TrimSpace(token) == "" {
		return readinessFail("vault", "Vault 凭据中心", "安全", true, "VAULT_TOKEN 未配置")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimRight(addr, "/")+"/v1/auth/token/lookup-self", nil)
	if err != nil {
		return readinessFail("vault", "Vault 凭据中心", "安全", true, err.Error())
	}
	request.Header.Set("X-Vault-Token", token)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	response, err := client.Do(request)
	if err != nil {
		return readinessFail("vault", "Vault 凭据中心", "安全", true, "无法连接 Vault："+err.Error())
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	switch {
	case response.StatusCode == http.StatusOK:
		return readinessPass("vault", "Vault 凭据中心", "安全", true, "连接与令牌校验通过")
	case response.StatusCode == http.StatusForbidden:
		return readinessFail("vault", "Vault 凭据中心", "安全", true, "VAULT_TOKEN 无效或已过期，请重新签发令牌并更新 Secret cmdb-gateway-env")
	default:
		return readinessFail("vault", "Vault 凭据中心", "安全", true, fmt.Sprintf("Vault 返回 HTTP %d", response.StatusCode))
	}
}

func readinessHTTP(ctx context.Context, key, name, category string, required bool, baseURL, endpoint string) ReadinessCheck {
	if strings.TrimSpace(baseURL) == "" {
		return readinessWarning(key, name, category, required, "服务地址未配置")
	}
	url := strings.TrimRight(baseURL, "/") + endpoint
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return readinessWarning(key, name, category, required, err.Error())
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	response, err := client.Do(request)
	if err != nil {
		return readinessWarning(key, name, category, required, err.Error())
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 500 {
		return readinessPass(key, name, category, required, fmt.Sprintf("HTTP %d", response.StatusCode))
	}
	return readinessWarning(key, name, category, required, fmt.Sprintf("HTTP %d", response.StatusCode))
}
func envValue(key string) string { return strings.TrimSpace(os.Getenv(key)) }
