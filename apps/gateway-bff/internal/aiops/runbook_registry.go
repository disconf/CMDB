package aiops

import (
	"context"
	"os"
	"strings"
	"time"
)

func (s *Service) UpdateRunbook(ctx context.Context, runbookID string, input RunbookPolicy, user string) (RunbookPolicy, error) {
	runbookID = strings.TrimSpace(runbookID)
	if runbookID == "" {
		return RunbookPolicy{}, ErrValidation
	}
	if s.jobs == nil {
		return RunbookPolicy{}, ErrNotReady
	}
	input.RunbookID = runbookID
	input.Title = strings.TrimSpace(input.Title)
	input.TemplateID = strings.TrimSpace(input.TemplateID)
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	input.Risk = strings.ToLower(strings.TrimSpace(input.Risk))
	input.ExecutionMode = strings.ToLower(strings.TrimSpace(input.ExecutionMode))
	input.ChangeWindow = strings.TrimSpace(input.ChangeWindow)
	input.RollbackRunbookID = strings.TrimSpace(input.RollbackRunbookID)
	if input.ChangeWindow == "" {
		input.ChangeWindow = "*"
	}
	if input.ExecutionMode == "" {
		if input.Mode == "change" {
			input.ExecutionMode = "simulate"
		} else {
			input.ExecutionMode = "live"
		}
	}
	if input.MaxTargets <= 0 {
		input.MaxTargets = 1
	}
	if input.Title == "" || input.TemplateID == "" || input.MaxTargets < 1 || input.MaxTargets > 10 {
		return RunbookPolicy{}, ErrValidation
	}
	if input.Mode != "read-only" && input.Mode != "change" {
		return RunbookPolicy{}, ErrValidation
	}
	if input.Risk != "low" && input.Risk != "medium" && input.Risk != "high" {
		return RunbookPolicy{}, ErrValidation
	}
	if input.Mode == "read-only" {
		input.ExecutionMode = "live"
		input.LiveDailyLimit = 0
		input.LiveCooldownMinutes = 0
		input.RequireRollback = false
		input.RequireCanary = false
	} else {
		input.RequireCanary = true
	}
	if input.Mode == "change" && (input.LiveDailyLimit < 0 || input.LiveDailyLimit > 50 || input.LiveCooldownMinutes < 0 || input.LiveCooldownMinutes > 1440) {
		return RunbookPolicy{}, ErrValidation
	}
	if input.Mode == "change" && input.ExecutionMode != "simulate" && input.ExecutionMode != "live" {
		return RunbookPolicy{}, ErrValidation
	}
	if input.Mode == "change" && input.ExecutionMode == "live" && input.Enabled && !strings.EqualFold(strings.TrimSpace(os.Getenv("AIOPS_ALLOW_CHANGE_RUNBOOKS")), "true") {
		return RunbookPolicy{}, ErrChangeDisabled
	}
	templates := s.jobs.Templates()
	found := false
	for _, template := range templates {
		if template.ID == input.TemplateID {
			found = true
			break
		}
	}
	if !found {
		return RunbookPolicy{}, ErrValidation
	}
	if input.RequireRollback && input.ExecutionMode == "live" && input.Enabled && input.RollbackRunbookID == "" {
		return RunbookPolicy{}, ErrValidation
	}
	if input.RollbackRunbookID != "" {
		if input.Mode != "change" || input.RollbackRunbookID == runbookID {
			return RunbookPolicy{}, ErrValidation
		}
		rollbackPolicy, ok := s.policyFor(input.RollbackRunbookID)
		if !ok || rollbackPolicy.Mode != "change" {
			return RunbookPolicy{}, ErrValidation
		}
		rollbackTemplateFound := false
		for _, template := range templates {
			if template.ID == rollbackPolicy.TemplateID {
				rollbackTemplateFound = true
				break
			}
		}
		if !rollbackTemplateFound {
			return RunbookPolicy{}, ErrValidation
		}
	}
	environments := normalizeEnvironments(input.AllowedEnvironments)
	if len(environments) == 0 {
		environments = []string{"*"}
	}
	input.AllowedEnvironments = environments
	input.ApprovalRequired = true
	input.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	input.UpdatedBy = strings.TrimSpace(user)
	if _, ok := s.policyFor(runbookID); !ok {
		return RunbookPolicy{}, ErrRunbookNotFound
	}
	if err := s.repo.SaveRunbookPolicy(ctx, input); err != nil {
		return RunbookPolicy{}, err
	}
	s.mu.Lock()
	for i := range s.runbooks {
		if s.runbooks[i].RunbookID == runbookID {
			s.runbooks[i] = input
			break
		}
	}
	s.mu.Unlock()
	return input, nil
}

func (s *Service) ToggleRunbook(ctx context.Context, runbookID, user string) (RunbookPolicy, error) {
	item, ok := s.policyFor(runbookID)
	if !ok {
		return RunbookPolicy{}, ErrRunbookNotFound
	}
	item.Enabled = !item.Enabled
	return s.UpdateRunbook(ctx, runbookID, item, user)
}

func normalizeEnvironments(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
