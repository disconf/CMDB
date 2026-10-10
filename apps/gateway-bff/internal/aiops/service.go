package aiops

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

var (
	ErrNotFound        = errors.New("analysis not found")
	ErrValidation      = errors.New("validation")
	ErrNotReady        = errors.New("analysis dependencies not ready")
	ErrNoEvidence      = errors.New("no new evidence")
	ErrRunbookNotFound = errors.New("runbook not found")
	ErrChangeDisabled  = errors.New("change runbooks disabled")
)

type Service struct {
	mu            sync.RWMutex
	repo          repository
	knowledge     []Knowledge
	analyses      []Analysis
	executions    []Execution
	runbooks      []RunbookPolicy
	changeControl ChangeControl
	canaries      []CanaryValidation
	alerts        AlertProvider
	assets        AssetProvider
	impact        ImpactProvider
	tickets       TicketProvider
	releases      ReleaseProvider
	tools         ToolProvider
	jobs          JobProvider
}

func NewService() *Service {
	s := &Service{repo: newMemoryRepository(), knowledge: defaultKnowledge(), analyses: []Analysis{}, executions: []Execution{}, runbooks: []RunbookPolicy{}, changeControl: ChangeControl{ID: "default", LiveEnabled: true}, canaries: []CanaryValidation{}}
	if databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL")); databaseURL != "" {
		repo, err := openPostgres(databaseURL)
		if err != nil {
			panic(fmt.Sprintf("initialize aiops repository: %v", err))
		}
		s.repo = repo
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if analyses, err := s.repo.LoadAnalyses(ctx); err == nil {
		s.analyses = analyses
	}
	if executions, err := s.repo.LoadExecutions(ctx); err == nil {
		s.executions = executions
	}
	if control, err := s.repo.LoadChangeControl(ctx); err == nil {
		s.changeControl = control
	}
	if canaries, err := s.repo.LoadCanaryValidations(ctx); err == nil {
		s.canaries = canaries
	}
	if runbooks, err := s.repo.LoadRunbookPolicies(ctx); err == nil && len(runbooks) > 0 {
		s.runbooks = runbooks
	} else {
		s.runbooks = defaultRunbookPolicies()
		for _, item := range s.runbooks {
			_ = s.repo.SaveRunbookPolicy(ctx, item)
		}
	}
	for _, item := range defaultRunbookPolicies() {
		found := false
		for _, existing := range s.runbooks {
			if existing.RunbookID == item.RunbookID {
				found = true
				break
			}
		}
		if !found {
			s.runbooks = append(s.runbooks, item)
			_ = s.repo.SaveRunbookPolicy(ctx, item)
		}
	}
	return s
}

func (s *Service) SetAlertProvider(provider AlertProvider)     { s.alerts = provider }
func (s *Service) SetAssetProvider(provider AssetProvider)     { s.assets = provider }
func (s *Service) SetImpactProvider(provider ImpactProvider)   { s.impact = provider }
func (s *Service) SetTicketProvider(provider TicketProvider)   { s.tickets = provider }
func (s *Service) SetReleaseProvider(provider ReleaseProvider) { s.releases = provider }
func (s *Service) SetJobProvider(provider JobProvider)         { s.jobs = provider }
func (s *Service) SetToolProvider(provider ToolProvider)       { s.tools = provider }

func (s *Service) ready() error {
	if s.alerts == nil || s.assets == nil {
		return ErrNotReady
	}
	return nil
}

func (s *Service) Summary() Summary {
	summary := Summary{KnowledgeCount: len(s.knowledge)}
	if s.alerts != nil {
		summary.OpenAnomalies = s.alerts.ActiveCount()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	today := time.Now().Format("2006-01-02")
	totalConfidence := 0
	for _, item := range s.analyses {
		if strings.HasPrefix(item.CreatedAt, today) {
			summary.AnalyzedToday++
		}
		totalConfidence += item.Confidence
		if item.Feedback == nil {
			continue
		}
		summary.FeedbackCount++
		switch item.Feedback.Status {
		case "confirmed":
			summary.ConfirmedFeedback++
		case "rejected":
			summary.RejectedFeedback++
		}
	}
	if len(s.analyses) > 0 {
		summary.AverageConfidence = float64(totalConfidence) / float64(len(s.analyses))
	}
	if summary.FeedbackCount > 0 {
		summary.Accuracy = math.Round(float64(summary.ConfirmedFeedback)/float64(summary.FeedbackCount)*10000) / 100
	}
	for _, item := range s.executions {
		if item.Status == "pending-approval" {
			summary.PendingActions++
		}
	}
	return summary
}

func (s *Service) Analyses() []Analysis {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]Analysis(nil), s.analyses...)
	sort.SliceStable(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result
}

func (s *Service) Get(id string) (Analysis, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.analyses {
		if item.ID == id {
			return item, nil
		}
	}
	return Analysis{}, ErrNotFound
}
func (s *Service) Alerts() []monitor.Alert {
	if s.alerts == nil {
		return []monitor.Alert{}
	}
	items := s.alerts.Alerts()
	result := make([]monitor.Alert, 0, len(items))
	for _, item := range items {
		if strings.ToLower(item.Status) != "resolved" {
			result = append(result, item)
		}
	}
	return result
}

func (s *Service) AnalyzeActive(ctx context.Context) ([]Analysis, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	items := s.Alerts()
	if len(items) > 50 {
		items = items[:50]
	}
	result := make([]Analysis, 0, len(items))
	for _, alert := range items {
		item, err := s.Analyze(ctx, alert.ID)
		if err != nil {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Service) Execute(ctx context.Context, analysisID, runbookID, user string) (Execution, error) {
	if strings.TrimSpace(analysisID) == "" || strings.TrimSpace(runbookID) == "" || strings.TrimSpace(user) == "" {
		return Execution{}, ErrValidation
	}
	analysis, err := s.Get(analysisID)
	if err != nil {
		return Execution{}, err
	}
	var selected *Recommendation
	for i := range analysis.Recommendations {
		if analysis.Recommendations[i].RunbookID == runbookID {
			selected = &analysis.Recommendations[i]
			break
		}
	}
	if selected == nil {
		return Execution{}, ErrValidation
	}
	policy, ok := s.policyFor(selected.RunbookID)
	if !ok {
		policy = RunbookPolicy{RunbookID: selected.RunbookID, Title: selected.Title, Mode: "read-only", ExecutionMode: "live", ChangeWindow: "*"}
	}
	plan := s.buildChangePlan(analysis, *selected, policy, user, "", false)
	if s.tickets == nil {
		return Execution{}, ErrNotReady
	}
	priority := "medium"
	if strings.EqualFold(analysis.Severity, "critical") {
		priority = "critical"
	} else if strings.EqualFold(analysis.Severity, "warning") {
		priority = "high"
	}
	assetID, projectGroup := "", ""
	if len(analysis.RelatedAssets) > 0 {
		assetID = analysis.RelatedAssets[0].ID
		projectGroup = analysis.RelatedAssets[0].ProjectGroup
	}
	ticket, err := s.tickets.Create(tickets.CreateInput{
		Title:              "AIOps Runbook 执行审批：" + selected.Title,
		Type:               "change",
		Priority:           priority,
		Applicant:          user,
		Description:        "AIOps 分析 " + analysis.ID + " 建议执行 Runbook " + selected.RunbookID + "。平台只创建审批工单，不直接执行服务器操作。",
		RelatedAsset:       assetID,
		RelatedAlert:       analysis.AlertID,
		ProjectGroup:       projectGroup,
		AutomationTemplate: selected.RunbookID,
	})
	if err != nil {
		return Execution{}, err
	}
	item := Execution{ID: newID("aiexec"), AnalysisID: analysis.ID, RunbookID: selected.RunbookID, RunbookTitle: selected.Title, Status: "pending-approval", RequestedBy: user, RequestedAt: time.Now().Format("2006-01-02 15:04:05"), ApprovalRequired: true, TicketID: ticket.ID, ChangePlan: plan, VerificationStatus: "pending", VerificationSummary: "等待任务执行后验证"}
	if err = s.repo.SaveExecution(ctx, item); err != nil {
		return Execution{}, err
	}
	s.mu.Lock()
	s.executions = append([]Execution{item}, s.executions...)
	s.mu.Unlock()
	return item, nil
}

func (s *Service) Executions() []Execution {
	return s.refreshExecutions()
}

func (s *Service) ExecutionStats() ExecutionStats {
	executions := s.refreshExecutions()
	stats := ExecutionStats{ByRunbook: []RunbookExecutionStat{}}
	byRunbook := map[string]*RunbookExecutionStat{}
	for _, item := range executions {
		stats.Total++
		if item.Simulated {
			stats.Simulated++
		} else {
			stats.Live++
		}
		switch item.Status {
		case "pending-approval":
			stats.PendingApproval++
		case "running":
			stats.Running++
		case "success":
			stats.Success++
		case "failed":
			stats.Failed++
		case "rejected":
			stats.Rejected++
		}
		key := item.RunbookID
		row := byRunbook[key]
		if row == nil {
			row = &RunbookExecutionStat{RunbookID: item.RunbookID, RunbookTitle: item.RunbookTitle}
			byRunbook[key] = row
		}
		row.Total++
		if item.Simulated {
			row.Simulated++
		} else {
			row.Live++
		}
		switch item.Status {
		case "success":
			row.Success++
		case "failed":
			row.Failed++
		case "pending-approval", "running":
			row.InProgress++
		}
	}
	completed := stats.Success + stats.Failed
	if completed > 0 {
		stats.SuccessRate = math.Round(float64(stats.Success)/float64(completed)*10000) / 100
	}
	for _, row := range byRunbook {
		completed := row.Success + row.Failed
		if completed > 0 {
			row.SuccessRate = math.Round(float64(row.Success)/float64(completed)*10000) / 100
		}
		stats.ByRunbook = append(stats.ByRunbook, *row)
	}
	sort.SliceStable(stats.ByRunbook, func(i, j int) bool {
		if stats.ByRunbook[i].Total != stats.ByRunbook[j].Total {
			return stats.ByRunbook[i].Total > stats.ByRunbook[j].Total
		}
		return stats.ByRunbook[i].RunbookID < stats.ByRunbook[j].RunbookID
	})
	return stats
}

func (s *Service) SetFeedback(ctx context.Context, analysisID string, in FeedbackInput, reviewer string) (Analysis, error) {
	status := strings.ToLower(strings.TrimSpace(in.Status))
	if status != "confirmed" && status != "rejected" && status != "partial" {
		return Analysis{}, ErrValidation
	}
	note := strings.TrimSpace(in.Note)
	if len(note) > 1000 {
		return Analysis{}, ErrValidation
	}
	item, err := s.Get(analysisID)
	if err != nil {
		return Analysis{}, err
	}
	corrected := strings.TrimSpace(in.CorrectedRootCause)
	item.Feedback = &Feedback{Status: status, CorrectedRootCause: corrected, Note: note, Reviewer: strings.TrimSpace(reviewer), ReviewedAt: time.Now().Format("2006-01-02 15:04:05")}
	if corrected != "" {
		item.RootCause = corrected
	}
	item.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	if err = s.repo.SaveAnalysis(ctx, item); err != nil {
		return Analysis{}, err
	}
	s.mu.Lock()
	for i := range s.analyses {
		if s.analyses[i].ID == item.ID {
			s.analyses[i] = item
			break
		}
	}
	s.mu.Unlock()
	return item, nil
}
func (s *Service) Knowledge() []Knowledge { return append([]Knowledge(nil), s.knowledge...) }

func (s *Service) SearchKnowledge(q string) []Knowledge {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return s.Knowledge()
	}
	result := []Knowledge{}
	for _, item := range s.knowledge {
		haystack := strings.ToLower(item.Title + " " + item.Summary + " " + strings.Join(item.Tags, " "))
		if strings.Contains(haystack, q) {
			result = append(result, item)
		}
	}
	return result
}
