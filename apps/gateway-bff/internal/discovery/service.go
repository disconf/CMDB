package discovery

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/demo"
)

var ErrNotFound = errors.New("not found")

type Agent struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Hostname      string `json:"hostname"`
	IP            string `json:"ip"`
	OS            string `json:"os"`
	Region        string `json:"region"`
	Status        string `json:"status"`
	Version       string `json:"version"`
	LastHeartbeat string `json:"lastHeartbeat"`
}
type RegisterInput struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	OS       string `json:"os"`
	Region   string `json:"region"`
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
	UpdatedAt  string           `json:"updatedAt,omitempty"`
	Discovered int              `json:"discovered"`
	Imported   int              `json:"imported"`
	Items      []DiscoveredItem `json:"items"`
}
type TaskEvent struct {
	ID        string `json:"id"`
	TaskID    string `json:"taskId"`
	Level     string `json:"level"`
	Event     string `json:"event"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}
type TaskResultSummary struct {
	Discovered int `json:"discovered"`
	Imported   int `json:"imported"`
	Merged     int `json:"merged"`
	Pending    int `json:"pending"`
	Rejected   int `json:"rejected"`
	Conflicts  int `json:"conflicts"`
	Failed     int `json:"failed"`
}
type TaskDetail struct {
	Task    Task              `json:"task"`
	Summary TaskResultSummary `json:"summary"`
	Events  []TaskEvent       `json:"events"`
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
	TaskID       string `json:"taskId"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	IP           string `json:"ip"`
	Type         string `json:"type"`
	Source       string `json:"source"`
	State        string `json:"state"`
	Confidence   int    `json:"confidence"`
	DiscoveredAt string `json:"discoveredAt"`
}
type IngestResult struct {
	TaskID    string `json:"taskId"`
	Accepted  int    `json:"accepted"`
	Imported  int    `json:"imported"`
	Merged    int    `json:"merged"`
	Conflicts int    `json:"conflicts"`
	Failed    int    `json:"failed"`
}
type DiscoveredItem struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	IP           string           `json:"ip"`
	Type         string           `json:"type"`
	Confidence   int              `json:"confidence"`
	State        string           `json:"state"`
	Result       string           `json:"result,omitempty"`
	Message      string           `json:"message,omitempty"`
	UpdatedAt    string           `json:"updatedAt,omitempty"`
	Attributes   []cmdb.Attribute `json:"attributes"`
	PreserveType bool             `json:"preserveType,omitempty"`
}
type AgentReport struct {
	AgentID        string `json:"agentId"`
	Type           string `json:"type"`
	Hostname       string `json:"hostname"`
	IP             string `json:"ip"`
	OS             string `json:"os"`
	Kernel         string `json:"kernel"`
	Architecture   string `json:"architecture"`
	CPUCount       int    `json:"cpuCount"`
	MemoryBytes    uint64 `json:"memoryBytes"`
	DiskBytes      uint64 `json:"diskBytes"`
	BootTime       string `json:"bootTime"`
	Version        string `json:"version"`
	Virtualization string `json:"virtualization"`
}
type Service struct {
	mu                sync.RWMutex
	agents            []Agent
	tasks             []Task
	db                *sql.DB
	cmdb              *cmdb.Service
	agentToken        string
	agentSeen         map[string]time.Time
	offlineAfter      time.Duration
	credResolver      func(id string) (string, string, error)
	events            map[string][]TaskEvent
	nextEvent         int64
	remoteExecutions  []RemoteExecution
	remoteRunner      RemoteRunner
	accessGrants      []AccessGrant
	hostKeys          []RemoteHostKey
	remoteSessions    []RemoteSession
	terminalApprovals []TerminalApproval
	terminalTickets   map[string]terminalTicketRecord
	terminalSequences map[string]uint64
	terminals         map[string]*RemoteTerminalChannel
}

func NewService() *Service {
	return NewServiceWithCMDB(nil)
}
func NewServiceWithCMDB(cmdbService *cmdb.Service) *Service {
	s := &Service{cmdb: cmdbService, agents: []Agent{}, tasks: []Task{}}
	if demo.Enabled() {
		s.tasks = []Task{{ID: "disc-001", Name: "生产区主机发现", Source: "agent", Scope: "华东生产区", Status: "completed", CreatedAt: "2026-07-15 12:30", Discovered: 3, Items: sampleItems()}}
	}
	s.agentToken = os.Getenv("AGENT_SHARED_TOKEN")
	s.agentSeen = map[string]time.Time{}
	s.events = map[string][]TaskEvent{}
	s.nextEvent = time.Now().UnixNano()
	s.remoteExecutions = []RemoteExecution{}
	s.terminalTickets = map[string]terminalTicketRecord{}
	s.terminalSequences = map[string]uint64{}
	s.terminals = map[string]*RemoteTerminalChannel{}
	s.terminalApprovals = []TerminalApproval{}
	s.remoteRunner = sshRemoteRunner
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
		if err := s.loadRemoteExecutions(); err != nil {
			panic(fmt.Sprintf("load remote executions: %v", err))
		}
		if err := s.loadAccessGrants(); err != nil {
			panic(fmt.Sprintf("load remote access grants: %v", err))
		}
		if err := s.loadHostKeys(); err != nil {
			panic(fmt.Sprintf("load remote host keys: %v", err))
		}
		if err := s.loadRemoteSessions(); err != nil {
			panic(fmt.Sprintf("load remote sessions: %v", err))
		}
		if err := s.loadTerminalApprovals(); err != nil {
			panic(fmt.Sprintf("load terminal approvals: %v", err))
		}
	}
	demoAgents := []Agent{{"agt-01", "上海区域 Agent", "sh-agent-01", "10.8.0.11", "linux", "华东", "online", "1.2.0", "刚刚"}, {"agt-02", "K8s 采集 Agent", "k8s-collector", "10.8.0.12", "linux", "华东", "online", "1.2.0", "12 秒前"}, {"agt-03", "北京网络 Agent", "bj-net-agent", "10.9.0.21", "linux", "华北", "offline", "1.1.8", "18 分钟前"}}
	if s.db == nil && demo.Enabled() {
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
	asset, err := s.cmdb.UpsertAgentAsset(cmdb.AgentAssetInput{ID: in.AgentID, Type: in.Type, Hostname: in.Hostname, IP: in.IP, OS: in.OS, Kernel: in.Kernel, Architecture: in.Architecture, CPUCount: in.CPUCount, MemoryBytes: in.MemoryBytes, DiskBytes: in.DiskBytes, BootTime: in.BootTime, AgentVersion: in.Version, Virtualization: in.Virtualization})
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
//	CMDB_AUTO_SCAN_NODE_CIDRS    (comma separated, ports from CMDB_AUTO_SCAN_NODE_PORTS, default 9100,19100)
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
	nodePorts := autoNodeExporterPorts()
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
				if _, err := s.ScanNodeExporter(ctx, NodeExporterScanInput{CIDRs: splitList(node), Ports: nodePorts}); err != nil {
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

func autoNodeExporterPorts() []int {
	for _, name := range []string{"CMDB_AUTO_SCAN_NODE_PORTS", "CMDB_AUTO_SCAN_NODE_PORT"} {
		if value := os.Getenv(name); value != "" {
			if ports := parseNodeExporterPorts(value); len(ports) > 0 {
				return ports
			}
		}
	}
	return []int{9100, 19100}
}

func parseNodeExporterPorts(value string) []int {
	seen := map[int]bool{}
	ports := make([]int, 0)
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		port, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || port < 1 || port > 65535 || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
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
		for _, item := range t.Items {
			if item.State == "pending" || item.State == "approving" {
				r.Pending++
			}
		}
	}
	return r
}
func (s *Service) CreateTask(in CreateTaskInput) (Task, error) {
	if in.Name == "" || in.Source == "" {
		return Task{}, errors.New("validation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := Task{ID: fmt.Sprintf("disc-%03d", len(s.tasks)+1), Name: in.Name, Source: in.Source, Scope: in.Scope, Status: "pending", CreatedAt: time.Now().Format("2006-01-02 15:04"), UpdatedAt: time.Now().Format("2006-01-02 15:04:05")}
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
		if err := s.markImported(item); err != nil {
			return "", err
		}
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
			if !item.PreserveType {
				if err := s.cmdb.ReclassifyHost(existingID, item.Type, src); err != nil && !errors.Is(err, cmdb.ErrNotFound) {
					return "", err
				}
			}
			if err := s.markImported(item); err != nil {
				return "", err
			}
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
			if !item.PreserveType {
				if err := s.cmdb.ReclassifyHost(item.ID, item.Type, src); err != nil && !errors.Is(err, cmdb.ErrNotFound) {
					return "", err
				}
			}
			if err := s.markImported(item); err != nil {
				return "", err
			}
			return "merged", nil
		}
		return "", err
	}
	if err := s.markImported(item); err != nil {
		return "", err
	}
	return "imported", nil
}

// Ingest accepts discovered items from collectors (ssh/snmp/node_exporter) and auto-imports them to CMDB.
func (s *Service) Ingest(in IngestInput) (IngestResult, error) {
	if strings.TrimSpace(in.Source) == "" || len(in.Items) == 0 {
		return IngestResult{}, errors.New("validation")
	}
	taskID := in.Source + "-" + time.Now().Format("20060102")
	result := IngestResult{TaskID: taskID}
	now := time.Now()
	created := false
	s.mu.Lock()
	index := -1
	for i := range s.tasks {
		if s.tasks[i].ID == taskID {
			index = i
			break
		}
	}
	if index < 0 {
		s.tasks = append(s.tasks, Task{ID: taskID, Name: in.Source + " 自动发现", Source: in.Source, Scope: in.Scope, Status: "running", CreatedAt: now.Format("2006-01-02 15:04"), UpdatedAt: now.Format("2006-01-02 15:04:05")})
		index = len(s.tasks) - 1
		created = true
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
		existingIndex := -1
		for i := range task.Items {
			if task.Items[i].ID == item.ID {
				existingIndex = i
				break
			}
		}
		item.State = "pending"
		item.Result = "pending"
		item.Message = "等待刷新兴库"
		item.UpdatedAt = now.Format("2006-01-02 15:04:05")
		if existingIndex >= 0 {
			task.Items[existingIndex] = item
		} else {
			task.Items = append(task.Items, item)
		}
		pending = append(pending, item)
		if err := s.persistItem(taskID, item, itemPayload(item)); err != nil {
			s.mu.Unlock()
			return IngestResult{}, err
		}
	}
	task.Discovered = len(task.Items)
	task.UpdatedAt = now.Format("2006-01-02 15:04:05")
	s.mu.Unlock()
	if created {
		s.recordTaskEvent(taskID, "info", "task.started", "发现任务已创建")
	}
	if in.RequireApproval {
		s.mu.Lock()
		if index < len(s.tasks) {
			s.tasks[index].Status = "pending-approval"
			s.tasks[index].UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
			_ = s.persistTask(s.tasks[index])
		}
		s.mu.Unlock()
		result.Accepted = len(pending)
		s.recordTaskEvent(taskID, "warning", "approval.required", fmt.Sprintf("发现 %d 个资源，等待人工审批", len(pending)))
		return result, nil
	}

	for _, item := range pending {
		status, err := s.importDiscovered(item, in.Source)
		if err != nil {
			result.Failed++
			_ = s.setMemoryItemOutcome(taskID, item.ID, "failed", "failed", err.Error())
			_ = s.updatePersistedItemOutcome(item.ID, "failed", "failed", err.Error())
			s.recordTaskEvent(taskID, "error", "item.failed", fmt.Sprintf("%s (%s): %v", item.Name, item.IP, err))
			continue
		}
		result.Accepted++
		switch status {
		case "imported":
			result.Imported++
		case "merged":
			result.Merged++
		case "conflict":
			result.Conflicts++
		}
		if err := s.updatePersistedItemOutcome(item.ID, "imported", status, ""); err != nil {
			result.Failed++
			s.recordTaskEvent(taskID, "error", "item.persist_failed", fmt.Sprintf("%s: %v", item.ID, err))
			continue
		}
		if err := s.setMemoryItemOutcome(taskID, item.ID, "imported", status, ""); err != nil && s.db == nil {
			return IngestResult{}, err
		}
		s.recordTaskEvent(taskID, "success", "item."+status, fmt.Sprintf("%s (%s) -> %s", item.Name, item.IP, status))
	}
	s.mu.Lock()
	if index < len(s.tasks) {
		s.tasks[index].Discovered = len(s.tasks[index].Items)
		s.tasks[index].Imported = 0
		for _, item := range s.tasks[index].Items {
			if item.State == "imported" {
				s.tasks[index].Imported++
			}
		}
		s.tasks[index].Status = "completed"
		s.tasks[index].UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
		if err := s.persistTask(s.tasks[index]); err != nil {
			s.mu.Unlock()
			return IngestResult{}, err
		}
	}
	s.mu.Unlock()
	s.recordTaskEvent(taskID, "info", "task.completed", fmt.Sprintf("发现 %d，入库 %d，合并 %d，冲突 %d，失败 %d", len(pending), result.Imported, result.Merged, result.Conflicts, result.Failed))
	return result, nil
}

func (s *Service) PendingList() ([]PendingItem, error) {
	if s.db != nil {
		return s.persistedPendingItems()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []PendingItem{}
	for _, task := range s.tasks {
		for _, item := range task.Items {
			if item.State == "pending" {
				out = append(out, PendingItem{TaskID: task.ID, ID: item.ID, Name: item.Name, IP: item.IP, Type: item.Type, Source: task.Source, State: item.State, Confidence: item.Confidence})
			}
		}
	}
	return out, nil
}

func (s *Service) pendingForDecision(itemID string) (DiscoveredItem, string, string, error) {
	if s.db != nil {
		return s.persistedPendingItem(itemID)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, task := range s.tasks {
		for _, item := range task.Items {
			if item.ID == itemID && item.State == "pending" {
				return item, task.Source, task.ID, nil
			}
		}
	}
	return DiscoveredItem{}, "", "", ErrNotFound
}

func (s *Service) ApprovePending(itemID string) (string, error) {
	item, source, taskID, err := s.pendingForDecision(itemID)
	if err != nil {
		return "", err
	}
	claimed := false
	if s.db != nil {
		if err := s.claimPersistedItem(itemID); err != nil {
			return "", err
		}
		claimed = true
	}
	status, err := s.importDiscovered(item, source)
	if err != nil {
		if claimed {
			_ = s.setPersistedItemState(itemID, "approving", "pending")
		}
		return "", err
	}
	if err := s.updatePersistedItemOutcome(itemID, "imported", status, ""); err != nil {
		return "", err
	}
	if err := s.setMemoryItemOutcome(taskID, itemID, "imported", status, ""); err != nil && s.db == nil {
		return "", err
	}
	s.recordTaskEvent(taskID, "success", "approval.approved", fmt.Sprintf("%s (%s) -> %s", item.Name, item.IP, status))
	return status, nil
}

func (s *Service) RejectPending(itemID string) error {
	_, _, taskID, err := s.pendingForDecision(itemID)
	if err != nil {
		return err
	}
	if s.db != nil {
		if err := s.claimPersistedItem(itemID); err != nil {
			return err
		}
		if err := s.setPersistedItemState(itemID, "approving", "rejected"); err != nil {
			_ = s.setPersistedItemState(itemID, "approving", "pending")
			return err
		}
	}
	if err := s.updatePersistedItemOutcome(itemID, "rejected", "rejected", "人工审批拒绝"); err != nil {
		return err
	}
	if err := s.setMemoryItemOutcome(taskID, itemID, "rejected", "rejected", "人工审批拒绝"); err != nil && s.db == nil {
		return err
	}
	s.recordTaskEvent(taskID, "warning", "approval.rejected", itemID+" 已被人工拒绝")
	return nil
}

func (s *Service) setMemoryItemOutcome(taskID, itemID, state, result, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Format("2006-01-02 15:04:05")
	for i := range s.tasks {
		if taskID != "" && s.tasks[i].ID != taskID {
			continue
		}
		for j := range s.tasks[i].Items {
			if s.tasks[i].Items[j].ID != itemID {
				continue
			}
			s.tasks[i].Items[j].State = state
			s.tasks[i].Items[j].Result = result
			s.tasks[i].Items[j].Message = message
			s.tasks[i].Items[j].UpdatedAt = now
			hasPending := false
			s.tasks[i].Imported = 0
			for _, item := range s.tasks[i].Items {
				if item.State == "imported" {
					s.tasks[i].Imported++
				}
				if item.State == "pending" || item.State == "approving" {
					hasPending = true
				}
			}
			if hasPending {
				s.tasks[i].Status = "pending-approval"
			} else {
				s.tasks[i].Status = "completed"
			}
			s.tasks[i].UpdatedAt = now
			if err := s.persistTask(s.tasks[i]); err != nil {
				return err
			}
			return nil
		}
	}
	return ErrNotFound
}

func (s *Service) recordTaskEvent(taskID, level, event, message string) {
	if taskID == "" {
		return
	}
	now := time.Now()
	s.mu.Lock()
	s.nextEvent++
	eventID := fmt.Sprintf("tev-%d", s.nextEvent)
	s.mu.Unlock()
	item := TaskEvent{ID: eventID, TaskID: taskID, Level: level, Event: event, Message: message, CreatedAt: now.Format("2006-01-02 15:04:05")}
	if s.db != nil {
		if _, err := s.db.Exec(`INSERT INTO discovery_task_events(id,task_id,level,event,message,created_at) VALUES($1,$2,$3,$4,$5,now())`, item.ID, item.TaskID, item.Level, item.Event, item.Message); err != nil {
			slog.Error("record discovery task event", "task", taskID, "error", err)
		}
	}
	s.mu.Lock()
	s.events[taskID] = append(s.events[taskID], item)
	if len(s.events[taskID]) > 500 {
		s.events[taskID] = s.events[taskID][len(s.events[taskID])-500:]
	}
	s.mu.Unlock()
}

func taskResultSummary(task Task) TaskResultSummary {
	summary := TaskResultSummary{Discovered: len(task.Items)}
	for _, item := range task.Items {
		switch item.State {
		case "imported":
			switch item.Result {
			case "merged":
				summary.Merged++
			default:
				summary.Imported++
			}
		case "pending", "approving":
			summary.Pending++
		case "rejected":
			summary.Rejected++
		case "conflict":
			summary.Conflicts++
		case "failed":
			summary.Failed++
		}
	}
	return summary
}

func (s *Service) TaskDetail(id string) (TaskDetail, error) {
	var task Task
	var events []TaskEvent
	if s.db != nil {
		var err error
		task, events, err = s.persistedTaskDetail(id)
		if err != nil {
			return TaskDetail{}, err
		}
	} else {
		s.mu.RLock()
		found := false
		for _, candidate := range s.tasks {
			if candidate.ID == id {
				task = candidate
				found = true
				break
			}
		}
		events = append([]TaskEvent(nil), s.events[id]...)
		s.mu.RUnlock()
		if !found {
			return TaskDetail{}, ErrNotFound
		}
	}
	return TaskDetail{Task: task, Summary: taskResultSummary(task), Events: events}, nil
}

func itemPayload(item DiscoveredItem) []byte {
	payload, err := json.Marshal(item)
	if err != nil {
		return []byte(`{}`)
	}
	return payload
}

func (s *Service) RunTask(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			if !demo.Enabled() {
				return Task{}, errors.New("task execution requires a configured discovery collector")
			}
			s.tasks[i].Status = "completed"
			s.tasks[i].Items = sampleItems()
			s.tasks[i].Discovered = len(s.tasks[i].Items)
			if err := s.persistTask(s.tasks[i]); err != nil {
				return Task{}, err
			}
			for _, item := range s.tasks[i].Items {
				if err := s.persistItem(id, item, itemPayload(item)); err != nil {
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
	return []DiscoveredItem{
		{ID: "found-01", Name: "new-app-node-01", IP: "10.20.8.31", Type: "physical-server", Confidence: 96, State: "pending", Result: "pending", Message: "等待入库"},
		{ID: "found-02", Name: "new-app-node-02", IP: "10.20.8.32", Type: "physical-server", Confidence: 94, State: "pending", Result: "pending", Message: "等待入库"},
		{ID: "found-03", Name: "prod-k8s-worker-03", IP: "10.22.1.23", Type: "k8s-node", Confidence: 99, State: "pending", Result: "pending", Message: "等待入库"},
	}
}
