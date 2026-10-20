package system

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var ErrNotFound = errors.New("not found")
var ErrValidation = errors.New("validation")
var ErrConflict = errors.New("conflict")
var ErrForbidden = errors.New("forbidden")
var ErrKeycloakNotConfigured = errors.New("keycloak admin is not configured")

type User struct {
	ID                  string               `json:"id"`
	Username            string               `json:"username"`
	DisplayName         string               `json:"displayName"`
	Department          string               `json:"department"`
	Email               string               `json:"email,omitempty"`
	Roles               []string             `json:"roles"`
	Status              string               `json:"status"`
	LastLogin           string               `json:"lastLogin"`
	DataScope           string               `json:"dataScope"`
	ProjectGroups       []string             `json:"projectGroups"`
	ScopeTags           []string             `json:"scopeTags"`
	KeycloakGroups      []string             `json:"keycloakGroups,omitempty"`
	KeycloakID          string               `json:"keycloakId,omitempty"`
	SyncedAt            string               `json:"syncedAt,omitempty"`
	DeletedAt           string               `json:"deletedAt,omitempty"`
	DeletedBy           string               `json:"deletedBy,omitempty"`
	PermissionOverrides []PermissionOverride `json:"permissionOverrides"`
}
type PermissionOverride struct {
	Code   string `json:"code"`
	Effect string `json:"effect"`
	Reason string `json:"reason,omitempty"`
}
type Role struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Users         int      `json:"users"`
	Permissions   []string `json:"permissions"`
	DataScope     string   `json:"dataScope"`
	ProjectGroups []string `json:"projectGroups"`
	ScopeTags     []string `json:"scopeTags"`
	System        bool     `json:"system"`
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
	mu         sync.RWMutex
	users      []User
	roles      []Role
	modules    []Module
	audits     []Audit
	settings   []Setting
	db         *sql.DB
	lastReload time.Time
}

func NewService() *Service {
	service := &Service{users: defaultUsers(), roles: defaultRoles(), modules: defaultModules(), audits: defaultAudits(), settings: defaultSettings()}
	if url := strings.TrimSpace(os.Getenv("DATABASE_URL")); url != "" {
		db, err := openPostgres(url)
		if err != nil {
			panic(fmt.Sprintf("initialize system repository: %v", err))
		}
		service.db = db
		if err = service.seedDefaults(context.Background()); err != nil {
			panic(fmt.Sprintf("seed system repository: %v", err))
		}
		if err = service.reload(context.Background()); err != nil {
			panic(fmt.Sprintf("load system repository: %v", err))
		}
	}
	return service
}
func (s *Service) Users() []User {
	result := make([]User, 0, len(s.users))
	for _, item := range s.allUsers() {
		if item.Status != "deleted" {
			result = append(result, item)
		}
	}
	return result
}

func (s *Service) allUsers() []User {
	if s.db != nil {
		_ = s.reload(context.Background())
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneUsers(s.users)
}
func (s *Service) Roles() []Role {
	if s.db != nil {
		_ = s.reload(context.Background())
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := cloneRoles(s.roles)
	counts := map[string]int{}
	for _, user := range s.users {
		if user.Status == "deleted" {
			continue
		}
		for _, roleID := range user.Roles {
			counts[roleID]++
		}
	}
	for index := range result {
		result[index].System = isSystemRole(result[index].ID)
		result[index].Users = counts[result[index].ID]
	}
	return result
}
func (s *Service) Modules() []Module { return append([]Module(nil), s.modules...) }
func (s *Service) Audits() []Audit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Audit(nil), s.audits...)
}
func (s *Service) Settings() []Setting { return append([]Setting(nil), s.settings...) }
func (s *Service) Summary() Summary {
	r := Summary{Roles: len(s.roles), Modules: len(s.modules), AuditsToday: len(s.audits)}
	for _, u := range s.users {
		if u.Status == "deleted" {
			continue
		}
		r.Users++
		if u.Status == "active" {
			r.ActiveUsers++
		}
	}
	return r
}
func (s *Service) audit(actor, action, resource, detail string) {
	s.audits = append([]Audit{{"audit-" + time.Now().Format("150405"), time.Now().Format("15:04:05"), actor, action, resource, "127.0.0.1", "success", detail}}, s.audits...)
}

func isSystemRole(id string) bool {
	switch id {
	case "platform-admin", "viewer", "operator", "approver":
		return true
	}
	return false
}
func (s *Service) ToggleUser(id, actor string) (User, error) {
	for _, user := range s.Users() {
		if user.ID != id {
			continue
		}
		next := "active"
		if user.Status == "active" {
			next = "disabled"
		}
		return s.UpdateUser(id, UserUpdateInput{DisplayName: user.DisplayName, Department: user.Department, Email: user.Email, Roles: user.Roles, Status: next, DataScope: user.DataScope, ProjectGroups: user.ProjectGroups, ScopeTags: user.ScopeTags}, actor)
	}
	return User{}, ErrNotFound
}
func (s *Service) UpdateRole(id string, permissions []string, actor string) (Role, error) {
	return s.UpdateRoleProfile(id, RoleUpdateInput{Permissions: permissions}, actor)
}
