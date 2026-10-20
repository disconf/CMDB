package system

import (
	"context"
	"strings"
	"time"
)

type AccessProfile struct {
	DataScope         string
	ProjectGroups     []string
	ScopeTags         []string
	Permissions       []string
	DeniedPermissions []string
}

func (s *Service) AccessProfile(username string) (AccessProfile, error) {
	username = strings.TrimSpace(username)
	s.mu.RLock()
	stale := s.db != nil && time.Since(s.lastReload) > 10*time.Second
	users := cloneUsers(s.users)
	roles := cloneRoles(s.roles)
	s.mu.RUnlock()
	if stale {
		if err := s.reload(context.Background()); err == nil {
			s.mu.RLock()
			users = cloneUsers(s.users)
			roles = cloneRoles(s.roles)
			s.mu.RUnlock()
		}
	}
	byRole := make(map[string]Role, len(roles))
	for _, role := range roles {
		byRole[role.ID] = role
	}
	for _, user := range users {
		if user.Username != username {
			continue
		}
		if user.Status == "deleted" || user.Status == "disabled" || user.Status == "locked" {
			return AccessProfile{}, ErrNotFound
		}
		profile := AccessProfile{DataScope: user.DataScope, ProjectGroups: append([]string(nil), user.ProjectGroups...), ScopeTags: append([]string(nil), user.ScopeTags...), Permissions: []string{}, DeniedPermissions: []string{}}
		profile.ProjectGroups = mergeValues(profile.ProjectGroups, user.KeycloakGroups)
		for _, roleID := range user.Roles {
			role, ok := byRole[roleID]
			if !ok {
				continue
			}
			if containsValueIn(role.Permissions, "*") {
				for _, permission := range permissionCatalog {
					profile.Permissions = mergeValues(profile.Permissions, []string{permission.Code})
				}
			} else {
				profile.Permissions = mergeValues(profile.Permissions, role.Permissions)
			}
			profile.ProjectGroups = mergeValues(profile.ProjectGroups, role.ProjectGroups)
			profile.ScopeTags = mergeValues(profile.ScopeTags, role.ScopeTags)
			if role.DataScope == "all" {
				profile.DataScope = "all"
			}
		}
		for _, override := range user.PermissionOverrides {
			switch override.Effect {
			case "allow":
				profile.Permissions = mergeValues(profile.Permissions, []string{override.Code})
			case "deny":
				profile.Permissions = removeValue(profile.Permissions, override.Code)
				profile.DeniedPermissions = mergeValues(profile.DeniedPermissions, []string{override.Code})
			}
		}
		if profile.DataScope == "" {
			profile.DataScope = "groups"
		}
		return profile, nil
	}
	return AccessProfile{}, ErrNotFound
}

func removeValue(values []string, target string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}

func containsValueIn(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Service) UpdateRoleProfile(id string, input RoleUpdateInput, actor string) (Role, error) {
	var existing Role
	found := false
	for _, item := range s.Roles() {
		if item.ID == id {
			existing = item
			found = true
			break
		}
	}
	if !found {
		return Role{}, ErrNotFound
	}
	if name := strings.TrimSpace(input.Name); name != "" {
		existing.Name = name
	}
	if input.Description != "" {
		existing.Description = strings.TrimSpace(input.Description)
	}
	for _, role := range s.Roles() {
		if role.ID != id && strings.EqualFold(role.Name, existing.Name) {
			return Role{}, ErrConflict
		}
	}
	if input.Permissions != nil {
		if id == "platform-admin" {
			existing.Permissions = []string{"*"}
		} else {
			permissions, err := normalizePermissions(input.Permissions, false)
			if err != nil {
				return Role{}, err
			}
			existing.Permissions = permissions
		}
	}
	if input.DataScope != "" {
		if !validDataScope(input.DataScope) {
			return Role{}, ErrValidation
		}
		existing.DataScope = input.DataScope
	}
	if input.ProjectGroups != nil {
		existing.ProjectGroups = mergeValues(nil, input.ProjectGroups)
	}
	if input.ScopeTags != nil {
		existing.ScopeTags = mergeValues(nil, input.ScopeTags)
	}
	if existing.ID == "" || existing.Name == "" {
		return Role{}, ErrValidation
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.roles {
		if s.roles[index].ID != id {
			continue
		}
		if s.db != nil {
			if err := upsertSystemRole(context.Background(), s.db, existing); err != nil {
				return Role{}, err
			}
		}
		s.roles[index] = existing
		s.audit(actor, "更新角色权限", existing.Name, "权限和数据范围已更新")
		existing.System = isSystemRole(existing.ID)
		return cloneRole(existing), nil
	}
	return Role{}, ErrNotFound
}

func mergeValues(base, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	result := make([]string, 0, len(base)+len(extra))
	for _, value := range append(append([]string(nil), base...), extra...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func cloneUser(item User) User {
	item.Roles = append([]string(nil), item.Roles...)
	item.ProjectGroups = append([]string(nil), item.ProjectGroups...)
	item.ScopeTags = append([]string(nil), item.ScopeTags...)
	item.KeycloakGroups = append([]string(nil), item.KeycloakGroups...)
	item.PermissionOverrides = append([]PermissionOverride(nil), item.PermissionOverrides...)
	return item
}

func cloneUsers(items []User) []User {
	result := make([]User, len(items))
	for index := range items {
		result[index] = cloneUser(items[index])
	}
	return result
}

func cloneRole(item Role) Role {
	item.Permissions = append([]string(nil), item.Permissions...)
	item.ProjectGroups = append([]string(nil), item.ProjectGroups...)
	item.ScopeTags = append([]string(nil), item.ScopeTags...)
	return item
}

func cloneRoles(items []Role) []Role {
	result := make([]Role, len(items))
	for index := range items {
		result[index] = cloneRole(items[index])
	}
	return result
}

func (s *Service) RecordAudit(actor, action, resource, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit(actor, action, resource, detail)
}

type UserUpdateInput struct {
	DisplayName         string               `json:"displayName"`
	Department          string               `json:"department"`
	Email               string               `json:"email"`
	Roles               []string             `json:"roles"`
	Status              string               `json:"status"`
	DataScope           string               `json:"dataScope"`
	ProjectGroups       []string             `json:"projectGroups"`
	ScopeTags           []string             `json:"scopeTags"`
	PermissionOverrides []PermissionOverride `json:"permissionOverrides"`
}

type RoleUpdateInput struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Permissions   []string `json:"permissions"`
	DataScope     string   `json:"dataScope"`
	ProjectGroups []string `json:"projectGroups"`
	ScopeTags     []string `json:"scopeTags"`
}
