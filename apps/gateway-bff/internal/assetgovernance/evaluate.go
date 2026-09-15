package assetgovernance

import (
	"context"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

type Report struct {
	Evaluated     int      `json:"evaluated"`
	Issues        int      `json:"issues"`
	Critical      int      `json:"critical"`
	Warning       int      `json:"warning"`
	Tickets       int      `json:"tickets"`
	Alerts        int      `json:"alerts"`
	RuleEvaluated int      `json:"ruleEvaluated"`
	RuleMatched   int      `json:"ruleMatched"`
	RuleAssigned  int      `json:"ruleAssigned"`
	Errors        []string `json:"errors,omitempty"`
}

func (s *Service) Issues() []Issue {
	return s.collectIssues(false)
}

func (s *Service) Evaluate(operator string) (Report, error) {
	ruleReport := s.EvaluateGovernanceRules(operator)
	issues := s.collectIssues(true)
	report := Report{Issues: len(issues), RuleEvaluated: ruleReport.Evaluated, RuleMatched: ruleReport.Matched, RuleAssigned: ruleReport.Assigned}
	for _, issue := range issues {
		report.Evaluated++
		if issue.Severity == "critical" {
			report.Critical++
		} else {
			report.Warning++
		}
	}
	alerts := []monitor.CMDBGovernanceAlert{}
	for _, issue := range issues {
		if issue.ReviewStatus == "confirmed" || issue.ReviewStatus == "retired" {
			continue
		}
		if issue.Severity != "critical" {
			continue
		}
		ticket, created, err := s.tickets.SyncAlert(tickets.AlertInput{ID: "asset-governance-" + issue.AssetID, Title: issue.AssetName + " 资产责任待确认", Severity: "critical", Status: "firing", Target: issue.IP, Message: issue.Detail, Owner: issue.Owner})
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
			continue
		}
		if created {
			report.Tickets++
		}
		if review, ok := s.review(issue.AssetID); ok && review.TicketID == "" {
			review.TicketID = ticket.ID
			review.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
			_ = s.saveReview(review)
		}
		alerts = append(alerts, monitor.CMDBGovernanceAlert{ID: "asset-governance-" + issue.AssetID, Title: issue.AssetName + " 资产责任待确认", Severity: "critical", Target: issue.IP, Message: issue.Detail, Owner: issue.Owner})
	}
	count, err := s.monitor.SyncCMDBGovernanceAlerts(alerts)
	report.Alerts = count
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
	}
	return report, nil
}

func (s *Service) collectIssues(persist bool) []Issue {
	assets := s.cmdb.InventoryAssets()
	result := make([]Issue, 0)
	for _, asset := range assets {
		if !governableAsset(asset.Type) {
			continue
		}
		issue, ok := issueForAsset(asset)
		if !ok {
			continue
		}
		review, exists := s.review(asset.ID)
		if !exists {
			review = Review{AssetID: asset.ID, Status: "pending", UpdatedAt: time.Now().Format("2006-01-02 15:04:05")}
		}
		if review.Status == "retired" {
			continue
		}
		if persist {
			_ = s.saveReview(review)
		}
		issue.ReviewStatus = review.Status
		issue.ReviewedAt = review.ReviewedAt
		issue.ReviewedBy = review.ReviewedBy
		issue.Note = review.Note
		issue.TicketID = review.TicketID
		issue.UpdatedAt = review.UpdatedAt
		result = append(result, issue)
	}
	return result
}

func issueForAsset(asset cmdb.Asset) (Issue, bool) {
	issue := Issue{AssetID: asset.ID, AssetName: asset.Name, AssetType: asset.Type, AssetTypeName: asset.TypeName, IP: asset.IP, Status: asset.Status, ProjectGroup: asset.ProjectGroup, Owner: asset.Owner, Location: asset.Location, LastSeenAt: asset.LastSeenAt, UpdatedAt: time.Now().Format("2006-01-02 15:04:05")}
	if strings.TrimSpace(asset.Owner) == "" || asset.Owner == "待分配" {
		issue.IssueType = "unowned"
		issue.Severity = "critical"
		issue.Detail = "资产未分配负责人"
		return issue, true
	}
	if strings.TrimSpace(asset.ProjectGroup) == "" || asset.ProjectGroup == "待确认" || asset.ProjectGroup == "自动发现" {
		issue.IssueType = "unassigned"
		issue.Severity = "warning"
		issue.Detail = "资产归属项目组未确认"
		return issue, true
	}
	if strings.TrimSpace(asset.Location) == "" {
		issue.IssueType = "missing-location"
		issue.Severity = "warning"
		issue.Detail = "资产缺少机房或位置信息"
		return issue, true
	}
	if asset.Status == "offline" {
		days := staleDays(asset.LastSeenAt)
		if days >= 7 {
			issue.IssueType = "long-offline"
			issue.Severity = "critical"
			issue.StaleDays = days
			issue.Detail = "资产已离线超过 7 天"
			return issue, true
		}
	}
	if days := staleDays(asset.LastSeenAt); days >= 30 {
		issue.IssueType = "stale"
		issue.Severity = "warning"
		issue.StaleDays = days
		issue.Detail = "资产超过 30 天未更新"
		return issue, true
	}
	return Issue{}, false
}

func governableAsset(assetType string) bool {
	switch assetType {
	case "physical-server", "virtual-machine", "network-device", "load-balancer", "database", "middleware", "cloud-host":
		return true
	default:
		return false
	}
}

func staleDays(value string) int {
	if value == "" {
		return 0
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.Local)
	if err != nil {
		return 0
	}
	return int(time.Since(parsed).Hours() / 24)
}

func (s *Service) review(assetID string) (Review, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.reviews[assetID]
	return item, ok
}

func (s *Service) saveReview(review Review) error {
	review.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	s.mu.Lock()
	s.reviews[review.AssetID] = review
	s.mu.Unlock()
	if s.db != nil {
		return upsertReview(context.Background(), s.db, review)
	}
	return nil
}

func (s *Service) Assign(assetID, owner, projectGroup, location, operator, note string) (cmdb.Asset, error) {
	asset, err := s.cmdb.GetAsset(assetID)
	if err != nil {
		return cmdb.Asset{}, err
	}
	if strings.TrimSpace(owner) == "" {
		return cmdb.Asset{}, ErrValidation
	}
	updated, err := s.cmdb.UpdateAsset(assetID, cmdb.UpdateAssetInput{Name: asset.Name, Status: asset.Status, IP: asset.IP, Environment: asset.Environment, ProjectGroup: firstValue(projectGroup, asset.ProjectGroup), Owner: owner, Location: firstValue(location, asset.Location), Tags: asset.Tags, Attributes: asset.Attributes, Relations: asset.Relations}, operator)
	if err != nil {
		return cmdb.Asset{}, err
	}
	review, _ := s.review(assetID)
	review.AssetID = assetID
	review.Status = "confirmed"
	review.Owner = owner
	review.ProjectGroup = updated.ProjectGroup
	review.Location = updated.Location
	review.ReviewedAt = time.Now().Format("2006-01-02 15:04:05")
	review.ReviewedBy = operator
	review.Note = note
	if err = s.saveReview(review); err != nil {
		return cmdb.Asset{}, err
	}
	return updated, nil
}

func (s *Service) Confirm(assetID, operator, note string) (Review, error) {
	if _, err := s.cmdb.GetAsset(assetID); err != nil {
		return Review{}, err
	}
	review, _ := s.review(assetID)
	review.AssetID = assetID
	review.Status = "confirmed"
	review.ReviewedAt = time.Now().Format("2006-01-02 15:04:05")
	review.ReviewedBy = operator
	review.Note = note
	if err := s.saveReview(review); err != nil {
		return Review{}, err
	}
	return review, nil
}

func (s *Service) Retire(assetID, operator, note string) (Review, error) {
	asset, err := s.cmdb.GetAsset(assetID)
	if err != nil {
		return Review{}, err
	}
	tags := append([]string(nil), asset.Tags...)
	if !containsValue(tags, "退役") {
		tags = append(tags, "退役")
	}
	if _, err = s.cmdb.UpdateAsset(assetID, cmdb.UpdateAssetInput{Name: asset.Name, Status: "offline", IP: asset.IP, Environment: asset.Environment, ProjectGroup: asset.ProjectGroup, Owner: asset.Owner, Location: asset.Location, Tags: tags, Attributes: asset.Attributes, Relations: asset.Relations}, operator); err != nil {
		return Review{}, err
	}
	review, _ := s.review(assetID)
	review.AssetID = assetID
	review.Status = "retired"
	review.ReviewedAt = time.Now().Format("2006-01-02 15:04:05")
	review.ReviewedBy = operator
	review.Note = note
	if err := s.saveReview(review); err != nil {
		return Review{}, err
	}
	return review, nil
}

func containsValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func firstValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
