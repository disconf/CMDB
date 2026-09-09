package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const version = "0.1.1"

type report struct {
	AgentID      string `json:"agentId"`
	Type         string `json:"type"`
	Hostname     string `json:"hostname"`
	IP           string `json:"ip"`
	OS           string `json:"os"`
	Kernel       string `json:"kernel"`
	Architecture string `json:"architecture"`
	CPUCount     int    `json:"cpuCount"`
	MemoryBytes  uint64 `json:"memoryBytes"`
	DiskBytes    uint64 `json:"diskBytes"`
	BootTime     string `json:"bootTime"`
	Version      string `json:"version"`
}

func main() {
	url, token := os.Getenv("CMDB_GATEWAY_URL"), os.Getenv("CMDB_AGENT_TOKEN")
	if url == "" || token == "" {
		panic("CMDB_GATEWAY_URL and CMDB_AGENT_TOKEN are required")
	}
	interval := 60 * time.Second
	if value := os.Getenv("CMDB_REPORT_INTERVAL"); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed >= 10*time.Second {
			interval = parsed
		}
	}
	client := &http.Client{Timeout: 15 * time.Second}
	for {
		if err := send(client, strings.TrimRight(url, "/")+"/api/v1/agent/report", token, collect()); err != nil {
			fmt.Fprintln(os.Stderr, time.Now().Format(time.RFC3339), err)
		}
		time.Sleep(interval)
	}
}
func detectAssetType() string {
	value := strings.ToLower(strings.TrimSpace(readFirst("/sys/class/dmi/id/product_name")))
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(readFirst("/sys/class/dmi/id/sys_vendor")))
	}
	for _, token := range []string{"virtual", "vmware", "kvm", "qemu", "xen", "hyper-v", "virtualbox", "bochs", "parallels"} {
		if strings.Contains(value, token) {
			return "virtual-machine"
		}
	}
	if _, err := os.Stat("/sys/hypervisor"); err == nil {
		return "virtual-machine"
	}
	return "physical-server"
}

func collect() report {
	hostname, _ := os.Hostname()
	id := os.Getenv("CMDB_AGENT_ID")
	if id == "" {
		id = "host-" + strings.ToLower(strings.ReplaceAll(hostname, "_", "-"))
	}
	assetType := strings.TrimSpace(os.Getenv("CMDB_AGENT_TYPE"))
	if assetType == "" {
		assetType = detectAssetType()
	}
	return report{AgentID: id, Type: assetType, Hostname: hostname, IP: primaryIP(), OS: osRelease(), Kernel: readFirst("/proc/sys/kernel/osrelease"), Architecture: runtime.GOARCH, CPUCount: runtime.NumCPU(), MemoryBytes: memory(), DiskBytes: disk(), BootTime: boot(), Version: version}
}
func send(client *http.Client, url, token string, value report) error {
	body, _ := json.Marshal(value)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gateway returned %s", resp.Status)
	}
	return nil
}
func primaryIP() string {
	connections, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, item := range connections {
		if item.Flags&net.FlagUp == 0 || item.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := item.Addrs()
		for _, address := range addresses {
			ip, _, _ := net.ParseCIDR(address.String())
			if v := ip.To4(); v != nil {
				return v.String()
			}
		}
	}
	return ""
}
func osRelease() string {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = strings.Trim(parts[1], `"`)
		}
	}
	if values["PRETTY_NAME"] != "" {
		return values["PRETTY_NAME"]
	}
	return values["ID"]
}
func memory() uint64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var kb uint64
		if _, err = fmt.Sscanf(scanner.Text(), "MemTotal: %d kB", &kb); err == nil {
			return kb * 1024
		}
	}
	return 0
}
func disk() uint64 {
	output, err := exec.Command("df", "-B1", "--output=size", "/").Output()
	if err != nil {
		return 0
	}
	lines := strings.Fields(string(output))
	if len(lines) < 2 {
		return 0
	}
	value, _ := strconv.ParseUint(lines[len(lines)-1], 10, 64)
	return value
}
func boot() string {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "btime ") {
			value, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "btime ")), 10, 64)
			if err == nil {
				return time.Unix(value, 0).Format(time.RFC3339)
			}
		}
	}
	return ""
}
func readFirst(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
