package system

import (
	"context"
	"strings"
	"time"
)

type AccessProfile struct {
	DataScope     string
	ProjectGroups []string
	ScopeTags     []string
	Permissions   []string
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
		profile := AccessProfile{DataScope: user.DataScope, ProjectGroups: append([]string(nil), user.ProjectGroups...), ScopeTags: append([]string(nil), user.ScopeTags...), Permissions: []string{}}
		profile.ProjectGroups = mergeValues(profile.ProjectGroups, user.KeycloakGroups)
		for _, roleID := range user.Roles {
			role, ok := byRole[roleID]
			if !ok {
				continue
			}
			profile.Permissions = mergeValues(profile.Permissions, role.Permissions)
			profile.ProjectGroups = mergeValues(profile.ProjectGroups, role.ProjectGroups)
			profile.ScopeTags = mergeValues(profile.ScopeTags, role.ScopeTags)
			if role.DataScope == "all" {
				profile.DataScope = "all"
			}
		}
		if profile.DataScope == "" {
			profile.DataScope = "groups"
		}
		return profile, nil
	}
	return AccessProfile{}, ErrNotFound
}

func (s *Service) UpdateUser(id string, input UserUpdateInput, actor string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.users {
		if s.users[index].ID != id {
			continue
		}
		item := s.users[index]
		if strings.TrimSpace(input.DisplayName) != "" {
			item.DisplayName = strings.TrimSpace(input.DisplayName)
		}
		item.Department = strings.TrimSpace(input.Department)
		item.Email = strings.TrimSpace(input.Email)
		if len(input.Roles) > 0 {
			item.Roles = mergeValues(nil, input.Roles)
		}
		if input.Status != "" {
			item.Status = input.Status
		}
		if input.DataScope != "" {
			item.DataScope = input.DataScope
		}
		item.ProjectGroups = mergeValues(nil, input.ProjectGroups)
		item.ScopeTags = mergeValues(nil, input.ScopeTags)
		if item.Username == "" || item.ID == "" {
			return User{}, ErrValidation
		}
		if s.db != nil {
			if err := upsertSystemUser(context.Background(), s.db, item); err != nil {
				return User{}, err
			}
		}
		s.users[index] = item
		s.audit(actor, "更新用户权限", item.Username, "数据范围和角色已更新")
		return cloneUser(item), nil
	}
	return User{}, ErrNotFound
}

func (s *Service) UpdateRoleProfile(id string, input RoleUpdateInput, actor string) (Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.roles {
		if s.roles[index].ID != id {
			continue
		}
		item := s.roles[index]
		if input.Permissions != nil {
			item.Permissions = mergeValues(nil, input.Permissions)
		}
		if input.DataScope != "" {
			item.DataScope = input.DataScope
		}
		if input.ProjectGroups != nil {
			item.ProjectGroups = mergeValues(nil, input.ProjectGroups)
		}
		if input.ScopeTags != nil {
			item.ScopeTags = mergeValues(nil, input.ScopeTags)
		}
		if item.ID == "" || item.Name == "" {
			return Role{}, ErrValidation
		}
		if s.db != nil {
			if err := upsertSystemRole(context.Background(), s.db, item); err != nil {
				return Role{}, err
			}
		}
		s.roles[index] = item
		s.audit(actor, "更新角色权限", item.Name, "权限和数据范围已更新")
		return cloneRole(item), nil
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
	DisplayName   string   `json:"displayName"`
	Department    string   `json:"department"`
	Email         string   `json:"email"`
	Roles         []string `json:"roles"`
	Status        string   `json:"status"`
	DataScope     string   `json:"dataScope"`
	ProjectGroups []string `json:"projectGroups"`
	ScopeTags     []string `json:"scopeTags"`
}

type RoleUpdateInput struct {
	Permissions   []string `json:"permissions"`
	DataScope     string   `json:"dataScope"`
	ProjectGroups []string `json:"projectGroups"`
	ScopeTags     []string `json:"scopeTags"`
}
