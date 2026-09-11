package jobs

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var playbookVariablePattern = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_.-]*)\s*\}\}`)

func playbookID() string { return fmt.Sprintf("pb-%d", time.Now().UnixNano()) }

func (s *Service) Playbooks() []Playbook {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Playbook, 0, len(s.playbooks))
	for _, item := range s.playbooks {
		out = append(out, clonePlaybook(item))
	}
	return out
}

func (s *Service) playbookByID(id string) (Playbook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.playbooks {
		if item.ID == id {
			return clonePlaybook(item), nil
		}
	}
	return Playbook{}, ErrNotFound
}

func validatePlaybookInput(input PlaybookInput) (PlaybookInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Content = strings.TrimSpace(input.Content)
	if input.Name == "" || input.Content == "" || len(input.Content) > 20000 {
		return input, ErrValidation
	}
	if input.Constants == nil {
		input.Constants = map[string]string{}
	}
	if input.Variables == nil {
		input.Variables = map[string]string{}
	}
	if len(input.Constants) > 100 || len(input.Variables) > 100 {
		return input, ErrValidation
	}
	for key, value := range input.Constants {
		if !playbookVariablePattern.MatchString("{{"+key+"}}") || len(value) > 2000 {
			return input, ErrValidation
		}
	}
	for key, value := range input.Variables {
		if !playbookVariablePattern.MatchString("{{"+key+"}}") || len(value) > 2000 {
			return input, ErrValidation
		}
	}
	if input.Enabled == nil {
		enabled := true
		input.Enabled = &enabled
	}
	return input, nil
}

func (s *Service) CreatePlaybook(input PlaybookInput, createdBy string) (Playbook, error) {
	input, err := validatePlaybookInput(input)
	if err != nil {
		return Playbook{}, err
	}
	item := Playbook{ID: playbookID(), Name: input.Name, Description: input.Description, Content: input.Content, Constants: cloneStringMap(input.Constants), Variables: cloneStringMap(input.Variables), Enabled: *input.Enabled, CreatedBy: createdBy}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.playbooks = append([]Playbook{item}, s.playbooks...)
	if s.db != nil {
		_ = upsertPlaybook(context.Background(), s.db, item)
	}
	return clonePlaybook(item), nil
}

func (s *Service) UpdatePlaybook(id string, input PlaybookInput) (Playbook, error) {
	input, err := validatePlaybookInput(input)
	if err != nil {
		return Playbook{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.playbooks {
		if s.playbooks[i].ID == id {
			s.playbooks[i].Name = input.Name
			s.playbooks[i].Description = input.Description
			s.playbooks[i].Content = input.Content
			s.playbooks[i].Constants = cloneStringMap(input.Constants)
			s.playbooks[i].Variables = cloneStringMap(input.Variables)
			s.playbooks[i].Enabled = *input.Enabled
			if s.db != nil {
				_ = upsertPlaybook(context.Background(), s.db, s.playbooks[i])
			}
			return clonePlaybook(s.playbooks[i]), nil
		}
	}
	return Playbook{}, ErrNotFound
}

func (s *Service) TogglePlaybook(id string) (Playbook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.playbooks {
		if s.playbooks[i].ID == id {
			s.playbooks[i].Enabled = !s.playbooks[i].Enabled
			if s.db != nil {
				_ = upsertPlaybook(context.Background(), s.db, s.playbooks[i])
			}
			return clonePlaybook(s.playbooks[i]), nil
		}
	}
	return Playbook{}, ErrNotFound
}

func (s *Service) DeletePlaybook(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.playbooks {
		if s.playbooks[i].ID == id {
			s.playbooks = append(s.playbooks[:i], s.playbooks[i+1:]...)
			if s.db != nil {
				return deletePlaybookRow(context.Background(), s.db, id)
			}
			return nil
		}
	}
	return ErrNotFound
}
func clonePlaybook(item Playbook) Playbook {
	item.Constants = cloneStringMap(item.Constants)
	item.Variables = cloneStringMap(item.Variables)
	return item
}

func cloneStringMap(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func compilePlaybook(playbook Playbook, overrides map[string]string) (string, error) {
	values := cloneStringMap(playbook.Constants)
	for key, value := range playbook.Variables {
		values[key] = value
	}
	for key, value := range overrides {
		if _, constant := playbook.Constants[key]; constant {
			return "", fmt.Errorf("constant %s cannot be overridden", key)
		}
		if _, defined := playbook.Variables[key]; !defined {
			return "", fmt.Errorf("variable %s is not defined", key)
		}
		values[key] = value
	}
	var missing string
	result := playbookVariablePattern.ReplaceAllStringFunc(playbook.Content, func(match string) string {
		pieces := playbookVariablePattern.FindStringSubmatch(match)
		if len(pieces) != 2 {
			return match
		}
		if value, ok := values[pieces[1]]; ok {
			return value
		}
		missing = pieces[1]
		return match
	})
	if missing != "" {
		return "", fmt.Errorf("missing playbook variable %s", missing)
	}
	return result, nil
}

func (s *Service) commandForJobLocked(job Job) string {
	if job.PlaybookID != "" {
		for _, playbook := range s.playbooks {
			if playbook.ID == job.PlaybookID {
				command, _ := compilePlaybook(playbook, job.Variables)
				return command
			}
		}
		return ""
	}
	for _, template := range s.templates {
		if template.ID == job.TemplateID {
			return template.Command
		}
	}
	return ""
}
