package discovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"cmdb/gateway-bff/internal/cmdb"
)

// SSHScanInput lists CIDRs to scan over SSH. Credentials come from server env
// CMDB_SSH_USERNAME / CMDB_SSH_PASSWORD (kept in a k8s Secret), never in payload.
type SSHScanInput struct {
	CIDRs           []string `json:"cidrs"`
	Port            int      `json:"port"`
	DefaultType     string   `json:"defaultType"`
	CredentialID    string   `json:"credentialId"`
	RequireApproval bool     `json:"requireApproval"`
	TaskID          string   `json:"taskId,omitempty"`
	TaskName        string   `json:"taskName,omitempty"`
}

type SSHScanResult struct {
	Scanned   int                `json:"scanned"`
	Found     int                `json:"found"`
	Adopted   int                `json:"adopted"`
	Merged    int                `json:"merged"`
	Conflicts int                `json:"conflicts"`
	TaskID    string             `json:"taskId,omitempty"`
	Hosts     []NodeExporterHost `json:"hosts"`
}

const sshInventoryCommand = `hostname; grep '^PRETTY_NAME=' /etc/os-release | cut -d= -f2; uname -r; uname -m; nproc; awk '/MemTotal/ {print $2}' /proc/meminfo; df -B1 --output=size / | tail -1; awk '/^btime / {print $2}' /proc/stat; (systemd-detect-virt --vm 2>/dev/null || echo none)`

func parseSSHInventory(ip string, output string) *NodeExporterHost {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 8 {
		return nil
	}
	virt := "none"
	if len(lines) >= 9 {
		virt = strings.TrimSpace(lines[8])
	}
	host := &NodeExporterHost{IP: ip, Virtual: virt != "" && virt != "none"}
	host.Name = strings.TrimSpace(strings.Trim(lines[0], "\""))
	host.OS = strings.TrimSpace(strings.Trim(lines[1], "\""))
	host.Kernel = strings.TrimSpace(lines[2])
	host.Architecture = strings.TrimSpace(lines[3])
	host.CPUCount, _ = strconv.Atoi(strings.TrimSpace(lines[4]))
	memKB, _ := strconv.ParseInt(strings.TrimSpace(lines[5]), 10, 64)
	host.MemoryBytes = uint64(memKB) * 1024
	diskBytes, _ := strconv.ParseUint(strings.TrimSpace(lines[6]), 10, 64)
	host.DiskBytes = diskBytes
	if btime, err := strconv.ParseInt(strings.TrimSpace(lines[7]), 10, 64); err == nil && btime > 0 {
		host.BootTime = time.Unix(btime, 0).Format(time.RFC3339)
	}
	host.Attributes = []cmdb.Attribute{
		{Name: "os", Label: "操作系统", Value: host.OS},
		{Name: "kernel", Label: "内核", Value: host.Kernel},
		{Name: "architecture", Label: "架构", Value: host.Architecture},
		{Name: "cpu", Label: "CPU核心", Value: fmt.Sprint(host.CPUCount)},
		{Name: "memory_bytes", Label: "内存字节", Value: fmt.Sprint(host.MemoryBytes)},
		{Name: "disk_bytes", Label: "磁盘字节(根分区)", Value: fmt.Sprint(host.DiskBytes)},
	}
	if host.BootTime != "" {
		host.Attributes = append(host.Attributes, cmdb.Attribute{Name: "boot_time", Label: "启动时间", Value: host.BootTime})
	}
	if virt != "" && virt != "none" {
		host.Attributes = append(host.Attributes, cmdb.Attribute{Name: "virtualization_type", Label: "虚拟化类型", Value: virt})
	}
	return host
}

func runSSHRaw(ip string, port int, username, password, command string, timeout time.Duration) (string, error) {
	if username == "" || password == "" {
		return "", errors.New("credentials not configured")
	}
	config := &ssh.ClientConfig{User: username, Auth: []ssh.AuthMethod{ssh.Password(password)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: timeout}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, port), config)
	if err != nil {
		return "", err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	output, err := session.CombinedOutput(command)
	return string(output), err
}

type AgentInstallInput struct {
	Hosts         []string `json:"hosts"`
	Port          int      `json:"port"`
	AssetType     string   `json:"assetType"`
	GatewayURL    string   `json:"gatewayUrl"`
	CredentialID  string   `json:"credentialId"`
	TargetVersion string   `json:"targetVersion,omitempty"`
}
type AgentInstallResult struct {
	Host string `json:"host"`
	OK   bool   `json:"ok"`
	Info string `json:"info"`
}

// AgentBatchInstall installs the cmdb-agent on hosts via SSH. The agent binary is
// downloaded by each host from the gateway itself (GET /api/v1/agent/install/linux-amd64).
func (s *Service) agentBatchInstall(in AgentInstallInput) ([]AgentInstallResult, error) {
	username := os.Getenv("CMDB_SSH_USERNAME")
	password := os.Getenv("CMDB_SSH_PASSWORD")
	if in.CredentialID != "" {
		u, sec, err := s.resolveCredential(in.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("resolve SSH credential: %w", err)
		}
		if u == "" || sec == "" {
			return nil, errors.New("SSH credential is empty")
		}
		username, password = u, sec
	}
	if username == "" || password == "" {
		return nil, errors.New("SSH credentials are not configured")
	}
	if target := strings.TrimSpace(in.TargetVersion); target != "" && normalizeAgentVersion(target) != normalizeAgentVersion(AgentBundleVersion()) {
		return nil, fmt.Errorf("agent bundle version %s is not available", target)
	}
	gatewayURL := strings.TrimRight(in.GatewayURL, "/")
	if gatewayURL == "" {
		gatewayURL = strings.TrimRight(os.Getenv("CMDB_AGENT_GATEWAY_URL"), "/")
	}
	if gatewayURL == "" {
		gatewayURL = "http://172.28.69.161:30080"
	}
	port := in.Port
	if port == 0 {
		port = 22
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("invalid port")
	}
	assetType := strings.TrimSpace(in.AssetType)
	token := strings.TrimSpace(os.Getenv("AGENT_SHARED_TOKEN"))
	if token == "" {
		return nil, errors.New("AGENT_SHARED_TOKEN is not configured")
	}
	results := []AgentInstallResult{}
	for _, ip := range in.Hosts {
		envContent := "CMDB_GATEWAY_URL=" + gatewayURL + "\nCMDB_AGENT_TOKEN=" + token + "\nCMDB_AGENT_TYPE=" + assetType + "\nCMDB_REPORT_INTERVAL=60s"
		script := "set -e; mkdir -p /opt/cmdb-agent; curl -fsSL --connect-timeout 5 -H 'Authorization: Bearer " + token + "' '" + gatewayURL + "/api/v1/agent/install/linux-amd64' -o /opt/cmdb-agent/cmdb-agent.new; mv -f /opt/cmdb-agent/cmdb-agent.new /opt/cmdb-agent/cmdb-agent; chmod 755 /opt/cmdb-agent/cmdb-agent; printf '%b' '" + envContent + "' > /opt/cmdb-agent/cmdb-agent.env; chmod 600 /opt/cmdb-agent/cmdb-agent.env; cat > /etc/systemd/system/cmdb-agent.service <<'U'\n[Unit]\nDescription=CMDB Agent\nAfter=network-online.target\n[Service]\nEnvironmentFile=/opt/cmdb-agent/cmdb-agent.env\nExecStart=/opt/cmdb-agent/cmdb-agent\nRestart=always\n[Install]\nWantedBy=multi-user.target\nU\nsystemctl daemon-reload; systemctl enable cmdb-agent >/dev/null 2>&1 || true; systemctl restart cmdb-agent; sleep 2; echo ACTIVE=$(systemctl is-active cmdb-agent)"
		out, err := runSSHRaw(ip, port, username, password, script, 30*time.Second)
		info := ""
		ok := false
		if err != nil {
			info = err.Error()
		} else {
			info = out
			ok = true
		}
		results = append(results, AgentInstallResult{Host: ip, OK: ok, Info: info})
	}
	return results, nil
}

// AgentBatchUpgrade reinstalls the currently served agent bundle. Versioned binary
// hosting is intentionally not faked: the gateway only advertises the bundle it can
// actually serve, so an upgrade is an idempotent reinstall of that bundle.
func (s *Service) agentBatchUpgrade(in AgentInstallInput) ([]AgentInstallResult, error) {
	target := strings.TrimSpace(in.TargetVersion)
	if target == "" || normalizeAgentVersion(target) != normalizeAgentVersion(AgentBundleVersion()) {
		return nil, fmt.Errorf("agent bundle version %s is not available", target)
	}
	return s.agentBatchInstall(in)
}

// AgentBatchUninstall stops and removes cmdb-agent from hosts via SSH.
func (s *Service) agentBatchUninstall(hosts []string, port int, credentialID string) ([]AgentInstallResult, error) {
	username := os.Getenv("CMDB_SSH_USERNAME")
	password := os.Getenv("CMDB_SSH_PASSWORD")
	if credentialID != "" {
		u, sec, err := s.resolveCredential(credentialID)
		if err != nil {
			return nil, fmt.Errorf("resolve SSH credential: %w", err)
		}
		if u == "" || sec == "" {
			return nil, errors.New("SSH credential is empty")
		}
		username, password = u, sec
	}
	if username == "" || password == "" {
		return nil, errors.New("SSH credentials are not configured")
	}
	if port == 0 {
		port = 22
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("invalid port")
	}
	script := "systemctl stop cmdb-agent 2>/dev/null; systemctl disable cmdb-agent 2>/dev/null; rm -f /etc/systemd/system/cmdb-agent.service /opt/cmdb-agent/cmdb-agent /opt/cmdb-agent/cmdb-agent.env; rmdir /opt/cmdb-agent 2>/dev/null || true; systemctl daemon-reload; echo UNINSTALLED"
	results := []AgentInstallResult{}
	for _, ip := range hosts {
		out, err := runSSHRaw(ip, port, username, password, script, 20*time.Second)
		info := ""
		ok := false
		if err != nil {
			info = err.Error()
		} else {
			info = out
			ok = true
		}
		results = append(results, AgentInstallResult{Host: ip, OK: ok, Info: info})
	}
	return results, nil
}

func AgentBundleVersion() string {
	v := strings.TrimSpace(os.Getenv("CMDB_AGENT_VERSION"))
	if v == "" {
		return "0.1.2"
	}
	return v
}

func normalizeAgentVersion(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "v")
}

type AgentVersionItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Hostname      string `json:"hostname"`
	IP            string `json:"ip"`
	Version       string `json:"version"`
	Status        string `json:"status"`
	LastHeartbeat string `json:"lastHeartbeat"`
	Current       bool   `json:"current"`
	Outdated      bool   `json:"outdated"`
	Unknown       bool   `json:"unknown"`
	Source        string `json:"source"`
}

type AgentVersionBucket struct {
	Version string `json:"version"`
	Count   int    `json:"count"`
}

type AgentVersionReport struct {
	CurrentVersion      string               `json:"currentVersion"`
	NodeExporterVersion string               `json:"nodeExporterVersion"`
	Total               int                  `json:"total"`
	Online              int                  `json:"online"`
	Offline             int                  `json:"offline"`
	Current             int                  `json:"current"`
	Outdated            int                  `json:"outdated"`
	Unknown             int                  `json:"unknown"`
	Versions            []AgentVersionBucket `json:"versions"`
	Agents              []AgentVersionItem   `json:"agents"`
}

// AgentVersionReport merges in-memory heartbeats with persisted CMDB agent assets
// so the version ledger remains useful immediately after a gateway restart.
func (s *Service) AgentVersionReport() AgentVersionReport {
	current := normalizeAgentVersion(AgentBundleVersion())
	report := AgentVersionReport{
		CurrentVersion: AgentBundleVersion(), NodeExporterVersion: NodeExporterVersion(),
		Versions: []AgentVersionBucket{}, Agents: []AgentVersionItem{},
	}
	byID := map[string]int{}
	appendItem := func(item AgentVersionItem) {
		item.Version = strings.TrimSpace(item.Version)
		if item.Version == "" || item.Version == "-" || strings.EqualFold(item.Version, "unknown") {
			item.Unknown = true
		} else if normalizeAgentVersion(item.Version) == current {
			item.Current = true
		} else {
			item.Outdated = true
		}
		if item.Status != "online" && item.Status != "offline" {
			item.Status = "pending"
		}
		if index, ok := byID[item.ID]; ok {
			existing := &report.Agents[index]
			if item.Version != "" && item.Version != "-" {
				existing.Version = item.Version
			}
			if item.Status != "" && item.Status != "pending" {
				existing.Status = item.Status
			}
			if item.LastHeartbeat != "" {
				existing.LastHeartbeat = item.LastHeartbeat
			}
			existing.Current = existing.Version != "" && normalizeAgentVersion(existing.Version) == current
			existing.Unknown = existing.Version == "" || existing.Version == "-" || strings.EqualFold(existing.Version, "unknown")
			existing.Outdated = !existing.Current && !existing.Unknown
			return
		}
		byID[item.ID] = len(report.Agents)
		report.Agents = append(report.Agents, item)
	}
	for _, agent := range s.Agents() {
		appendItem(AgentVersionItem{ID: agent.ID, Name: agent.Name, Hostname: agent.Hostname, IP: agent.IP, Version: agent.Version, Status: agent.Status, LastHeartbeat: agent.LastHeartbeat, Source: "heartbeat"})
	}
	if s.cmdb != nil {
		for _, asset := range s.cmdb.MonitoringAssets() {
			version := assetAttribute(asset, "agent_version")
			status := "pending"
			if asset.Status == "online" || asset.Status == "warning" {
				status = "online"
			} else if asset.Status == "offline" {
				status = "offline"
			}
			appendItem(AgentVersionItem{ID: asset.ID, Name: asset.Name, Hostname: asset.Name, IP: asset.IP, Version: version, Status: status, LastHeartbeat: asset.LastSeenAt, Source: "cmdb"})
		}
	}
	buckets := map[string]int{}
	for _, item := range report.Agents {
		report.Total++
		switch item.Status {
		case "online":
			report.Online++
		case "offline":
			report.Offline++
		}
		switch {
		case item.Unknown:
			report.Unknown++
		case item.Current:
			report.Current++
		default:
			report.Outdated++
		}
		buckets[item.Version]++
	}
	for version, count := range buckets {
		report.Versions = append(report.Versions, AgentVersionBucket{Version: version, Count: count})
	}
	sort.Slice(report.Versions, func(i, j int) bool {
		iCurrent := report.Versions[i].Version == report.CurrentVersion
		jCurrent := report.Versions[j].Version == report.CurrentVersion
		if iCurrent != jCurrent {
			return iCurrent
		}
		return report.Versions[i].Version < report.Versions[j].Version
	})
	sort.Slice(report.Agents, func(i, j int) bool {
		if report.Agents[i].Outdated != report.Agents[j].Outdated {
			return report.Agents[i].Outdated
		}
		if report.Agents[i].Status != report.Agents[j].Status {
			return report.Agents[i].Status < report.Agents[j].Status
		}
		return report.Agents[i].IP < report.Agents[j].IP
	})
	return report
}

func assetAttribute(asset cmdb.Asset, name string) string {
	for _, attribute := range asset.Attributes {
		if attribute.Name == name {
			return strings.TrimSpace(attribute.Value)
		}
	}
	return ""
}

func runSSHInventory(ip string, port int, username, password string, timeout time.Duration) *NodeExporterHost {
	if username == "" || password == "" {
		return nil
	}
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	address := fmt.Sprintf("%s:%d", ip, port)
	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return nil
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return nil
	}
	defer session.Close()
	output, err := session.CombinedOutput(sshInventoryCommand)
	if err != nil {
		return nil
	}
	return parseSSHInventory(ip, string(output))
}

// ScanSSH scans CIDRs over SSH and adopts found hosts into CMDB (deduped by IP).
func (s *Service) ScanSSH(ctx context.Context, in SSHScanInput) (SSHScanResult, error) {
	result := SSHScanResult{}
	if len(in.CIDRs) == 0 {
		return result, errors.New("validation")
	}
	port := in.Port
	if port == 0 {
		port = 22
	}
	if port < 1 || port > 65535 {
		return result, errors.New("invalid port")
	}
	username := os.Getenv("CMDB_SSH_USERNAME")
	password := os.Getenv("CMDB_SSH_PASSWORD")
	if in.CredentialID != "" {
		if u, sec, err := s.resolveCredential(in.CredentialID); err == nil && sec != "" {
			username, password = u, sec
		}
	}
	if username == "" || password == "" {
		return result, errors.New("CMDB_SSH_USERNAME/CMDB_SSH_PASSWORD not configured")
	}
	defaultType := strings.TrimSpace(in.DefaultType)
	var targets []string
	for _, cidr := range in.CIDRs {
		ips, err := expandCIDR(cidr)
		if err != nil {
			return result, err
		}
		targets = append(targets, ips...)
	}
	result.Scanned = len(targets)
	var mu sync.Mutex
	sem := make(chan struct{}, 48)
	var wg sync.WaitGroup
	found := make([]*NodeExporterHost, 0, 64)
	for _, ip := range targets {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			select {
			case <-ctx.Done():
				return
			default:
			}
			if host := runSSHInventory(address, port, username, password, 1500*time.Millisecond); host != nil {
				mu.Lock()
				found = append(found, host)
				mu.Unlock()
			}
		}(ip)
	}
	wg.Wait()
	result.Found = len(found)
	items := make([]DiscoveredItem, 0, len(found))
	for _, host := range found {
		itemType := defaultType
		if itemType == "" {
			if host.Virtual {
				itemType = "virtual-machine"
			} else {
				itemType = "physical-server"
			}
		}
		item := DiscoveredItem{
			ID:         "ssh-" + strings.ReplaceAll(host.IP, ".", "-"),
			Name:       host.Name,
			IP:         host.IP,
			Type:       itemType,
			Confidence: 95,
			Attributes: host.Attributes,
		}
		if item.Name == "" {
			item.Name = host.IP
		}
		items = append(items, item)
		result.Hosts = append(result.Hosts, *host)
	}
	if len(items) > 0 {
		ingestResult, err := s.Ingest(IngestInput{TaskID: in.TaskID, TaskName: in.TaskName, Source: "ssh", Scope: strings.Join(in.CIDRs, ","), Items: items, RequireApproval: in.RequireApproval})
		if err != nil {
			return result, err
		}
		result.Adopted = ingestResult.Imported
		result.Merged = ingestResult.Merged
		result.Conflicts = ingestResult.Conflicts
		result.TaskID = ingestResult.TaskID
	}
	return result, nil
}
