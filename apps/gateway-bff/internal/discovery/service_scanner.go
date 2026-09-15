package discovery

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

type ServiceScanInput struct {
	CIDRs           []string `json:"cidrs"`
	Port            int      `json:"port"`
	CredentialID    string   `json:"credentialId"`
	RequireApproval bool     `json:"requireApproval"`
	TaskID          string   `json:"taskId,omitempty"`
	TaskName        string   `json:"taskName,omitempty"`
}

type ServiceScanResult struct {
	Scanned   int                 `json:"scanned"`
	Found     int                 `json:"found"`
	Adopted   int                 `json:"adopted"`
	Merged    int                 `json:"merged"`
	Conflicts int                 `json:"conflicts"`
	Services  []DiscoveredItem    `json:"services"`
	Hosts     []ServiceHostResult `json:"hosts"`
	TaskID    string              `json:"taskId,omitempty"`
}

type ServiceHostResult struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname,omitempty"`
	Status   string `json:"status"`
	Services int    `json:"services"`
	Error    string `json:"error,omitempty"`
}

type servicePort struct {
	Port    int
	PID     int
	Process string
}

type discoveredService struct {
	Subtype      string
	Category     string
	Port         int
	Process      string
	User         string
	DataDir      string
	LogPath      string
	JVMPort      string
	ExporterName string
	ExporterPort string
}

const serviceInventoryCommand = `set -e
echo HOSTNAME_BEGIN
hostname
echo HOSTNAME_END
echo PORTS_BEGIN
(ss -lntup 2>/dev/null || netstat -lntup 2>/dev/null || true)
echo PORTS_END
echo PROCESS_BEGIN
ps -eo pid=,user=,comm=,args= 2>/dev/null
echo PROCESS_END`

var pidPattern = regexp.MustCompile(`pid=(\d+)`)
var jvmPortPattern = regexp.MustCompile(`(?:jmxremote\.port=|jmxremote\.port=|com\.sun\.management\.jmxremote\.port=)(\d+)`)
var dataDirPattern = regexp.MustCompile(`(?:--log\.dirs=|--datadir=|--data-dir=|-Dkafka\.logs\.dir=|log\.dirs=)([^\s]+)`)
var logPathPattern = regexp.MustCompile(`(?:--logpath=|--log-path=|--error-log=|--log-file=)([^\s]+)`)

func parseServiceInventory(ip, hostname, output string) []discoveredService {
	ports := parseServicePorts(output)
	processes := parseServiceProcesses(output)
	services := make([]discoveredService, 0)
	for _, process := range processes {
		subtype, category := detectServiceProcess(process.Name, process.Args)
		if subtype == "" {
			continue
		}
		port := 0
		for _, item := range ports {
			if item.PID == process.PID {
				port = item.Port
				break
			}
		}
		if port == 0 {
			port = defaultServicePort(subtype)
		}
		dataDir := firstMatch(dataDirPattern, process.Args)
		logPath := firstMatch(logPathPattern, process.Args)
		jvmPort := firstMatch(jvmPortPattern, process.Args)
		exporterName, exporterPort := exporterForService(subtype, ports, processes)
		services = append(services, discoveredService{
			Subtype: subtype, Category: category, Port: port, Process: process.Name,
			User: process.User, DataDir: dataDir, LogPath: logPath, JVMPort: jvmPort,
			ExporterName: exporterName, ExporterPort: exporterPort,
		})
	}
	return dedupeServices(services)
}

type serviceProcess struct {
	PID  int
	User string
	Name string
	Args string
}

func parseServiceProcesses(output string) []serviceProcess {
	lines := sectionLines(output, "PROCESS_BEGIN", "PROCESS_END")
	result := make([]serviceProcess, 0)
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		name := fields[2]
		args := strings.Join(fields[3:], " ")
		result = append(result, serviceProcess{PID: pid, User: fields[1], Name: name, Args: args})
	}
	return result
}

func parseServicePorts(output string) []servicePort {
	lines := sectionLines(output, "PORTS_BEGIN", "PORTS_END")
	result := make([]servicePort, 0)
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if !strings.Contains(line, "LISTEN") && !strings.Contains(line, "tcp") && !strings.Contains(line, "udp") {
			continue
		}
		port := 0
		for _, candidate := range fields[3:] {
			if value := portFromAddress(candidate); value > 0 {
				port = value
				break
			}
		}
		if port == 0 {
			continue
		}
		pid := 0
		if match := pidPattern.FindStringSubmatch(line); len(match) == 2 {
			pid, _ = strconv.Atoi(match[1])
		}
		process := ""
		if index := strings.Index(line, "users:((\""); index >= 0 {
			remainder := line[index+len("users:((\""):]
			if end := strings.Index(remainder, "\""); end > 0 {
				process = remainder[:end]
			}
		}
		result = append(result, servicePort{Port: port, PID: pid, Process: process})
	}
	return result
}

func portFromAddress(value string) int {
	value = strings.TrimSpace(value)
	if len(value) < 2 || !strings.Contains(value, ":") {
		return 0
	}
	index := strings.LastIndex(value, ":")
	portText := strings.Trim(value[index+1:], "[]")
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}
func sectionLines(output, begin, end string) []string {
	start := strings.Index(output, begin)
	finish := strings.Index(output, end)
	if start < 0 || finish < 0 || finish <= start {
		return nil
	}
	body := output[start+len(begin) : finish]
	return strings.Split(strings.TrimSpace(body), "\n")
}

func detectServiceProcess(name, args string) (string, string) {
	value := strings.ToLower(name + " " + args)
	if strings.Contains(value, "_exporter") || strings.Contains(value, "-prometheus-exporter") {
		return "", ""
	}
	switch {
	case strings.Contains(value, "kafka.kafka") || strings.Contains(value, "kafka-server-start") || strings.Contains(value, "kafka.server"):
		return "kafka", "middleware"
	case strings.Contains(value, "mysqld") || strings.Contains(value, "mariadbd"):
		return "mysql", "database"
	case strings.Contains(value, "postgres"):
		return "postgresql", "database"
	case strings.Contains(value, "mongod"):
		return "mongodb", "database"
	case strings.Contains(value, "redis-server"):
		return "redis", "database"
	case strings.Contains(value, "nginx"):
		return "nginx", "middleware"
	default:
		return "", ""
	}
}

func exporterForService(subtype string, ports []servicePort, processes []serviceProcess) (string, string) {
	exporterName := map[string]string{"kafka": "kafka_exporter", "mysql": "mysqld_exporter", "postgresql": "postgres_exporter", "mongodb": "mongodb_exporter", "redis": "redis_exporter", "nginx": "nginx-prometheus-exporter"}[subtype]
	if exporterName == "" {
		return "", ""
	}
	for _, process := range processes {
		value := strings.ToLower(process.Name + " " + process.Args)
		if strings.Contains(value, strings.ReplaceAll(exporterName, "-", "_")) || strings.Contains(value, exporterName) {
			for _, port := range ports {
				if port.PID == process.PID {
					return exporterName, strconv.Itoa(port.Port)
				}
			}
			return exporterName, strconv.Itoa(defaultExporterPort(subtype))
		}
	}
	return exporterName, ""
}

func defaultServicePort(subtype string) int {
	return map[string]int{"kafka": 9092, "mysql": 3306, "postgresql": 5432, "mongodb": 27017, "redis": 6379, "nginx": 80}[subtype]
}

func defaultExporterPort(subtype string) int {
	return map[string]int{"kafka": 9308, "mysql": 9104, "postgresql": 9187, "mongodb": 9216, "redis": 9121, "nginx": 9113}[subtype]
}

func firstMatch(pattern *regexp.Regexp, value string) string {
	match := pattern.FindStringSubmatch(value)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func dedupeServices(items []discoveredService) []discoveredService {
	seen := map[string]bool{}
	out := make([]discoveredService, 0, len(items))
	for _, item := range items {
		key := item.Subtype + ":" + strconv.Itoa(item.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func (s *Service) ScanServices(ctx context.Context, in ServiceScanInput) (ServiceScanResult, error) {
	result := ServiceScanResult{}
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
	username := strings.TrimSpace(getenv("CMDB_SSH_USERNAME"))
	password := getenv("CMDB_SSH_PASSWORD")
	if in.CredentialID != "" {
		if u, secret, err := s.resolveCredential(in.CredentialID); err == nil && secret != "" {
			username, password = u, secret
		}
	}
	if username == "" || password == "" {
		return result, errors.New("CMDB_SSH_USERNAME/CMDB_SSH_PASSWORD not configured")
	}
	targets := make([]string, 0)
	for _, cidr := range in.CIDRs {
		ips, err := expandCIDR(cidr)
		if err != nil {
			return result, err
		}
		targets = append(targets, ips...)
	}
	result.Scanned = len(targets)
	var mu sync.Mutex
	sem := make(chan struct{}, 24)
	var wg sync.WaitGroup
	hostServices := map[string][]discoveredService{}
	hostNames := map[string]string{}
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
			output, err := runSSHRaw(address, port, username, password, serviceInventoryCommand, 5*time.Second)
			if err != nil {
				mu.Lock()
				result.Hosts = append(result.Hosts, ServiceHostResult{IP: address, Status: "failed", Error: err.Error()})
				mu.Unlock()
				return
			}
			hostname := strings.TrimSpace(sectionText(output, "HOSTNAME_BEGIN", "HOSTNAME_END"))
			services := parseServiceInventory(address, hostname, output)
			status := "no-service"
			if len(services) > 0 {
				status = "discovered"
			}
			mu.Lock()
			result.Hosts = append(result.Hosts, ServiceHostResult{IP: address, Hostname: hostname, Status: status, Services: len(services)})
			if len(services) > 0 {
				hostNames[address] = hostname
				hostServices[address] = services
			}
			mu.Unlock()
		}(ip)
	}
	wg.Wait()
	items := make([]DiscoveredItem, 0)
	for ip, services := range hostServices {
		host := s.findCMDBHost(ip)
		for _, service := range services {
			items = append(items, s.discoveredServiceItem(ip, hostNames[ip], host, service))
		}
	}
	result.Found = len(items)
	if len(items) > 0 {
		ingestResult, err := s.Ingest(IngestInput{TaskID: in.TaskID, TaskName: in.TaskName, Source: "service-scanner", Scope: strings.Join(in.CIDRs, ","), Items: items, RequireApproval: in.RequireApproval})
		if err != nil {
			return result, err
		}
		result.Adopted = ingestResult.Imported
		result.Merged = ingestResult.Merged
		result.Conflicts = ingestResult.Conflicts
		result.TaskID = ingestResult.TaskID
	}
	result.Services = items
	return result, nil
}

func sectionText(output, begin, end string) string {
	start := strings.Index(output, begin)
	finish := strings.Index(output, end)
	if start < 0 || finish < 0 || finish <= start {
		return ""
	}
	return output[start+len(begin) : finish]
}

func getenv(key string) string { return strings.TrimSpace(os.Getenv(key)) }

func (s *Service) findCMDBHost(ip string) cmdb.Asset {
	if s.cmdb == nil || strings.TrimSpace(ip) == "" {
		return cmdb.Asset{}
	}
	for _, asset := range s.cmdb.InventoryAssets() {
		if asset.IP == ip && (asset.Type == "physical-server" || asset.Type == "virtual-machine" || asset.Type == "k8s-node") {
			return asset
		}
	}
	return cmdb.Asset{}
}

func (s *Service) discoveredServiceItem(ip, hostname string, host cmdb.Asset, service discoveredService) DiscoveredItem {
	name := strings.TrimSpace(hostname)
	if name == "" {
		name = ip
	}
	itemType := "middleware"
	if service.Category == "database" {
		itemType = "database"
	}
	attrs := []cmdb.Attribute{
		{Name: "middleware_type", Label: "中间件类型", Value: service.Subtype},
		{Name: "database_type", Label: "数据库类型", Value: service.Subtype},
		{Name: "listen_port", Label: "监听端口", Value: strconv.Itoa(service.Port)},
		{Name: "process_name", Label: "进程名称", Value: service.Process},
		{Name: "run_user", Label: "启动用户", Value: service.User},
		{Name: "data_dir", Label: "数据目录", Value: service.DataDir},
		{Name: "log_path", Label: "日志路径", Value: service.LogPath},
		{Name: "jvm_port", Label: "JVM端口", Value: service.JVMPort},
		{Name: "exporter_address", Label: "Exporter地址", Value: ip},
		{Name: "exporter_port", Label: "Exporter端口", Value: service.ExporterPort},
	}
	relations := []cmdb.Relation{}
	if host.ID != "" {
		relations = append(relations, cmdb.Relation{Type: "runs-on", TargetID: host.ID, TargetName: host.Name})
	}
	return DiscoveredItem{
		ID:   "service-" + strings.ReplaceAll(service.Subtype+"-"+ip+"-"+strconv.Itoa(service.Port), ".", "-"),
		Name: name + "-" + service.Subtype + "-" + strconv.Itoa(service.Port),
		IP:   ip, Type: itemType, Confidence: 90, Attributes: attrs, Relations: relations,
	}
}
