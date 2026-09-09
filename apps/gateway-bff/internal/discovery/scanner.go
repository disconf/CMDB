package discovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

// NodeExporterScanInput lists CIDRs to scan for node_exporter (/metrics on a port).
type NodeExporterScanInput struct {
	CIDRs       []string `json:"cidrs"`
	Port        int      `json:"port"`
	DefaultType string   `json:"defaultType"`
}

type NodeExporterHost struct {
	IP           string           `json:"ip"`
	Name         string           `json:"name"`
	OS           string           `json:"os"`
	Kernel       string           `json:"kernel"`
	Architecture string           `json:"architecture"`
	CPUCount     int              `json:"cpuCount"`
	MemoryBytes  uint64           `json:"memoryBytes"`
	DiskBytes    uint64           `json:"diskBytes"`
	BootTime     string           `json:"bootTime"`
	Attributes   []cmdb.Attribute `json:"attributes"`
}

type NodeExporterScanResult struct {
	Scanned   int                `json:"scanned"`
	Found     int                `json:"found"`
	Adopted   int                `json:"adopted"`
	Merged    int                `json:"merged"`
	Conflicts int                `json:"conflicts"`
	Hosts     []NodeExporterHost `json:"hosts"`
}

var labelPattern = regexp.MustCompile(`([a-zA-Z0-9_]+)="([^"]*)"`)

func expandCIDR(cidr string) ([]string, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	ip := network.IP.To4()
	if ip == nil {
		return nil, fmt.Errorf("IPv6 not supported")
	}
	var out []string
	for candidate := append(net.IP(nil), ip...); network.Contains(candidate); incIP(candidate) {
		out = append(out, candidate.String())
	}
	return out, nil
}

func incIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}

func parseNodeExporter(ip string, body string) *NodeExporterHost {
	host := &NodeExporterHost{IP: ip, Attributes: []cmdb.Attribute{}}
	cpuSet := map[string]bool{}
	var mem, diskRoot uint64
	diskRootFound := false
	osName := ""
	for _, line := range strings.Split(body, "\n") {
		labels := map[string]string{}
		for _, m := range labelPattern.FindAllStringSubmatch(line, -1) {
			labels[m[1]] = m[2]
		}
		value, _ := strconv.ParseFloat(lastField(line), 64)
		switch {
		case strings.HasPrefix(line, "node_uname_info"):
			host.Name = labels["nodename"]
			host.Kernel = labels["release"]
			host.Architecture = labels["machine"]
		case strings.HasPrefix(line, "node_os_info"):
			host.OS = labels["pretty_name"]
			if host.OS == "" {
				host.OS = strings.TrimSpace(labels["name"] + " " + labels["version_id"])
			}
			osName = labels["id"]
		case strings.HasPrefix(line, "node_memory_MemTotal_bytes"):
			mem = uint64(value)
		case strings.HasPrefix(line, "node_cpu_seconds_total"):
			if cpu := labels["cpu"]; cpu != "" {
				cpuSet[cpu] = true
			}
		case strings.HasPrefix(line, "node_boot_time_seconds"):
			if value > 0 {
				host.BootTime = time.Unix(int64(value), 0).Format(time.RFC3339)
			}
		case strings.HasPrefix(line, "node_filesystem_size_bytes"):
			if labels["mountpoint"] == "/" {
				diskRoot = uint64(value)
				diskRootFound = true
			}
		}
	}
	host.CPUCount = len(cpuSet)
	host.MemoryBytes = mem
	if diskRootFound {
		host.DiskBytes = diskRoot
	}
	if host.OS == "" {
		host.OS = osName
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

func lastField(line string) string {
	trimmed := strings.TrimSpace(line)
	idx := strings.LastIndex(trimmed, " ")
	if idx < 0 {
		return trimmed
	}
	return strings.TrimSpace(trimmed[idx+1:])
}

func probeNodeExporter(client *http.Client, ip string, port int) *NodeExporterHost {
	url := fmt.Sprintf("http://%s:%d/metrics", ip, port)
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	request.Header.Set("Accept", "text/plain")
	response, err := client.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}
	limited := io.LimitReader(response.Body, 8<<20)
	bodyBytes, err := io.ReadAll(limited)
	if err != nil || !strings.Contains(string(bodyBytes), "node_uname_info") {
		return nil
	}
	return parseNodeExporter(ip, string(bodyBytes))
}

// ScanNodeExporter scans CIDRs for node_exporter and adopts found hosts into CMDB.
func (s *Service) ScanNodeExporter(ctx context.Context, in NodeExporterScanInput) (NodeExporterScanResult, error) {
	result := NodeExporterScanResult{}
	if len(in.CIDRs) == 0 {
		return result, errors.New("validation")
	}
	port := in.Port
	if port == 0 {
		port = 9100
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
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 600 * time.Millisecond, KeepAlive: 0}).DialContext,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       5 * time.Second,
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 900 * time.Millisecond,
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	var mu sync.Mutex
	sem := make(chan struct{}, 128)
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
			if host := probeNodeExporter(client, address, port); host != nil {
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
			ID:         "node-exporter-" + strings.ReplaceAll(host.IP, ".", "-"),
			Name:       host.Name,
			IP:         host.IP,
			Type:       defaultType,
			Confidence: 90,
			Attributes: host.Attributes,
		}
		if item.Name == "" {
			item.Name = host.IP
		}
		items = append(items, item)
		result.Hosts = append(result.Hosts, *host)
	}
	if len(items) > 0 {
		ingestResult, err := s.Ingest(IngestInput{Source: "node-exporter", Scope: strings.Join(in.CIDRs, ","), Items: items})
		if err != nil {
			return result, err
		}
		result.Adopted = ingestResult.Imported
		result.Merged = ingestResult.Merged
		result.Conflicts = ingestResult.Conflicts
	}
	return result, nil
}
