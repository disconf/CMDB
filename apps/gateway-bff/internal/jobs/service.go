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
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Command     string `json:"command"`
	Risk        string `json:"risk"`
	LastRun     string `json:"lastRun"`
	Enabled     bool   `json:"enabled"`
}
type TemplateInput struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Command     string `json:"command"`
	Risk        string `json:"risk"`
}
type Playbook struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Content     string            `json:"content"`
	Constants   map[string]string `json:"constants"`
	Variables   map[string]string `json:"variables"`
	Enabled     bool              `json:"enabled"`
	CreatedBy   string            `json:"createdBy"`
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
type Job struct {
	ID             string            `json:"id"`
	TemplateID     string            `json:"templateId"`
	Name           string            `json:"name"`
	Targets        []string          `json:"targets"`
	Status         string            `json:"status"`
	Progress       int               `json:"progress"`
	Operator       string            `json:"operator"`
	StartedAt      string            `json:"startedAt"`
	Duration       string            `json:"duration"`
	Logs           []Log             `json:"logs"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	Attempt        int               `json:"attempt"`
	ApprovedBy     string            `json:"approvedBy"`
	PlaybookID     string            `json:"playbookId"`
	Variables      map[string]string `json:"variables"`
}
type Schedule struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	TemplateID     string   `json:"templateId"`
	TemplateName   string   `json:"templateName"`
	Targets        []string `json:"targets"`
	Cron           string   `json:"cron"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	Enabled        bool     `json:"enabled"`
	NextRun        string   `json:"nextRun"`
	LastRun        string   `json:"lastRun"`
	LastStatus     string   `json:"lastStatus"`
	LastJobID      string   `json:"lastJobId"`
	CreatedBy      string   `json:"createdBy"`
}
type ScheduleInput struct {
	Name           string   `json:"name"`
	TemplateID     string   `json:"templateId"`
	Targets        []string `json:"targets"`
	Cron           string   `json:"cron"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	Enabled        *bool    `json:"enabled"`
}
type CreateInput struct {
	TemplateID     string            `json:"templateId"`
	PlaybookID     string            `json:"playbookId"`
	Variables      map[string]string `json:"variables"`
	Targets        []string          `json:"targets"`
	Operator       string            `json:"operator"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
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
	nodeExporterTemplate := Template{"tpl-node-exporter", "安装 Node Exporter", "监控", "为CMDB主机安装并启动标准主机指标采集器", "install-node-exporter", "medium", "--", true}
	s := &Service{playbooks: []Playbook{}, templates: []Template{{"tpl-health", "主机健康巡检", "巡检", "检查 CPU、内存、磁盘与关键进程", "health-check --full", "low", "--", true}, nodeExporterTemplate, {"tpl-restart", "应用滚动重启", "变更", "按实例顺序执行优雅重启", "rolling-restart --wait", "medium", "--", true}, {"tpl-clean", "日志空间清理", "维护", "清理超过保留周期的归档日志", "log-cleanup --days 14", "low", "--", true}, {"tpl-patch", "安全补丁安装", "安全", "安装已审批的系统安全更新", "patch-install --approved", "high", "--", true}}, schedules: []Schedule{}, next: 1, cancels: map[string]context.CancelFunc{}, executor: newExecutorFromEnv(), location: scheduleLocation()}
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
	foundNodeExporter := false
	for _, item := range s.templates {
		foundNodeExporter = foundNodeExporter || item.ID == nodeExporterTemplate.ID
	}
	if !foundNodeExporter {
		s.templates = append(s.templates, nodeExporterTemplate)
		if s.db != nil {
			_ = upsertTemplate(context.Background(), s.db, nodeExporterTemplate)
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
	t := Template{ID: fmt.Sprintf("tpl-%d", time.Now().UnixNano()), Name: in.Name, Category: in.Category, Description: in.Description, Command: in.Command, Risk: in.Risk, LastRun: "--", Enabled: true}
	s.templates = append([]Template{t}, s.templates...)
	if s.db != nil {
		_ = upsertTemplate(context.Background(), s.db, t)
	}
	return t, nil
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
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Command) == "" || (in.Risk != "low" && in.Risk != "medium" && in.Risk != "high") {
		return ErrValidation
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
func (s *Service) Jobs() []Job                { s.mu.RLock(); defer s.mu.RUnlock(); return cloneJobs(s.jobs) }
func (s *Service) Get(id string) (Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, j := range s.jobs {
		if j.ID == id {
			return cloneJob(j), nil
		}
	}
	return Job{}, ErrNotFound
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
	j := Job{ID: fmt.Sprintf("job-%d-%03d", now.Unix(), s.next), TemplateID: tpl.ID, Name: tpl.Name, Targets: append([]string(nil), in.Targets...), Status: status, Operator: in.Operator, StartedAt: now.Format("2006-01-02 15:04:05"), Logs: []Log{{now.Format("15:04:05"), "info", message}}, TimeoutSeconds: in.TimeoutSeconds, Attempt: 0}
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
	j := Job{ID: fmt.Sprintf("job-%d-%03d", now.Unix(), s.next), TemplateID: "playbook:" + playbook.ID, PlaybookID: playbook.ID, Name: playbook.Name, Variables: cloneStringMap(in.Variables), Targets: append([]string(nil), in.Targets...), Status: "awaiting_approval", Operator: in.Operator, StartedAt: now.Format("2006-01-02 15:04:05"), Logs: []Log{{now.Format("15:04:05"), "info", "Playbook 已创建，等待其他管理员审批"}}, TimeoutSeconds: in.TimeoutSeconds, Attempt: 0}
	s.next++
	s.jobs = append([]Job{j}, s.jobs...)
	s.persistLocked(j)
	return cloneJob(j), nil
}
func (s *Service) Run(id string) (Job, error)   { return s.start(id, false) }
func (s *Service) Retry(id string) (Job, error) { return s.start(id, true) }
func (s *Service) Approve(id, approver string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		j := &s.jobs[i]
		if j.ID == id {
			if j.Status != "awaiting_approval" || approver == "" || approver == j.Operator {
				return Job{}, ErrInvalidState
			}
			j.Status = "pending"
			j.ApprovedBy = approver
			j.Logs = append(j.Logs, Log{time.Now().Format("15:04:05"), "success", "高风险任务已由 " + approver + " 审批通过"})
			s.persistLocked(*j)
			return cloneJob(*j), nil
		}
	}
	return Job{}, ErrNotFound
}
func (s *Service) start(id string, retry bool) (Job, error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(j.TimeoutSeconds)*time.Second)
	s.cancels[id] = cancel
	j.Status = "running"
	j.Progress = 0
	j.Attempt++
	j.Logs = append(j.Logs, Log{time.Now().Format("15:04:05"), "info", fmt.Sprintf("第 %d 次执行已进入队列", j.Attempt)})
	s.persistLocked(*j)
	attempt := j.Attempt
	command := s.commandForJobLocked(*j)
	request := ExecutionRequest{JobID: j.ID, Command: command, Targets: append([]string(nil), j.Targets...), Operator: j.Operator, Attempt: attempt, TimeoutSeconds: j.TimeoutSeconds}
	out := cloneJob(*j)
	s.mu.Unlock()
	go s.execute(ctx, id, attempt, request)
	return out, nil
}
func (s *Service) Cancel(id string) (Job, error) {
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
	s.finish(id, attempt, result.Status, progress, start, result.Message)
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
func (s *Service) finish(id string, attempt int, status string, progress int, start time.Time, message string) {
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
			delete(s.cancels, id)
			s.persistLocked(s.jobs[i])
			return
		}
	}
}
func (s *Service) persistLocked(j Job) {
	if s.db != nil {
		_ = upsertJob(context.Background(), s.db, j)
	}
}
func cloneJob(j Job) Job {
	j.Targets = append([]string(nil), j.Targets...)
	j.Logs = append([]Log(nil), j.Logs...)
	return j
}
func cloneJobs(items []Job) []Job {
	out := make([]Job, len(items))
	for i := range items {
		out[i] = cloneJob(items[i])
	}
	return out
}
