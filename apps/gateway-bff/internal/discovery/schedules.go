package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrScheduleRunning = errors.New("schedule already running")

type CollectionSchedule struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	CIDRs           []string `json:"cidrs"`
	Port            int      `json:"port,omitempty"`
	Ports           []int    `json:"ports,omitempty"`
	DefaultType     string   `json:"defaultType,omitempty"`
	CredentialID    string   `json:"credentialId,omitempty"`
	RequireApproval bool     `json:"requireApproval"`
	IntervalMinutes int      `json:"intervalMinutes"`
	Enabled         bool     `json:"enabled"`
	Running         bool     `json:"running"`
	NextRunAt       string   `json:"nextRunAt,omitempty"`
	LastRunAt       string   `json:"lastRunAt,omitempty"`
	LastTaskID      string   `json:"lastTaskId,omitempty"`
	LastStatus      string   `json:"lastStatus,omitempty"`
	LastError       string   `json:"lastError,omitempty"`
	CreatedBy       string   `json:"createdBy,omitempty"`
	CreatedAt       string   `json:"createdAt,omitempty"`
	UpdatedAt       string   `json:"updatedAt,omitempty"`
}

type CollectionScheduleInput struct {
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	CIDRs           []string `json:"cidrs"`
	Port            int      `json:"port"`
	Ports           []int    `json:"ports"`
	DefaultType     string   `json:"defaultType"`
	CredentialID    string   `json:"credentialId"`
	RequireApproval bool     `json:"requireApproval"`
	IntervalMinutes int      `json:"intervalMinutes"`
	Enabled         *bool    `json:"enabled"`
}

type CollectionScheduleRunResult struct {
	ScheduleID string `json:"scheduleId"`
	TaskID     string `json:"taskId,omitempty"`
	Status     string `json:"status"`
	Scanned    int    `json:"scanned"`
	Found      int    `json:"found"`
	Adopted    int    `json:"adopted"`
	Merged     int    `json:"merged"`
	Conflicts  int    `json:"conflicts"`
	Message    string `json:"message,omitempty"`
}

type collectionScheduleRunner func(context.Context, CollectionSchedule, string) (CollectionScheduleRunResult, error)

func newCollectionScheduleID() string {
	return fmt.Sprintf("disc-sch-%d", time.Now().UnixNano())
}

func normalizeCollectionScheduleInput(in CollectionScheduleInput) (CollectionScheduleInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	in.DefaultType = strings.TrimSpace(in.DefaultType)
	in.CredentialID = strings.TrimSpace(in.CredentialID)
	if in.Kind != "node-exporter" && in.Kind != "ssh" && in.Kind != "snmp" {
		return in, errors.New("invalid schedule kind")
	}
	if in.Name == "" {
		in.Name = map[string]string{"node-exporter": "node_exporter 定时扫描", "ssh": "SSH 定时采集", "snmp": "SNMP 定时采集"}[in.Kind]
	}
	if len([]rune(in.Name)) > 80 {
		return in, errors.New("schedule name too long")
	}
	if in.IntervalMinutes == 0 {
		in.IntervalMinutes = 60
	}
	if in.IntervalMinutes < 1 || in.IntervalMinutes > 1440 {
		return in, errors.New("interval must be between 1 and 1440 minutes")
	}
	seenCIDRs := map[string]bool{}
	cidrs := make([]string, 0, len(in.CIDRs))
	for _, raw := range in.CIDRs {
		value := strings.TrimSpace(raw)
		if value == "" || seenCIDRs[value] {
			continue
		}
		if _, _, err := net.ParseCIDR(value); err != nil {
			return in, fmt.Errorf("invalid CIDR %q", value)
		}
		seenCIDRs[value] = true
		cidrs = append(cidrs, value)
	}
	if len(cidrs) == 0 {
		return in, errors.New("at least one CIDR is required")
	}
	if len(cidrs) > 64 {
		return in, errors.New("too many CIDRs")
	}
	in.CIDRs = cidrs
	if in.Enabled == nil {
		enabled := true
		in.Enabled = &enabled
	}
	switch in.Kind {
	case "node-exporter":
		ports := normalizeSchedulePorts(in.Ports)
		if len(ports) == 0 && in.Port > 0 && in.Port <= 65535 {
			ports = []int{in.Port}
		}
		if len(ports) == 0 {
			ports = []int{9100, 19100}
		}
		if len(ports) > 8 {
			return in, errors.New("too many exporter ports")
		}
		in.Ports = ports
		in.Port = ports[0]
	case "ssh":
		if in.Port == 0 {
			in.Port = 22
		}
		if in.Port < 1 || in.Port > 65535 {
			return in, errors.New("invalid SSH port")
		}
		if in.DefaultType != "" && in.DefaultType != "physical-server" && in.DefaultType != "virtual-machine" {
			return in, errors.New("invalid SSH asset type")
		}
	case "snmp":
		if in.Port == 0 {
			in.Port = 161
		}
		if in.Port < 1 || in.Port > 65535 {
			return in, errors.New("invalid SNMP port")
		}
		if in.DefaultType != "" && in.DefaultType != "network-device" && in.DefaultType != "physical-server" {
			return in, errors.New("invalid SNMP asset type")
		}
	}
	in.Ports = normalizeSchedulePorts(in.Ports)
	return in, nil
}

func normalizeSchedulePorts(values []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(values))
	for _, port := range values {
		if port < 1 || port > 65535 || seen[port] {
			continue
		}
		seen[port] = true
		out = append(out, port)
	}
	return out
}

func cloneCollectionSchedule(in CollectionSchedule) CollectionSchedule {
	in.CIDRs = append([]string(nil), in.CIDRs...)
	in.Ports = append([]int(nil), in.Ports...)
	return in
}

func nextScheduleRun(now time.Time, intervalMinutes int, enabled bool) string {
	if !enabled {
		return ""
	}
	return now.Add(time.Duration(intervalMinutes) * time.Minute).UTC().Format(time.RFC3339)
}

func (s *Service) CollectionSchedules() ([]CollectionSchedule, error) {
	if s.db != nil {
		return s.loadCollectionSchedules()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CollectionSchedule, 0, len(s.schedules))
	for _, item := range s.schedules {
		out = append(out, cloneCollectionSchedule(item))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (s *Service) collectionScheduleByID(id string) (CollectionSchedule, error) {
	if s.db != nil {
		return s.loadCollectionSchedule(id)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.schedules {
		if item.ID == id {
			return cloneCollectionSchedule(item), nil
		}
	}
	return CollectionSchedule{}, ErrNotFound
}

func (s *Service) saveCollectionSchedule(item CollectionSchedule) error {
	if s.db != nil {
		return s.upsertCollectionSchedule(item)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.schedules {
		if s.schedules[i].ID == item.ID {
			s.schedules[i] = cloneCollectionSchedule(item)
			return nil
		}
	}
	s.schedules = append([]CollectionSchedule{cloneCollectionSchedule(item)}, s.schedules...)
	return nil
}

func (s *Service) CreateCollectionSchedule(in CollectionScheduleInput, createdBy string) (CollectionSchedule, error) {
	normalized, err := normalizeCollectionScheduleInput(in)
	if err != nil {
		return CollectionSchedule{}, err
	}
	now := time.Now().UTC()
	item := CollectionSchedule{
		ID:              newCollectionScheduleID(),
		Name:            normalized.Name,
		Kind:            normalized.Kind,
		CIDRs:           normalized.CIDRs,
		Port:            normalized.Port,
		Ports:           normalized.Ports,
		DefaultType:     normalized.DefaultType,
		CredentialID:    normalized.CredentialID,
		RequireApproval: normalized.RequireApproval,
		IntervalMinutes: normalized.IntervalMinutes,
		Enabled:         *normalized.Enabled,
		CreatedBy:       strings.TrimSpace(createdBy),
		CreatedAt:       now.Format(time.RFC3339),
		UpdatedAt:       now.Format(time.RFC3339),
	}
	item.NextRunAt = nextScheduleRun(now, item.IntervalMinutes, item.Enabled)
	if err := s.saveCollectionSchedule(item); err != nil {
		return CollectionSchedule{}, err
	}
	return cloneCollectionSchedule(item), nil
}

func (s *Service) UpdateCollectionSchedule(id string, in CollectionScheduleInput) (CollectionSchedule, error) {
	current, err := s.collectionScheduleByID(id)
	if err != nil {
		return CollectionSchedule{}, err
	}
	normalized, err := normalizeCollectionScheduleInput(in)
	if err != nil {
		return CollectionSchedule{}, err
	}
	now := time.Now().UTC()
	current.Name = normalized.Name
	current.Kind = normalized.Kind
	current.CIDRs = normalized.CIDRs
	current.Port = normalized.Port
	current.Ports = normalized.Ports
	current.DefaultType = normalized.DefaultType
	current.CredentialID = normalized.CredentialID
	current.RequireApproval = normalized.RequireApproval
	current.IntervalMinutes = normalized.IntervalMinutes
	current.Enabled = *normalized.Enabled
	current.NextRunAt = nextScheduleRun(now, current.IntervalMinutes, current.Enabled)
	current.UpdatedAt = now.Format(time.RFC3339)
	if err := s.saveCollectionSchedule(current); err != nil {
		return CollectionSchedule{}, err
	}
	return cloneCollectionSchedule(current), nil
}

func (s *Service) ToggleCollectionSchedule(id string) (CollectionSchedule, error) {
	current, err := s.collectionScheduleByID(id)
	if err != nil {
		return CollectionSchedule{}, err
	}
	now := time.Now().UTC()
	current.Enabled = !current.Enabled
	current.NextRunAt = nextScheduleRun(now, current.IntervalMinutes, current.Enabled)
	current.UpdatedAt = now.Format(time.RFC3339)
	if err := s.saveCollectionSchedule(current); err != nil {
		return CollectionSchedule{}, err
	}
	return cloneCollectionSchedule(current), nil
}

func (s *Service) DeleteCollectionSchedule(id string) error {
	if s.db != nil {
		return s.deleteCollectionScheduleRow(id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.schedules {
		if s.schedules[i].ID == id {
			s.schedules = append(s.schedules[:i], s.schedules[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (s *Service) RunCollectionSchedule(ctx context.Context, id, operator string) (CollectionScheduleRunResult, error) {
	var schedule CollectionSchedule
	if strings.TrimSpace(operator) == "" {
		operator = "manual"
	}
	if s.db != nil {
		claimed, possible, err := s.claimCollectionScheduleRow(ctx, id)
		if err != nil {
			return CollectionScheduleRunResult{}, err
		}
		if !possible {
			if _, err := s.loadCollectionSchedule(id); errors.Is(err, ErrNotFound) {
				return CollectionScheduleRunResult{}, ErrNotFound
			}
			return CollectionScheduleRunResult{}, ErrScheduleRunning
		}
		schedule = claimed
	} else {
		s.mu.Lock()
		found := -1
		for i := range s.schedules {
			if s.schedules[i].ID == id {
				found = i
				break
			}
		}
		if found < 0 {
			s.mu.Unlock()
			return CollectionScheduleRunResult{}, ErrNotFound
		}
		if s.schedules[found].Running {
			s.mu.Unlock()
			return CollectionScheduleRunResult{}, ErrScheduleRunning
		}
		s.schedules[found].Running = true
		schedule = cloneCollectionSchedule(s.schedules[found])
		s.mu.Unlock()
	}
	return s.executeClaimedCollectionSchedule(ctx, schedule, operator)
}

func (s *Service) executeClaimedCollectionSchedule(ctx context.Context, schedule CollectionSchedule, operator string) (CollectionScheduleRunResult, error) {
	started := time.Now().UTC()
	taskID := fmt.Sprintf("%s-run-%d", schedule.ID, started.UnixNano())
	taskName := schedule.Name + " · 采集结果"
	if err := s.startCollectionTask(taskID, taskName, schedule.Kind, strings.Join(schedule.CIDRs, ",")); err != nil {
		return CollectionScheduleRunResult{}, err
	}
	runner := s.scheduleRunner
	if runner == nil {
		runner = s.executeCollectionSchedule
	}
	result, runErr := runner(ctx, schedule, taskID)
	result.ScheduleID = schedule.ID
	result.TaskID = taskID
	if runErr != nil {
		result.Status = "failed"
		result.Message = runErr.Error()
		_ = s.finishCollectionTask(taskID, "failed", runErr.Error())
	} else {
		taskStatus := "completed"
		if detail, err := s.TaskDetail(taskID); err == nil && detail.Task.Status != "" {
			taskStatus = detail.Task.Status
		}
		if taskStatus == "running" {
			_ = s.finishCollectionTask(taskID, "completed", fmt.Sprintf("扫描 %d，发现 %d，入库 %d，合并 %d", result.Scanned, result.Found, result.Adopted, result.Merged))
			taskStatus = "completed"
		}
		result.Status = taskStatus
	}
	finished := time.Now().UTC()
	schedule.Running = false
	schedule.LastRunAt = finished.Format(time.RFC3339)
	schedule.LastTaskID = taskID
	schedule.LastStatus = result.Status
	schedule.LastError = ""
	if runErr != nil {
		schedule.LastError = runErr.Error()
	}
	schedule.NextRunAt = nextScheduleRun(finished, schedule.IntervalMinutes, schedule.Enabled)
	schedule.UpdatedAt = finished.Format(time.RFC3339)
	if err := s.saveCollectionSchedule(schedule); err != nil && runErr == nil {
		runErr = err
	}
	return result, runErr
}

func (s *Service) executeCollectionSchedule(ctx context.Context, schedule CollectionSchedule, taskID string) (CollectionScheduleRunResult, error) {
	result := CollectionScheduleRunResult{ScheduleID: schedule.ID, TaskID: taskID}
	switch schedule.Kind {
	case "node-exporter":
		res, err := s.ScanNodeExporter(ctx, NodeExporterScanInput{CIDRs: schedule.CIDRs, Port: schedule.Port, Ports: schedule.Ports, DefaultType: schedule.DefaultType, RequireApproval: schedule.RequireApproval, TaskID: taskID, TaskName: schedule.Name})
		if err != nil {
			return result, err
		}
		result.TaskID = res.TaskID
		result.Scanned, result.Found, result.Adopted, result.Merged, result.Conflicts = res.Scanned, res.Found, res.Adopted, res.Merged, res.Conflicts
	case "ssh":
		res, err := s.ScanSSH(ctx, SSHScanInput{CIDRs: schedule.CIDRs, Port: schedule.Port, DefaultType: schedule.DefaultType, CredentialID: schedule.CredentialID, RequireApproval: schedule.RequireApproval, TaskID: taskID, TaskName: schedule.Name})
		if err != nil {
			return result, err
		}
		result.TaskID = res.TaskID
		result.Scanned, result.Found, result.Adopted, result.Merged, result.Conflicts = res.Scanned, res.Found, res.Adopted, res.Merged, res.Conflicts
	case "snmp":
		res, err := s.ScanSNMP(ctx, SNMPScanInput{CIDRs: schedule.CIDRs, Port: schedule.Port, DefaultType: schedule.DefaultType, CredentialID: schedule.CredentialID, RequireApproval: schedule.RequireApproval, TaskID: taskID, TaskName: schedule.Name})
		if err != nil {
			return result, err
		}
		result.TaskID = res.TaskID
		result.Scanned, result.Found, result.Adopted, result.Merged, result.Conflicts = res.Scanned, res.Found, res.Adopted, res.Merged, res.Conflicts
	default:
		return result, errors.New("invalid schedule kind")
	}
	return result, nil
}

func (s *Service) startCollectionTask(taskID, name, source, scope string) error {
	s.mu.Lock()
	for _, task := range s.tasks {
		if task.ID == taskID {
			s.mu.Unlock()
			return nil
		}
	}
	task := Task{ID: taskID, Name: name, Source: source, Scope: scope, Status: "running", CreatedAt: time.Now().Format("2006-01-02 15:04"), UpdatedAt: time.Now().Format("2006-01-02 15:04:05")}
	s.tasks = append([]Task{task}, s.tasks...)
	if err := s.persistTask(task); err != nil {
		s.tasks = s.tasks[1:]
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	s.recordTaskEvent(taskID, "info", "task.started", "定时采集计划已启动")
	return nil
}

func (s *Service) finishCollectionTask(taskID, status, message string) error {
	s.mu.Lock()
	found := false
	for i := range s.tasks {
		if s.tasks[i].ID == taskID {
			if s.tasks[i].Status != "pending-approval" {
				s.tasks[i].Status = status
			}
			s.tasks[i].UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
			if err := s.persistTask(s.tasks[i]); err != nil {
				s.mu.Unlock()
				return err
			}
			found = true
			break
		}
	}
	s.mu.Unlock()
	if found {
		level := "success"
		event := "task.completed"
		if status == "failed" {
			level = "error"
			event = "task.failed"
		}
		s.recordTaskEvent(taskID, level, event, message)
	}
	return nil
}

func (s *Service) RunCollectionSchedules(ctx context.Context) {
	if s.db == nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		s.runDueCollectionSchedules(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) runDueCollectionSchedules(ctx context.Context) {
	concurrency := 2
	if value := strings.TrimSpace(os.Getenv("CMDB_DISCOVERY_SCHEDULE_CONCURRENCY")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 1 && parsed <= 16 {
			concurrency = parsed
		}
	}
	sem := make(chan struct{}, concurrency)
	for i := 0; i < concurrency; i++ {
		schedule, found, err := s.claimDueCollectionSchedule(ctx)
		if err != nil || !found {
			return
		}
		sem <- struct{}{}
		go func(item CollectionSchedule) {
			defer func() { <-sem }()
			if _, err := s.executeClaimedCollectionSchedule(ctx, item, "scheduler"); err != nil {
				// executeClaimedCollectionSchedule persists the failure on the schedule row.
			}
		}(schedule)
	}
}
