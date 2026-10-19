package aiops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/objectstore"
	"cmdb/gateway-bff/internal/tickets"
)

var (
	ErrNotFound            = errors.New("analysis not found")
	ErrUnauthorized        = errors.New("knowledge unauthorized")
	ErrConflict            = errors.New("knowledge version conflict")
	ErrValidation          = errors.New("validation")
	ErrNotReady            = errors.New("analysis dependencies not ready")
	ErrNoEvidence          = errors.New("no new evidence")
	ErrRunbookNotFound     = errors.New("runbook not found")
	ErrChangeDisabled      = errors.New("change runbooks disabled")
	ErrObjectStoreNotReady = errors.New("object storage not configured")
)

const knowledgeAttachmentMaxBytes = 20 << 20

type knowledgeObjectStore interface {
	Put(context.Context, string, []byte, string) (string, error)
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
	Bucket() string
}

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
	objectStore   knowledgeObjectStore
	embedder      knowledgeEmbeddingProvider
	llm           knowledgeLLMProvider
}

func NewService() *Service {
	s := &Service{repo: newMemoryRepository(), knowledge: defaultKnowledge(), analyses: []Analysis{}, executions: []Execution{}, runbooks: []RunbookPolicy{}, changeControl: ChangeControl{ID: "default", LiveEnabled: true}, canaries: []CanaryValidation{}}
	store, storeErr := objectstore.NewFromEnv()
	if storeErr != nil {
		panic(fmt.Sprintf("initialize AIOps object storage: %v", storeErr))
	}
	s.objectStore = store
	embedder, embedderErr := newKnowledgeEmbeddingProvider()
	if embedderErr != nil {
		panic(fmt.Sprintf("initialize knowledge embedding provider: %v", embedderErr))
	}
	s.embedder = embedder
	s.llm = newKnowledgeLLMProvider()
	if databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL")); databaseURL != "" {
		repo, err := openPostgres(databaseURL)
		if err != nil {
			panic(fmt.Sprintf("initialize aiops repository: %v", err))
		}
		s.repo = repo
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if knowledge, err := s.repo.LoadKnowledge(ctx); err == nil && len(knowledge) > 0 {
		s.knowledge = knowledge
	} else {
		s.knowledge = defaultKnowledge()
		for _, item := range s.knowledge {
			_ = s.repo.SaveKnowledge(ctx, item)
		}
	}
	s.ensureKnowledgeLayout(ctx)
	for _, item := range s.knowledge {
		if versions, err := s.repo.LoadKnowledgeVersions(ctx, item.ID); err == nil && len(versions) == 0 {
			_, _ = s.saveKnowledgeVersion(item, "system", "初始化版本")
		}
		if vector, err := s.repo.LoadKnowledgeEmbedding(ctx, item.ID); err == nil && len(vector) == 0 {
			_ = s.indexKnowledge(item)
		}
	}
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
	summary := Summary{}
	if s.alerts != nil {
		summary.OpenAnomalies = s.alerts.ActiveCount()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	summary.KnowledgeCount = len(s.knowledge)
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
	result := append([]Analysis{}, s.analyses...)
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
func (s *Service) Knowledge() []Knowledge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]Knowledge{}, s.knowledge...)
	sort.SliceStable(result, func(i, j int) bool { return result[i].UpdatedAt > result[j].UpdatedAt })
	return result
}

func (s *Service) SearchKnowledge(q string) []Knowledge {
	return s.searchKnowledge(q, false)
}

func (s *Service) SearchKnowledgeAll(q string) []Knowledge {
	return s.searchKnowledge(q, true)
}

func (s *Service) SearchKnowledgeForActor(q string, includeDrafts bool, actor KnowledgeActor) []Knowledge {
	items := s.searchKnowledge(q, includeDrafts)
	result := make([]Knowledge, 0, len(items))
	for _, item := range items {
		if s.canViewKnowledgeDocument(actor, item) {
			result = append(result, item)
		}
	}
	return result
}

func (s *Service) SearchKnowledgeHybridForActor(query string, includeDrafts bool, actor KnowledgeActor) []Knowledge {
	items := s.SearchKnowledgeHybrid(query, includeDrafts)
	result := make([]Knowledge, 0, len(items))
	for _, item := range items {
		if s.canViewKnowledgeDocument(actor, item) {
			result = append(result, item)
		}
	}
	return result
}

func (s *Service) searchKnowledge(q string, includeDrafts bool) []Knowledge {
	_ = s.reloadKnowledge()
	q = strings.ToLower(strings.TrimSpace(q))
	s.mu.RLock()
	type scoredKnowledge struct {
		item  Knowledge
		score int
	}
	scored := make([]scoredKnowledge, 0, len(s.knowledge))
	for _, item := range s.knowledge {
		if !includeDrafts && item.Status != "published" {
			continue
		}
		score := 1
		if q != "" {
			score = knowledgeSearchScore(item, q)
			if score == 0 {
				continue
			}
		}
		scored = append(scored, scoredKnowledge{item: item, score: score})
	}
	s.mu.RUnlock()
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].item.Helpful != scored[j].item.Helpful {
			return scored[i].item.Helpful > scored[j].item.Helpful
		}
		return scored[i].item.UpdatedAt > scored[j].item.UpdatedAt
	})
	result := make([]Knowledge, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.item)
	}
	return result
}

func knowledgeSearchScore(item Knowledge, query string) int {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return 1
	}
	title := strings.ToLower(item.Title)
	category := strings.ToLower(item.Category)
	summary := strings.ToLower(item.Summary)
	content := strings.ToLower(item.Content)
	if item.ContentFormat == "html" {
		content = strings.ToLower(stripHTML(item.Content))
	}
	tags := strings.ToLower(strings.Join(item.Tags, " "))
	score := 0
	for _, term := range terms {
		matched := false
		if strings.Contains(title, term) {
			score += 100
			matched = true
		}
		if strings.Contains(category, term) {
			score += 50
			matched = true
		}
		if strings.Contains(tags, term) {
			score += 40
			matched = true
		}
		if strings.Contains(summary, term) {
			score += 25
			matched = true
		}
		if strings.Contains(content, term) {
			score += 10
			matched = true
		}
		if !matched {
			return 0
		}
	}
	return score
}

func (s *Service) knowledgeSuggestionsFor(rootCause, alertTitle string, limit int) []Knowledge {
	if limit <= 0 {
		return []Knowledge{}
	}
	queries := []string{knowledgeKeyword(rootCause), alertTitle}
	result := []Knowledge{}
	seen := map[string]bool{}
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}
		for _, item := range s.SearchKnowledgeHybrid(query, false) {
			if space, err := s.knowledgeSpace(item.SpaceID); err != nil || space.Kind == "private" {
				continue
			}
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			result = append(result, item)
			if len(result) >= limit {
				return result
			}
		}
	}
	return result
}

func knowledgeKeyword(rootCause string) string {
	switch {
	case strings.Contains(rootCause, "内存"):
		return "内存"
	case strings.Contains(rootCause, "磁盘"):
		return "磁盘"
	case strings.Contains(rootCause, "网络"):
		return "网络"
	case strings.Contains(rootCause, "Kubernetes") || strings.Contains(rootCause, "Pod"):
		return "Kubernetes"
	case strings.Contains(rootCause, "延迟"):
		return "延迟"
	case strings.Contains(rootCause, "数据库"):
		return "数据库"
	default:
		return ""
	}
}

func (s *Service) GetKnowledge(id string) (Knowledge, error) {
	id = strings.TrimSpace(id)
	_ = s.reloadKnowledge()
	s.mu.Lock()
	for i := range s.knowledge {
		if s.knowledge[i].ID == id {
			s.knowledge[i].Views++
			item := s.knowledge[i]
			s.mu.Unlock()
			_ = s.repo.SaveKnowledge(context.Background(), item)
			return item, nil
		}
	}
	s.mu.Unlock()
	return Knowledge{}, ErrNotFound
}

func (s *Service) CreateKnowledge(input KnowledgeInput, author string) (Knowledge, error) {
	item, err := normalizeKnowledgeInput(Knowledge{}, input)
	if err != nil {
		return Knowledge{}, err
	}
	if item.SpaceID == "" {
		item.SpaceID = knowledgeDefaultSpaceID
	}
	if item.FolderID == "" {
		item.FolderID = s.knowledgeFolderForCategory(item.Category).ID
	}
	item.Related = s.resolveKnowledgeRefs(item.Related)
	now := time.Now().Format("2006-01-02 15:04:05")
	item.ID = newID("kb")
	item.Source = "manual"
	item.Author = strings.TrimSpace(author)
	if item.Owner == "" {
		item.Owner = item.Author
	}
	item.Version = 1
	if item.Status == "published" {
		item.PublishedVersion = 1
	}
	item.CreatedAt = now
	item.UpdatedAt = now
	if err = s.repo.SaveKnowledge(context.Background(), item); err != nil {
		return Knowledge{}, err
	}
	s.mu.Lock()
	s.knowledge = append([]Knowledge{item}, s.knowledge...)
	s.mu.Unlock()
	_ = s.indexKnowledge(item)
	_, _ = s.saveKnowledgeVersion(item, author, "创建知识")
	return item, nil
}

func (s *Service) UpdateKnowledge(id string, input KnowledgeInput, author string) (Knowledge, error) {
	_ = s.reloadKnowledge()
	s.mu.Lock()
	index := -1
	for i := range s.knowledge {
		if s.knowledge[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return Knowledge{}, ErrNotFound
	}
	existing := s.knowledge[index]
	if input.Version > 0 && existing.Version > 0 && input.Version != existing.Version {
		s.mu.Unlock()
		return Knowledge{}, ErrConflict
	}
	updated, err := normalizeKnowledgeInput(existing, input)
	if err != nil {
		s.mu.Unlock()
		return Knowledge{}, err
	}
	updated.Related = s.resolveKnowledgeRefs(updated.Related)
	updated.ID = existing.ID
	updated.Source = existing.Source
	updated.SourceID = existing.SourceID
	updated.Author = strings.TrimSpace(author)
	if updated.Owner == "" {
		updated.Owner = updated.Author
	}
	updated.Version = existing.Version + 1
	if updated.Status == "published" {
		updated.PublishedVersion = updated.Version
	}
	updated.CreatedAt = existing.CreatedAt
	updated.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	s.knowledge[index] = updated
	s.mu.Unlock()
	if err = s.repo.SaveKnowledge(context.Background(), updated); err != nil {
		return Knowledge{}, err
	}
	_ = s.indexKnowledge(updated)
	_, _ = s.saveKnowledgeVersion(updated, author, "编辑知识")
	return updated, nil
}

func (s *Service) DeleteKnowledge(id string) error {
	if err := s.repo.DeleteKnowledge(context.Background(), id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.knowledge {
		if s.knowledge[i].ID == id {
			s.knowledge = append(s.knowledge[:i], s.knowledge[i+1:]...)
			return nil
		}
	}
	return nil
}

func (s *Service) reloadKnowledge() error {
	items, err := s.repo.LoadKnowledge(context.Background())
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.knowledge = items
	s.mu.Unlock()
	return nil
}

func (s *Service) MarkKnowledgeHelpful(id string) (Knowledge, error) {
	_ = s.reloadKnowledge()
	s.mu.Lock()
	for i := range s.knowledge {
		if s.knowledge[i].ID == id {
			s.knowledge[i].Helpful++
			s.knowledge[i].UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
			item := s.knowledge[i]
			s.mu.Unlock()
			if err := s.repo.SaveKnowledge(context.Background(), item); err != nil {
				return Knowledge{}, err
			}
			return item, nil
		}
	}
	s.mu.Unlock()
	return Knowledge{}, ErrNotFound
}

func (s *Service) KnowledgeVersions(id string) ([]KnowledgeVersion, error) {
	if _, ok := s.knowledgeExists(id); !ok {
		return nil, ErrNotFound
	}
	items, err := s.repo.LoadKnowledgeVersions(context.Background(), id)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Version > items[j].Version })
	return items, nil
}

func (s *Service) RestoreKnowledge(id string, version int, author string) (Knowledge, error) {
	versions, err := s.repo.LoadKnowledgeVersions(context.Background(), id)
	if err != nil {
		return Knowledge{}, err
	}
	var target *KnowledgeVersion
	for index := range versions {
		if versions[index].Version == version {
			target = &versions[index]
			break
		}
	}
	if target == nil {
		return Knowledge{}, ErrNotFound
	}
	_ = s.reloadKnowledge()
	s.mu.RLock()
	exists := false
	for _, item := range s.knowledge {
		if item.ID == id {
			exists = true
			break
		}
	}
	s.mu.RUnlock()
	if !exists {
		return Knowledge{}, ErrNotFound
	}
	restored := target.Snapshot
	restored.ID = id
	restored.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	if err = s.repo.SaveKnowledge(context.Background(), restored); err != nil {
		return Knowledge{}, err
	}
	_ = s.indexKnowledge(restored)
	s.mu.Lock()
	for index := range s.knowledge {
		if s.knowledge[index].ID == id {
			s.knowledge[index] = restored
			break
		}
	}
	s.mu.Unlock()
	_, _ = s.saveKnowledgeVersion(restored, author, fmt.Sprintf("从版本 %d 还原", version))
	return restored, nil
}

func (s *Service) knowledgeExists(id string) (Knowledge, bool) {
	_ = s.reloadKnowledge()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.knowledge {
		if item.ID == id {
			return item, true
		}
	}
	return Knowledge{}, false
}

func (s *Service) saveKnowledgeVersion(item Knowledge, author, summary string) (KnowledgeVersion, error) {
	versions, err := s.repo.LoadKnowledgeVersions(context.Background(), item.ID)
	if err != nil {
		return KnowledgeVersion{}, err
	}
	maxVersion := 0
	for _, version := range versions {
		if version.Version > maxVersion {
			maxVersion = version.Version
		}
	}
	createdAt := time.Now().Format("2006-01-02 15:04:05")
	version := KnowledgeVersion{ID: newID("kbv"), KnowledgeID: item.ID, Version: maxVersion + 1, Snapshot: item, ChangedBy: strings.TrimSpace(author), ChangeSummary: strings.TrimSpace(summary), CreatedAt: createdAt}
	if err = s.repo.SaveKnowledgeVersion(context.Background(), version); err != nil {
		return KnowledgeVersion{}, err
	}
	return version, nil
}

func (s *Service) KnowledgeAttachments(id string) ([]KnowledgeAttachment, error) {
	if _, ok := s.knowledgeExists(id); !ok {
		return nil, ErrNotFound
	}
	return s.repo.LoadKnowledgeAttachments(context.Background(), id)
}

func (s *Service) UploadKnowledgeAttachment(ctx context.Context, knowledgeID, filename, contentType string, content []byte, uploader string) (KnowledgeAttachment, error) {
	if _, ok := s.knowledgeExists(knowledgeID); !ok {
		return KnowledgeAttachment{}, ErrNotFound
	}
	if s.objectStore == nil {
		return KnowledgeAttachment{}, ErrObjectStoreNotReady
	}
	if len(content) == 0 || len(content) > knowledgeAttachmentMaxBytes {
		return KnowledgeAttachment{}, ErrValidation
	}
	filename = sanitizeAttachmentFilename(filename)
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	sum := sha256.Sum256(content)
	item := KnowledgeAttachment{ID: newID("kba"), KnowledgeID: knowledgeID, Filename: filename, ContentType: contentType, Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:]), UploadedBy: strings.TrimSpace(uploader), CreatedAt: time.Now().Format("2006-01-02 15:04:05")}
	item.Key = "knowledge/" + safeID(knowledgeID) + "/" + item.ID + attachmentExtension(filename)
	if _, err := s.objectStore.Put(ctx, item.Key, content, contentType); err != nil {
		return KnowledgeAttachment{}, err
	}
	if err := s.repo.SaveKnowledgeAttachment(ctx, item); err != nil {
		_ = s.objectStore.Delete(ctx, item.Key)
		return KnowledgeAttachment{}, err
	}
	return item, nil
}

func (s *Service) DownloadKnowledgeAttachment(ctx context.Context, knowledgeID, attachmentID string) (KnowledgeAttachment, []byte, error) {
	if _, ok := s.knowledgeExists(knowledgeID); !ok {
		return KnowledgeAttachment{}, nil, ErrNotFound
	}
	if s.objectStore == nil {
		return KnowledgeAttachment{}, nil, ErrObjectStoreNotReady
	}
	items, err := s.repo.LoadKnowledgeAttachments(ctx, knowledgeID)
	if err != nil {
		return KnowledgeAttachment{}, nil, err
	}
	for _, item := range items {
		if item.ID == attachmentID {
			content, err := s.objectStore.Get(ctx, item.Key)
			if err != nil {
				return KnowledgeAttachment{}, nil, err
			}
			return item, content, nil
		}
	}
	return KnowledgeAttachment{}, nil, ErrNotFound
}

func (s *Service) DeleteKnowledgeAttachment(ctx context.Context, knowledgeID, attachmentID string) error {
	items, err := s.repo.LoadKnowledgeAttachments(ctx, knowledgeID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID != attachmentID {
			continue
		}
		if s.objectStore != nil {
			if err = s.objectStore.Delete(ctx, item.Key); err != nil {
				return err
			}
		}
		return s.repo.DeleteKnowledgeAttachment(ctx, attachmentID)
	}
	return ErrNotFound
}

func sanitizeAttachmentFilename(value string) string {
	value = strings.TrimSpace(path.Base(strings.ReplaceAll(value, "\\", "/")))
	value = strings.Trim(value, ". ")
	if value == "" {
		return "attachment"
	}
	return value
}

func attachmentExtension(filename string) string {
	extension := strings.ToLower(path.Ext(filename))
	if len(extension) > 16 {
		return ""
	}
	return extension
}

func (s *Service) CreateKnowledgeFromAnalysis(analysisID, author string) (Knowledge, error) {
	analysis, err := s.Get(analysisID)
	if err != nil {
		return Knowledge{}, err
	}
	steps := []string{}
	for _, recommendation := range analysis.Recommendations {
		steps = append(steps, recommendation.Steps...)
	}
	content := "## 故障摘要\n" + analysis.Summary + "\n\n## 根因判断\n" + analysis.RootCause + "\n\n## 处置步骤\n- " + strings.Join(steps, "\n- ")
	item, err := s.CreateKnowledge(KnowledgeInput{Title: analysis.Title, Category: knowledgeCategory(analysis.RootCause), Summary: analysis.Summary, Content: content, Tags: []string{analysis.Severity, analysis.RootCause, analysis.Target}, Status: "draft"}, author)
	if err != nil {
		return Knowledge{}, err
	}
	item.Source = "analysis"
	item.SourceID = analysis.ID
	s.mu.Lock()
	for i := range s.knowledge {
		if s.knowledge[i].ID == item.ID {
			s.knowledge[i].Source = item.Source
			s.knowledge[i].SourceID = item.SourceID
			item = s.knowledge[i]
			break
		}
	}
	s.mu.Unlock()
	if err = s.repo.SaveKnowledge(context.Background(), item); err != nil {
		return Knowledge{}, err
	}
	_ = s.indexKnowledge(item)
	return item, nil
}

func (s *Service) indexKnowledge(item Knowledge) error {
	vector, err := s.embedText(context.Background(), knowledgeEmbeddingText(item))
	if err != nil {
		return err
	}
	return s.repo.SaveKnowledgeEmbedding(context.Background(), item.ID, s.EmbeddingModel(), knowledgeEmbeddingDim, vector)
}

func (s *Service) embedText(ctx context.Context, value string) ([]float32, error) {
	value = maskSensitiveText(value)
	if s.embedder != nil {
		if vector, err := s.embedder.Embed(ctx, value); err == nil {
			return vector, nil
		}
	}
	return embedKnowledgeText(value), nil
}

func (s *Service) EmbeddingModel() string {
	if s.embedder == nil {
		return knowledgeEmbeddingModel
	}
	return s.embedder.Name()
}

func (s *Service) SearchKnowledgeHybrid(query string, includeDrafts bool) []Knowledge {
	lexical := s.searchKnowledge(query, includeDrafts)
	if strings.TrimSpace(query) == "" {
		return lexical
	}
	queryVector, embedErr := s.embedText(context.Background(), query)
	if embedErr != nil {
		return lexical
	}
	matches, err := s.repo.SearchKnowledgeVector(context.Background(), queryVector, 30)
	if err != nil || len(matches) == 0 {
		return lexical
	}
	_ = s.reloadKnowledge()
	s.mu.RLock()
	byID := make(map[string]Knowledge, len(s.knowledge))
	for _, item := range s.knowledge {
		if includeDrafts || item.Status == "published" {
			byID[item.ID] = item
		}
	}
	s.mu.RUnlock()
	return mergeKnowledgeHybrid(lexical, matches, byID, 30)
}

func (s *Service) ReindexKnowledge() (int, error) {
	_ = s.reloadKnowledge()
	s.mu.RLock()
	items := append([]Knowledge(nil), s.knowledge...)
	s.mu.RUnlock()
	for _, item := range items {
		if err := s.indexKnowledge(item); err != nil {
			return 0, err
		}
	}
	return len(items), nil
}

func normalizeKnowledgeInput(existing Knowledge, input KnowledgeInput) (Knowledge, error) {
	item := existing
	if value := strings.TrimSpace(input.SpaceID); value != "" {
		item.SpaceID = value
	}
	if value := strings.TrimSpace(input.FolderID); value != "" {
		item.FolderID = value
	}
	item.Title = strings.TrimSpace(input.Title)
	item.Category = strings.TrimSpace(input.Category)
	item.Summary = strings.TrimSpace(input.Summary)
	item.Content = strings.TrimSpace(input.Content)
	item.ContentFormat = strings.ToLower(strings.TrimSpace(input.ContentFormat))
	if item.ContentFormat == "" {
		item.ContentFormat = "markdown"
	}
	if item.ContentFormat != "markdown" && item.ContentFormat != "html" {
		return Knowledge{}, ErrValidation
	}
	if value := strings.TrimSpace(input.Owner); value != "" {
		item.Owner = value
	}
	item.Visibility = strings.ToLower(strings.TrimSpace(input.Visibility))
	if item.Visibility == "" {
		item.Visibility = "organization"
	}
	if item.Visibility != "private" && item.Visibility != "project" && item.Visibility != "organization" {
		return Knowledge{}, ErrValidation
	}
	item.Tags = make([]string, 0, len(input.Tags))
	for _, tag := range input.Tags {
		if value := strings.TrimSpace(tag); value != "" && !containsString(item.Tags, value) {
			item.Tags = append(item.Tags, value)
		}
	}
	item.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if item.Status == "" {
		item.Status = "published"
	}
	if item.Status != "published" && item.Status != "draft" && item.Status != "archived" {
		return Knowledge{}, ErrValidation
	}
	if item.Title == "" || item.Summary == "" || item.Content == "" {
		return Knowledge{}, ErrValidation
	}
	if input.Related != nil {
		item.Related = normalizeKnowledgeRefs(input.Related)
	}
	return item, nil
}

func normalizeKnowledgeRefs(items []KnowledgeRef) []KnowledgeRef {
	result := make([]KnowledgeRef, 0, len(items))
	for _, item := range items {
		item.Type = strings.ToLower(strings.TrimSpace(item.Type))
		item.ID = strings.TrimSpace(item.ID)
		item.Name = strings.TrimSpace(item.Name)
		item.Status = strings.TrimSpace(item.Status)
		if item.ID == "" || (item.Type != "asset" && item.Type != "ticket" && item.Type != "alert") {
			continue
		}
		duplicate := false
		for _, existing := range result {
			if existing.Type == item.Type && existing.ID == item.ID {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, item)
		}
	}
	return result
}

func (s *Service) resolveKnowledgeRefs(items []KnowledgeRef) []KnowledgeRef {
	result := normalizeKnowledgeRefs(items)
	for index := range result {
		item := &result[index]
		switch item.Type {
		case "asset":
			if s.assets != nil {
				if asset, err := s.assets.GetAsset(item.ID); err == nil {
					item.Name, item.Status = asset.Name, asset.Status
				}
			}
		case "ticket":
			if s.tickets != nil {
				if tickets, err := s.tickets.List(); err == nil {
					for _, ticket := range tickets {
						if ticket.ID == item.ID {
							item.Name, item.Status = ticket.Title, ticket.Status
							break
						}
					}
				}
			}
		case "alert":
			if s.alerts != nil {
				for _, alert := range s.alerts.Alerts() {
					if alert.ID == item.ID {
						item.Name, item.Status = alert.Title, alert.Status
						break
					}
				}
			}
		}
	}
	return result
}

func knowledgeCategory(rootCause string) string {
	switch {
	case strings.Contains(rootCause, "数据库"):
		return "数据库"
	case strings.Contains(rootCause, "Kubernetes") || strings.Contains(rootCause, "Pod"):
		return "Kubernetes"
	case strings.Contains(rootCause, "网络"):
		return "网络"
	case strings.Contains(rootCause, "磁盘") || strings.Contains(rootCause, "内存"):
		return "主机"
	default:
		return "故障处理"
	}
}
