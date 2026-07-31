package tickets

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrNotFound = errors.New("ticket not found")
var ErrValidation = errors.New("validation")

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
	SLA                string  `json:"sla"`
	Risk               string  `json:"risk"`
	RelatedAsset       string  `json:"relatedAsset"`
	AutomationTemplate string  `json:"automationTemplate"`
	Steps              []Step  `json:"steps"`
	Timeline           []Event `json:"timeline"`
}
type CreateInput struct {
	Title       string `json:"title"`
	Type        string `json:"type"`
	Priority    string `json:"priority"`
	Applicant   string `json:"applicant"`
	Description string `json:"description"`
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
}

func NewService() *Service {
	return &Service{tickets: []Ticket{{"TKT-20260715-001", "生产订单服务扩容", "change", "high", "pending", "王强", "周敏", "订单峰值增长，申请扩容两个服务实例", "14:05", "剩余 42 分钟", "medium", "service-order", "tpl-restart", []Step{{"submit", "提交申请", "completed", "王强", "14:05"}, {"owner", "业务负责人审批", "completed", "李明", "14:18"}, {"ops", "运维审批", "current", "周敏", ""}, {"execute", "自动化执行", "pending", "系统", ""}}, []Event{{"14:05", "王强", "提交工单", "容量评估报告已附加"}, {"14:18", "李明", "审批通过", "业务侧确认扩容窗口"}}}, {"TKT-20260715-002", "申请测试环境数据库", "resource", "medium", "processing", "李娟", "吴涛", "为新项目申请 MySQL 测试实例", "13:42", "剩余 3 小时", "low", "", "", []Step{{"submit", "提交申请", "completed", "李娟", "13:42"}, {"ops", "资源审批", "completed", "吴涛", "14:02"}, {"provision", "资源交付", "current", "平台组", ""}}, []Event{{"13:42", "李娟", "提交申请", "规格 4C8G"}, {"14:02", "吴涛", "审批通过", "资源配额充足"}}}, {"TKT-20260715-003", "支付网关紧急回滚", "incident", "critical", "approved", "周敏", "张伟", "新版本出现间歇性超时，需要回滚", "12:20", "已完成", "high", "service-payment", "tpl-restart", []Step{{"submit", "紧急申请", "completed", "周敏", "12:20"}, {"approve", "紧急审批", "completed", "张伟", "12:24"}, {"execute", "回滚执行", "completed", "系统", "12:31"}}, []Event{{"12:20", "周敏", "提交紧急变更", "触发告警 ALERT-001"}, {"12:24", "张伟", "审批通过", "启动应急流程"}, {"12:31", "系统", "执行完成", "服务指标恢复"}}}}, next: 4}
}
func (s *Service) List() []Ticket {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Ticket(nil), s.tickets...)
}
func (s *Service) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r := Summary{Total: len(s.tickets), ApprovedToday: 1, SLAWarning: 1}
	for _, x := range s.tickets {
		if x.Status == "pending" {
			r.Pending++
		}
		if x.Status == "processing" {
			r.Processing++
		}
	}
	return r
}
func (s *Service) Create(in CreateInput) (Ticket, error) {
	if in.Title == "" || in.Type == "" {
		return Ticket{}, ErrValidation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	x := Ticket{ID: fmt.Sprintf("TKT-20260715-%03d", s.next), Title: in.Title, Type: in.Type, Priority: in.Priority, Status: "pending", Applicant: in.Applicant, Assignee: "运维审批组", Description: in.Description, CreatedAt: time.Now().Format("15:04"), SLA: "剩余 4 小时", Risk: "medium", Steps: []Step{{"submit", "提交申请", "completed", in.Applicant, time.Now().Format("15:04")}, {"approve", "运维审批", "current", "运维审批组", ""}, {"execute", "执行交付", "pending", "系统", ""}}, Timeline: []Event{{time.Now().Format("15:04"), in.Applicant, "提交工单", in.Description}}}
	s.next++
	s.tickets = append([]Ticket{x}, s.tickets...)
	return x, nil
}
func (s *Service) change(id, status, actor, comment string) (Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tickets {
		if s.tickets[i].ID == id {
			s.tickets[i].Status = status
			s.tickets[i].Timeline = append(s.tickets[i].Timeline, Event{time.Now().Format("15:04"), actor, map[bool]string{true: "审批通过", false: "驳回申请"}[status == "approved"], comment})
			if len(s.tickets[i].Steps) > 1 {
				s.tickets[i].Steps[1].Status = "completed"
				s.tickets[i].Steps[1].CompletedAt = time.Now().Format("15:04")
			}
			return s.tickets[i], nil
		}
	}
	return Ticket{}, ErrNotFound
}
func (s *Service) Approve(id, actor, comment string) (Ticket, error) {
	return s.change(id, "approved", actor, comment)
}
func (s *Service) Reject(id, actor, comment string) (Ticket, error) {
	return s.change(id, "rejected", actor, comment)
}
