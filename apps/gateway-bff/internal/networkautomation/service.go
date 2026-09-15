package networkautomation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

var ErrNotFound = errors.New("automation policy not found")
var ErrValidation = errors.New("automation policy validation")

type Service struct {
	mu        sync.RWMutex
	db        *sql.DB
	cmdb      *cmdb.Service
	tickets   *tickets.Service
	monitor   *monitor.Service
	jobs      *jobs.Service
	policies  []Policy
	lifecycle map[string]Lifecycle
}

func defaultPolicy() Policy {
	now := time.Now().Format("2006-01-02 15:04:05")
	return Policy{ID: "default-critical-network", Name: "严重网络异常自动闭环", Enabled: true, Priority: 10, Severity: "critical", AnomalyType: "*", AutoTicket: true, Assignee: "网络运维组", EscalationMinutes: 30, EscalateTo: "网络负责人", VerificationTemplate: "tpl-health", CreatedAt: now, UpdatedAt: now}
}

func NewService(cmdbService *cmdb.Service, ticketsService *tickets.Service, monitorService *monitor.Service, jobsService *jobs.Service) *Service {
	service := &Service{cmdb: cmdbService, tickets: ticketsService, monitor: monitorService, jobs: jobsService, policies: []Policy{defaultPolicy()}, lifecycle: map[string]Lifecycle{}}
	if url := strings.TrimSpace(os.Getenv("DATABASE_URL")); url != "" {
		db, err := openPostgres(url)
		if err != nil {
			panic(fmt.Sprintf("initialize network automation repository: %v", err))
		}
		service.db = db
		policies, loadErr := loadPolicies(context.Background(), db)
		if loadErr != nil {
			panic(fmt.Sprintf("load network automation policies: %v", loadErr))
		}
		if len(policies) == 0 {
			policies = []Policy{defaultPolicy()}
			for _, item := range policies {
				_ = upsertPolicy(context.Background(), db, item)
			}
		}
		service.policies = policies
		lifecycle, lifecycleErr := loadLifecycle(context.Background(), db)
		if lifecycleErr != nil {
			panic(fmt.Sprintf("load network anomaly lifecycle: %v", lifecycleErr))
		}
		service.lifecycle = lifecycle
	}
	return service
}

func (s *Service) Policies() []Policy {
	if s.db != nil {
		if items, err := loadPolicies(context.Background(), s.db); err == nil {
			s.mu.Lock()
			s.policies = items
			s.mu.Unlock()
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]Policy(nil), s.policies...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func (s *Service) CreatePolicy(input PolicyInput) (Policy, error) {
	item, err := buildPolicy(input, Policy{ID: fmt.Sprintf("network-policy-%d", time.Now().UnixNano())})
	if err != nil {
		return Policy{}, err
	}
	if s.db != nil {
		if err := upsertPolicy(context.Background(), s.db, item); err != nil {
			return Policy{}, err
		}
	}
	s.mu.Lock()
	s.policies = append([]Policy{item}, s.policies...)
	s.mu.Unlock()
	return item, nil
}

func (s *Service) UpdatePolicy(id string, input PolicyInput) (Policy, error) {
	current, err := s.policyByID(id)
	if err != nil {
		return Policy{}, err
	}
	item, err := buildPolicy(input, current)
	if err != nil {
		return Policy{}, err
	}
	if s.db != nil {
		if err := upsertPolicy(context.Background(), s.db, item); err != nil {
			return Policy{}, err
		}
	}
	s.mu.Lock()
	for i := range s.policies {
		if s.policies[i].ID == id {
			s.policies[i] = item
		}
	}
	s.mu.Unlock()
	return item, nil
}

func (s *Service) DeletePolicy(id string) error {
	if _, err := s.policyByID(id); err != nil {
		return err
	}
	if id == "default-critical-network" {
		return ErrValidation
	}
	if s.db != nil {
		if _, err := s.db.ExecContext(context.Background(), `DELETE FROM cmdb_network_automation_policies WHERE id=$1`, id); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.policies[:0]
	for _, item := range s.policies {
		if item.ID != id {
			filtered = append(filtered, item)
		}
	}
	s.policies = filtered
	return nil
}

func (s *Service) Lifecycle() []Lifecycle {
	if s.db != nil {
		if items, err := loadLifecycle(context.Background(), s.db); err == nil {
			s.mu.Lock()
			s.lifecycle = items
			s.mu.Unlock()
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Lifecycle, 0, len(s.lifecycle))
	for _, item := range s.lifecycle {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LastSeenAt > result[j].LastSeenAt })
	return result
}

func (s *Service) policyByID(id string) (Policy, error) {
	for _, item := range s.Policies() {
		if item.ID == id {
			return item, nil
		}
	}
	return Policy{}, ErrNotFound
}

func buildPolicy(input PolicyInput, current Policy) (Policy, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Policy{}, ErrValidation
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	item := current
	item.Name = name
	item.Enabled = true
	if input.Enabled != nil {
		item.Enabled = *input.Enabled
	}
	if item.ID == "" {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	item.Priority = input.Priority
	if item.Priority == 0 {
		item.Priority = 10
	}
	item.Severity = firstValue(input.Severity, "critical")
	item.AnomalyType = firstValue(input.AnomalyType, "*")
	item.Environment = strings.TrimSpace(input.Environment)
	item.ProjectGroup = strings.TrimSpace(input.ProjectGroup)
	item.Location = strings.TrimSpace(input.Location)
	item.AutoTicket = input.AutoTicket
	item.Assignee = strings.TrimSpace(input.Assignee)
	item.EscalationMinutes = input.EscalationMinutes
	if item.EscalationMinutes < 0 {
		return Policy{}, ErrValidation
	}
	item.EscalateTo = strings.TrimSpace(input.EscalateTo)
	item.VerificationTemplate = strings.TrimSpace(input.VerificationTemplate)
	return item, nil
}

func firstValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
