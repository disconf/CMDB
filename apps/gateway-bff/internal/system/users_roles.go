package system

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._@-]{3,64}$`)

type UserCreateInput struct {
	Username            string               `json:"username"`
	DisplayName         string               `json:"displayName"`
	Department          string               `json:"department"`
	Email               string               `json:"email"`
	Password            string               `json:"password"`
	Roles               []string             `json:"roles"`
	DataScope           string               `json:"dataScope"`
	ProjectGroups       []string             `json:"projectGroups"`
	ScopeTags           []string             `json:"scopeTags"`
	PermissionOverrides []PermissionOverride `json:"permissionOverrides"`
}

type RoleCreateInput struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Permissions   []string `json:"permissions"`
	DataScope     string   `json:"dataScope"`
	ProjectGroups []string `json:"projectGroups"`
	ScopeTags     []string `json:"scopeTags"`
}

func (s *Service) CreateUser(input UserCreateInput, actor string) (User, string, error) {
	input.Username = strings.TrimSpace(input.Username)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Department = strings.TrimSpace(input.Department)
	input.Email = strings.TrimSpace(input.Email)
	input.Password = strings.TrimSpace(input.Password)
	if !usernamePattern.MatchString(input.Username) {
		return User{}, "", ErrValidation
	}
	if input.DisplayName == "" {
		input.DisplayName = input.Username
	}
	if input.Email != "" && (!strings.Contains(input.Email, "@") || strings.ContainsAny(input.Email, " \t\r\n")) {
		return User{}, "", ErrValidation
	}
	if !validDataScope(input.DataScope) {
		return User{}, "", ErrValidation
	}
	roles, err := s.validateRoleIDs(input.Roles)
	if err != nil || len(roles) == 0 {
		return User{}, "", ErrValidation
	}
	overrides, err := normalizePermissionOverrides(input.PermissionOverrides)
	if err != nil {
		return User{}, "", err
	}
	if err = validateAdminOverrides(roles, overrides); err != nil {
		return User{}, "", err
	}
	for _, item := range s.Users() {
		if strings.EqualFold(item.Username, input.Username) {
			return User{}, "", ErrConflict
		}
	}
	initialPassword := input.Password
	if initialPassword == "" {
		initialPassword, err = randomPassword(20)
		if err != nil {
			return User{}, "", err
		}
	} else if len(initialPassword) < 8 {
		return User{}, "", ErrValidation
	}

	keycloakID := ""
	if s.db != nil {
		admin, adminErr := newKeycloakAdmin(context.Background())
		if adminErr != nil {
			return User{}, "", adminErr
		}
		keycloakID, err = admin.CreateUser(context.Background(), input, initialPassword)
		if err != nil {
			return User{}, "", err
		}
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	item := User{
		ID: newSystemID("u"), Username: input.Username, DisplayName: input.DisplayName,
		Department: input.Department, Email: input.Email, Roles: roles, Status: "active",
		DataScope: input.DataScope, ProjectGroups: mergeValues(nil, input.ProjectGroups),
		ScopeTags: mergeValues(nil, input.ScopeTags), PermissionOverrides: overrides,
		KeycloakID: keycloakID, SyncedAt: now,
	}

	s.mu.Lock()
	if s.db != nil {
		if err = upsertSystemUser(context.Background(), s.db, item); err != nil {
			s.mu.Unlock()
			return User{}, "", err
		}
	}
	s.users = append(s.users, item)
	s.audit(actor, "创建用户", item.Username, "角色: "+strings.Join(item.Roles, ","))
	s.mu.Unlock()
	if input.Password != "" {
		initialPassword = ""
	}
	return cloneUser(item), initialPassword, nil
}

func (s *Service) UpdateUser(id string, input UserUpdateInput, actor string) (User, error) {
	var existing User
	found := false
	for _, item := range s.Users() {
		if item.ID == id {
			existing = item
			found = true
			break
		}
	}
	if !found {
		return User{}, ErrNotFound
	}
	next := existing
	if strings.TrimSpace(input.DisplayName) != "" {
		next.DisplayName = strings.TrimSpace(input.DisplayName)
	}
	next.Department = strings.TrimSpace(input.Department)
	next.Email = strings.TrimSpace(input.Email)
	if input.Roles != nil {
		roles, err := s.validateRoleIDs(input.Roles)
		if err != nil {
			return User{}, ErrValidation
		}
		next.Roles = roles
	}
	if input.Status != "" {
		if input.Status != "active" && input.Status != "disabled" && input.Status != "locked" {
			return User{}, ErrValidation
		}
		next.Status = input.Status
	}
	if input.DataScope != "" {
		if !validDataScope(input.DataScope) {
			return User{}, ErrValidation
		}
		next.DataScope = input.DataScope
	}
	next.ProjectGroups = mergeValues(nil, input.ProjectGroups)
	next.ScopeTags = mergeValues(nil, input.ScopeTags)
	if input.PermissionOverrides != nil {
		overrides, err := normalizePermissionOverrides(input.PermissionOverrides)
		if err != nil {
			return User{}, err
		}
		next.PermissionOverrides = overrides
	}
	if err := validateAdminOverrides(next.Roles, next.PermissionOverrides); err != nil {
		return User{}, err
	}
	if next.Username == "" || next.ID == "" {
		return User{}, ErrValidation
	}
	if actor == existing.Username {
		if next.Status != "active" || !hasValue(next.Roles, "platform-admin") {
			return User{}, ErrForbidden
		}
	}
	if hasValue(existing.Roles, "platform-admin") && (next.Status != "active" || !hasValue(next.Roles, "platform-admin")) {
		if s.adminCount() <= 1 {
			return User{}, ErrConflict
		}
	}

	if s.db != nil && existing.KeycloakID != "" {
		admin, err := newKeycloakAdmin(context.Background())
		if err != nil {
			return User{}, err
		}
		if existing.Status != next.Status {
			if err = admin.SetEnabled(context.Background(), existing.KeycloakID, next.Status == "active"); err != nil {
				return User{}, err
			}
			if next.Status != "active" {
				_ = admin.Logout(context.Background(), existing.KeycloakID)
			}
		}
		if existing.DisplayName != next.DisplayName || existing.Email != next.Email {
			if err = admin.UpdateProfile(context.Background(), existing.KeycloakID, next.DisplayName, next.Email); err != nil {
				return User{}, err
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.users {
		if s.users[index].ID != id {
			continue
		}
		if s.db != nil {
			if err := upsertSystemUser(context.Background(), s.db, next); err != nil {
				return User{}, err
			}
		}
		s.users[index] = next
		s.audit(actor, "更新用户权限", next.Username, "角色和数据范围已更新")
		return cloneUser(next), nil
	}
	return User{}, ErrNotFound
}

func (s *Service) DeleteUser(id, actor string) (User, error) {
	var existing User
	found := false
	for _, item := range s.Users() {
		if item.ID == id {
			existing = item
			found = true
			break
		}
	}
	if !found {
		return User{}, ErrNotFound
	}
	if existing.Username == actor {
		return User{}, ErrForbidden
	}
	if hasValue(existing.Roles, "platform-admin") && s.adminCount() <= 1 {
		return User{}, ErrConflict
	}
	if s.db != nil && existing.KeycloakID != "" {
		admin, err := newKeycloakAdmin(context.Background())
		if err != nil {
			return User{}, err
		}
		if err = admin.SetEnabled(context.Background(), existing.KeycloakID, false); err != nil {
			return User{}, err
		}
		_ = admin.Logout(context.Background(), existing.KeycloakID)
	}
	existing.Status = "deleted"
	existing.Roles = nil
	existing.DataScope = "none"
	existing.ProjectGroups = nil
	existing.ScopeTags = nil
	existing.KeycloakGroups = nil
	existing.PermissionOverrides = nil
	existing.DeletedAt = time.Now().Format("2006-01-02 15:04:05")
	existing.DeletedBy = actor

	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.users {
		if s.users[index].ID != id {
			continue
		}
		if s.db != nil {
			if err := upsertSystemUser(context.Background(), s.db, existing); err != nil {
				return User{}, err
			}
		}
		s.users[index] = existing
		s.audit(actor, "删除用户", existing.Username, "账号已禁用并进入回收站")
		return cloneUser(existing), nil
	}
	return User{}, ErrNotFound
}

func (s *Service) ResetUserPassword(id, actor string) (string, error) {
	var existing User
	found := false
	for _, item := range s.Users() {
		if item.ID == id {
			existing = item
			found = true
			break
		}
	}
	if !found {
		return "", ErrNotFound
	}
	password, err := randomPassword(20)
	if err != nil {
		return "", err
	}
	if s.db != nil && existing.KeycloakID != "" {
		admin, adminErr := newKeycloakAdmin(context.Background())
		if adminErr != nil {
			return "", adminErr
		}
		if err = admin.ResetPassword(context.Background(), existing.KeycloakID, password); err != nil {
			return "", err
		}
		_ = admin.Logout(context.Background(), existing.KeycloakID)
	}
	s.mu.Lock()
	s.audit(actor, "重置用户密码", existing.Username, "已生成一次性密码")
	s.mu.Unlock()
	return password, nil
}

func (s *Service) CreateRole(input RoleCreateInput, actor string) (Role, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || !validDataScope(input.DataScope) {
		return Role{}, ErrValidation
	}
	permissions, err := normalizePermissions(input.Permissions, false)
	if err != nil {
		return Role{}, err
	}
	for _, role := range s.Roles() {
		if strings.EqualFold(role.Name, name) {
			return Role{}, ErrConflict
		}
	}
	item := Role{
		ID: newSystemID("role"), Name: name, Description: strings.TrimSpace(input.Description),
		Permissions: permissions, DataScope: input.DataScope,
		ProjectGroups: mergeValues(nil, input.ProjectGroups), ScopeTags: mergeValues(nil, input.ScopeTags),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		if err = upsertSystemRole(context.Background(), s.db, item); err != nil {
			return Role{}, err
		}
	}
	s.roles = append(s.roles, item)
	s.audit(actor, "创建角色", item.Name, "权限: "+strings.Join(item.Permissions, ","))
	item.System = false
	return cloneRole(item), nil
}

func (s *Service) DeleteRole(id, actor string) error {
	if isSystemRole(id) {
		return ErrForbidden
	}
	for _, user := range s.Users() {
		if hasValue(user.Roles, id) {
			return ErrConflict
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.roles {
		if s.roles[index].ID != id {
			continue
		}
		if s.db != nil {
			if _, err := s.db.ExecContext(context.Background(), `DELETE FROM platform_roles WHERE id=$1`, id); err != nil {
				return err
			}
		}
		removed := s.roles[index]
		s.roles = append(s.roles[:index], s.roles[index+1:]...)
		s.audit(actor, "删除角色", removed.Name, "角色已删除")
		return nil
	}
	return ErrNotFound
}

func (s *Service) UsersWithRole(roleID string) []string {
	result := []string{}
	for _, user := range s.Users() {
		if hasValue(user.Roles, roleID) {
			result = append(result, user.Username)
		}
	}
	return result
}

func (s *Service) validateRoleIDs(values []string) ([]string, error) {
	byID := map[string]Role{}
	for _, role := range s.Roles() {
		byID[role.ID] = role
	}
	result := []string{}
	for _, value := range mergeValues(nil, values) {
		if _, ok := byID[value]; !ok {
			return nil, ErrValidation
		}
		result = append(result, value)
	}
	return result, nil
}

func (s *Service) adminCount() int {
	count := 0
	for _, user := range s.Users() {
		if hasValue(user.Roles, "platform-admin") {
			count++
		}
	}
	return count
}

func validDataScope(value string) bool {
	switch value {
	case "all", "groups", "tags", "none":
		return true
	}
	return false
}

func newSystemID(prefix string) string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return prefix + "-" + time.Now().Format("20060102150405")
	}
	return prefix + "-" + hex.EncodeToString(buffer)
}

func randomPassword(length int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#%"
	buffer := make([]byte, length)
	random := make([]byte, length)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for index := range buffer {
		buffer[index] = alphabet[int(random[index])%len(alphabet)]
	}
	return string(buffer), nil
}

func normalizePermissionOverrides(values []PermissionOverride) ([]PermissionOverride, error) {
	result := []PermissionOverride{}
	seen := map[string]string{}
	for _, item := range values {
		code := strings.TrimSpace(item.Code)
		effect := strings.ToLower(strings.TrimSpace(item.Effect))
		if code == "" {
			continue
		}
		if !validPermission(code) || (effect != "allow" && effect != "deny") {
			return nil, ErrValidation
		}
		if previous, ok := seen[code]; ok && previous != effect {
			return nil, ErrValidation
		}
		seen[code] = effect
		result = append(result, PermissionOverride{Code: code, Effect: effect, Reason: strings.TrimSpace(item.Reason)})
	}
	return result, nil
}

func validateAdminOverrides(roles []string, overrides []PermissionOverride) error {
	if !hasValue(roles, "platform-admin") {
		return nil
	}
	for _, item := range overrides {
		if item.Code == "system:manage" && item.Effect == "deny" {
			return ErrForbidden
		}
	}
	return nil
}

func hasValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
