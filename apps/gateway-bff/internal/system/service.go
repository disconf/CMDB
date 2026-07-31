package system

import (
	"errors"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")

type User struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Department  string   `json:"department"`
	Roles       []string `json:"roles"`
	Status      string   `json:"status"`
	LastLogin   string   `json:"lastLogin"`
}
type Role struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Users       int      `json:"users"`
	Permissions []string `json:"permissions"`
}
type Module struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Order      int    `json:"order"`
	Permission string `json:"permission"`
}
type Audit struct {
	ID       string `json:"id"`
	Time     string `json:"time"`
	Actor    string `json:"actor"`
	Action   string `json:"action"`
	Resource string `json:"resource"`
	IP       string `json:"ip"`
	Result   string `json:"result"`
	Detail   string `json:"detail"`
}
type Setting struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Value       string `json:"value"`
	Category    string `json:"category"`
	Description string `json:"description"`
}
type Summary struct {
	Users       int `json:"users"`
	ActiveUsers int `json:"activeUsers"`
	Roles       int `json:"roles"`
	Modules     int `json:"modules"`
	AuditsToday int `json:"auditsToday"`
}
type Service struct {
	mu       sync.RWMutex
	users    []User
	roles    []Role
	modules  []Module
	audits   []Audit
	settings []Setting
}

func NewService() *Service {
	return &Service{users: []User{{"u-admin", "admin", "运维管理员", "平台运维部", []string{"platform-admin"}, "active", "15:12"}, {"u-viewer", "viewer", "只读用户", "研发中心", []string{"viewer"}, "active", "昨天 18:20"}, {"u-ops-01", "zhangwei", "张伟", "平台运维部", []string{"operator"}, "active", "14:58"}, {"u-dba-01", "wutao", "吴涛", "数据库组", []string{"dba", "approver"}, "active", "14:32"}, {"u-dev-01", "wangqiang", "王强", "电商研发部", []string{"developer"}, "locked", "13:06"}}, roles: []Role{{"platform-admin", "平台管理员", "拥有所有平台管理权限", 1, []string{"*"}}, {"operator", "运维操作员", "资产、监控、任务与工具箱操作", 8, []string{"cmdb:view", "monitor:manage", "job:manage", "toolbox:use"}}, {"viewer", "只读用户", "查看大屏、资产、拓扑和监控", 12, []string{"dashboard:view", "cmdb:view", "topology:view", "monitor:view"}}, {"approver", "审批人", "工单与发布审批", 4, []string{"ticket:manage", "release:view"}}}, modules: []Module{{"dashboard", "全局态势", true, 1, "dashboard:view"}, {"cmdb", "CMDB 资产", true, 2, "cmdb:view"}, {"topology", "拓扑中心", true, 3, "topology:view"}, {"monitor", "监控告警", true, 4, "monitor:view"}, {"jobs", "任务管理", true, 5, "job:view"}, {"aiops", "AI 运维", true, 10, "aiops:view"}}, audits: []Audit{{"audit-001", "15:12:06", "admin", "用户登录", "管理后台", "10.20.1.8", "success", "认证成功"}, {"audit-002", "14:58:22", "zhangwei", "执行诊断", "prod-api-01", "10.20.1.21", "success", "主机健康检查"}}, settings: []Setting{{"session.timeout", "会话超时", "30", "安全", "无操作自动退出分钟数"}, {"audit.retention", "审计保留期", "180", "审计", "审计日志保留天数"}, {"discovery.interval", "发现周期", "15", "采集", "自动发现间隔分钟数"}, {"alert.silence.max", "最大静默时间", "24", "告警", "告警最大静默小时数"}}}
}
func (s *Service) Users() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]User(nil), s.users...)
}
func (s *Service) Roles() []Role {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Role(nil), s.roles...)
}
func (s *Service) Modules() []Module { return append([]Module(nil), s.modules...) }
func (s *Service) Audits() []Audit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Audit(nil), s.audits...)
}
func (s *Service) Settings() []Setting { return append([]Setting(nil), s.settings...) }
func (s *Service) Summary() Summary {
	r := Summary{Users: len(s.users), Roles: len(s.roles), Modules: len(s.modules), AuditsToday: len(s.audits)}
	for _, u := range s.users {
		if u.Status == "active" {
			r.ActiveUsers++
		}
	}
	return r
}
func (s *Service) audit(actor, action, resource, detail string) {
	s.audits = append([]Audit{{"audit-" + time.Now().Format("150405"), time.Now().Format("15:04:05"), actor, action, resource, "127.0.0.1", "success", detail}}, s.audits...)
}
func (s *Service) ToggleUser(id, actor string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.users {
		if s.users[i].ID == id {
			if s.users[i].Status == "active" {
				s.users[i].Status = "disabled"
			} else {
				s.users[i].Status = "active"
			}
			s.audit(actor, "切换用户状态", s.users[i].Username, s.users[i].Status)
			return s.users[i], nil
		}
	}
	return User{}, ErrNotFound
}
func (s *Service) UpdateRole(id string, permissions []string, actor string) (Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.roles {
		if s.roles[i].ID == id {
			s.roles[i].Permissions = permissions
			s.audit(actor, "更新角色权限", s.roles[i].Name, "权限项已更新")
			return s.roles[i], nil
		}
	}
	return Role{}, ErrNotFound
}
