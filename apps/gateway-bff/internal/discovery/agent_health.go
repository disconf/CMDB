package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const agentHealthCheckColumns = `id,status,credential_id,ssh_port,gateway_url,hosts,results,remediation,total,healthy,warning,critical,requested_by,created_at,finished_at`

type AgentHealthInput struct {
	Hosts        []string `json:"hosts"`
	Port         int      `json:"port"`
	CredentialID string   `json:"credentialId"`
	GatewayURL   string   `json:"gatewayUrl"`
}

type AgentHealthIssue struct {
	Severity   string `json:"severity"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
}

type AgentHealthResult struct {
	Host                string             `json:"host"`
	Hostname            string             `json:"hostname"`
	OK                  bool               `json:"ok"`
	Status              string             `json:"status"`
	Score               int                `json:"score"`
	AgentBinary         bool               `json:"agentBinary"`
	AgentServiceActive  bool               `json:"agentServiceActive"`
	AgentServiceEnabled bool               `json:"agentServiceEnabled"`
	AgentVersion        string             `json:"agentVersion"`
	RestartCount        int                `json:"restartCount"`
	UptimeSeconds       int64              `json:"uptimeSeconds"`
	CPUCount            int                `json:"cpuCount"`
	DiskFreeKB          int64              `json:"diskFreeKb"`
	DiskUsagePercent    float64            `json:"diskUsagePercent"`
	MemoryUsagePercent  float64            `json:"memoryUsagePercent"`
	Load1               float64            `json:"load1"`
	GatewayOK           bool               `json:"gatewayOk"`
	GatewayCode         string             `json:"gatewayCode"`
	NodeExporterActive  bool               `json:"nodeExporterActive"`
	NodeExporterPorts   []int              `json:"nodeExporterPorts"`
	NodeExporterPort    int                `json:"nodeExporterPort"`
	Journal             string             `json:"journal"`
	Issues              []AgentHealthIssue `json:"issues"`
	Message             string             `json:"message"`
}

type AgentRemediationInput struct {
	Action string   `json:"action"`
	Hosts  []string `json:"hosts,omitempty"`
}

type AgentRemediationResult struct {
	Host         string `json:"host"`
	Action       string `json:"action"`
	OK           bool   `json:"ok"`
	BeforeStatus string `json:"beforeStatus"`
	AfterStatus  string `json:"afterStatus"`
	Info         string `json:"info"`
	CreatedAt    string `json:"createdAt"`
}

type AgentHealthCheck struct {
	ID           string                   `json:"id"`
	Status       string                   `json:"status"`
	CredentialID string                   `json:"credentialId"`
	SSHPort      int                      `json:"sshPort"`
	GatewayURL   string                   `json:"gatewayUrl"`
	Hosts        []string                 `json:"hosts"`
	Results      []AgentHealthResult      `json:"results"`
	Remediation  []AgentRemediationResult `json:"remediation"`
	Total        int                      `json:"total"`
	Healthy      int                      `json:"healthy"`
	Warning      int                      `json:"warning"`
	Critical     int                      `json:"critical"`
	RequestedBy  string                   `json:"requestedBy"`
	CreatedAt    string                   `json:"createdAt"`
	FinishedAt   string                   `json:"finishedAt"`
}

func newAgentHealthCheckID() string {
	return fmt.Sprintf("agent-health-%d", time.Now().UnixNano())
}

func normalizeAgentHealthInput(in AgentHealthInput) AgentHealthInput {
	in.Hosts = normalizeAgentHosts(in.Hosts)
	in.CredentialID = strings.TrimSpace(in.CredentialID)
	in.GatewayURL = strings.TrimSpace(in.GatewayURL)
	if in.Port == 0 {
		in.Port = 22
	}
	return in
}

func cloneAgentHealthCheck(item AgentHealthCheck) AgentHealthCheck {
	item.Hosts = append([]string(nil), item.Hosts...)
	item.Results = append([]AgentHealthResult(nil), item.Results...)
	for i := range item.Results {
		item.Results[i].NodeExporterPorts = append([]int(nil), item.Results[i].NodeExporterPorts...)
		item.Results[i].Issues = append([]AgentHealthIssue(nil), item.Results[i].Issues...)
	}
	item.Remediation = append([]AgentRemediationResult(nil), item.Remediation...)
	return item
}

func (s *Service) AgentHealthChecks(limit int) ([]AgentHealthCheck, error) {
	if s.db != nil {
		return s.loadAgentHealthChecks(limit)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit > len(s.agentHealthChecks) {
		limit = len(s.agentHealthChecks)
	}
	out := make([]AgentHealthCheck, 0, limit)
	for _, item := range s.agentHealthChecks[:limit] {
		out = append(out, cloneAgentHealthCheck(item))
	}
	return out, nil
}

func (s *Service) AgentHealthCheck(id string) (AgentHealthCheck, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return AgentHealthCheck{}, ErrNotFound
	}
	if s.db != nil {
		return s.loadAgentHealthCheck(id)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.agentHealthChecks {
		if item.ID == id {
			return cloneAgentHealthCheck(item), nil
		}
	}
	return AgentHealthCheck{}, ErrNotFound
}

func (s *Service) RunAgentHealthCheck(in AgentHealthInput, actor string) (AgentHealthCheck, error) {
	in = normalizeAgentHealthInput(in)
	expandedHosts, expandErr := expandAgentHealthHosts(in.Hosts)
	if expandErr != nil {
		return AgentHealthCheck{}, expandErr
	}
	in.Hosts = expandedHosts
	if len(in.Hosts) == 0 {
		return AgentHealthCheck{}, errors.New("hosts are required")
	}
	if in.Port < 1 || in.Port > 65535 {
		return AgentHealthCheck{}, errors.New("invalid port")
	}
	if strings.TrimSpace(actor) == "" {
		actor = "system"
	}
	username, password, err := s.resolveAgentSSHCredentials(in.CredentialID)
	if err != nil {
		return AgentHealthCheck{}, err
	}
	gatewayURL, err := normalizeAgentGatewayURL(in.GatewayURL)
	if err != nil {
		return AgentHealthCheck{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	item := AgentHealthCheck{
		ID:           newAgentHealthCheckID(),
		Status:       "running",
		CredentialID: in.CredentialID,
		SSHPort:      in.Port,
		GatewayURL:   gatewayURL,
		Hosts:        append([]string(nil), in.Hosts...),
		Results:      []AgentHealthResult{},
		Remediation:  []AgentRemediationResult{},
		Total:        len(in.Hosts),
		RequestedBy:  strings.TrimSpace(actor),
		CreatedAt:    now,
	}
	if err := s.saveAgentHealthCheck(item); err != nil {
		return AgentHealthCheck{}, err
	}
	for _, host := range item.Hosts {
		item.Results = append(item.Results, s.inspectAgentHealthHost(host, in.Port, username, password, gatewayURL))
	}
	item.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	recomputeAgentHealthCheck(&item)
	if err := s.saveAgentHealthCheck(item); err != nil {
		return AgentHealthCheck{}, err
	}
	return cloneAgentHealthCheck(item), nil
}

func (s *Service) RemediateAgentHealth(id string, in AgentRemediationInput, _ string) (AgentHealthCheck, error) {
	item, err := s.AgentHealthCheck(id)
	if err != nil {
		return AgentHealthCheck{}, err
	}
	if item.Status == "running" {
		return AgentHealthCheck{}, errors.New("health check is still running")
	}
	action, err := normalizeAgentRemediationAction(in.Action)
	if err != nil {
		return AgentHealthCheck{}, err
	}
	hosts := normalizeAgentHosts(in.Hosts)
	if len(hosts) == 0 {
		for _, result := range item.Results {
			if result.Status != "healthy" {
				hosts = append(hosts, result.Host)
			}
		}
	}
	hosts = normalizeAgentHosts(hosts)
	if len(hosts) == 0 {
		return AgentHealthCheck{}, errors.New("no unhealthy hosts to remediate")
	}
	username, password, err := s.resolveAgentSSHCredentials(item.CredentialID)
	if err != nil {
		return AgentHealthCheck{}, err
	}
	gatewayURL := strings.TrimRight(item.GatewayURL, "/")
	if gatewayURL == "" {
		gatewayURL, err = normalizeAgentGatewayURL("")
		if err != nil {
			return AgentHealthCheck{}, err
		}
	}
	for _, host := range hosts {
		before := agentHealthResultByHost(item.Results, host)
		result := AgentRemediationResult{Host: host, Action: action, BeforeStatus: before.Status, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
		var runErr error
		if action == "reinstall-agent" {
			results, installErr := s.agentBatchInstall(AgentInstallInput{Hosts: []string{host}, Port: item.SSHPort, CredentialID: item.CredentialID, GatewayURL: gatewayURL})
			runErr = installErr
			if installErr == nil {
				if len(results) == 0 || !results[0].OK {
					runErr = errors.New("reinstall did not complete")
					if len(results) > 0 {
						runErr = errors.New(results[0].Info)
					}
				} else {
					result.Info = results[0].Info
				}
			}
		} else {
			script, scriptErr := agentRemediationScript(action)
			if scriptErr != nil {
				runErr = scriptErr
			} else {
				out, commandErr := runSSHRaw(host, item.SSHPort, username, password, script, 30*time.Second)
				result.Info = strings.TrimSpace(out)
				runErr = commandErr
			}
		}
		after := s.inspectAgentHealthHost(host, item.SSHPort, username, password, gatewayURL)
		result.AfterStatus = after.Status
		result.OK = runErr == nil
		if runErr != nil && result.Info == "" {
			result.Info = runErr.Error()
		}
		item.Results = replaceAgentHealthResult(item.Results, after)
		item.Remediation = append(item.Remediation, result)
	}
	item.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	recomputeAgentHealthCheck(&item)
	if err := s.saveAgentHealthCheck(item); err != nil {
		return AgentHealthCheck{}, err
	}
	return cloneAgentHealthCheck(item), nil
}

func expandAgentHealthHosts(hosts []string) ([]string, error) {
	out := []string{}
	for _, host := range normalizeAgentHosts(hosts) {
		if !strings.Contains(host, "/") {
			out = append(out, host)
			continue
		}
		expanded, err := expandCIDR(host)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %s: %w", host, err)
		}
		if len(expanded) > 1024 {
			return nil, fmt.Errorf("CIDR %s expands to %d hosts, maximum is 1024", host, len(expanded))
		}
		out = append(out, expanded...)
	}
	out = normalizeAgentHosts(out)
	if len(out) > 2048 {
		return nil, errors.New("too many target hosts, maximum is 2048")
	}
	return out, nil
}

func normalizeAgentRemediationAction(action string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "restart-agent", "restart_node_exporter", "enable-agent", "reset-agent", "restart-node-exporter", "reinstall-agent":
		return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(action)), "_", "-"), nil
	default:
		return "", errors.New("invalid remediation action")
	}
}

func agentRemediationScript(action string) (string, error) {
	switch action {
	case "restart-agent":
		return "set -e; systemctl restart cmdb-agent; sleep 2; systemctl is-active cmdb-agent", nil
	case "enable-agent":
		return "set -e; systemctl enable cmdb-agent >/dev/null 2>&1 || true; systemctl start cmdb-agent; sleep 2; systemctl is-active cmdb-agent", nil
	case "reset-agent":
		return "set -e; systemctl reset-failed cmdb-agent 2>/dev/null || true; systemctl start cmdb-agent; sleep 2; systemctl is-active cmdb-agent", nil
	case "restart-node-exporter":
		return "set -e; systemctl restart node_exporter 2>/dev/null || systemctl restart node-exporter; sleep 2; systemctl is-active node_exporter 2>/dev/null || systemctl is-active node-exporter", nil
	default:
		return "", errors.New("unsupported remediation action")
	}
}

func (s *Service) inspectAgentHealthHost(host string, port int, username, password, gatewayURL string) AgentHealthResult {
	output, err := runSSHRaw(host, port, username, password, agentHealthScript(gatewayURL), 35*time.Second)
	if err != nil {
		return AgentHealthResult{
			Host: host, Status: "critical", Score: 0, OK: false,
			Issues:  []AgentHealthIssue{{Severity: "critical", Code: "ssh.unreachable", Message: "SSH 巡检失败：" + err.Error(), Suggestion: "检查 SSH 端口、账号密码、网络 ACL 与目标主机 sshd 状态。"}},
			Message: "SSH 巡检失败：" + err.Error(),
		}
	}
	return parseAgentHealthOutput(host, output)
}

func agentHealthScript(gatewayURL string) string {
	healthURL := shellSingleQuote(strings.TrimRight(gatewayURL, "/") + "/api/v1/health")
	return `set +e
printf 'HOSTNAME='; hostname 2>/dev/null || printf 'unknown'; printf '\n'
printf 'AGENT_BINARY='; if [ -x /opt/cmdb-agent/cmdb-agent ]; then printf 'true'; else printf 'false'; fi; printf '\n'
printf 'AGENT_ACTIVE='; if command -v systemctl >/dev/null 2>&1; then systemctl is-active cmdb-agent 2>/dev/null || true; else printf 'unknown'; fi; printf '\n'
printf 'AGENT_ENABLED='; if command -v systemctl >/dev/null 2>&1; then systemctl is-enabled cmdb-agent 2>/dev/null || true; else printf 'unknown'; fi; printf '\n'
printf 'AGENT_VERSION='; if [ -x /opt/cmdb-agent/cmdb-agent ]; then /opt/cmdb-agent/cmdb-agent --version 2>/dev/null | head -n 1; else printf 'unknown'; fi; printf '\n'
printf 'RESTART_COUNT='; systemctl show -p NRestarts --value cmdb-agent 2>/dev/null || printf '0'; printf '\n'
printf 'UPTIME_SECONDS='; awk '{print int($1)}' /proc/uptime 2>/dev/null || printf '0'; printf '\n'
printf 'CPU_COUNT='; nproc 2>/dev/null || awk '/^processor/{c++}END{print c+0}' /proc/cpuinfo 2>/dev/null || printf '0'; printf '\n'
printf 'DISK_FREE_KB='; df -Pk / 2>/dev/null | awk 'NR==2{print $4}'; printf '\n'
printf 'DISK_USAGE_PERCENT='; df -Pk / 2>/dev/null | awk 'NR==2{gsub(/%/,"",$5);print $5}'; printf '\n'
printf 'MEMORY_USAGE_PERCENT='; awk '/MemTotal/{t=$2}/MemAvailable/{a=$2}END{if(t>0)printf "%.1f",(t-a)*100/t;else printf "0"}' /proc/meminfo 2>/dev/null; printf '\n'
printf 'LOAD1='; awk '{print $1}' /proc/loadavg 2>/dev/null || printf '0'; printf '\n'
printf 'GATEWAY_CODE='; if command -v curl >/dev/null 2>&1; then curl -sS -o /dev/null -w '%{http_code}' --connect-timeout 5 ` + healthURL + ` 2>/dev/null || true; else printf 'unavailable'; fi; printf '\n'
printf 'NODE_EXPORTER_ACTIVE='; if command -v systemctl >/dev/null 2>&1 && { systemctl is-active --quiet node_exporter 2>/dev/null || systemctl is-active --quiet node-exporter 2>/dev/null; }; then printf 'active'; elif command -v pgrep >/dev/null 2>&1 && pgrep -f '[n]ode_exporter' >/dev/null 2>&1; then printf 'running'; else printf 'inactive'; fi; printf '\n'
printf 'NODE_EXPORTER_PORTS='; if command -v pgrep >/dev/null 2>&1 && command -v ss >/dev/null 2>&1; then for pid in $(pgrep -f '[n]ode_exporter' 2>/dev/null); do ss -lntp 2>/dev/null | awk -v p="pid=$pid," 'index($0,p){print $4}'; done | sed -n 's/.*:\([0-9][0-9]*\)$/\1/p' | sort -nu | tr '\n' ',' | sed 's/,$//'; fi; printf '\n'
printf 'JOURNAL='; journalctl -u cmdb-agent -n 5 --no-pager 2>/dev/null | tr '\n' ' ' | cut -c1-800; printf '\n'
`
}

func parseAgentHealthOutput(host, output string) AgentHealthResult {
	values := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	result := AgentHealthResult{
		Host:              host,
		Hostname:          values["HOSTNAME"],
		AgentBinary:       strings.EqualFold(values["AGENT_BINARY"], "true"),
		AgentVersion:      strings.TrimSpace(values["AGENT_VERSION"]),
		GatewayCode:       strings.TrimSpace(values["GATEWAY_CODE"]),
		Journal:           values["JOURNAL"],
		NodeExporterPorts: parseAgentHealthPorts(values["NODE_EXPORTER_PORTS"]),
		Issues:            []AgentHealthIssue{},
	}
	if result.Hostname == "" {
		result.Hostname = host
	}
	result.AgentServiceActive = values["AGENT_ACTIVE"] == "active"
	result.AgentServiceEnabled = values["AGENT_ENABLED"] == "enabled"
	result.RestartCount, _ = strconv.Atoi(values["RESTART_COUNT"])
	result.UptimeSeconds, _ = strconv.ParseInt(values["UPTIME_SECONDS"], 10, 64)
	result.CPUCount, _ = strconv.Atoi(values["CPU_COUNT"])
	result.DiskFreeKB, _ = strconv.ParseInt(values["DISK_FREE_KB"], 10, 64)
	result.DiskUsagePercent, _ = strconv.ParseFloat(values["DISK_USAGE_PERCENT"], 64)
	result.MemoryUsagePercent, _ = strconv.ParseFloat(values["MEMORY_USAGE_PERCENT"], 64)
	result.Load1, _ = strconv.ParseFloat(values["LOAD1"], 64)
	if code, err := strconv.Atoi(result.GatewayCode); err == nil {
		result.GatewayOK = code >= 200 && code < 300
	}
	result.NodeExporterActive = values["NODE_EXPORTER_ACTIVE"] == "active" || values["NODE_EXPORTER_ACTIVE"] == "running"
	if len(result.NodeExporterPorts) > 0 {
		result.NodeExporterPort = result.NodeExporterPorts[0]
	}
	scoreAgentHealth(&result, AgentBundleVersion())
	return result
}

func scoreAgentHealth(result *AgentHealthResult, currentVersion string) {
	if result.Issues == nil {
		result.Issues = []AgentHealthIssue{}
	}
	score := 100
	add := func(severity, code, message, suggestion string, penalty int) {
		result.Issues = append(result.Issues, AgentHealthIssue{Severity: severity, Code: code, Message: message, Suggestion: suggestion})
		score -= penalty
	}
	if !result.AgentBinary {
		add("critical", "agent.binary_missing", "未发现 /opt/cmdb-agent/cmdb-agent。", "使用 Agent 部署任务安装或修复 Agent。", 35)
	}
	if !result.AgentServiceActive {
		add("critical", "agent.service_inactive", "cmdb-agent systemd 服务未运行。", "先尝试启用并重启 cmdb-agent，必要时重装。", 45)
	}
	if !result.AgentServiceEnabled {
		add("warning", "agent.service_disabled", "cmdb-agent 未设置为开机自启。", "执行安全自愈“启用 Agent”。", 8)
	}
	if !result.GatewayOK {
		code := result.GatewayCode
		if code == "" {
			code = "unavailable"
		}
		add("critical", "gateway.unreachable", "Agent 到 Gateway 健康检查不可达（"+code+"）。", "检查 DNS、端口、网络 ACL、Ingress/Service 和证书。", 45)
	}
	if result.AgentVersion == "" || result.AgentVersion == "unknown" {
		add("warning", "agent.version_unknown", "无法读取 Agent 版本。", "确认二进制可执行并检查 systemd 日志。", 5)
	} else if currentVersion != "" && !strings.Contains(strings.ToLower(result.AgentVersion), strings.ToLower(strings.TrimPrefix(currentVersion, "v"))) {
		add("warning", "agent.version_outdated", "Agent 版本可能不是当前服务端版本 "+currentVersion+"。", "在 Agent 部署页执行批量升级。", 5)
	}
	if result.RestartCount > 20 {
		add("critical", "agent.restart_loop", fmt.Sprintf("cmdb-agent 重启次数异常（%d）。", result.RestartCount), "检查日志、网络与依赖后执行安全自愈。", 15)
	} else if result.RestartCount > 5 {
		add("warning", "agent.restart_frequent", fmt.Sprintf("cmdb-agent 近期重启 %d 次。", result.RestartCount), "检查 systemd 日志，必要时执行重启或重装。", 8)
	}
	if result.DiskUsagePercent >= 90 {
		add("critical", "system.disk_critical", fmt.Sprintf("根分区使用率 %.1f%%。", result.DiskUsagePercent), "清理日志、归档数据或扩容磁盘。", 20)
	} else if result.DiskUsagePercent >= 80 {
		add("warning", "system.disk_warning", fmt.Sprintf("根分区使用率 %.1f%%。", result.DiskUsagePercent), "持续观察并提前清理磁盘空间。", 8)
	}
	if result.MemoryUsagePercent >= 90 {
		add("critical", "system.memory_critical", fmt.Sprintf("内存使用率 %.1f%%。", result.MemoryUsagePercent), "检查异常进程或扩容内存。", 12)
	} else if result.MemoryUsagePercent >= 80 {
		add("warning", "system.memory_warning", fmt.Sprintf("内存使用率 %.1f%%。", result.MemoryUsagePercent), "观察进程内存增长并安排扩容。", 6)
	}
	if result.CPUCount > 0 && result.Load1 > float64(result.CPUCount)*2 {
		severity := "warning"
		penalty := 8
		if result.Load1 > float64(result.CPUCount)*4 {
			severity = "critical"
			penalty = 12
		}
		add(severity, "system.load_high", fmt.Sprintf("1 分钟负载 %.2f，CPU 核心数 %d。", result.Load1, result.CPUCount), "定位高负载进程并检查资源配额。", penalty)
	}
	if !result.NodeExporterActive {
		add("warning", "node_exporter.inactive", "未检测到运行中的 node_exporter。", "确认端口和 systemd 服务；如未部署可忽略或使用 Exporter 管理安装。", 5)
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	result.Score = score
	switch {
	case score >= 85:
		result.Status = "healthy"
	case score >= 60:
		result.Status = "warning"
	default:
		result.Status = "critical"
	}
	result.OK = result.Status == "healthy"
	if len(result.Issues) == 0 {
		result.Message = "Agent、Gateway 与系统资源均健康"
	} else {
		result.Message = fmt.Sprintf("%d 项检查需要关注", len(result.Issues))
	}
}

func parseAgentHealthPorts(raw string) []int {
	seen := map[int]bool{}
	out := []int{}
	for _, value := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == ';' }) {
		port, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || port < 1 || port > 65535 || seen[port] {
			continue
		}
		seen[port] = true
		out = append(out, port)
	}
	return out
}

func agentHealthResultByHost(items []AgentHealthResult, host string) AgentHealthResult {
	for _, item := range items {
		if item.Host == host {
			return item
		}
	}
	return AgentHealthResult{Host: host, Status: "unknown"}
}

func replaceAgentHealthResult(items []AgentHealthResult, result AgentHealthResult) []AgentHealthResult {
	for i := range items {
		if items[i].Host == result.Host {
			items[i] = result
			return items
		}
	}
	return append(items, result)
}

func recomputeAgentHealthCheck(item *AgentHealthCheck) {
	item.Total = len(item.Results)
	if item.Total == 0 {
		item.Total = len(item.Hosts)
	}
	item.Healthy, item.Warning, item.Critical = 0, 0, 0
	for _, result := range item.Results {
		switch result.Status {
		case "healthy":
			item.Healthy++
		case "warning":
			item.Warning++
		default:
			item.Critical++
		}
	}
	switch {
	case item.Critical > 0:
		item.Status = "critical"
	case item.Warning > 0:
		item.Status = "warning"
	default:
		item.Status = "healthy"
	}
}

func (s *Service) saveAgentHealthCheck(item AgentHealthCheck) error {
	item = cloneAgentHealthCheck(item)
	if item.Hosts == nil {
		item.Hosts = []string{}
	}
	if item.Results == nil {
		item.Results = []AgentHealthResult{}
	}
	if item.Remediation == nil {
		item.Remediation = []AgentRemediationResult{}
	}
	if s.db != nil {
		if err := s.persistAgentHealthCheck(item); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.agentHealthChecks {
		if s.agentHealthChecks[i].ID == item.ID {
			s.agentHealthChecks[i] = item
			return nil
		}
	}
	s.agentHealthChecks = append([]AgentHealthCheck{item}, s.agentHealthChecks...)
	if len(s.agentHealthChecks) > 200 {
		s.agentHealthChecks = s.agentHealthChecks[:200]
	}
	return nil
}

type agentHealthScanner interface {
	Scan(dest ...any) error
}

func scanAgentHealthCheck(row agentHealthScanner) (AgentHealthCheck, error) {
	var item AgentHealthCheck
	var hostsRaw, resultsRaw, remediationRaw []byte
	err := row.Scan(&item.ID, &item.Status, &item.CredentialID, &item.SSHPort, &item.GatewayURL, &hostsRaw, &resultsRaw, &remediationRaw, &item.Total, &item.Healthy, &item.Warning, &item.Critical, &item.RequestedBy, &item.CreatedAt, &item.FinishedAt)
	if err != nil {
		return AgentHealthCheck{}, err
	}
	if err = json.Unmarshal(hostsRaw, &item.Hosts); err != nil {
		return AgentHealthCheck{}, err
	}
	if err = json.Unmarshal(resultsRaw, &item.Results); err != nil {
		return AgentHealthCheck{}, err
	}
	if err = json.Unmarshal(remediationRaw, &item.Remediation); err != nil {
		return AgentHealthCheck{}, err
	}
	if item.Hosts == nil {
		item.Hosts = []string{}
	}
	if item.Results == nil {
		item.Results = []AgentHealthResult{}
	}
	if item.Remediation == nil {
		item.Remediation = []AgentRemediationResult{}
	}
	return item, nil
}

func (s *Service) loadAgentHealthChecks(limit int) ([]AgentHealthCheck, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(context.Background(), `SELECT `+agentHealthCheckColumns+` FROM agent_health_checks ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AgentHealthCheck{}
	for rows.Next() {
		item, scanErr := scanAgentHealthCheck(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) loadAgentHealthCheck(id string) (AgentHealthCheck, error) {
	item, err := scanAgentHealthCheck(s.db.QueryRowContext(context.Background(), `SELECT `+agentHealthCheckColumns+` FROM agent_health_checks WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AgentHealthCheck{}, ErrNotFound
	}
	return item, err
}

func (s *Service) persistAgentHealthCheck(item AgentHealthCheck) error {
	hosts, err := json.Marshal(item.Hosts)
	if err != nil {
		return err
	}
	results, err := json.Marshal(item.Results)
	if err != nil {
		return err
	}
	remediation, err := json.Marshal(item.Remediation)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(context.Background(), `INSERT INTO agent_health_checks(
id,status,credential_id,ssh_port,gateway_url,hosts,results,remediation,total,healthy,warning,critical,requested_by,created_at,finished_at)
VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8::jsonb,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT(id) DO UPDATE SET status=excluded.status,credential_id=excluded.credential_id,ssh_port=excluded.ssh_port,gateway_url=excluded.gateway_url,hosts=excluded.hosts,results=excluded.results,remediation=excluded.remediation,total=excluded.total,healthy=excluded.healthy,warning=excluded.warning,critical=excluded.critical,requested_by=excluded.requested_by,finished_at=excluded.finished_at`,
		item.ID, item.Status, item.CredentialID, item.SSHPort, item.GatewayURL, string(hosts), string(results), string(remediation), item.Total, item.Healthy, item.Warning, item.Critical, item.RequestedBy, item.CreatedAt, item.FinishedAt)
	return err
}
