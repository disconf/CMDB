package discovery

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

var ErrNotFound = errors.New("not found")

type Agent struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	OS       string `json:"os"
	"strings"`
	Region        string `json:"region"`
	Status        string `json:"status"`
	Version       string `json:"version"`
	LastHeartbeat string `json:"lastHeartbeat"`
}
type RegisterInput struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	OS       string `json:"os"
	"strings"`
	Region string `json:"region"`
}
type Summary struct {
	Total   int `json:"total"`
	Online  int `json:"online"`
	Offline int `json:"offline"`
	Tasks   int `json:"tasks"`
	Pending int `json:"pending"`
}
type Task struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Source     string           `json:"source"`
	Scope      string           `json:"scope"`
	Status     string           `json:"status"`
	CreatedAt  string           `json:"createdAt"`
	Discovered int              `json:"discovered"`
	Imported   int              `json:"imported"`
	Items      []DiscoveredItem `json:"items"`
}
type CreateTaskInput struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Scope  string `json:"scope"`
}
type IngestInput struct {
	Source          string           `json:"source"`
	Scope           string           `json:"scope"`
	Items           []DiscoveredItem `json:"items"`
	RequireApproval bool             `json:"requireApproval"`
}

type PendingItem struct {
	TaskID string `json:"taskId"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Type   string `json:"type"`
	Source string `json:"source"`
	State  string `json:"state"`
}
type IngestResult struct {
	TaskID    string `json:"taskId"`
	Accepted  int    `json:"accepted"`
	Imported  int    `json:"imported"`
	Merged    int    `json:"merged"`
	Conflicts int    `json:"conflicts"`
}
type DiscoveredItem struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	IP         string           `json:"ip"`
	Type       string           `json:"type"`
	Confidence int              `json:"confidence"`
	State      string           `json:"state"`
	Attributes []cmdb.Attribute `json:"attributes"`
}
type AgentReport struct {
	AgentID  string `json:"agentId"`
	Type     string `json:"type"`
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	OS       string `json:"os"
	"strings"`
	Kernel       string `json:"kernel"`
	Architecture string `json:"architecture"`
	CPUCount     int    `json:"cpuCount"`
	MemoryBytes  uint64 `json:"memoryBytes"`
	DiskBytes    uint64 `json:"diskBytes"`
	BootTime     string `json:"bootTime"`
	Version      string `json:"version"`
}
type Service struct {
	mu           sync.RWMutex
	agents       []Agent
	tasks        []Task
	db           *sql.DB
	cmdb         *cmdb.Service
	agentToken   string
	agentSeen    map[string]time.Time
	offlineAfter time.Duration
	credResolver func(id string) (string, string, error)
}

func NewService() *Service {
	return NewServiceWithCMDB(nil)
}
func NewServiceWithCMDB(cmdbService *cmdb.Service) *Service {
	s := &Service{cmdb: cmdbService, agents: []Agent{}, tasks: []Task{{ID: "disc-001", Name: "生产区主机发现", Source: "agent", Scope: "华东生产区", Status: "completed", CreatedAt: "2026-07-15 12:30", Discovered: 3, Items: sampleItems()}}}
	s.agentToken = os.Getenv("AGENT_SHARED_TOKEN")
	s.agentSeen = map[string]time.Time{}
	s.offlineAfter = 3 * time.Minute
	if value := os.Getenv("AGENT_OFFLINE_AFTER"); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed >= 30*time.Second {
			s.offlineAfter = parsed
		}
	}
	if url := os.Getenv("DATABASE_URL"); url != "" {
		db, tasks, err := openPostgres(url)
		if err != nil {
			panic(fmt.Sprintf("initialize discovery repository: %v", err))
		}
		s.db = db
		s.tasks = tasks
	}
	demoAgents := []Agent{{"agt-01", "上海区域 Agent", "sh-agent-01", "10.8.0.11", "linux", "华东", "online", "1.2.0", "刚刚"}, {"agt-02", "K8s 采集 Agent", "k8s-collector", "10.8.0.12", "linux", "华东", "online", "1.2.0", "12 秒前"}, {"agt-03", "北京网络 Agent", "bj-net-agent", "10.9.0.21", "linux", "华北", "offline", "1.1.8", "18 分钟前"}}
	if s.db == nil {
		s.agents = demoAgents
	}
	return s
}
func (s *Service) SetCredentialResolver(fn func(id string) (string, string, error)) {
	s.credResolver = fn
}

func (s *Service) resolveCredential(id string) (string, string, error) {
	if id == "" || s.credResolver == nil {
		return "", "", nil
	}
	return s.credResolver(id)
}

func (s *Service) AuthorizeAgentToken(token string) bool {
	if s.agentToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.agentToken)) == 1
}

func (s *Service) Report(token string, in AgentReport) (cmdb.Asset, error) {
	if s.agentToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.agentToken)) != 1 {
		return cmdb.Asset{}, errors.New("unauthorized")
	}
	if s.cmdb == nil || in.AgentID == "" || in.Hostname == "" {
		return cmdb.Asset{}, errors.New("validation")
	}
	asset, err := s.cmdb.UpsertAgentAsset(cmdb.AgentAssetInput{ID: in.AgentID, Type: in.Type, Hostname: in.Hostname, IP: in.IP, OS: in.OS, Kernel: in.Kernel, Architecture: in.Architecture, CPUCount: in.CPUCount, MemoryBytes: in.MemoryBytes, DiskBytes: in.DiskBytes, BootTime: in.BootTime, AgentVersion: in.Version})
	if err != nil {
		return cmdb.Asset{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentSeen[in.AgentID] = time.Now()
	for i := range s.agents {
		if s.agents[i].ID == in.AgentID {
			s.agents[i].Hostname = in.Hostname
			s.agents[i].IP = in.IP
			s.agents[i].OS = in.OS
			s.agents[i].Status = "online"
			s.agents[i].Version = in.Version
			s.agents[i].LastHeartbeat = time.Now().Format("15:04:05")
			return asset, nil
		}
	}
	s.agents = append(s.agents, Agent{ID: in.AgentID, Name: in.Hostname, Hostname: in.Hostname, IP: in.IP, OS: in.OS, Status: "online", Version: in.Version, LastHeartbeat: time.Now().Format("15:04:05")})
	return asset, nil
}
func (s *Service) RunAgentHealth(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for i := range s.agents {
				seen, tracked := s.agentSeen[s.agents[i].ID]
				if tracked && now.Sub(seen) > s.offlineAfter && s.agents[i].Status != "offline" {
					s.agents[i].Status = "offline"
					s.agents[i].LastHeartbeat = "超时离线"
					if s.cmdb != nil {
						_ = s.cmdb.MarkAgentAssetOffline(s.agents[i].ID)
					}
				}
			}
			s.mu.Unlock()
		}
	}
}

// AutoScanLoops runs periodic auto-scan jobs configured via env vars:
//
//	CMDB_AUTO_SCAN_INTERVAL_MIN  (default 5)
//	CMDB_AUTO_SCAN_SNMP_CIDRS    (comma separated, uses CMDB_SNMP_COMMUNITY)
//	CMDB_AUTO_SCAN_NODE_CIDRS    (comma separated, port 9100)
//	CMDB_AUTO_SCAN_SSH_CIDRS     (comma separated, uses CMDB_SSH_*)
func (s *Service) AutoScanLoops(ctx context.Context) {
	interval := 5
	if v := os.Getenv("CMDB_AUTO_SCAN_INTERVAL_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 1440 {
			interval = n
		}
	}
	snmp := os.Getenv("CMDB_AUTO_SCAN_SNMP_CIDRS")
	node := os.Getenv("CMDB_AUTO_SCAN_NODE_CIDRS")
	ssh := os.Getenv("CMDB_AUTO_SCAN_SSH_CIDRS")
	if snmp == "" && node == "" && ssh == "" {
		return
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if snmp != "" {
				if _, err := s.ScanSNMP(ctx, SNMPScanInput{CIDRs: splitList(snmp)}); err != nil {
					slog.Error("auto snmp scan", "error", err)
				}
			}
			if node != "" {
				if _, err := s.ScanNodeExporter(ctx, NodeExporterScanInput{CIDRs: splitList(node)}); err != nil {
					slog.Error("auto node scan", "error", err)
				}
			}
			if ssh != "" {
				if _, err := s.ScanSSH(ctx, SSHScanInput{CIDRs: splitList(ssh)}); err != nil {
					slog.Error("auto ssh scan", "error", err)
				}
			}
		}
	}
}

func splitList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (s *Service) Register(in RegisterInput) (Agent, error) {
	if in.Name == "" || in.Hostname == "" {
		return Agent{}, errors.New("validation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a := Agent{ID: fmt.Sprintf("agt-%02d", len(s.agents)+1), Name: in.Name, Hostname: in.Hostname, IP: in.IP, OS: in.OS, Region: in.Region, Status: "pending", Version: "-", LastHeartbeat: "尚未连接"}
	s.agents = append(s.agents, a)
	return a, nil
}
func (s *Service) Heartbeat(id, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.agents {
		if s.agents[i].ID == id {
			s.agents[i].Status = "online"
			s.agents[i].Version = version
			s.agents[i].LastHeartbeat = time.Now().Format("15:04:05")
			return nil
		}
	}
	return ErrNotFound
}
func (s *Service) Agents() []Agent {
	return s.AgentList("", "")
}

func (s *Service) AgentList(query, status string) []Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Agent{}
	for _, a := range s.agents {
		if status != "" && a.Status != status {
			continue
		}
		if query != "" {
			needle := strings.ToLower(query)
			hay := strings.ToLower(a.Name + " " + a.Hostname + " " + a.IP + " " + a.OS)
			if !strings.Contains(hay, needle) {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

func (s *Service) AgentDetail(id string) (Agent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.agents {
		if a.ID == id {
			return a, true
		}
	}
	return Agent{}, false
}
func (s *Service) Tasks() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Task(nil), s.tasks...)
}
func (s *Service) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := Summary{Total: len(s.agents), Tasks: len(s.tasks)}
	for _, a := range s.agents {
		if a.Status == "online" {
			r.Online++
		} else {
			r.Offline++
		}
	}
	for _, t := range s.tasks {
		r.Pending += t.Discovered - t.Imported
	}
	return r
}
func (s *Service) CreateTask(in CreateTaskInput) (Task, error) {
	if in.Name == "" || in.Source == "" {
		return Task{}, errors.New("validation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := Task{ID: fmt.Sprintf("disc-%03d", len(s.tasks)+1), Name: in.Name, Source: in.Source, Scope: in.Scope, Status: "pending", CreatedAt: time.Now().Format("2006-01-02 15:04")}
	s.tasks = append(s.tasks, t)
	if err := s.persistTask(t); err != nil {
		s.tasks = s.tasks[:len(s.tasks)-1]
		return Task{}, err
	}
	return t, nil
}
func (s *Service) markImported(item DiscoveredItem) error {
	if s.db != nil {
		if _, err := s.db.Exec("UPDATE discovery_items SET state='imported' WHERE id=$1", item.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) importDiscovered(item DiscoveredItem, source string) (string, error) {
	if s.cmdb == nil {
		_ = s.markImported(item)
		return "imported", nil
	}
	src := source
	if src == "" {
		src = "discovery"
	}
	// Same host already known under a different ID -> merge/refresh, keep canonical asset.
	if item.IP != "" {
		if existingID := s.cmdb.FindHostByIP(item.IP, item.ID); existingID != "" {
			if _, err := s.cmdb.MergeHostInventory(existingID, src, item.Attributes); err != nil {
				return "", err
			}
			_ = s.markImported(item)
			return "merged", nil
		}
	}
	_, err := s.cmdb.CreateAsset(cmdb.CreateAssetInput{ID: item.ID, Name: item.Name, Type: item.Type, Status: "online", IP: item.IP, Environment: "待确认", ProjectGroup: "自动发现", Owner: "待分配", Source: src, Tags: []string{src}, Attributes: item.Attributes}, "discovery-reconcile")
	if err != nil {
		if errors.Is(err, cmdb.ErrConflict) {
			// Same ID already exists -> refresh in place.
			if _, err2 := s.cmdb.MergeHostInventory(item.ID, src, item.Attributes); err2 != nil {
				return "", err2
			}
			_ = s.markImported(item)
			return "merged", nil
		}
		return "", err
	}
	_ = s.markImported(item)
	return "imported", nil
}

// Ingest accepts discovered items from collectors (ssh/snmp/node_exporter) and auto-imports them to CMDB.
func (s *Service) Ingest(in IngestInput) (IngestResult, error) {
	if strings.TrimSpace(in.Source) == "" || len(in.Items) == 0 {
		return IngestResult{}, errors.New("validation")
	}
	taskID := in.Source + "-" + time.Now().Format("20060102")
	result := IngestResult{TaskID: taskID}
	s.mu.Lock()
	index := -1
	for i := range s.tasks {
		if s.tasks[i].ID == taskID {
			index = i
			break
		}
	}
	if index < 0 {
		s.tasks = append(s.tasks, Task{ID: taskID, Name: in.Source + " 自动发现", Source: in.Source, Scope: in.Scope, Status: "running", CreatedAt: time.Now().Format("2006-01-02 15:04")})
		index = len(s.tasks) - 1
		if err := s.persistTask(s.tasks[index]); err != nil {
			s.tasks = s.tasks[:len(s.tasks)-1]
			s.mu.Unlock()
			return IngestResult{}, err
		}
	}
	task := &s.tasks[index]
	var pending []DiscoveredItem
	for _, item := range in.Items {
		if item.ID == "" {
			continue
		}
		exists := false
		for _, existing := range task.Items {
			if existing.ID == item.ID {
				exists = true
				break
			}
		}
		if exists {
			result.Conflicts++
			continue
		}
		item.State = "pending"
		task.Items = append(task.Items, item)
		pending = append(pending, item)
		if err := s.persistItem(taskID, item, []byte(`{}`)); err != nil {
			s.mu.Unlock()
			return IngestResult{}, err
		}
	}
	s.mu.Unlock()
	if in.RequireApproval {
		s.mu.Lock()
		if index < len(s.tasks) {
			s.tasks[index].Status = "pending-approval"
			s.tasks[index].Discovered = len(s.tasks[index].Items)
			_ = s.persistTask(s.tasks[index])
		}
		s.mu.Unlock()
		result.Accepted = len(pending)
		return result, nil
	}
	handled := map[string]bool{}

	for _, item := range pending {
		status, err := s.importDiscovered(item, in.Source)
		if err != nil {
			return IngestResult{}, err
		}
		result.Accepted++
		if status == "imported" {
			result.Imported++
		} else if status == "merged" {
			result.Merged++
		} else if status == "conflict" {
			result.Conflicts++
		}
		handled[item.ID] = true
	}
	s.mu.Lock()
	if index < len(s.tasks) {
		s.tasks[index].Discovered = len(s.tasks[index].Items)
		for i := range s.tasks[index].Items {
			if handled[s.tasks[index].Items[i].ID] {
				s.tasks[index].Items[i].State = "imported"
			}
		}
		s.tasks[index].Imported = 0
		for _, item := range s.tasks[index].Items {
			if item.State == "imported" {
				s.tasks[index].Imported++
			}
		}
		if err := s.persistTask(s.tasks[index]); err != nil {
			s.mu.Unlock()
			return IngestResult{}, err
		}
	}
	s.mu.Unlock()
	return result, nil
}

func (s *Service) PendingList() []PendingItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []PendingItem{}
	for _, task := range s.tasks {
		for _, item := range task.Items {
			if item.State == "pending" {
				out = append(out, PendingItem{TaskID: task.ID, ID: item.ID, Name: item.Name, IP: item.IP, Type: item.Type, Source: task.Source, State: item.State})
			}
		}
	}
	return out
}

func (s *Service) ApprovePending(itemID string) (string, error) {
	s.mu.RLock()
	var item DiscoveredItem
	source, taskIndex, itemIndex := "", -1, -1
	for i := range s.tasks {
		for j := range s.tasks[i].Items {
			if s.tasks[i].Items[j].ID == itemID && s.tasks[i].Items[j].State == "pending" {
				item = s.tasks[i].Items[j]
				source = s.tasks[i].Source
				taskIndex, itemIndex = i, j
			}
		}
	}
	s.mu.RUnlock()
	if taskIndex < 0 {
		return "", ErrNotFound
	}
	status, err := s.importDiscovered(item, source)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.tasks[taskIndex].Items[itemIndex].State = "imported"
	s.tasks[taskIndex].Status = "completed"
	s.tasks[taskIndex].Imported++
	_ = s.persistTask(s.tasks[taskIndex])
	s.mu.Unlock()
	if s.db != nil {
		_, _ = s.db.Exec("UPDATE discovery_items SET state='imported' WHERE id=$1", itemID)
	}
	return status, nil
}

func (s *Service) RejectPending(itemID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		for j := range s.tasks[i].Items {
			if s.tasks[i].Items[j].ID == itemID && s.tasks[i].Items[j].State == "pending" {
				s.tasks[i].Items[j].State = "rejected"
				_ = s.persistTask(s.tasks[i])
				if s.db != nil {
					_, _ = s.db.Exec("UPDATE discovery_items SET state='rejected' WHERE id=$1", itemID)
				}
				return nil
			}
		}
	}
	return ErrNotFound
}

func (s *Service) RunTask(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks[i].Status = "completed"
			s.tasks[i].Items = sampleItems()
			s.tasks[i].Discovered = len(s.tasks[i].Items)
			if err := s.persistTask(s.tasks[i]); err != nil {
				return Task{}, err
			}
			for _, item := range s.tasks[i].Items {
				if err := s.persistItem(id, item, []byte(`{}`)); err != nil {
					return Task{}, err
				}
			}
			return s.tasks[i], nil
		}
	}
	return Task{}, ErrNotFound
}
func (s *Service) Reconcile(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			for j := range s.tasks[i].Items {
				if s.tasks[i].Items[j].State == "imported" {
					continue
				}
				if s.cmdb != nil {
					item := s.tasks[i].Items[j]
					_, err := s.cmdb.CreateAsset(cmdb.CreateAssetInput{ID: item.ID, Name: item.Name, Type: item.Type, Status: "online", IP: item.IP, Environment: "待确认", ProjectGroup: "自动发现", Owner: "待分配", Source: "discovery", Tags: []string{"自动发现"}, Attributes: item.Attributes}, "discovery-reconcile")
					if err != nil && !errors.Is(err, cmdb.ErrConflict) {
						return Task{}, err
					}
				}
				s.tasks[i].Items[j].State = "imported"
				if s.db != nil {
					if _, err := s.db.Exec("UPDATE discovery_items SET state='imported' WHERE id=$1", s.tasks[i].Items[j].ID); err != nil {
						return Task{}, err
					}
				}
			}
			s.tasks[i].Imported = s.tasks[i].Discovered
			return s.tasks[i], nil
		}
	}
	return Task{}, ErrNotFound
}
func sampleItems() []DiscoveredItem {
	return []DiscoveredItem{{"found-01", "new-app-node-01", "10.20.8.31", "physical-server", 96, "pending", nil}, {"found-02", "new-app-node-02", "10.20.8.32", "physical-server", 94, "pending", nil}, {"found-03", "prod-k8s-worker-03", "10.22.1.23", "k8s-node", 99, "pending", nil}}
}
