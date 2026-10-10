package aiops

import (
	"context"
	"strings"
	"time"
)

func (s *Service) ChangeControl() ChangeControl {
	s.mu.Lock()
	defer s.mu.Unlock()
	control := s.changeControl
	if control.Frozen && strings.TrimSpace(control.FreezeUntil) != "" {
		if until, err := parseAITime(control.FreezeUntil); err == nil && time.Now().After(until) {
			control.Frozen = false
			control.Reason = ""
			control.FreezeUntil = ""
			control.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
			s.changeControl = control
			_ = s.repo.SaveChangeControl(context.Background(), control)
		}
	}
	return control
}

func (s *Service) UpdateChangeControl(ctx context.Context, input ChangeControl, user string) (ChangeControl, error) {
	s.mu.Lock()
	current := s.changeControl
	s.mu.Unlock()
	if input.Frozen && strings.TrimSpace(input.Reason) == "" {
		return ChangeControl{}, ErrValidation
	}
	if !current.LiveEnabled && input.LiveEnabled && !strings.EqualFold(strings.TrimSpace(getenv("AIOPS_ALLOW_CHANGE_RUNBOOKS")), "true") {
		return ChangeControl{}, ErrChangeDisabled
	}
	input.ID = "default"
	input.Reason = strings.TrimSpace(input.Reason)
	input.FreezeUntil = strings.TrimSpace(input.FreezeUntil)
	input.UpdatedBy = strings.TrimSpace(user)
	input.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	if input.FreezeUntil != "" {
		if _, err := parseAITime(input.FreezeUntil); err != nil {
			return ChangeControl{}, ErrValidation
		}
	}
	if err := s.repo.SaveChangeControl(ctx, input); err != nil {
		return ChangeControl{}, err
	}
	s.mu.Lock()
	s.changeControl = input
	s.mu.Unlock()
	return input, nil
}

func (s *Service) changeControlBlocked(now time.Time) (bool, string) {
	control := s.ChangeControl()
	if !control.LiveEnabled {
		return true, "平台真实变更总开关已关闭"
	}
	if !control.Frozen {
		return false, ""
	}
	if strings.TrimSpace(control.FreezeUntil) != "" {
		if until, err := parseAITime(control.FreezeUntil); err == nil && now.After(until) {
			return false, ""
		}
	}
	reason := strings.TrimSpace(control.Reason)
	if reason == "" {
		reason = "未填写冻结原因"
	}
	if control.FreezeUntil != "" {
		return true, "变更冻结至 " + control.FreezeUntil + "：" + reason
	}
	return true, "变更已冻结：" + reason
}
