package networkautomation

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

type ComplianceRule struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Action         string `json:"action"`
	Type           string `json:"type"`
	Value          string `json:"value"`
	Environment    string `json:"environment"`
	ProjectGroup   string `json:"projectGroup"`
	Location       string `json:"location"`
	VLAN           string `json:"vlan"`
	Status         string `json:"status"`
	Description    string `json:"description"`
	ExpiresAt      string `json:"expiresAt,omitempty"`
	ReminderHours  int    `json:"reminderHours"`
	LastRemindedAt string `json:"lastRemindedAt,omitempty"`
	CreatedBy      string `json:"createdBy"`
	ApprovedBy     string `json:"approvedBy,omitempty"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

type ComplianceRuleInput struct {
	Name          string `json:"name"`
	Action        string `json:"action"`
	Type          string `json:"type"`
	Value         string `json:"value"`
	Environment   string `json:"environment"`
	ProjectGroup  string `json:"projectGroup"`
	Location      string `json:"location"`
	VLAN          string `json:"vlan"`
	Description   string `json:"description"`
	ExpiresAt     string `json:"expiresAt"`
	ReminderHours int    `json:"reminderHours"`
}

type ComplianceDecision struct {
	ID           string `json:"id"`
	AnomalyID    string `json:"anomalyId"`
	RuleID       string `json:"ruleId,omitempty"`
	RuleName     string `json:"ruleName,omitempty"`
	Status       string `json:"status"`
	Reason       string `json:"reason"`
	Title        string `json:"title"`
	IP           string `json:"ip,omitempty"`
	MACAddress   string `json:"macAddress,omitempty"`
	VLAN         string `json:"vlan,omitempty"`
	Environment  string `json:"environment,omitempty"`
	ProjectGroup string `json:"projectGroup,omitempty"`
	Location     string `json:"location,omitempty"`
	FirstSeenAt  string `json:"firstSeenAt"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
}

func (s *Service) ComplianceRules() []ComplianceRule {
	if s.db != nil {
		if items, err := loadComplianceRules(context.Background(), s.db); err == nil {
			s.mu.Lock()
			s.complianceRules = items
			s.mu.Unlock()
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]ComplianceRule(nil), s.complianceRules...)
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result
}

func (s *Service) CreateComplianceRule(input ComplianceRuleInput, operator string) (ComplianceRule, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	item := ComplianceRule{ID: fmt.Sprintf("compliance-%d", time.Now().UnixNano()), Name: strings.TrimSpace(input.Name), Action: strings.TrimSpace(input.Action), Type: strings.TrimSpace(input.Type), Value: strings.TrimSpace(input.Value), Environment: strings.TrimSpace(input.Environment), ProjectGroup: strings.TrimSpace(input.ProjectGroup), Location: strings.TrimSpace(input.Location), VLAN: strings.TrimSpace(input.VLAN), Description: strings.TrimSpace(input.Description), ExpiresAt: strings.TrimSpace(input.ExpiresAt), ReminderHours: input.ReminderHours, Status: "pending", CreatedBy: operator, CreatedAt: now, UpdatedAt: now}
	if item.Name == "" || item.Value == "" {
		return ComplianceRule{}, ErrValidation
	}
	if item.Action == "" {
		item.Action = "allow"
	}
	if item.ReminderHours <= 0 {
		item.ReminderHours = 24
	}
	if item.Action == "temporary" && item.ExpiresAt == "" {
		return ComplianceRule{}, ErrValidation
	}
	if item.Type == "" {
		item.Type = "mac"
	}
	if !validComplianceRule(item) {
		return ComplianceRule{}, ErrValidation
	}
	if s.db != nil {
		if err := upsertComplianceRule(context.Background(), s.db, item); err != nil {
			return ComplianceRule{}, err
		}
	}
	s.mu.Lock()
	s.complianceRules = append([]ComplianceRule{item}, s.complianceRules...)
	s.mu.Unlock()
	return item, nil
}

func (s *Service) ApproveComplianceRule(id, operator string) (ComplianceRule, error) {
	item, err := s.complianceRuleByID(id)
	if err != nil {
		return ComplianceRule{}, err
	}
	item.Status = "approved"
	item.ApprovedBy = operator
	item.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	if s.db != nil {
		if err := upsertComplianceRule(context.Background(), s.db, item); err != nil {
			return ComplianceRule{}, err
		}
	}
	s.mu.Lock()
	for i := range s.complianceRules {
		if s.complianceRules[i].ID == id {
			s.complianceRules[i] = item
		}
	}
	s.mu.Unlock()
	return item, nil
}

func (s *Service) DeleteComplianceRule(id string) error {
	if _, err := s.complianceRuleByID(id); err != nil {
		return err
	}
	if s.db != nil {
		if _, err := s.db.ExecContext(context.Background(), `DELETE FROM cmdb_network_compliance_rules WHERE id=$1`, id); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.complianceRules[:0]
	for _, item := range s.complianceRules {
		if item.ID != id {
			filtered = append(filtered, item)
		}
	}
	s.complianceRules = filtered
	return nil
}

func (s *Service) complianceRuleByID(id string) (ComplianceRule, error) {
	for _, item := range s.ComplianceRules() {
		if item.ID == id {
			return item, nil
		}
	}
	return ComplianceRule{}, ErrNotFound
}

func validComplianceRule(item ComplianceRule) bool {
	switch item.Action {
	case "allow", "temporary", "deny":
	default:
		return false
	}
	switch item.Type {
	case "mac", "ip", "cidr", "vlan":
	default:
		return false
	}
	if item.Type == "cidr" && !strings.Contains(item.Value, "/") {
		return false
	}
	return true
}

func (s *Service) ComplianceDecisions() []ComplianceDecision {
	anomalies := s.cmdb.NetworkAnomalies()
	result := make([]ComplianceDecision, 0)
	for _, anomaly := range anomalies {
		decision := s.complianceDecision(anomaly)
		if decision.Status == "not-applicable" {
			continue
		}
		result = append(result, decision)
		if s.db != nil {
			_ = upsertComplianceDecision(context.Background(), s.db, decision)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].FirstSeenAt > result[j].FirstSeenAt })
	return result
}

func (s *Service) complianceDecision(anomaly cmdb.NetworkAnomaly) ComplianceDecision {
	now := time.Now().Format("2006-01-02 15:04:05")
	decision := ComplianceDecision{ID: "compliance-decision-" + strings.TrimPrefix(anomaly.ID, "net-anomaly-"), AnomalyID: anomaly.ID, Status: "unmatched", Reason: "没有匹配的合规规则", Title: anomaly.Title, IP: anomaly.IP, MACAddress: anomaly.MACAddress, VLAN: anomaly.VLAN, Environment: anomaly.Environment, ProjectGroup: anomaly.ProjectGroup, Location: anomaly.Location, FirstSeenAt: now}
	if anomaly.Type != "unmanaged-access" {
		decision.Status = "not-applicable"
		return decision
	}
	rules := s.ComplianceRules()
	sort.SliceStable(rules, func(i, j int) bool {
		return complianceActionRank(rules[i].Action) < complianceActionRank(rules[j].Action)
	})
	for _, rule := range rules {
		if rule.Status != "approved" || complianceExpired(rule, time.Now()) {
			continue
		}
		if rule.Environment != "" && rule.Environment != anomaly.Environment {
			continue
		}
		if rule.ProjectGroup != "" && rule.ProjectGroup != anomaly.ProjectGroup {
			continue
		}
		if rule.Location != "" && !strings.Contains(anomaly.Location, rule.Location) {
			continue
		}
		if rule.VLAN != "" && rule.VLAN != anomaly.VLAN {
			continue
		}
		if !complianceRuleMatches(rule, anomaly) {
			continue
		}
		decision.RuleID = rule.ID
		decision.RuleName = rule.Name
		decision.Reason = rule.Description
		decision.ExpiresAt = rule.ExpiresAt
		switch rule.Action {
		case "allow":
			decision.Status = "allowed"
		case "temporary":
			decision.Status = "temporary"
		case "deny":
			decision.Status = "denied"
		}
		if decision.Reason == "" {
			decision.Reason = "匹配合规规则 " + rule.Name
		}
		return decision
	}
	return decision
}

func complianceRuleMatches(rule ComplianceRule, anomaly cmdb.NetworkAnomaly) bool {
	value := strings.TrimSpace(rule.Value)
	switch rule.Type {
	case "mac":
		return normalizeMACValue(value) != "" && normalizeMACValue(value) == normalizeMACValue(anomaly.MACAddress)
	case "ip":
		return value == anomaly.IP
	case "cidr":
		_, network, err := net.ParseCIDR(value)
		return err == nil && network.Contains(net.ParseIP(anomaly.IP))
	case "vlan":
		return value == anomaly.VLAN
	default:
		return false
	}
}

func complianceExpired(rule ComplianceRule, now time.Time) bool {
	if rule.ExpiresAt == "" {
		return false
	}
	expiry, err := time.ParseInLocation("2006-01-02 15:04:05", rule.ExpiresAt, time.Local)
	if err != nil {
		expiry, err = time.ParseInLocation("2006-01-02", rule.ExpiresAt, time.Local)
	}
	return err == nil && now.After(expiry)
}

func complianceActionRank(action string) int {
	switch action {
	case "deny":
		return 0
	case "temporary":
		return 1
	default:
		return 2
	}
}
func normalizeMACValue(value string) string {
	return strings.ToUpper(strings.NewReplacer(":", "", "-", "", ".", "", " ", "").Replace(strings.TrimSpace(value)))
}
