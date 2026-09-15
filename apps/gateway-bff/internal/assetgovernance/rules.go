package assetgovernance

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

type GovernanceRule struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Enabled            bool   `json:"enabled"`
	Priority           int    `json:"priority"`
	MatchAssetType     string `json:"matchAssetType"`
	MatchSource        string `json:"matchSource"`
	MatchIPCIDR        string `json:"matchIpCidr"`
	MatchProjectGroup  string `json:"matchProjectGroup"`
	MatchTag           string `json:"matchTag"`
	Owner              string `json:"owner"`
	TargetProjectGroup string `json:"targetProjectGroup"`
	Location           string `json:"location"`
	Note               string `json:"note"`
	CreatedBy          string `json:"createdBy"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

type GovernanceRuleInput struct {
	Name               string `json:"name"`
	Enabled            bool   `json:"enabled"`
	Priority           int    `json:"priority"`
	MatchAssetType     string `json:"matchAssetType"`
	MatchSource        string `json:"matchSource"`
	MatchIPCIDR        string `json:"matchIpCidr"`
	MatchProjectGroup  string `json:"matchProjectGroup"`
	MatchTag           string `json:"matchTag"`
	Owner              string `json:"owner"`
	TargetProjectGroup string `json:"targetProjectGroup"`
	Location           string `json:"location"`
	Note               string `json:"note"`
}

type GovernanceRuleReport struct {
	Evaluated int      `json:"evaluated"`
	Matched   int      `json:"matched"`
	Assigned  int      `json:"assigned"`
	Skipped   int      `json:"skipped"`
	Errors    []string `json:"errors,omitempty"`
}

type AssetGovernanceBulkInput struct {
	AssetIDs     []string `json:"assetIds"`
	Action       string   `json:"action"`
	Owner        string   `json:"owner"`
	ProjectGroup string   `json:"projectGroup"`
	Location     string   `json:"location"`
	Note         string   `json:"note"`
}

type AssetGovernanceBulkItem struct {
	AssetID   string `json:"assetId"`
	AssetName string `json:"assetName"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

type AssetGovernanceBulkResult struct {
	Total     int                       `json:"total"`
	Succeeded int                       `json:"succeeded"`
	Failed    int                       `json:"failed"`
	Items     []AssetGovernanceBulkItem `json:"items"`
}

func (s *Service) GovernanceRules() []GovernanceRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]GovernanceRule(nil), s.rules...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func (s *Service) CreateGovernanceRule(input GovernanceRuleInput, operator string) (GovernanceRule, error) {
	item, err := buildGovernanceRule(input, GovernanceRule{ID: fmt.Sprintf("governance-rule-%d", time.Now().UnixNano()), CreatedAt: time.Now().Format("2006-01-02 15:04:05")}, operator)
	if err != nil {
		return GovernanceRule{}, err
	}
	if s.db != nil {
		if err = upsertGovernanceRule(context.Background(), s.db, item); err != nil {
			return GovernanceRule{}, err
		}
	}
	s.mu.Lock()
	s.rules = append([]GovernanceRule{item}, s.rules...)
	s.mu.Unlock()
	return item, nil
}

func (s *Service) UpdateGovernanceRule(id string, input GovernanceRuleInput, operator string) (GovernanceRule, error) {
	current, ok := s.governanceRule(id)
	if !ok {
		return GovernanceRule{}, ErrNotFound
	}
	item, err := buildGovernanceRule(input, current, operator)
	if err != nil {
		return GovernanceRule{}, err
	}
	if s.db != nil {
		if err = upsertGovernanceRule(context.Background(), s.db, item); err != nil {
			return GovernanceRule{}, err
		}
	}
	s.mu.Lock()
	for index := range s.rules {
		if s.rules[index].ID == id {
			s.rules[index] = item
			break
		}
	}
	s.mu.Unlock()
	return item, nil
}

func (s *Service) DeleteGovernanceRule(id string) error {
	if _, ok := s.governanceRule(id); !ok {
		return ErrNotFound
	}
	if s.db != nil {
		if err := deleteGovernanceRule(context.Background(), s.db, id); err != nil {
			return err
		}
	}
	s.mu.Lock()
	for index := range s.rules {
		if s.rules[index].ID == id {
			s.rules = append(s.rules[:index], s.rules[index+1:]...)
			break
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) EvaluateGovernanceRules(operator string) GovernanceRuleReport {
	report := GovernanceRuleReport{}
	issues := s.Issues()
	for _, issue := range issues {
		if issue.ReviewStatus == "confirmed" || issue.ReviewStatus == "retired" || issue.IssueType != "unowned" {
			continue
		}
		report.Evaluated++
		asset, err := s.cmdb.GetAsset(issue.AssetID)
		if err != nil {
			report.Errors = append(report.Errors, issue.AssetID+": "+err.Error())
			continue
		}
		rule, ok := s.matchGovernanceRule(asset)
		if !ok {
			continue
		}
		report.Matched++
		if strings.TrimSpace(asset.Owner) != "" && asset.Owner != "待分配" {
			report.Skipped++
			continue
		}
		if _, err = s.Assign(asset.ID, rule.Owner, firstValue(rule.TargetProjectGroup, asset.ProjectGroup), firstValue(rule.Location, asset.Location), operator+"-rule:"+rule.Name, rule.Note); err != nil {
			report.Errors = append(report.Errors, asset.ID+": "+err.Error())
			continue
		}
		report.Assigned++
	}
	return report
}

func (s *Service) BulkGovernance(input AssetGovernanceBulkInput, operator string) (AssetGovernanceBulkResult, error) {
	input.Action = strings.TrimSpace(strings.ToLower(input.Action))
	if input.Action != "assign" && input.Action != "confirm" && input.Action != "retire" {
		return AssetGovernanceBulkResult{}, ErrValidation
	}
	if input.Action == "assign" && strings.TrimSpace(input.Owner) == "" {
		return AssetGovernanceBulkResult{}, ErrValidation
	}
	ids := uniqueStrings(input.AssetIDs)
	if len(ids) == 0 || len(ids) > 500 {
		return AssetGovernanceBulkResult{}, ErrValidation
	}
	result := AssetGovernanceBulkResult{Total: len(ids), Items: make([]AssetGovernanceBulkItem, 0, len(ids))}
	for _, assetID := range ids {
		item := AssetGovernanceBulkItem{AssetID: assetID}
		asset, err := s.cmdb.GetAsset(assetID)
		if err == nil {
			item.AssetName = asset.Name
		}
		switch input.Action {
		case "assign":
			_, err = s.Assign(assetID, input.Owner, input.ProjectGroup, input.Location, operator, input.Note)
		case "confirm":
			_, err = s.Confirm(assetID, operator, input.Note)
		case "retire":
			_, err = s.Retire(assetID, operator, input.Note)
		}
		if err != nil {
			item.Error = err.Error()
			result.Failed++
		} else {
			item.Success = true
			result.Succeeded++
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *Service) matchGovernanceRule(asset cmdb.Asset) (GovernanceRule, bool) {
	for _, rule := range s.GovernanceRules() {
		if !rule.Enabled {
			continue
		}
		if rule.MatchAssetType != "" && rule.MatchAssetType != asset.Type {
			continue
		}
		if rule.MatchSource != "" && rule.MatchSource != asset.Source {
			continue
		}
		if rule.MatchProjectGroup != "" && rule.MatchProjectGroup != asset.ProjectGroup {
			continue
		}
		if rule.MatchTag != "" && !containsValue(asset.Tags, rule.MatchTag) {
			continue
		}
		if rule.MatchIPCIDR != "" && !ipInCIDR(asset.IP, rule.MatchIPCIDR) {
			continue
		}
		return rule, true
	}
	return GovernanceRule{}, false
}

func (s *Service) governanceRule(id string) (GovernanceRule, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.rules {
		if item.ID == id {
			return item, true
		}
	}
	return GovernanceRule{}, false
}

func buildGovernanceRule(input GovernanceRuleInput, current GovernanceRule, operator string) (GovernanceRule, error) {
	item := current
	item.Name = strings.TrimSpace(input.Name)
	item.Enabled = input.Enabled
	item.Priority = input.Priority
	item.MatchAssetType = strings.TrimSpace(input.MatchAssetType)
	item.MatchSource = strings.TrimSpace(input.MatchSource)
	item.MatchIPCIDR = strings.TrimSpace(input.MatchIPCIDR)
	item.MatchProjectGroup = strings.TrimSpace(input.MatchProjectGroup)
	item.MatchTag = strings.TrimSpace(input.MatchTag)
	item.Owner = strings.TrimSpace(input.Owner)
	item.TargetProjectGroup = strings.TrimSpace(input.TargetProjectGroup)
	item.Location = strings.TrimSpace(input.Location)
	item.Note = strings.TrimSpace(input.Note)
	item.CreatedBy = firstValue(item.CreatedBy, operator)
	item.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	if item.Name == "" || item.Owner == "" {
		return GovernanceRule{}, ErrValidation
	}
	if item.MatchAssetType == "" && item.MatchSource == "" && item.MatchIPCIDR == "" && item.MatchProjectGroup == "" && item.MatchTag == "" {
		return GovernanceRule{}, ErrValidation
	}
	if item.MatchIPCIDR != "" {
		if _, err := netip.ParsePrefix(item.MatchIPCIDR); err != nil {
			return GovernanceRule{}, ErrValidation
		}
	}
	if item.Priority == 0 {
		item.Priority = 10
	}
	return item, nil
}

func ipInCIDR(value, expression string) bool {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(expression))
	if err != nil {
		return false
	}
	return prefix.Contains(address)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
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
