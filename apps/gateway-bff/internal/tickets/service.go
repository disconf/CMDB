package tickets

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

var ErrNotFound = errors.New("ticket not found")
var ErrValidation = errors.New("validation")
var ErrInvalidState = errors.New("invalid state")

const (
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
)

type Step struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Assignee    string `json:"assignee"`
	CompletedAt string `json:"completedAt,omitempty"`
}

type Event struct {
	Time    string `json:"time"`
	Actor   string `json:"actor"`
	Action  string `json:"action"`
	Comment string `json:"comment"`
}

type Ticket struct {
	ID                 string  `json:"id"`
	Title              string  `json:"title"`
	Type               string  `json:"type"`
	Priority           string  `json:"priority"`
	Status             string  `json:"status"`
	Applicant          string  `json:"applicant"`
	Assignee           string  `json:"assignee"`
	Description        string  `json:"description"`
	CreatedAt          string  `json:"createdAt"`
	UpdatedAt          string  `json:"updatedAt,omitempty"`
	SLA                string  `json:"sla"`
	SLADueAt           string  `json:"slaDueAt,omitempty"`
	Risk               string  `json:"risk"`
	RelatedAsset       string  `json:"relatedAsset"`
	RelatedAlert       string  `json:"relatedAlert"`
	RelatedTask        string  `json:"relatedTask"`
	RelatedRelease     string  `json:"relatedRelease"`
	ProjectGroup       string  `json:"projectGroup"`
	AutomationTemplate string  `json:"automationTemplate"`
	EscalationLevel    int     `json:"escalationLevel"`
	EscalatedAt        string  `json:"escalatedAt,omitempty"`
	Steps              []Step  `json:"steps"`
	Timeline           []Event `json:"timeline"`

	createdAt time.Time
	updatedAt time.Time
	slaDueAt  time.Time
}

type CreateInput struct {
	Title              string `json:"title"`
	Type               string `json:"type"`
	Priority           string `json:"priority"`
	Applicant          string `json:"applicant"`
	Description        string `json:"description"`
	RelatedAsset       string `json:"relatedAsset"`
	RelatedAlert       string `json:"relatedAlert"`
	RelatedTask        string `json:"relatedTask"`
	RelatedRelease     string `json:"relatedRelease"`
	ProjectGroup       string `json:"projectGroup"`
	AutomationTemplate string `json:"automationTemplate"`
}

type AlertInput struct {
	ID       string
	Title    string
	Severity string
	Status   string
	Target   string
	Message  string
	Owner    string
}

type Summary struct {
	Total         int `json:"total"`
	Pending       int `json:"pending"`
	Processing    int `json:"processing"`
	ApprovedToday int `json:"approvedToday"`
	SLAWarning    int `json:"slaWarning"`
}

type Service struct {
	mu      sync.RWMutex
	tickets []Ticket
	next    int
	db      *sql.DB
}

func NewService() *Service {
	s := &Service{next: 1}
	if demo.Enabled() {
		now := time.Now()
		s.tickets = []Ticket{
			newSeedTicket("TKT-20260715-001", "生产订单服务扩容", "change", "high", StatusPending, "王强", "周敏", "订单峰值增长，申请扩容两个服务实例", "service-order", "", "订单平台", "tpl-restart", now.Add(-35*time.Minute)),
			newSeedTicket("TKT-20260715-002", "申请测试环境数据库", "resource", "medium", StatusApproved, "李娟", "吴涛", "为新项目申请 MySQL 测试实例", "", "", "研发测试", "", now.Add(-2*time.Hour)),
			newSeedTicket("TKT-20260715-003", "支付网关紧急回滚", "incident", "critical", StatusCompleted, "周敏", "张伟", "新版本出现间歇性超时，需要回滚", "service-payment", "ALERT-001", "支付平台", "tpl-restart", now.Add(-3*time.Hour)),
		}
		s.next = 4
	}
	if url := strings.TrimSpace(os.Getenv("DATABASE_URL")); url != "" {
		db, err := openPostgres(url)
		if err != nil {
			panic(fmt.Sprintf("initialize tickets repository: %v", err))
		}
		s.db = db
	}
	return s
}

func newSeedTicket(id, title, kind, priority, status, applicant, assignee, description, asset, alert, group, template string, created time.Time) Ticket {
	item := Ticket{
		ID: id, Title: title, Type: kind, Priority: priority, Status: status,
		Applicant: applicant, Assignee: assignee, Description: description,
		RelatedAsset: asset, RelatedAlert: alert, ProjectGroup: group,
		AutomationTemplate: template, Risk: riskForPriority(priority),
		createdAt: created, updatedAt: created,
		Steps: []Step{
			{ID: "submit", Name: "提交申请", Status: "completed", Assignee: applicant, CompletedAt: created.Format("15:04")},
			{ID: "approve", Name: "审批确认", Status: stepStatus(status, "completed"), Assignee: assignee, CompletedAt: decisionTime(status, created).Format("15:04")},
			{ID: "execute", Name: "执行交付", Status: stepStatus(status, "completed"), Assignee: "系统"},
		},
		Timeline: []Event{{Time: created.Format("15:04"), Actor: applicant, Action: "提交工单", Comment: description}},
	}
	if status == StatusPending {
		item.Steps[1].Status = "current"
		item.Steps[1].CompletedAt = ""
		item.Steps[2].Status = "pending"
	}
	applyDerived(&item, time.Now())
	return item
}

func stepStatus(status, done string) string {
	if status == StatusRejected || status == StatusPending {
		return "pending"
	}
	return done
}

func decisionTime(status string, created time.Time) time.Time {
	if status == StatusPending {
		return time.Time{}
	}
	return created.Add(15 * time.Minute)
}

func (s *Service) List() ([]Ticket, error) {
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		return loadTickets(ctx, s.db)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Ticket, len(s.tickets))
	for i := range s.tickets {
		out[i] = cloneTicket(s.tickets[i])
		applyDerived(&out[i], time.Now())
	}
	return out, nil
}

func (s *Service) RunSLAEscalation(ctx context.Context, interval time.Duration) {
	if interval < 30*time.Second {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, _ = s.EscalateOverdue()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) EscalateOverdue() (int, error) {
	items, err := s.List()
	if err != nil {
		return 0, err
	}
	now := time.Now()
	escalated := 0
	for _, item := range items {
		if !item.statusActive() || item.slaDueAt.IsZero() || now.Before(item.slaDueAt) {
			continue
		}
		level := slaEscalationLevel(item, now)
		if level <= item.EscalationLevel {
			continue
		}
		_, err := s.mutate(item.ID, func(current *Ticket, at time.Time) error {
			if !current.statusActive() {
				return ErrInvalidState
			}
			current.EscalationLevel = level
			current.EscalatedAt = at.Format("2006-01-02 15:04")
			switch level {
			case 1:
				current.Assignee = "运维值班组长"
				current.Priority = "high"
			case 2:
				current.Assignee = "运维经理"
				current.Priority = "critical"
			default:
				current.Assignee = "平台负责人"
				current.Priority = "critical"
			}
			current.Risk = riskForPriority(current.Priority)
			current.Timeline = append(current.Timeline, Event{Time: current.EscalatedAt, Actor: "SLA引擎", Action: "SLA超时升级", Comment: fmt.Sprintf("工单已超过SLA，升级至第 %d 级，处理人：%s", level, current.Assignee)})
			return nil
		})
		if err != nil {
			return escalated, err
		}
		escalated++
	}
	return escalated, nil
}

func slaEscalationLevel(item Ticket, now time.Time) int {
	overdue := now.Sub(item.slaDueAt)
	base := slaDuration(item.Priority)
	if base <= 0 {
		base = 4 * time.Hour
	}
	if overdue >= 2*base {
		return 3
	}
	if overdue >= base {
		return 2
	}
	return 1
}

func (s *Service) Summary() (Summary, error) {
	items, err := s.List()
	if err != nil {
		return Summary{}, err
	}
	now := time.Now()
	result := Summary{Total: len(items)}
	for _, item := range items {
		switch item.Status {
		case StatusPending:
			result.Pending++
		case StatusApproved, StatusCompleted:
			result.Processing++
		}
		if item.statusActive() {
			due, dueErr := time.Parse(time.RFC3339, item.SLADueAt)
			if dueErr == nil && due.Before(now.Add(30*time.Minute)) {
				result.SLAWarning++
			}
		}
		if item.updatedAt.In(now.Location()).Format("2006-01-02") == now.Format("2006-01-02") && item.Status != StatusPending && item.Status != StatusRejected && item.Status != StatusCancelled {
			result.ApprovedToday++
		}
	}
	return result, nil
}

func (s *Service) Get(id string) (Ticket, error) {
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return loadTicketByID(ctx, s.db, id)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.tickets {
		if s.tickets[i].ID == id {
			item := cloneTicket(s.tickets[i])
			applyDerived(&item, time.Now())
			return item, nil
		}
	}
	return Ticket{}, ErrNotFound
}

func (s *Service) Create(in CreateInput) (Ticket, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Type = strings.TrimSpace(in.Type)
	in.Priority = normalizePriority(in.Priority)
	in.Applicant = strings.TrimSpace(in.Applicant)
	if in.Title == "" || in.Type == "" || in.Applicant == "" || !validType(in.Type) {
		return Ticket{}, ErrValidation
	}
	now := time.Now()
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return Ticket{}, err
		}
		defer tx.Rollback()
		id, err := nextTicketID(ctx, tx, now)
		if err != nil {
			return Ticket{}, err
		}
		item := buildTicket(id, in, now)
		if err := upsertTicket(ctx, tx, item); err != nil {
			return Ticket{}, err
		}
		if err := tx.Commit(); err != nil {
			return Ticket{}, err
		}
		applyDerived(&item, now)
		return cloneTicket(item), nil
	}
	id, err := s.nextID(now)
	if err != nil {
		return Ticket{}, err
	}
	item := buildTicket(id, in, now)
	s.mu.Lock()
	s.tickets = append([]Ticket{item}, s.tickets...)
	s.mu.Unlock()
	applyDerived(&item, now)
	return cloneTicket(item), nil
}

func buildTicket(id string, in CreateInput, now time.Time) Ticket {
	assignee := "运维审批组"
	steps := []Step{
		{ID: "submit", Name: "提交申请", Status: "completed", Assignee: in.Applicant, CompletedAt: now.Format("15:04")},
		{ID: "approve", Name: "运维审批", Status: "current", Assignee: assignee},
		{ID: "execute", Name: "执行交付", Status: "pending", Assignee: "系统"},
	}
	item := Ticket{
		ID: id, Title: in.Title, Type: in.Type, Priority: in.Priority, Status: StatusPending,
		Applicant: in.Applicant, Assignee: assignee, Description: strings.TrimSpace(in.Description),
		RelatedAsset: strings.TrimSpace(in.RelatedAsset), RelatedAlert: strings.TrimSpace(in.RelatedAlert), RelatedTask: strings.TrimSpace(in.RelatedTask), RelatedRelease: strings.TrimSpace(in.RelatedRelease),
		ProjectGroup: strings.TrimSpace(in.ProjectGroup), AutomationTemplate: strings.TrimSpace(in.AutomationTemplate),
		Risk: riskForPriority(in.Priority), Steps: steps,
		Timeline:  []Event{{Time: now.Format("2006-01-02 15:04"), Actor: in.Applicant, Action: "提交工单", Comment: strings.TrimSpace(in.Description)}},
		createdAt: now, updatedAt: now,
	}
	applyDerived(&item, now)
	return item
}

func (s *Service) Approve(id, actor, comment string) (Ticket, error) {
	return s.mutate(id, func(item *Ticket, now time.Time) error {
		if item.Status != StatusPending {
			return ErrInvalidState
		}
		if actor == "" {
			return ErrValidation
		}
		item.Status = StatusApproved
		completeStep(item, "approve", actor, now)
		item.Steps[2].Status = "current"
		item.Timeline = append(item.Timeline, Event{Time: now.Format("2006-01-02 15:04"), Actor: actor, Action: "审批通过", Comment: strings.TrimSpace(comment)})
		return nil
	})
}

func (s *Service) Reject(id, actor, comment string) (Ticket, error) {
	return s.mutate(id, func(item *Ticket, now time.Time) error {
		if item.Status != StatusPending {
			return ErrInvalidState
		}
		if actor == "" {
			return ErrValidation
		}
		item.Status = StatusRejected
		completeStep(item, "approve", actor, now)
		item.Timeline = append(item.Timeline, Event{Time: now.Format("2006-01-02 15:04"), Actor: actor, Action: "驳回申请", Comment: strings.TrimSpace(comment)})
		return nil
	})
}

func (s *Service) Comment(id, actor, comment string) (Ticket, error) {
	comment = strings.TrimSpace(comment)
	if comment == "" || actor == "" {
		return Ticket{}, ErrValidation
	}
	return s.mutate(id, func(item *Ticket, now time.Time) error {
		item.Timeline = append(item.Timeline, Event{Time: now.Format("2006-01-02 15:04"), Actor: actor, Action: "添加备注", Comment: comment})
		return nil
	})
}

func (s *Service) Assign(id, assignee, actor string) (Ticket, error) {
	assignee = strings.TrimSpace(assignee)
	actor = strings.TrimSpace(actor)
	if assignee == "" || actor == "" {
		return Ticket{}, ErrValidation
	}
	return s.mutate(id, func(item *Ticket, now time.Time) error {
		if item.Status == StatusRejected || item.Status == StatusCompleted || item.Status == StatusCancelled {
			return ErrInvalidState
		}
		item.Assignee = assignee
		for i := range item.Steps {
			if item.Steps[i].Status == "current" {
				item.Steps[i].Assignee = assignee
			}
		}
		item.Timeline = append(item.Timeline, Event{Time: now.Format("2006-01-02 15:04"), Actor: actor, Action: "分派处理人", Comment: assignee})
		return nil
	})
}

func (s *Service) Close(id, actor, comment string) (Ticket, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return Ticket{}, ErrValidation
	}
	return s.mutate(id, func(item *Ticket, now time.Time) error {
		if item.Status != StatusApproved {
			return ErrInvalidState
		}
		item.Status = StatusCompleted
		completeStep(item, "execute", actor, now)
		item.Timeline = append(item.Timeline, Event{Time: now.Format("2006-01-02 15:04"), Actor: actor, Action: "完成工单", Comment: strings.TrimSpace(comment)})
		return nil
	})
}

func (s *Service) Cancel(id, actor, comment string) (Ticket, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return Ticket{}, ErrValidation
	}
	return s.mutate(id, func(item *Ticket, now time.Time) error {
		if item.Status != StatusPending {
			return ErrInvalidState
		}
		item.Status = StatusCancelled
		for i := range item.Steps {
			if item.Steps[i].Status == "current" {
				item.Steps[i].Status = "pending"
			}
		}
		item.Timeline = append(item.Timeline, Event{Time: now.Format("2006-01-02 15:04"), Actor: actor, Action: "取消工单", Comment: strings.TrimSpace(comment)})
		return nil
	})
}

func (s *Service) SyncAlert(in AlertInput) (Ticket, bool, error) {
	in.ID = strings.TrimSpace(in.ID)
	in.Title = strings.TrimSpace(in.Title)
	if in.ID == "" || in.Title == "" {
		return Ticket{}, false, ErrValidation
	}
	if strings.EqualFold(strings.TrimSpace(in.Status), "resolved") {
		item, err := s.resolveAlert(in)
		return item, false, err
	}
	return s.ensureAlertTicket(in)
}

func (s *Service) ensureAlertTicket(in AlertInput) (Ticket, bool, error) {
	now := time.Now()
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return Ticket{}, false, err
		}
		defer tx.Rollback()
		existing, err := findTriggeredAlertTicket(ctx, tx, in.ID)
		if err == nil {
			if err := tx.Commit(); err != nil {
				return Ticket{}, false, err
			}
			existing.updatedAt = now
			applyDerived(&existing, now)
			return cloneTicket(existing), false, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Ticket{}, false, err
		}
		id, err := nextTicketID(ctx, tx, now)
		if err != nil {
			return Ticket{}, false, err
		}
		item := buildAlertTicket(id, in, now)
		if err := upsertTicket(ctx, tx, item); err != nil {
			return Ticket{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return Ticket{}, false, err
		}
		applyDerived(&item, now)
		return cloneTicket(item), true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tickets {
		if s.tickets[i].RelatedAlert == in.ID && s.tickets[i].statusActive() {
			item := cloneTicket(s.tickets[i])
			applyDerived(&item, now)
			return item, false, nil
		}
	}
	id, err := s.nextIDLocked(now)
	if err != nil {
		return Ticket{}, false, err
	}
	item := buildAlertTicket(id, in, now)
	s.tickets = append([]Ticket{item}, s.tickets...)
	applyDerived(&item, now)
	return cloneTicket(item), true, nil
}

func (s *Service) resolveAlert(in AlertInput) (Ticket, error) {
	items, err := s.List()
	if err != nil {
		return Ticket{}, err
	}
	for _, item := range items {
		if item.RelatedAlert != in.ID || !item.statusActive() {
			continue
		}
		return s.mutate(item.ID, func(current *Ticket, now time.Time) error {
			if !current.statusActive() {
				return ErrInvalidState
			}
			current.Status = StatusCompleted
			for i := range current.Steps {
				if current.Steps[i].Status == "current" || current.Steps[i].Status == "pending" {
					current.Steps[i].Status = "completed"
					current.Steps[i].CompletedAt = now.Format("15:04")
				}
			}
			current.Timeline = append(current.Timeline, Event{Time: now.Format("2006-01-02 15:04"), Actor: "alertmanager", Action: "告警恢复", Comment: "告警已恢复，自动关闭工单"})
			return nil
		})
	}
	return Ticket{}, nil
}

func buildAlertTicket(id string, in AlertInput, now time.Time) Ticket {
	priority := alertPriority(in.Severity)
	title := in.Title
	if title == "" {
		title = "告警事件：" + in.Target
	}
	assignee := strings.TrimSpace(in.Owner)
	if assignee == "" {
		assignee = "监控值班组"
	}
	description := strings.TrimSpace(in.Message)
	item := Ticket{
		ID: id, Title: title, Type: "incident", Priority: priority, Status: StatusPending,
		Applicant: "Alertmanager", Assignee: assignee, Description: description,
		RelatedAsset: strings.TrimSpace(in.Target), RelatedAlert: in.ID,
		Risk: riskForPriority(priority),
		Steps: []Step{
			{ID: "trigger", Name: "告警触发", Status: "completed", Assignee: "Prometheus", CompletedAt: now.Format("15:04")},
			{ID: "ack", Name: "故障确认", Status: "current", Assignee: assignee},
			{ID: "recover", Name: "恢复验证", Status: "pending", Assignee: "系统"},
		},
		Timeline:  []Event{{Time: now.Format("2006-01-02 15:04"), Actor: "Alertmanager", Action: "告警自动生成工单", Comment: description}},
		createdAt: now, updatedAt: now,
	}
	applyDerived(&item, now)
	return item
}

func (s *Service) mutate(id string, change func(*Ticket, time.Time) error) (Ticket, error) {
	now := time.Now()
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return Ticket{}, err
		}
		defer tx.Rollback()
		item, err := loadTicketByIDForUpdate(ctx, tx, id)
		if err != nil {
			return Ticket{}, err
		}
		if err := change(&item, now); err != nil {
			return Ticket{}, err
		}
		item.updatedAt = now
		applyDerived(&item, now)
		if err := upsertTicket(ctx, tx, item); err != nil {
			return Ticket{}, err
		}
		if err := tx.Commit(); err != nil {
			return Ticket{}, err
		}
		return cloneTicket(item), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tickets {
		if s.tickets[i].ID != id {
			continue
		}
		item := cloneTicket(s.tickets[i])
		if err := change(&item, now); err != nil {
			return Ticket{}, err
		}
		item.updatedAt = now
		applyDerived(&item, now)
		s.tickets[i] = item
		return cloneTicket(item), nil
	}
	return Ticket{}, ErrNotFound
}

func (s *Service) nextID(now time.Time) (string, error) {
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return nextTicketID(ctx, s.db, now)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextIDLocked(now)
}

func (s *Service) nextIDLocked(now time.Time) (string, error) {
	s.next++
	return fmt.Sprintf("TKT-%s-%04d", now.Format("20060102"), s.next-1), nil
}

func cloneTicket(item Ticket) Ticket {
	item.Steps = append([]Step(nil), item.Steps...)
	item.Timeline = append([]Event(nil), item.Timeline...)
	return item
}

func applyDerived(item *Ticket, now time.Time) {
	if item.createdAt.IsZero() {
		item.createdAt = now
	}
	if item.updatedAt.IsZero() {
		item.updatedAt = item.createdAt
	}
	item.createdAt = item.createdAt.In(now.Location())
	item.updatedAt = item.updatedAt.In(now.Location())
	if item.slaDueAt.IsZero() {
		item.slaDueAt = item.createdAt.Add(slaDuration(item.Priority))
	}
	item.createdAt = item.createdAt.In(now.Location())
	item.slaDueAt = item.slaDueAt.In(now.Location())
	item.CreatedAt = item.createdAt.Format("2006-01-02 15:04")
	item.UpdatedAt = item.updatedAt.Format("2006-01-02 15:04")
	item.SLADueAt = item.slaDueAt.Format(time.RFC3339)
	remaining := item.slaDueAt.Sub(now)
	if remaining >= 0 {
		item.SLA = "剩余 " + compactDuration(remaining)
	} else {
		item.SLA = "已超时 " + compactDuration(-remaining)
	}
}

func compactDuration(value time.Duration) string {
	if value < time.Hour {
		return fmt.Sprintf("%d 分钟", int(value.Minutes()+0.5))
	}
	hours := int(value.Hours())
	minutes := int(value.Minutes()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%d 小时", hours)
	}
	return fmt.Sprintf("%d 小时 %d 分钟", hours, minutes)
}

func slaDuration(priority string) time.Duration {
	switch normalizePriority(priority) {
	case "critical":
		return 30 * time.Minute
	case "high":
		return 2 * time.Hour
	case "low":
		return 8 * time.Hour
	default:
		return 4 * time.Hour
	}
}

func normalizePriority(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical", "high", "low":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "medium"
	}
}

func alertPriority(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "high":
		return "critical"
	case "warning", "medium":
		return "high"
	default:
		return "medium"
	}
}

func riskForPriority(priority string) string {
	switch normalizePriority(priority) {
	case "critical", "high":
		return "high"
	case "low":
		return "low"
	default:
		return "medium"
	}
}

func validType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "incident", "change", "resource", "release", "access", "inspection":
		return true
	default:
		return false
	}
}

func completeStep(item *Ticket, id string, actor string, now time.Time) {
	for i := range item.Steps {
		if item.Steps[i].ID == id {
			item.Steps[i].Status = "completed"
			item.Steps[i].Assignee = actor
			item.Steps[i].CompletedAt = now.Format("15:04")
			return
		}
	}
}

func (item Ticket) statusActive() bool {
	return item.Status == StatusPending || item.Status == StatusApproved
}
