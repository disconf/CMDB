package toolbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/k8s"
	"cmdb/gateway-bff/internal/topology"
)

type Credential struct {
	Username string
	Secret   string
}

type CredentialResolver func(string) (Credential, error)

type KubernetesDiagnoser interface {
	DiagnosePod(context.Context, string, string, string) (k8s.PodDiagnosis, error)
}

type TopologyGraphProvider interface {
	Graph(string) topology.Graph
}

type Service struct {
	store       executionStore
	tools       []Tool
	credentials CredentialResolver
	kubernetes  KubernetesDiagnoser
	topology    TopologyGraphProvider
	timeout     time.Duration
}

func NewService() *Service {
	s := &Service{store: newMemoryStore(), timeout: toolboxTimeout()}
	if databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL")); databaseURL != "" {
		store, err := openPostgres(databaseURL)
		if err != nil {
			panic("migrate toolbox repository: " + err.Error())
		}
		s.store = store
	}
	s.tools = toolDefinitions()
	return s
}

func toolboxTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv("TOOLBOX_TIMEOUT"))
	if value == "" {
		return 12 * time.Second
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < time.Second || duration > time.Minute {
		return 12 * time.Second
	}
	return duration
}

func toolDefinitions() []Tool {
	return []Tool{
		{ID: "dns", Name: "DNS 解析", Category: "network", Description: "解析域名 A/AAAA 记录并检查解析时延", Icon: "Search", Placeholder: "example.com", Risk: "low", ReadOnly: true, Fields: []InputField{{Name: "target", Label: "域名", Placeholder: "api.example.com", Required: true}}},
		{ID: "tcp", Name: "TCP 端口探测", Category: "network", Description: "从服务端验证 TCP 端口可达性与握手时延", Icon: "Network", Placeholder: "10.20.1.11:443", Risk: "low", ReadOnly: true, Fields: []InputField{{Name: "target", Label: "目标地址", Placeholder: "10.20.1.11:443", Required: true}}},
		{ID: "http", Name: "HTTP 接口测试", Category: "network", Description: "执行只读 HTTP GET，返回状态码、响应时延和截断响应", Icon: "Radio", Placeholder: "https://example.com/health", Risk: "low", ReadOnly: true, Fields: []InputField{{Name: "target", Label: "URL", Placeholder: "https://example.com/health", Required: true}}},
		{ID: "tls", Name: "SSL 证书检查", Category: "network", Description: "检查 TLS 握手、证书主体、签发者、有效期和剩余天数", Icon: "ShieldCheck", Placeholder: "example.com:443", Risk: "low", ReadOnly: true, Fields: []InputField{{Name: "target", Label: "地址", Placeholder: "example.com:443", Required: true}}},
		{ID: "prometheus", Name: "Prometheus 查询", Category: "observability", Description: "执行即时 PromQL 查询，用于指标核验和故障排查", Icon: "Activity", Placeholder: "up", Risk: "low", ReadOnly: true, Fields: []InputField{{Name: "query", Label: "PromQL", Placeholder: "up{job=\"cmdb-node-exporter\"}", Required: true}}},
		{ID: "loki", Name: "Loki 日志检索", Category: "logs", Description: "执行 LogQL 范围查询，返回最近日志流", Icon: "FileSearch", Placeholder: "{app=\"gateway\"} |= \"error\"", Risk: "low", ReadOnly: true, Fields: []InputField{{Name: "query", Label: "LogQL", Placeholder: "{app=\"gateway\"} |= \"error\"", Required: true}}},
		{ID: "k8s-pod", Name: "Pod 异常诊断", Category: "kubernetes", Description: "汇总 Pod 状态、重启次数、事件和最近容器日志", Icon: "Boxes", Placeholder: "ops-qa/gateway-xxx", Risk: "medium", ReadOnly: true, Fields: []InputField{{Name: "namespace", Label: "命名空间", Placeholder: "ops-qa", Required: true}, {Name: "name", Label: "Pod 名称", Placeholder: "gateway-xxxx", Required: true}, {Name: "container", Label: "容器（可选）", Placeholder: "gateway"}}},
		{ID: "host-health", Name: "主机健康检查", Category: "host", Description: "通过 Vault 凭据 SSH 检查负载、内存、磁盘和运行时间", Icon: "Server", Placeholder: "10.20.1.11:22", Risk: "low", ReadOnly: true, Fields: []InputField{{Name: "target", Label: "主机地址", Placeholder: "10.20.1.11:22", Required: true}, {Name: "credentialId", Label: "SSH 凭据", Placeholder: "cred-...", Required: true, Secret: true}}},
		{ID: "host-process", Name: "进程查询", Category: "host", Description: "通过固定只读命令查询匹配进程的 PID、用户和命令", Icon: "Cpu", Placeholder: "10.20.1.11:22", Risk: "low", ReadOnly: true, Fields: []InputField{{Name: "target", Label: "主机地址", Placeholder: "10.20.1.11:22", Required: true}, {Name: "query", Label: "进程关键字", Placeholder: "nginx", Required: true}, {Name: "credentialId", Label: "SSH 凭据", Placeholder: "cred-...", Required: true, Secret: true}}},
		{ID: "host-port", Name: "监听端口查询", Category: "host", Description: "通过固定只读命令检查指定端口监听和进程占用", Icon: "Network", Placeholder: "10.20.1.11:22", Risk: "low", ReadOnly: true, Fields: []InputField{{Name: "target", Label: "主机地址", Placeholder: "10.20.1.11:22", Required: true}, {Name: "query", Label: "端口", Placeholder: "443", Required: true}, {Name: "credentialId", Label: "SSH 凭据", Placeholder: "cred-...", Required: true, Secret: true}}},
		{ID: "db-connect", Name: "数据库连通性", Category: "database", Description: "通过 Vault 凭据连接 PostgreSQL 并执行只读 SELECT 1", Icon: "Database", Placeholder: "10.20.1.30:5432", Risk: "low", ReadOnly: true, Fields: []InputField{{Name: "target", Label: "数据库地址", Placeholder: "10.20.1.30:5432", Required: true}, {Name: "query", Label: "数据库名", Placeholder: "postgres", Required: true}, {Name: "credentialId", Label: "数据库凭据", Placeholder: "cred-...", Required: true, Secret: true}}},
		{ID: "topology-path", Name: "拓扑路径检查", Category: "topology", Description: "根据 CMDB 关系图计算两个资产之间的最短依赖路径", Icon: "GitBranch", Placeholder: "source-asset-id", Risk: "low", ReadOnly: true, Fields: []InputField{{Name: "target", Label: "源资产", Placeholder: "asset-id 或名称", Required: true}, {Name: "query", Label: "目标资产", Placeholder: "asset-id 或名称", Required: true}, {Name: "environment", Label: "环境（可选）", Placeholder: "production"}}},
	}
}

func (s *Service) SetCredentialResolver(resolver CredentialResolver)       { s.credentials = resolver }
func (s *Service) SetKubernetesDiagnoser(diagnoser KubernetesDiagnoser)    { s.kubernetes = diagnoser }
func (s *Service) SetTopologyGraphProvider(provider TopologyGraphProvider) { s.topology = provider }

func (s *Service) Tools() []Tool {
	result := append([]Tool(nil), s.tools...)
	for i := range result {
		result[i].Configured = s.toolConfigured(result[i].ID)
	}
	return result
}

func (s *Service) toolConfigured(id string) bool {
	switch id {
	case "prometheus":
		return strings.TrimSpace(os.Getenv("PROMETHEUS_URL")) != ""
	case "loki":
		return strings.TrimSpace(os.Getenv("LOKI_URL")) != ""
	case "k8s-pod":
		return s.kubernetes != nil
	case "host-health", "host-process", "host-port", "db-connect":
		return s.credentials != nil
	case "topology-path":
		return s.topology != nil
	default:
		return true
	}
}

func (s *Service) Summary(ctx context.Context) (Summary, error) {
	summary, err := s.store.Summary(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary.Tools = len(s.tools)
	for _, tool := range s.tools {
		if s.toolConfigured(tool.ID) {
			summary.ConfiguredTools++
		}
	}
	return summary, nil
}

func (s *Service) History(ctx context.Context, limit int) ([]Result, error) {
	return s.store.List(ctx, limit)
}

func (s *Service) Run(ctx context.Context, in RunInput) (Result, error) {
	tool, ok := s.tool(in.ToolID)
	if !ok {
		return Result{}, ErrValidation
	}
	if !s.toolConfigured(tool.ID) {
		return Result{}, ErrNotConfigured
	}
	if err := validateInput(tool.ID, in); err != nil {
		return Result{}, ErrValidation
	}
	now := time.Now()
	result := Result{ID: newExecutionID(), ToolID: tool.ID, ToolName: tool.Name, Category: tool.Category, Target: displayTarget(in), Operator: strings.TrimSpace(in.Operator), StartedAt: now.Format("2006-01-02 15:04:05"), Output: []Line{}, Parameters: inputParameters(in)}
	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	lines, status, summary, err := s.execute(runCtx, tool.ID, in)
	result.Output = append(result.Output, lines...)
	result.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
	result.DurationMs = time.Since(now).Milliseconds()
	result.Duration = formatDuration(result.DurationMs)
	result.Status = status
	result.Summary = summary
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		if result.Summary == "" {
			result.Summary = "诊断执行失败"
		}
		result.Output = append(result.Output, Line{Time: time.Now().Format("15:04:05"), Level: "error", Text: err.Error()})
	}
	if result.Status == "" {
		result.Status = "success"
	}
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer saveCancel()
	if saveErr := s.store.Save(saveCtx, result); saveErr != nil {
		return Result{}, fmt.Errorf("archive diagnostic result: %w", saveErr)
	}
	if err != nil {
		return result, nil
	}
	return result, nil
}

func (s *Service) tool(id string) (Tool, bool) {
	id = strings.TrimSpace(id)
	for _, item := range s.tools {
		if item.ID == id {
			return item, true
		}
	}
	return Tool{}, false
}

func (s *Service) execute(ctx context.Context, toolID string, in RunInput) ([]Line, string, string, error) {
	switch toolID {
	case "dns":
		return s.runDNS(ctx, in)
	case "tcp":
		return s.runTCP(ctx, in)
	case "http":
		return s.runHTTP(ctx, in)
	case "tls":
		return s.runTLS(ctx, in)
	case "prometheus":
		return s.runPrometheus(ctx, in)
	case "loki":
		return s.runLoki(ctx, in)
	case "k8s-pod":
		return s.runKubernetesPod(ctx, in)
	case "host-health":
		return s.runHostHealth(ctx, in)
	case "host-process":
		return s.runHostProcess(ctx, in)
	case "host-port":
		return s.runHostPort(ctx, in)
	case "db-connect":
		return s.runDatabase(ctx, in)
	case "topology-path":
		return s.runTopologyPath(in)
	default:
		return nil, "failed", "不支持的工具", ErrValidation
	}
}

func validateInput(toolID string, in RunInput) error {
	required := map[string][]string{
		"dns":           {"target"},
		"tcp":           {"target"},
		"http":          {"target"},
		"tls":           {"target"},
		"prometheus":    {"query"},
		"loki":          {"query"},
		"k8s-pod":       {"namespace", "name"},
		"host-health":   {"target", "credentialId"},
		"host-process":  {"target", "query", "credentialId"},
		"host-port":     {"target", "query", "credentialId"},
		"db-connect":    {"target", "query", "credentialId"},
		"topology-path": {"target", "query"},
	}
	values := map[string]string{"target": in.Target, "query": in.Query, "credentialId": in.CredentialID, "namespace": in.Namespace, "name": in.Name, "container": in.Container, "environment": in.Environment}
	for _, field := range required[toolID] {
		if strings.TrimSpace(values[field]) == "" {
			return ErrValidation
		}
	}
	if len(in.Target) > 2048 || len(in.Query) > 4096 || len(in.Name) > 253 || len(in.Namespace) > 253 {
		return ErrValidation
	}
	return nil
}

func displayTarget(in RunInput) string {
	for _, value := range []string{in.Target, in.Query, in.Name} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "-"
}

func inputParameters(in RunInput) map[string]string {
	values := map[string]string{}
	if value := strings.TrimSpace(in.Target); value != "" {
		values["target"] = value
	}
	if value := strings.TrimSpace(in.Query); value != "" {
		values["query"] = value
	}
	if value := strings.TrimSpace(in.CredentialID); value != "" {
		values["credentialId"] = value
	}
	if value := strings.TrimSpace(in.Namespace); value != "" {
		values["namespace"] = value
	}
	if value := strings.TrimSpace(in.Name); value != "" {
		values["name"] = value
	}
	if value := strings.TrimSpace(in.Container); value != "" {
		values["container"] = value
	}
	if value := strings.TrimSpace(in.Environment); value != "" {
		values["environment"] = value
	}
	return values
}

func newExecutionID() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "diag-" + hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("diag-%d", time.Now().UnixNano())
}
