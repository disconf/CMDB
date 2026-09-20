package cicd

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	ErrNotFound      = errors.New("integration not found")
	ErrNotConfigured = errors.New("integration not configured")
)

const (
	IntegrationHarbor = "harbor"
	IntegrationArgoCD = "argocd"
	IntegrationGitLab = "gitlab"

	StatusNotConfigured = "not_configured"
	StatusUnknown       = "unknown"
	StatusConnected     = "connected"
	StatusError         = "error"
)

var integrationOrder = []string{IntegrationHarbor, IntegrationArgoCD, IntegrationGitLab}

type Config struct {
	InsecureSkipVerify bool
	HTTPTimeout        time.Duration
	HarborURL          string
	HarborUsername     string
	HarborPassword     string
	ArgoCDURL          string
	ArgoCDToken        string
	ArgoCDUsername     string
	ArgoCDPassword     string
	GitLabURL          string
	GitLabToken        string
}

type Integration struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Type          string            `json:"type"`
	Endpoint      string            `json:"endpoint"`
	Enabled       bool              `json:"enabled"`
	Status        string            `json:"status"`
	Message       string            `json:"message"`
	LastCheckedAt string            `json:"lastCheckedAt,omitempty"`
	Details       map[string]string `json:"details,omitempty"`
}

type Service struct {
	mu           sync.RWMutex
	config       Config
	integrations map[string]Integration
	client       *http.Client
	db           *sql.DB

	resourceMu    sync.Mutex
	resourceCache ResourceOverview
	resourceAt    time.Time
}

type probeResult struct {
	Message string
	Details map[string]string
}

func NewService() *Service {
	service := NewServiceWithConfig(configFromEnv())
	if databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL")); databaseURL != "" {
		db, err := sql.Open("pgx", databaseURL)
		if err != nil {
			panic(fmt.Sprintf("initialize CI/CD status repository: %v", err))
		}
		if err := service.initStatusStore(context.Background(), db); err != nil {
			_ = db.Close()
			panic(fmt.Sprintf("migrate CI/CD status repository: %v", err))
		}
		service.db = db
	}
	return service
}

func NewServiceWithConfig(config Config) *Service {
	config = normalizeConfig(config)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: config.InsecureSkipVerify} // #nosec G402 - explicitly controlled by deployment configuration.
	service := &Service{
		config:       config,
		integrations: make(map[string]Integration, len(integrationOrder)),
		client:       &http.Client{Timeout: config.HTTPTimeout, Transport: transport},
	}
	service.integrations[IntegrationHarbor] = Integration{
		ID:       IntegrationHarbor,
		Name:     "Harbor 镜像仓库",
		Type:     "container-registry",
		Endpoint: strings.TrimRight(config.HarborURL, "/"),
		Enabled:  config.HarborURL != "" && config.HarborUsername != "" && config.HarborPassword != "",
		Status:   StatusNotConfigured,
		Message:  "请配置 Harbor 地址和管理员凭据",
		Details: map[string]string{
			"authMode":    "basic",
			"requiredEnv": "CICD_HARBOR_URL, CICD_HARBOR_USERNAME, CICD_HARBOR_PASSWORD",
		},
	}
	service.integrations[IntegrationArgoCD] = Integration{
		ID:       IntegrationArgoCD,
		Name:     "Argo CD 持续部署",
		Type:     "gitops",
		Endpoint: strings.TrimRight(config.ArgoCDURL, "/"),
		Enabled:  config.ArgoCDURL != "" && (config.ArgoCDToken != "" || (config.ArgoCDUsername != "" && config.ArgoCDPassword != "")),
		Status:   StatusNotConfigured,
		Message:  "请配置 Argo CD 地址与认证信息",
		Details: map[string]string{
			"authMode":    argoAuthMode(config),
			"requiredEnv": "CICD_ARGOCD_URL, CICD_ARGOCD_TOKEN 或 CICD_ARGOCD_USERNAME/CICD_ARGOCD_PASSWORD",
		},
	}
	service.integrations[IntegrationGitLab] = Integration{
		ID:       IntegrationGitLab,
		Name:     "GitLab 代码仓库",
		Type:     "source-control",
		Endpoint: strings.TrimRight(config.GitLabURL, "/"),
		Enabled:  config.GitLabURL != "" && config.GitLabToken != "",
		Status:   StatusNotConfigured,
		Message:  "请配置 GitLab 地址与访问令牌",
		Details: map[string]string{
			"authMode":    "private-token",
			"requiredEnv": "CICD_GITLAB_URL, CICD_GITLAB_TOKEN",
		},
	}
	if item := service.integrations[IntegrationHarbor]; item.Enabled {
		item.Status = StatusUnknown
		item.Message = "配置已就绪，等待连接检测"
		service.integrations[IntegrationHarbor] = item
	}
	if item := service.integrations[IntegrationArgoCD]; item.Enabled {
		item.Status = StatusUnknown
		item.Message = "配置已就绪，等待连接检测"
		service.integrations[IntegrationArgoCD] = item
	}
	if item := service.integrations[IntegrationGitLab]; item.Enabled {
		item.Status = StatusUnknown
		item.Message = "配置已就绪，等待连接检测"
		service.integrations[IntegrationGitLab] = item
	}
	return service
}

func configFromEnv() Config {
	return Config{
		InsecureSkipVerify: boolFromEnv("CICD_INSECURE_SKIP_VERIFY"),
		HTTPTimeout:        durationFromEnv("CICD_HTTP_TIMEOUT", 8*time.Second),
		HarborURL:          firstNonEmpty(os.Getenv("CICD_HARBOR_URL"), "https://harbor.qa.bcavt.com"),
		HarborUsername:     strings.TrimSpace(os.Getenv("CICD_HARBOR_USERNAME")),
		HarborPassword:     strings.TrimSpace(os.Getenv("CICD_HARBOR_PASSWORD")),
		ArgoCDURL:          firstNonEmpty(os.Getenv("CICD_ARGOCD_URL"), "http://argocd-server.ops-qa.svc.cluster.local"),
		ArgoCDToken:        strings.TrimSpace(os.Getenv("CICD_ARGOCD_TOKEN")),
		ArgoCDUsername:     strings.TrimSpace(os.Getenv("CICD_ARGOCD_USERNAME")),
		ArgoCDPassword:     strings.TrimSpace(os.Getenv("CICD_ARGOCD_PASSWORD")),
		GitLabURL:          strings.TrimSpace(os.Getenv("CICD_GITLAB_URL")),
		GitLabToken:        strings.TrimSpace(os.Getenv("CICD_GITLAB_TOKEN")),
	}
}

func normalizeConfig(config Config) Config {
	if config.HTTPTimeout <= 0 {
		config.HTTPTimeout = 8 * time.Second
	}
	config.HarborURL = strings.TrimRight(strings.TrimSpace(config.HarborURL), "/")
	config.ArgoCDURL = strings.TrimRight(strings.TrimSpace(config.ArgoCDURL), "/")
	config.GitLabURL = strings.TrimRight(strings.TrimSpace(config.GitLabURL), "/")
	return config
}

func (s *Service) List() []Integration {
	s.reloadStatusStore(context.Background())
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Integration, 0, len(integrationOrder))
	for _, id := range integrationOrder {
		if item, ok := s.integrations[id]; ok {
			items = append(items, cloneIntegration(item))
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return integrationIndex(items[i].ID) < integrationIndex(items[j].ID)
	})
	return items
}

func (s *Service) Test(ctx context.Context, id string) (Integration, error) {
	s.mu.RLock()
	item, ok := s.integrations[id]
	s.mu.RUnlock()
	if !ok {
		return Integration{}, ErrNotFound
	}
	if !item.Enabled {
		return cloneIntegration(item), ErrNotConfigured
	}

	probeContext, cancel := context.WithTimeout(ctx, s.config.HTTPTimeout)
	defer cancel()
	result, err := s.probe(probeContext, item)

	now := time.Now().Format(time.RFC3339)
	s.mu.Lock()
	current := s.integrations[id]
	current.LastCheckedAt = now
	if err != nil {
		current.Status = StatusError
		current.Message = safeProbeMessage(err)
	} else {
		current.Status = StatusConnected
		current.Message = result.Message
		if current.Details == nil {
			current.Details = make(map[string]string)
		}
		for key, value := range result.Details {
			current.Details[key] = value
		}
	}
	s.integrations[id] = current
	updated := cloneIntegration(current)
	s.mu.Unlock()
	s.persistStatus(context.Background(), updated)
	return updated, err
}

func (s *Service) probe(ctx context.Context, item Integration) (probeResult, error) {
	switch item.ID {
	case IntegrationHarbor:
		return s.probeHarbor(ctx, item)
	case IntegrationArgoCD:
		return s.probeArgoCD(ctx, item)
	case IntegrationGitLab:
		return s.probeGitLab(ctx, item)
	default:
		return probeResult{}, fmt.Errorf("unsupported integration type %q", item.ID)
	}
}

func (s *Service) probeHarbor(ctx context.Context, item Integration) (probeResult, error) {
	var payload struct {
		HarborVersion string `json:"harbor_version"`
		AuthMode      string `json:"auth_mode"`
		RegistryURL   string `json:"registry_url"`
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURL(item.Endpoint, "/api/v2.0/systeminfo"), nil)
	if err != nil {
		return probeResult{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.SetBasicAuth(s.config.HarborUsername, s.config.HarborPassword)
	if err := s.doJSON(request, &payload); err != nil {
		return probeResult{}, err
	}
	details := map[string]string{"authMode": payload.AuthMode}
	if payload.HarborVersion != "" {
		details["version"] = payload.HarborVersion
	}
	if payload.RegistryURL != "" {
		details["registryUrl"] = payload.RegistryURL
	}
	return probeResult{Message: "Harbor 认证成功，系统信息可读取", Details: details}, nil
}

func (s *Service) probeArgoCD(ctx context.Context, item Integration) (probeResult, error) {
	token := s.config.ArgoCDToken
	authMode := "token"
	if token == "" {
		authMode = "session"
		var session struct {
			Token string `json:"token"`
		}
		body, _ := json.Marshal(map[string]string{
			"username": s.config.ArgoCDUsername,
			"password": s.config.ArgoCDPassword,
		})
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURL(item.Endpoint, "/api/v1/session"), strings.NewReader(string(body)))
		if err != nil {
			return probeResult{}, err
		}
		request.Header.Set("Content-Type", "application/json")
		if err := s.doJSON(request, &session); err != nil {
			return probeResult{}, err
		}
		if strings.TrimSpace(session.Token) == "" {
			return probeResult{}, errors.New("Argo CD 登录成功但未返回访问令牌")
		}
		token = session.Token
	}

	var applications struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURL(item.Endpoint, "/api/v1/applications?limit=1"), nil)
	if err != nil {
		return probeResult{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	if err := s.doJSON(request, &applications); err != nil {
		return probeResult{}, err
	}
	count := applications.Total
	if count == 0 {
		count = len(applications.Items)
	}
	return probeResult{
		Message: "Argo CD 认证成功，应用列表可读取",
		Details: map[string]string{
			"authMode":     authMode,
			"applications": strconv.Itoa(count),
		},
	}, nil
}

func (s *Service) probeGitLab(ctx context.Context, item Integration) (probeResult, error) {
	var payload struct {
		Version  string `json:"version"`
		Revision string `json:"revision"`
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURL(item.Endpoint, "/api/v4/version"), nil)
	if err != nil {
		return probeResult{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("PRIVATE-TOKEN", s.config.GitLabToken)
	if err := s.doJSON(request, &payload); err != nil {
		return probeResult{}, err
	}
	details := map[string]string{"authMode": "private-token"}
	if payload.Version != "" {
		details["version"] = payload.Version
	}
	if payload.Revision != "" {
		details["revision"] = payload.Revision
	}
	return probeResult{Message: "GitLab 访问令牌有效，版本信息可读取", Details: details}, nil
}

func (s *Service) doJSON(request *http.Request, target any) error {
	if request.Header.Get("Accept") == "" {
		request.Header.Set("Accept", "application/json")
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		message := strings.TrimSpace(string(body))
		if len(message) > 240 {
			message = message[:240]
		}
		if message == "" {
			message = response.Status
		}
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, message)
	}
	if target == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(target); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("invalid JSON response: %w", err)
	}
	return nil
}

func joinURL(base, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func safeProbeMessage(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	lower := strings.ToLower(message)
	switch {
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(lower, "timeout"):
		return "连接超时，请检查网络、地址或端口"
	case strings.Contains(lower, "x509"), strings.Contains(lower, "certificate"):
		return "TLS 证书校验失败，可按部署策略开启跳过校验"
	case strings.Contains(lower, "connection refused"):
		return "连接被拒绝，请检查服务是否可访问"
	case strings.Contains(lower, "no such host"), strings.Contains(lower, "dns"):
		return "域名解析失败，请检查地址"
	case strings.Contains(lower, "http 401"), strings.Contains(lower, "unauthorized"):
		return "认证失败，请检查账号、密码或访问令牌"
	case strings.Contains(lower, "http 403"), strings.Contains(lower, "forbidden"):
		return "权限不足，请检查集成账号权限"
	case strings.Contains(lower, "http 404"):
		return "接口不存在，请检查服务地址和 API 路径"
	default:
		if len(message) > 220 {
			message = message[:220]
		}
		return "连接失败：" + message
	}
}

func argoAuthMode(config Config) string {
	if config.ArgoCDToken != "" {
		return "token"
	}
	if config.ArgoCDUsername != "" && config.ArgoCDPassword != "" {
		return "session"
	}
	return "unconfigured"
}

func integrationIndex(id string) int {
	for index, value := range integrationOrder {
		if value == id {
			return index
		}
	}
	return len(integrationOrder)
}

func cloneIntegration(item Integration) Integration {
	copyItem := item
	if item.Details != nil {
		copyItem.Details = make(map[string]string, len(item.Details))
		for key, value := range item.Details {
			copyItem.Details[key] = value
		}
	}
	return copyItem
}

func boolFromEnv(key string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Service) initStatusStore(ctx context.Context, db *sql.DB) error {
	pingContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingContext); err != nil {
		return err
	}
	_, err := db.ExecContext(pingContext, `
		CREATE TABLE IF NOT EXISTS cicd_integration_status (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			last_checked_at TEXT NOT NULL DEFAULT '',
			details JSONB NOT NULL DEFAULT '{}'::jsonb,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	return err
}

func (s *Service) reloadStatusStore(ctx context.Context) {
	if s.db == nil {
		return
	}
	queryContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(queryContext, `
		SELECT id, status, message, COALESCE(last_checked_at, ''), details
		FROM cicd_integration_status
	`)
	if err != nil {
		return
	}
	defer rows.Close()
	states := make(map[string]Integration)
	for rows.Next() {
		var item Integration
		var rawDetails []byte
		if err := rows.Scan(&item.ID, &item.Status, &item.Message, &item.LastCheckedAt, &rawDetails); err != nil {
			return
		}
		if len(rawDetails) > 0 {
			_ = json.Unmarshal(rawDetails, &item.Details)
		}
		states[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return
	}
	s.mu.Lock()
	for id, state := range states {
		current, ok := s.integrations[id]
		if !ok {
			continue
		}
		current.Status = state.Status
		current.Message = state.Message
		current.LastCheckedAt = state.LastCheckedAt
		if current.Details == nil {
			current.Details = make(map[string]string)
		}
		for key, value := range state.Details {
			current.Details[key] = value
		}
		s.integrations[id] = current
	}
	s.mu.Unlock()
}

func (s *Service) persistStatus(ctx context.Context, item Integration) {
	if s.db == nil {
		return
	}
	details, err := json.Marshal(item.Details)
	if err != nil {
		return
	}
	writeContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(writeContext, `
		INSERT INTO cicd_integration_status (id, status, message, last_checked_at, details, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			message = EXCLUDED.message,
			last_checked_at = EXCLUDED.last_checked_at,
			details = EXCLUDED.details,
			updated_at = now()
	`, item.ID, item.Status, item.Message, item.LastCheckedAt, details)
}
