package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthorized       = errors.New("unauthorized")
)

type account struct {
	password string
	user     User
}

type Service struct {
	mu         sync.RWMutex
	accounts   map[string]account
	sessions   map[string]User
	modules    []Module
	issuerURL  string
	httpClient *http.Client
	redis      *redis.Client
	cacheTTL   time.Duration
}

func NewService() *Service {
	allPermissions := []string{"dashboard:view", "cmdb:view", "cmdb:manage", "discovery:view", "discovery:manage", "topology:view", "monitor:view", "monitor:manage", "job:view", "job:manage", "ticket:view", "ticket:manage", "release:view", "release:manage", "toolbox:use", "aiops:view", "aiops:execute", "system:manage"}
	service := &Service{
		accounts: map[string]account{
			"admin":  {"admin123", User{"u-admin", "admin", "运维管理员", []string{"platform-admin"}, allPermissions, []string{"华东业务组", "核心系统组"}}},
			"viewer": {"viewer123", User{"u-viewer", "viewer", "只读用户", []string{"viewer"}, []string{"dashboard:view", "cmdb:view", "topology:view", "monitor:view"}, []string{"华东业务组"}}},
		},
		sessions: make(map[string]User),
		modules:  []Module{{"dashboard", "全局态势", "/", "LayoutDashboard", "dashboard:view"}, {"cmdb", "CMDB 资产", "/cmdb", "Database", "cmdb:view"}, {"discovery", "Agent 与发现", "/discovery", "Radar", "discovery:view"}, {"topology", "拓扑中心", "/topology", "Network", "topology:view"}, {"monitor", "监控告警", "/monitor", "BellRing", "monitor:view"}, {"jobs", "任务管理", "/jobs", "ClipboardCheck", "job:view"}, {"tickets", "工单中心", "/tickets", "TicketCheck", "ticket:view"}, {"releases", "发布管理", "/releases", "Rocket", "release:view"}, {"toolbox", "排查工具", "/toolbox", "Wrench", "toolbox:use"}, {"aiops", "AI 运维", "/aiops", "Bot", "aiops:view"}, {"system", "系统管理", "/system", "Settings", "system:manage"}},
	}
	service.issuerURL = strings.TrimRight(os.Getenv("KEYCLOAK_ISSUER_URL"), "/")
	service.httpClient = &http.Client{Timeout: 5 * time.Second}
	service.cacheTTL = 60 * time.Second
	if configured := os.Getenv("AUTH_CACHE_TTL"); configured != "" {
		if duration, err := time.ParseDuration(configured); err == nil && duration > 0 && duration <= 5*time.Minute {
			service.cacheTTL = duration
		}
	}
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		if client, err := newRedisClient(redisURL); err == nil {
			service.redis = client
		}
	}
	return service
}

func (s *Service) Login(username, password string) (Session, error) {
	if s.issuerURL != "" {
		return Session{}, ErrInvalidCredentials
	}
	account, ok := s.accounts[username]
	if !ok || account.password != password {
		return Session{}, ErrInvalidCredentials
	}
	token, err := newToken()
	if err != nil {
		return Session{}, err
	}
	s.mu.Lock()
	s.sessions[token] = cloneUser(account.user)
	s.mu.Unlock()
	return Session{Token: token, User: cloneUser(account.user)}, nil
}

func (s *Service) CurrentUser(token string) (User, error) {
	if s.issuerURL != "" {
		if user, ok := s.cachedUser(token); ok {
			return user, nil
		}
		user, err := s.keycloakUser(token)
		if err == nil {
			s.cacheUser(token, user)
		}
		return user, err
	}
	s.mu.RLock()
	user, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return User{}, ErrUnauthorized
	}
	return cloneUser(user), nil
}

func (s *Service) Modules(token string) ([]Module, error) {
	user, err := s.CurrentUser(token)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(user.Permissions))
	for _, p := range user.Permissions {
		allowed[p] = struct{}{}
	}
	modules := make([]Module, 0, len(s.modules))
	for _, module := range s.modules {
		if _, ok := allowed[module.Permission]; ok {
			modules = append(modules, module)
		}
	}
	return modules, nil
}

func (s *Service) Logout(token string) {
	s.evictCachedUser(token)
	if s.issuerURL != "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func (s *Service) keycloakUser(token string) (User, error) {
	if token == "" {
		return User{}, ErrUnauthorized
	}
	request, err := http.NewRequest(http.MethodGet, s.issuerURL+"/protocol/openid-connect/userinfo", nil)
	if err != nil {
		return User{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := s.httpClient.Do(request)
	if err != nil {
		return User{}, fmt.Errorf("validate keycloak token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return User{}, ErrUnauthorized
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return User{}, ErrUnauthorized
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return User{}, ErrUnauthorized
	}
	var claims struct {
		Subject     string `json:"sub"`
		Username    string `json:"preferred_username"`
		Name        string `json:"name"`
		Issuer      string `json:"iss"`
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
		ProjectGroups []string `json:"project_groups"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Issuer != s.issuerURL {
		return User{}, ErrUnauthorized
	}
	var roles, permissions []string
	for _, role := range claims.RealmAccess.Roles {
		if strings.Contains(role, ":") {
			permissions = append(permissions, role)
		} else if role == "platform-admin" || role == "viewer" {
			roles = append(roles, role)
		}
	}
	displayName := claims.Name
	if displayName == "" {
		displayName = claims.Username
	}
	return User{ID: claims.Subject, Username: claims.Username, DisplayName: displayName, Roles: roles, Permissions: permissions, ProjectGroups: claims.ProjectGroups}, nil
}

func newToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
func cloneUser(user User) User {
	user.Roles = append([]string(nil), user.Roles...)
	user.Permissions = append([]string(nil), user.Permissions...)
	user.ProjectGroups = append([]string(nil), user.ProjectGroups...)
	return user
}
