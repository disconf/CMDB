package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const agentDeploymentColumns = `id,action,status,target_version,credential_id,ssh_port,gateway_url,asset_type,hosts,results,total,succeeded,failed,precheck,requested_by,message,retry_of,created_at,started_at,finished_at`

type AgentDeployment struct {
	ID            string               `json:"id"`
	Action        string               `json:"action"`
	Status        string               `json:"status"`
	TargetVersion string               `json:"targetVersion"`
	CredentialID  string               `json:"credentialId"`
	SSHPort       int                  `json:"sshPort"`
	GatewayURL    string               `json:"gatewayUrl"`
	AssetType     string               `json:"assetType"`
	Hosts         []string             `json:"hosts"`
	Results       []AgentInstallResult `json:"results"`
	Total         int                  `json:"total"`
	Succeeded     int                  `json:"succeeded"`
	Failed        int                  `json:"failed"`
	Precheck      bool                 `json:"precheck"`
	RequestedBy   string               `json:"requestedBy"`
	Message       string               `json:"message"`
	RetryOf       string               `json:"retryOf"`
	CreatedAt     string               `json:"createdAt"`
	StartedAt     string               `json:"startedAt"`
	FinishedAt    string               `json:"finishedAt"`
}

type AgentPrecheckInput struct {
	Hosts        []string `json:"hosts"`
	Port         int      `json:"port"`
	CredentialID string   `json:"credentialId"`
	GatewayURL   string   `json:"gatewayUrl"`
}

type AgentPrecheckResult struct {
	Host         string `json:"host"`
	OK           bool   `json:"ok"`
	OS           string `json:"os"`
	Kernel       string `json:"kernel"`
	Architecture string `json:"architecture"`
	Root         bool   `json:"root"`
	Systemd      bool   `json:"systemd"`
	Curl         bool   `json:"curl"`
	DiskFreeKB   int64  `json:"diskFreeKb"`
	GatewayOK    bool   `json:"gatewayOk"`
	GatewayCode  string `json:"gatewayCode"`
	Message      string `json:"message"`
}

func newAgentDeploymentID() string {
	return fmt.Sprintf("agent-deploy-%d", time.Now().UnixNano())
}

func normalizeAgentDeploymentAction(action string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "install":
		return "install", nil
	case "upgrade":
		return "upgrade", nil
	case "uninstall":
		return "uninstall", nil
	default:
		return "", errors.New("invalid agent deployment action")
	}
}

func normalizeAgentHosts(hosts []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(hosts))
	for _, raw := range hosts {
		host := strings.TrimSpace(raw)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, host)
	}
	return out
}

func normalizeAgentInstallInput(in AgentInstallInput) AgentInstallInput {
	in.Hosts = normalizeAgentHosts(in.Hosts)
	in.AssetType = strings.TrimSpace(in.AssetType)
	in.GatewayURL = strings.TrimSpace(in.GatewayURL)
	in.CredentialID = strings.TrimSpace(in.CredentialID)
	in.TargetVersion = strings.TrimSpace(in.TargetVersion)
	return in
}

func cloneAgentDeployment(item AgentDeployment) AgentDeployment {
	item.Hosts = append([]string(nil), item.Hosts...)
	item.Results = append([]AgentInstallResult(nil), item.Results...)
	return item
}

func (s *Service) AgentDeployments(limit int) ([]AgentDeployment, error) {
	if s.db != nil {
		return s.loadAgentDeployments(limit)
	}
	if limit <= 0 {
		limit = 50
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit > len(s.agentDeployments) {
		limit = len(s.agentDeployments)
	}
	out := make([]AgentDeployment, 0, limit)
	for _, item := range s.agentDeployments[:limit] {
		out = append(out, cloneAgentDeployment(item))
	}
	return out, nil
}

func (s *Service) AgentDeployment(id string) (AgentDeployment, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return AgentDeployment{}, ErrNotFound
	}
	if s.db != nil {
		return s.loadAgentDeployment(id)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.agentDeployments {
		if item.ID == id {
			return cloneAgentDeployment(item), nil
		}
	}
	return AgentDeployment{}, ErrNotFound
}

func (s *Service) RunAgentDeployment(in AgentInstallInput, action, actor string) (AgentDeployment, error) {
	return s.runAgentDeployment(in, action, actor, "")
}

func (s *Service) runAgentDeployment(in AgentInstallInput, action, actor, retryOf string) (AgentDeployment, error) {
	normalizedAction, err := normalizeAgentDeploymentAction(action)
	if err != nil {
		return AgentDeployment{}, err
	}
	in = normalizeAgentInstallInput(in)
	if len(in.Hosts) == 0 {
		return AgentDeployment{}, errors.New("hosts are required")
	}
	if strings.TrimSpace(actor) == "" {
		actor = "system"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	item := AgentDeployment{
		ID:            newAgentDeploymentID(),
		Action:        normalizedAction,
		Status:        "running",
		TargetVersion: in.TargetVersion,
		CredentialID:  in.CredentialID,
		SSHPort:       in.Port,
		GatewayURL:    in.GatewayURL,
		AssetType:     in.AssetType,
		Hosts:         append([]string(nil), in.Hosts...),
		Results:       []AgentInstallResult{},
		Total:         len(in.Hosts),
		Failed:        len(in.Hosts),
		RequestedBy:   strings.TrimSpace(actor),
		RetryOf:       strings.TrimSpace(retryOf),
		CreatedAt:     now,
		StartedAt:     now,
	}
	if item.SSHPort == 0 {
		item.SSHPort = 22
	}
	if err := s.saveAgentDeployment(item); err != nil {
		return AgentDeployment{}, err
	}

	results, runErr := s.executeAgentDeployment(item.Action, in)
	item.Results = results
	item.Succeeded = 0
	for _, result := range results {
		if result.OK {
			item.Succeeded++
		}
	}
	item.Failed = item.Total - item.Succeeded
	if item.Failed < 0 {
		item.Failed = 0
	}
	switch {
	case runErr != nil:
		item.Status = "failed"
		item.Message = runErr.Error()
		if len(results) == 0 {
			item.Failed = item.Total
		}
	case item.Succeeded == item.Total:
		item.Status = "success"
	case item.Succeeded == 0:
		item.Status = "failed"
	default:
		item.Status = "partial"
	}
	item.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.saveAgentDeployment(item); err != nil && runErr == nil {
		runErr = err
	}
	return cloneAgentDeployment(item), runErr
}

func (s *Service) executeAgentDeployment(action string, in AgentInstallInput) ([]AgentInstallResult, error) {
	switch action {
	case "install":
		return s.agentBatchInstall(in)
	case "upgrade":
		return s.agentBatchUpgrade(in)
	case "uninstall":
		return s.agentBatchUninstall(in.Hosts, in.Port, in.CredentialID)
	default:
		return nil, errors.New("invalid agent deployment action")
	}
}

func (s *Service) AgentBatchInstall(in AgentInstallInput) ([]AgentInstallResult, error) {
	item, err := s.RunAgentDeployment(in, "install", "")
	return item.Results, err
}

func (s *Service) AgentBatchUpgrade(in AgentInstallInput) ([]AgentInstallResult, error) {
	item, err := s.RunAgentDeployment(in, "upgrade", "")
	return item.Results, err
}

func (s *Service) AgentBatchUninstall(hosts []string, port int, credentialID string) ([]AgentInstallResult, error) {
	item, err := s.RunAgentDeployment(AgentInstallInput{Hosts: hosts, Port: port, CredentialID: credentialID}, "uninstall", "")
	return item.Results, err
}

func (s *Service) RetryAgentDeployment(id, actor string) (AgentDeployment, error) {
	current, err := s.AgentDeployment(id)
	if err != nil {
		return AgentDeployment{}, err
	}
	if current.Status == "running" {
		return AgentDeployment{}, errors.New("deployment is still running")
	}
	hosts := make([]string, 0, current.Failed)
	for _, result := range current.Results {
		if !result.OK {
			hosts = append(hosts, result.Host)
		}
	}
	if len(hosts) == 0 && current.Failed > 0 {
		hosts = append(hosts, current.Hosts...)
	}
	hosts = normalizeAgentHosts(hosts)
	if len(hosts) == 0 {
		return AgentDeployment{}, errors.New("deployment has no failed hosts")
	}
	in := AgentInstallInput{
		Hosts:         hosts,
		Port:          current.SSHPort,
		AssetType:     current.AssetType,
		GatewayURL:    current.GatewayURL,
		CredentialID:  current.CredentialID,
		TargetVersion: current.TargetVersion,
	}
	return s.runAgentDeployment(in, current.Action, actor, current.ID)
}

func (s *Service) AgentPrecheck(in AgentPrecheckInput) ([]AgentPrecheckResult, error) {
	hosts := normalizeAgentHosts(in.Hosts)
	if len(hosts) == 0 {
		return nil, errors.New("hosts are required")
	}
	port := in.Port
	if port == 0 {
		port = 22
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("invalid port")
	}
	username, password, err := s.resolveAgentSSHCredentials(in.CredentialID)
	if err != nil {
		return nil, err
	}
	gatewayURL, err := normalizeAgentGatewayURL(in.GatewayURL)
	if err != nil {
		return nil, err
	}
	script := agentPrecheckScript(gatewayURL)
	results := make([]AgentPrecheckResult, 0, len(hosts))
	for _, host := range hosts {
		output, runErr := runSSHRaw(host, port, username, password, script, 25*time.Second)
		if runErr != nil {
			results = append(results, AgentPrecheckResult{Host: host, Message: runErr.Error()})
			continue
		}
		results = append(results, parseAgentPrecheckOutput(host, output))
	}
	return results, nil
}

func (s *Service) resolveAgentSSHCredentials(credentialID string) (string, string, error) {
	username := strings.TrimSpace(os.Getenv("CMDB_SSH_USERNAME"))
	password := os.Getenv("CMDB_SSH_PASSWORD")
	credentialID = strings.TrimSpace(credentialID)
	if credentialID != "" {
		u, secret, err := s.resolveCredential(credentialID)
		if err != nil {
			return "", "", fmt.Errorf("resolve SSH credential: %w", err)
		}
		if strings.TrimSpace(u) == "" || secret == "" {
			return "", "", errors.New("SSH credential is empty")
		}
		username, password = u, secret
	}
	if username == "" || password == "" {
		return "", "", errors.New("SSH credentials are not configured")
	}
	return username, password, nil
}

func normalizeAgentGatewayURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = strings.TrimSpace(os.Getenv("CMDB_AGENT_GATEWAY_URL"))
	}
	if value == "" {
		value = "http://172.28.69.161:30080"
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("invalid gateway URL")
	}
	return strings.TrimRight(value, "/"), nil
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func agentPrecheckScript(gatewayURL string) string {
	versionURL := shellSingleQuote(strings.TrimRight(gatewayURL, "/") + "/api/v1/agent/install/version")
	return `set +e
printf 'OS='; uname -s 2>/dev/null || printf 'unknown'; printf '\n'
printf 'KERNEL='; uname -r 2>/dev/null || printf 'unknown'; printf '\n'
printf 'ARCH='; uname -m 2>/dev/null || printf 'unknown'; printf '\n'
printf 'ROOT='; if [ "$(id -u 2>/dev/null)" = "0" ]; then printf 'true'; else printf 'false'; fi; printf '\n'
printf 'SYSTEMD='; if command -v systemctl >/dev/null 2>&1; then printf 'true'; else printf 'false'; fi; printf '\n'
printf 'CURL='; if command -v curl >/dev/null 2>&1; then printf 'true'; else printf 'false'; fi; printf '\n'
printf 'DISK_FREE_KB='; if [ -d /opt ]; then df -Pk /opt; else df -Pk /; fi 2>/dev/null | awk 'NR==2{print $4}'; printf '\n'
printf 'GATEWAY_CODE='; if command -v curl >/dev/null 2>&1; then curl -fsSL --connect-timeout 5 -o /dev/null -w '%{http_code}' ` + versionURL + ` 2>/dev/null || true; else printf 'unavailable'; fi; printf '\n'
`
}

func parseAgentPrecheckOutput(host, output string) AgentPrecheckResult {
	values := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	result := AgentPrecheckResult{
		Host:         host,
		OS:           values["OS"],
		Kernel:       values["KERNEL"],
		Architecture: values["ARCH"],
		Root:         strings.EqualFold(values["ROOT"], "true"),
		Systemd:      strings.EqualFold(values["SYSTEMD"], "true"),
		Curl:         strings.EqualFold(values["CURL"], "true"),
		GatewayCode:  values["GATEWAY_CODE"],
	}
	if result.DiskFreeKB, _ = strconv.ParseInt(values["DISK_FREE_KB"], 10, 64); result.DiskFreeKB < 0 {
		result.DiskFreeKB = 0
	}
	if code, err := strconv.Atoi(result.GatewayCode); err == nil {
		result.GatewayOK = code >= 200 && code < 300
	}
	issues := []string{}
	if !result.Root {
		issues = append(issues, "当前用户无 root 权限")
	}
	if !result.Systemd {
		issues = append(issues, "未检测到 systemd")
	}
	if !result.Curl {
		issues = append(issues, "未检测到 curl")
	}
	if !result.GatewayOK {
		code := result.GatewayCode
		if code == "" {
			code = "无响应"
		}
		issues = append(issues, "网关不可达 ("+code+")")
	}
	if result.DiskFreeKB < 10*1024 {
		issues = append(issues, "可用磁盘不足 10MB")
	}
	if len(issues) == 0 {
		result.OK = true
		result.Message = "预检查通过"
	} else {
		result.Message = strings.Join(issues, "；")
	}
	return result
}

func (s *Service) saveAgentDeployment(item AgentDeployment) error {
	item = cloneAgentDeployment(item)
	if item.Results == nil {
		item.Results = []AgentInstallResult{}
	}
	if item.Hosts == nil {
		item.Hosts = []string{}
	}
	if s.db != nil {
		if err := s.persistAgentDeployment(item); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.agentDeployments {
		if s.agentDeployments[i].ID == item.ID {
			s.agentDeployments[i] = item
			return nil
		}
	}
	s.agentDeployments = append([]AgentDeployment{item}, s.agentDeployments...)
	if len(s.agentDeployments) > 200 {
		s.agentDeployments = s.agentDeployments[:200]
	}
	return nil
}

type agentDeploymentScanner interface {
	Scan(dest ...any) error
}

func scanAgentDeployment(row agentDeploymentScanner) (AgentDeployment, error) {
	var item AgentDeployment
	var hostsRaw, resultsRaw []byte
	err := row.Scan(
		&item.ID, &item.Action, &item.Status, &item.TargetVersion, &item.CredentialID, &item.SSHPort,
		&item.GatewayURL, &item.AssetType, &hostsRaw, &resultsRaw, &item.Total, &item.Succeeded,
		&item.Failed, &item.Precheck, &item.RequestedBy, &item.Message, &item.RetryOf,
		&item.CreatedAt, &item.StartedAt, &item.FinishedAt,
	)
	if err != nil {
		return AgentDeployment{}, err
	}
	if err := json.Unmarshal(hostsRaw, &item.Hosts); err != nil {
		return AgentDeployment{}, err
	}
	if err := json.Unmarshal(resultsRaw, &item.Results); err != nil {
		return AgentDeployment{}, err
	}
	if item.Hosts == nil {
		item.Hosts = []string{}
	}
	if item.Results == nil {
		item.Results = []AgentInstallResult{}
	}
	return item, nil
}

func (s *Service) loadAgentDeployments(limit int) ([]AgentDeployment, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(context.Background(), `SELECT `+agentDeploymentColumns+` FROM agent_deployments ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AgentDeployment{}
	for rows.Next() {
		item, err := scanAgentDeployment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) loadAgentDeployment(id string) (AgentDeployment, error) {
	item, err := scanAgentDeployment(s.db.QueryRowContext(context.Background(), `SELECT `+agentDeploymentColumns+` FROM agent_deployments WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AgentDeployment{}, ErrNotFound
	}
	return item, err
}

func (s *Service) persistAgentDeployment(item AgentDeployment) error {
	hosts, err := json.Marshal(item.Hosts)
	if err != nil {
		return err
	}
	results, err := json.Marshal(item.Results)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(context.Background(), `INSERT INTO agent_deployments(
id,action,status,target_version,credential_id,ssh_port,gateway_url,asset_type,hosts,results,total,succeeded,failed,precheck,requested_by,message,retry_of,created_at,started_at,finished_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
ON CONFLICT(id) DO UPDATE SET action=excluded.action,status=excluded.status,target_version=excluded.target_version,credential_id=excluded.credential_id,ssh_port=excluded.ssh_port,gateway_url=excluded.gateway_url,asset_type=excluded.asset_type,hosts=excluded.hosts,results=excluded.results,total=excluded.total,succeeded=excluded.succeeded,failed=excluded.failed,precheck=excluded.precheck,requested_by=excluded.requested_by,message=excluded.message,retry_of=excluded.retry_of,started_at=excluded.started_at,finished_at=excluded.finished_at`,
		item.ID, item.Action, item.Status, item.TargetVersion, item.CredentialID, item.SSHPort, item.GatewayURL, item.AssetType,
		string(hosts), string(results), item.Total, item.Succeeded, item.Failed, item.Precheck, item.RequestedBy, item.Message,
		item.RetryOf, item.CreatedAt, item.StartedAt, item.FinishedAt,
	)
	return err
}
