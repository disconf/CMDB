package jobs

import (
	"cmdb/gateway-bff/internal/demo"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")
var ErrValidation = errors.New("validation")
var ErrInvalidState = errors.New("invalid state")
var ErrUnsafeCommand = errors.New("unsafe command")

type Template struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Category    string            `json:"category"`
	Description string            `json:"description"`
	Command     string            `json:"command,omitempty"`
	PlaybookID  string            `json:"playbookId,omitempty"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	Version     int               `json:"version"`
	Risk        string            `json:"risk"`
	LastRun     string            `json:"lastRun"`
	Enabled     bool              `json:"enabled"`
	UpdatedAt   string            `json:"updatedAt,omitempty"`
	UpdatedBy   string            `json:"updatedBy,omitempty"`
}
type TemplateInput struct {
	Name        string            `json:"name"`
	Category    string            `json:"category"`
	Description string            `json:"description"`
	Command     string            `json:"command"`
	PlaybookID  string            `json:"playbookId"`
	Parameters  map[string]string `json:"parameters"`
	Risk        string            `json:"risk"`
}
type Playbook struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Content     string            `json:"content"`
	Constants   map[string]string `json:"constants"`
	Variables   map[string]string `json:"variables"`
	Enabled     bool              `json:"enabled"`
	Version     int               `json:"version"`
	CreatedBy   string            `json:"createdBy"`
	UpdatedAt   string            `json:"updatedAt,omitempty"`
}
type PlaybookInput struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Content     string            `json:"content"`
	Constants   map[string]string `json:"constants"`
	Variables   map[string]string `json:"variables"`
	Enabled     *bool             `json:"enabled"`
}
type Log struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}
type TargetResult struct {
	Target     string `json:"target"`
	AssetID    string `json:"assetId,omitempty"`
	AssetName  string `json:"assetName,omitempty"`
	Status     string `json:"status"`
	RC         int    `json:"rc"`
	Changed    bool   `json:"changed"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"durationMs"`
}

type Job struct {
	ID              string            `json:"id"`
	TemplateID      string            `json:"templateId"`
	TemplateVersion int               `json:"templateVersion"`
	Name            string            `json:"name"`
	Targets         []string          `json:"targets"`
	Status          string            `json:"status"`
	Progress        int               `json:"progress"`
	Operator        string            `json:"operator"`
	StartedAt       string            `json:"startedAt"`
	Duration        string            `json:"duration"`
	Logs            []Log             `json:"logs"`
	TimeoutSeconds  int               `json:"timeoutSeconds"`
	Attempt         int               `json:"attempt"`
	ApprovedBy      string            `json:"approvedBy"`
	PlaybookID      string            `json:"playbookId"`
	PlaybookVersion int               `json:"playbookVersion"`
	CredentialID    string            `json:"credentialId"`
	Variables       map[string]string `json:"variables"`
	Parameters      map[string]string `json:"parameters,omitempty"`
	TargetResults   []TargetResult    `json:"targetResults,omitempty"`
	ArtifactKeys    []string          `json:"artifactKeys,omitempty"`
}
type Schedule struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	TemplateID     string            `json:"templateId,omitempty"`
	TemplateName   string            `json:"templateName,omitempty"`
	PlaybookID     string            `json:"playbookId,omitempty"`
	PlaybookName   string            `json:"playbookName,omitempty"`
	Variables      map[string]string `json:"variables,omitempty"`
	Parameters     map[string]string `json:"parameters,omitempty"`
	Targets        []string          `json:"targets"`
	Cron           string            `json:"cron"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	Enabled        bool              `json:"enabled"`
	NextRun        string            `json:"nextRun"`
	LastRun        string            `json:"lastRun"`
	LastStatus     string            `json:"lastStatus"`
	LastJobID      string            `json:"lastJobId"`
	CreatedBy      string            `json:"createdBy"`
	CredentialID   string            `json:"credentialId"`
}
type ScheduleInput struct {
	Name           string            `json:"name"`
	TemplateID     string            `json:"templateId"`
	PlaybookID     string            `json:"playbookId"`
	Variables      map[string]string `json:"variables"`
	Parameters     map[string]string `json:"parameters"`
	Targets        []string          `json:"targets"`
	Cron           string            `json:"cron"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	CredentialID   string            `json:"credentialId"`
	Enabled        *bool             `json:"enabled"`
}
type CreateInput struct {
	TemplateID     string            `json:"templateId"`
	PlaybookID     string            `json:"playbookId"`
	Variables      map[string]string `json:"variables"`
	Parameters     map[string]string `json:"parameters"`
	Targets        []string          `json:"targets"`
	Operator       string            `json:"operator"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	CredentialID   string            `json:"credentialId"`
}
type ApprovalInput struct {
	Decision string `json:"decision"`
	Comment  string `json:"comment"`
}
type Summary struct {
	Templates    int `json:"templates"`
	Running      int `json:"running"`
	SuccessToday int `json:"successToday"`
	FailedToday  int `json:"failedToday"`
	Schedules    int `json:"schedules"`
	Playbooks    int `json:"playbooks"`
}

type Service struct {
	mu        sync.RWMutex
	templates []Template
	playbooks []Playbook
	jobs      []Job
	schedules []Schedule
	next      int
	db        *sql.DB
	cancels   map[string]context.CancelFunc
	executor  Executor
	location  *time.Location
}

func NewService() *Service {
	defaultTemplates := []Template{
		{ID: "tpl-health", Name: "主机健康巡检", Category: "巡检", Description: "检查 CPU、内存、磁盘与关键进程", Command: "health-check --full", Version: 1, Risk: "low", LastRun: "--", Enabled: true},
		{ID: "tpl-node-exporter", Name: "安装 Node Exporter", Category: "监控", Description: "为 CMDB 主机安装并启动标准主机指标采集器", Command: "install-node-exporter", Version: 1, Risk: "medium", LastRun: "--", Enabled: true},
		{ID: "tpl-restart", Name: "应用滚动重启", Category: "变更", Description: "按实例顺序执行优雅重启", Command: "rolling-restart --wait", Version: 1, Risk: "medium", LastRun: "--", Enabled: true},
		{ID: "tpl-clean", Name: "日志空间清理", Category: "维护", Description: "清理超过保留周期的归档日志", Command: "log-cleanup --days 14", Version: 1, Risk: "low", LastRun: "--", Enabled: true},
		{ID: "tpl-patch", Name: "安全补丁安装", Category: "安全", Description: "安装已审批的系统安全更新", Command: "patch-install --approved", Version: 1, Risk: "high", LastRun: "--", Enabled: true},
	}
	s := &Service{playbooks: []Playbook{}, templates: defaultTemplates, schedules: []Schedule{}, next: 1, cancels: map[string]context.CancelFunc{}, executor: newExecutorFromEnv(), location: scheduleLocation()}
	if !demo.Enabled() {
		s.schedules = []Schedule{}
	}
	if url := os.Getenv("DATABASE_URL"); url != "" {
		db, jobs, err := openPostgres(url)
		if err != nil {
			panic(fmt.Sprintf("initialize jobs repository: %v", err))
		}
		s.db, s.jobs = db, jobs
		if persisted, loadErr := loadTemplates(context.Background(), db); loadErr == nil && len(persisted) > 0 {
			s.templates = persisted
		} else {
			for _, item := range s.templates {
				_ = upsertTemplate(context.Background(), db, item)
			}
		}
		persistedSchedules, scheduleErr := loadSchedules(context.Background(), db)
		if scheduleErr != nil {
			panic(fmt.Sprintf("load job schedules: %v", scheduleErr))
		}
		s.schedules = persistedSchedules
		persistedPlaybooks, playbookErr := loadPlaybooks(context.Background(), db)
		if playbookErr != nil {
			panic(fmt.Sprintf("load job playbooks: %v", playbookErr))
		}
		s.playbooks = persistedPlaybooks
		s.next = len(jobs) + 1
	}
	for _, defaultItem := range defaultTemplates {
		found := false
		for _, item := range s.templates {
			if item.ID == defaultItem.ID {
				found = true
				break
			}
		}
		if found {
			continue
		}
		s.templates = append(s.templates, defaultItem)
		if s.db != nil {
			_ = upsertTemplate(context.Background(), s.db, defaultItem)
		}
	}
	return s
}

func (s *Service) Templates() []Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Template(nil), s.templates...)
}
func (s *Service) CreateTemplate(in TemplateInput) (Template, error) {
	if err := validateTemplate(in); err != nil {
		return Template{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Format("2006-01-02 15:04:05")
	t := Template{ID: fmt.Sprintf("tpl-%d", time.Now().UnixNano()), Name: in.Name, Category: in.Category, Description: in.Description, Command: in.Command, PlaybookID: in.PlaybookID, Parameters: cloneStringMap(in.Parameters), Version: 1, Risk: in.Risk, LastRun: "--", Enabled: true, UpdatedAt: now}
	s.templates = append([]Template{t}, s.templates...)
	if s.db != nil {
		_ = upsertTemplate(context.Background(), s.db, t)
	}
	return t, nil
}
func (s *Service) UpdateTemplate(id string, in TemplateInput) (Template, error) {
	if err := validateTemplate(in); err != nil {
		return Template{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.templates {
		if s.templates[i].ID != id {
			continue
		}
		s.templates[i].Name = in.Name
		s.templates[i].Category = in.Category
		s.templates[i].Description = in.Description
		s.templates[i].Command = in.Command
		s.templates[i].PlaybookID = in.PlaybookID
		s.templates[i].Parameters = cloneStringMap(in.Parameters)
		s.templates[i].Risk = in.Risk
		s.templates[i].Version++
		s.templates[i].UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
		if s.db != nil {
			_ = upsertTemplate(context.Background(), s.db, s.templates[i])
		}
		return s.templates[i], nil
	}
	return Template{}, ErrNotFound
}

func (s *Service) DeleteTemplate(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.templates {
		if s.templates[i].ID != id {
			continue
		}
		s.templates = append(s.templates[:i], s.templates[i+1:]...)
		if s.db != nil {
			return deleteTemplateRow(context.Background(), s.db, id)
		}
		return nil
	}
	return ErrNotFound
}

func (s *Service) DuplicateTemplate(id string, operator string) (Template, error) {
	s.mu.RLock()
	var source Template
	found := false
	for _, item := range s.templates {
		if item.ID == id {
			source = item
			found = true
			break
		}
	}
	s.mu.RUnlock()
	if !found {
		return Template{}, ErrNotFound
	}
	source.ID = fmt.Sprintf("tpl-%d", time.Now().UnixNano())
	source.Name = source.Name + " - 副本"
	source.Version = 1
	source.Enabled = true
	source.LastRun = "--"
	source.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	source.UpdatedBy = operator
	s.mu.Lock()
	s.templates = append([]Template{source}, s.templates...)
	if s.db != nil {
		_ = upsertTemplate(context.Background(), s.db, source)
	}
	s.mu.Unlock()
	return source, nil
}
func (s *Service) ToggleTemplate(id string) (Template, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.templates {
		if s.templates[i].ID == id {
			s.templates[i].Enabled = !s.templates[i].Enabled
			if s.db != nil {
				_ = upsertTemplate(context.Background(), s.db, s.templates[i])
			}
			return s.templates[i], nil
		}
	}
	return Template{}, ErrNotFound
}
func validateTemplate(in TemplateInput) error {
	if strings.TrimSpace(in.Name) == "" || (strings.TrimSpace(in.Command) == "" && strings.TrimSpace(in.PlaybookID) == "") || (in.Risk != "low" && in.Risk != "medium" && in.Risk != "high") {
		return ErrValidation
	}
	if strings.TrimSpace(in.Command) == "" {
		return nil
	}
	cmd := strings.ToLower(in.Command)
	blocked := []string{"rm -rf /", "mkfs", "dd if=/dev/zero", "shutdown -h", "reboot", "format c:"}
	for _, item := range blocked {
		if strings.Contains(cmd, item) {
			return ErrUnsafeCommand
		}
	}
	return nil
}
func (s *Service) Schedules() []Schedule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSchedules(s.schedules)
}
func (s *Service) ExecutorInfo() ExecutorInfo { return s.executor.Info() }
func (s *Service) Jobs() []Job {
	if s.db != nil {
		if jobs, err := loadJobs(context.Background(), s.db); err == nil {
			s.mu.Lock()
			s.jobs = cloneJobs(jobs)
			s.mu.Unlock()
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneJobs(s.jobs)
}
func (s *Service) Get(id string) (Job, error) {
	if s.db != nil {
		j, err := loadJobByID(context.Background(), s.db, id)
		if err == nil {
			s.mu.Lock()
			s.upsertJobMemoryLocked(j)
			s.mu.Unlock()
			return cloneJob(j), nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Job{}, err
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, j := range s.jobs {
		if j.ID == id {
			return cloneJob(j), nil
		}
	}
	return Job{}, ErrNotFound
}
func (s *Service) upsertJobMemoryLocked(job Job) {
	for i := range s.jobs {
		if s.jobs[i].ID == job.ID {
			s.jobs[i] = job
			return
		}
	}
	s.jobs = append([]Job{job}, s.jobs...)
}
func (s *Service) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := Summary{Templates: len(s.templates), Schedules: len(s.schedules), Playbooks: len(s.playbooks)}
	for _, j := range s.jobs {
		switch j.Status {
		case "running":
			r.Running++
		case "success":
			r.SuccessToday++
		case "failed":
			r.FailedToday++
		}
	}
	return r
}

func (s *Service) Create(in CreateInput) (Job, error) {
	in.CredentialID = strings.TrimSpace(in.CredentialID)
	if in.PlaybookID != "" {
		return s.createPlaybookExecution(in)
	}
	var tpl *Template
	for i := range s.templates {
		if s.templates[i].ID == in.TemplateID {
			tpl = &s.templates[i]
		}
	}
	if tpl == nil || !tpl.Enabled || len(in.Targets) == 0 {
		return Job{}, ErrValidation
	}
	if in.TimeoutSeconds <= 0 {
		in.TimeoutSeconds = 300
	}
	if in.TimeoutSeconds > 86400 {
		return Job{}, ErrValidation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	status, message := "pending", "任务已创建，等待执行"
	if tpl.Risk == "high" {
		status = "awaiting_approval"
		message = "高风险任务已创建，等待其他管理员审批"
	}
	j := Job{ID: fmt.Sprintf("job-%d-%03d", now.Unix(), s.next), TemplateID: tpl.ID, TemplateVersion: tpl.Version, PlaybookID: tpl.PlaybookID, Parameters: mergeStringMaps(tpl.Parameters, in.Parameters), Name: tpl.Name, Targets: append([]string(nil), in.Targets...), Status: status, Operator: in.Operator, StartedAt: now.Format("2006-01-02 15:04:05"), Logs: []Log{{now.Format("15:04:05"), "info", message}}, TimeoutSeconds: in.TimeoutSeconds, CredentialID: in.CredentialID, Attempt: 0}
	s.next++
	s.jobs = append([]Job{j}, s.jobs...)
	s.persistLocked(j)
	return cloneJob(j), nil
}

func (s *Service) createPlaybookExecution(in CreateInput) (Job, error) {
	playbook, err := s.playbookByID(in.PlaybookID)
	if err != nil || !playbook.Enabled || len(in.Targets) == 0 {
		return Job{}, ErrValidation
	}
	if _, err = compilePlaybook(playbook, in.Variables); err != nil {
		return Job{}, ErrValidation
	}
	if in.TimeoutSeconds <= 0 {
		in.TimeoutSeconds = 300
	}
	if in.TimeoutSeconds > 86400 {
		return Job{}, ErrValidation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	j := Job{ID: fmt.Sprintf("job-%d-%03d", now.Unix(), s.next), TemplateID: "playbook:" + playbook.ID, PlaybookID: playbook.ID, PlaybookVersion: playbook.Version, Parameters: mergeStringMaps(playbook.Variables, in.Variables), Name: playbook.Name, Variables: cloneStringMap(in.Variables), Targets: append([]string(nil), in.Targets...), Status: "awaiting_approval", Operator: in.Operator, StartedAt: now.Format("2006-01-02 15:04:05"), Logs: []Log{{now.Format("15:04:05"), "info", "Playbook 已创建，等待其他管理员审批"}}, TimeoutSeconds: in.TimeoutSeconds, CredentialID: in.CredentialID, Attempt: 0}
	s.next++
	s.jobs = append([]Job{j}, s.jobs...)
	s.persistLocked(j)
	return cloneJob(j), nil
}
func (s *Service) Run(id string) (Job, error)   { return s.startTargets(id, false, nil) }
func (s *Service) Retry(id string) (Job, error) { return s.startTargets(id, true, nil) }
func (s *Service) RetryTargets(id string, targets []string) (Job, error) {
	return s.startTargets(id, true, targets)
}
func (s *Service) Approve(id, approver string) (Job, error) {
	return s.Review(id, approver, ApprovalInput{Decision: "approve"})
}
func (s *Service) Review(id, reviewer string, input ApprovalInput) (Job, error) {
	decision := strings.ToLower(strings.TrimSpace(input.Decision))
	comment := strings.TrimSpace(input.Comment)
	if decision == "" {
		decision = "approve"
	}
	if reviewer == "" || (decision != "approve" && decision != "reject" && decision != "return") {
		return Job{}, ErrValidation
	}
	if len(comment) > 1000 {
		return Job{}, ErrValidation
	}
	if _, err := s.Get(id); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		j := &s.jobs[i]
		if j.ID != id {
			continue
		}
		if j.Status != "awaiting_approval" || reviewer == j.Operator {
			return Job{}, ErrInvalidState
		}
		now := time.Now().Format("15:04:05")
		switch decision {
		case "approve":
			j.Status = "pending"
			j.ApprovedBy = reviewer
			message := "高风险任务已由 " + reviewer + " 审批通过"
			if comment != "" {
				message += "，审批意见：" + comment
			}
			j.Logs = append(j.Logs, Log{Time: now, Level: "success", Message: message})
		case "reject":
			j.Status = "rejected"
			j.ApprovedBy = reviewer
			message := "高风险任务已由 " + reviewer + " 拒绝"
			if comment != "" {
				message += "，审批意见：" + comment
			}
			j.Logs = append(j.Logs, Log{Time: now, Level: "warning", Message: message})
		case "return":
			j.Status = "returned"
			j.ApprovedBy = reviewer
			message := "高风险任务已由 " + reviewer + " 退回修改"
			if comment != "" {
				message += "，审批意见：" + comment
			}
			j.Logs = append(j.Logs, Log{Time: now, Level: "info", Message: message})
		}
		s.persistLocked(*j)
		return cloneJob(*j), nil
	}
	return Job{}, ErrNotFound
}
func (s *Service) startTargets(id string, retry bool, requestedTargets []string) (Job, error) {
	if _, err := s.Get(id); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	idx := -1
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return Job{}, ErrNotFound
	}
	j := &s.jobs[idx]
	if (!retry && j.Status != "pending") || (retry && j.Status != "failed" && j.Status != "cancelled") {
		s.mu.Unlock()
		return Job{}, ErrInvalidState
	}
	targets := append([]string(nil), j.Targets...)
	if retry && len(requestedTargets) > 0 {
		allowed := map[string]string{}
		for _, target := range j.Targets {
			allowed[target] = target
		}
		targets = targets[:0]
		seen := map[string]bool{}
		for _, target := range requestedTargets {
			value, ok := allowed[strings.TrimSpace(target)]
			if !ok {
				s.mu.Unlock()
				return Job{}, ErrValidation
			}
			if !seen[value] {
				seen[value] = true
				targets = append(targets, value)
			}
		}
		if len(targets) == 0 {
			s.mu.Unlock()
			return Job{}, ErrValidation
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(j.TimeoutSeconds)*time.Second)
	s.cancels[id] = cancel
	j.Status = "running"
	j.Progress = 0
	j.Attempt++
	j.Logs = append(j.Logs, Log{time.Now().Format("15:04:05"), "info", fmt.Sprintf("第 %d 次执行已进入队列", j.Attempt)})
	s.persistLocked(*j)
	attempt := j.Attempt
	request := s.executionRequestForJobLocked(*j)
	request.JobID = j.ID
	request.Targets = targets
	request.Operator = j.Operator
	request.Attempt = attempt
	request.TimeoutSeconds = j.TimeoutSeconds
	out := cloneJob(*j)
	s.mu.Unlock()
	go s.execute(ctx, id, attempt, request)
	return out, nil
}
func (s *Service) Cancel(id string) (Job, error) {
	if _, err := s.Get(id); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			if s.jobs[i].Status != "running" {
				return Job{}, ErrInvalidState
			}
			if cancel := s.cancels[id]; cancel != nil {
				cancel()
			}
			s.jobs[i].Status = "cancelled"
			s.jobs[i].Logs = append(s.jobs[i].Logs, Log{time.Now().Format("15:04:05"), "warning", "操作人已请求取消任务"})
			s.persistLocked(s.jobs[i])
			return cloneJob(s.jobs[i]), nil
		}
	}
	return Job{}, ErrNotFound
}

func (s *Service) execute(ctx context.Context, id string, attempt int, request ExecutionRequest) {
	start := time.Now()
	result := s.executor.Execute(ctx, request, func(event ExecutionEvent) { s.updateWithLevel(id, attempt, event.Progress, event.Level, event.Message) })
	progress := 100
	if result.Status != "success" {
		job, _ := s.Get(id)
		progress = job.Progress
	}
	s.finish(id, attempt, result.Status, progress, start, result.Message, result.Targets, result.ArtifactKeys)
}
func (s *Service) update(id string, attempt, progress int, message string) {
	s.updateWithLevel(id, attempt, progress, "success", message)
}
func (s *Service) updateWithLevel(id string, attempt, progress int, level, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id && s.jobs[i].Status == "running" && s.jobs[i].Attempt == attempt {
			s.jobs[i].Progress = progress
			if progress < 0 {
				progress = 0
			}
			if progress > 100 {
				progress = 100
			}
			if level == "" {
				level = "info"
			}
			s.jobs[i].Logs = append(s.jobs[i].Logs, Log{time.Now().Format("15:04:05"), level, message})
			s.persistLocked(s.jobs[i])
			return
		}
	}
}
func (s *Service) finish(id string, attempt int, status string, progress int, start time.Time, message string, targetResults []TargetResult, artifactKeys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id && s.jobs[i].Status == "running" && s.jobs[i].Attempt == attempt {
			s.jobs[i].Status = status
			s.jobs[i].Progress = progress
			s.jobs[i].Duration = time.Since(start).Round(time.Millisecond).String()
			level := "success"
			if status != "success" {
				level = "warning"
			}
			s.jobs[i].Logs = append(s.jobs[i].Logs, Log{time.Now().Format("15:04:05"), level, message})
			s.jobs[i].TargetResults = mergeTargetResults(s.jobs[i].TargetResults, targetResults)
			s.jobs[i].ArtifactKeys = mergeArtifactKeys(s.jobs[i].ArtifactKeys, artifactKeys)
			delete(s.cancels, id)
			s.persistLocked(s.jobs[i])
			return
		}
	}
}

func mergeTargetResults(existing, incoming []TargetResult) []TargetResult {
	if len(incoming) == 0 {
		return append([]TargetResult(nil), existing...)
	}
	merged := append([]TargetResult(nil), existing...)
	positions := map[string]int{}
	for i, item := range merged {
		positions[item.Target] = i
	}
	for _, item := range incoming {
		if index, ok := positions[item.Target]; ok {
			merged[index] = item
			continue
		}
		positions[item.Target] = len(merged)
		merged = append(merged, item)
	}
	return merged
}
func mergeArtifactKeys(existing, incoming []string) []string {
	result := append([]string(nil), existing...)
	seen := map[string]bool{}
	for _, item := range result {
		seen[item] = true
	}
	for _, item := range incoming {
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func (s *Service) persistLocked(j Job) {
	if s.db != nil {
		_ = upsertJob(context.Background(), s.db, j)
	}
}
func cloneJob(j Job) Job {
	j.Targets = append([]string(nil), j.Targets...)
	j.Logs = append([]Log(nil), j.Logs...)
	j.Variables = cloneStringMap(j.Variables)
	j.Parameters = cloneStringMap(j.Parameters)
	j.TargetResults = append([]TargetResult(nil), j.TargetResults...)
	j.ArtifactKeys = append([]string(nil), j.ArtifactKeys...)
	return j
}
func cloneJobs(items []Job) []Job {
	out := make([]Job, len(items))
	for i := range items {
		out[i] = cloneJob(items[i])
	}
	return out
}
