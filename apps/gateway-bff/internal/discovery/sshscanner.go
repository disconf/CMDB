package discovery

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	CIDRs       []string `json:"cidrs"`
	Port        int      `json:"port"`
	DefaultType string   `json:"defaultType"`
}

type SSHScanResult struct {
	Scanned   int                `json:"scanned"`
	Found     int                `json:"found"`
	Adopted   int                `json:"adopted"`
	Merged    int                `json:"merged"`
	Conflicts int                `json:"conflicts"`
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
	Hosts     []string `json:"hosts"`
	Port      int      `json:"port"`
	AssetType string   `json:"assetType"`
}
type AgentInstallResult struct {
	Host string `json:"host"`
	OK   bool   `json:"ok"`
	Info string `json:"info"`
}

// AgentBatchInstall installs the cmdb-agent on hosts via SSH. The agent binary is
// fetched by each host from CMDB_AGENT_BUNDLE_URL (must be configured & reachable).
func (s *Service) AgentBatchInstall(in AgentInstallInput) ([]AgentInstallResult, error) {
	username := os.Getenv("CMDB_SSH_USERNAME")
	password := os.Getenv("CMDB_SSH_PASSWORD")
	bundleURL := os.Getenv("CMDB_AGENT_BUNDLE_URL")
	if bundleURL == "" {
		return nil, errors.New("CMDB_AGENT_BUNDLE_URL not configured (host the cmdb-agent binary, e.g. on MinIO/Harbor)")
	}
	port := in.Port
	if port == 0 {
		port = 22
	}
	assetType := in.AssetType
	if assetType == "" {
		assetType = "virtual-machine"
	}
	token := os.Getenv("AGENT_SHARED_TOKEN")
	results := []AgentInstallResult{}
	for _, ip := range in.Hosts {
		envContent := "CMDB_GATEWAY_URL=http://127.0.0.1:30080\nCMDB_AGENT_TOKEN=" + token + "\nCMDB_AGENT_TYPE=" + assetType + "\nCMDB_REPORT_INTERVAL=60s"
		script := "set -e; mkdir -p /opt/cmdb-agent; curl -fsSL --connect-timeout 5 '" + bundleURL + "' -o /opt/cmdb-agent/cmdb-agent; chmod 755 /opt/cmdb-agent/cmdb-agent; printf '%b' '" + envContent + "' > /opt/cmdb-agent/cmdb-agent.env; chmod 600 /opt/cmdb-agent/cmdb-agent.env; cat > /etc/systemd/system/cmdb-agent.service <<'U'\n[Unit]\nDescription=CMDB Agent\nAfter=network-online.target\n[Service]\nEnvironmentFile=/opt/cmdb-agent/cmdb-agent.env\nExecStart=/opt/cmdb-agent/cmdb-agent\nRestart=always\n[Install]\nWantedBy=multi-user.target\nU\nsystemctl daemon-reload; systemctl enable cmdb-agent >/dev/null 2>&1 || true; systemctl restart cmdb-agent; sleep 2; echo ACTIVE=$(systemctl is-active cmdb-agent)"
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
	username := os.Getenv("CMDB_SSH_USERNAME")
	password := os.Getenv("CMDB_SSH_PASSWORD")
	if username == "" || password == "" {
		return result, errors.New("CMDB_SSH_USERNAME/CMDB_SSH_PASSWORD not configured")
	}
	defaultType := strings.TrimSpace(in.DefaultType)
	if defaultType == "" {
		defaultType = "physical-server"
	}
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
		item := DiscoveredItem{
			ID:         "ssh-" + strings.ReplaceAll(host.IP, ".", "-"),
			Name:       host.Name,
			IP:         host.IP,
			Type:       defaultType,
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
		ingestResult, err := s.Ingest(IngestInput{Source: "ssh", Scope: strings.Join(in.CIDRs, ","), Items: items})
		if err != nil {
			return result, err
		}
		result.Adopted = ingestResult.Imported
		result.Merged = ingestResult.Merged
		result.Conflicts = ingestResult.Conflicts
	}
	return result, nil
}
