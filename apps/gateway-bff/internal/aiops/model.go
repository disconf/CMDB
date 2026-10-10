package aiops

import "context"

type Evidence struct {
	Type   string `json:"type"`
	Source string `json:"source,omitempty"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Weight int    `json:"weight"`
}

type Recommendation struct {
	RunbookID string   `json:"runbookId"`
	Title     string   `json:"title"`
	Risk      string   `json:"risk"`
	Impact    string   `json:"impact"`
	Duration  string   `json:"duration"`
	Steps     []string `json:"steps"`
}

type TimelineEvent struct {
	Time    string `json:"time"`
	Type    string `json:"type"`
	Source  string `json:"source"`
	Message string `json:"message"`
}

type RelatedAsset struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	IP           string `json:"ip"`
	Environment  string `json:"environment"`
	ProjectGroup string `json:"projectGroup"`
	Owner        string `json:"owner"`
	Location     string `json:"location"`
	Relation     string `json:"relation,omitempty"`
	CredentialID string `json:"credentialId,omitempty"`
}

type ImpactSummary struct {
	Total    int      `json:"total"`
	Healthy  int      `json:"healthy"`
	Warning  int      `json:"warning"`
	Critical int      `json:"critical"`
	Items    []string `json:"items"`
}

type RelatedTicket struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
}

type RelatedRelease struct {
	ID          string `json:"id"`
	Application string `json:"application"`
	Version     string `json:"version"`
	Status      string `json:"status"`
	StartedAt   string `json:"startedAt"`
}

type Feedback struct {
	Status             string `json:"status"`
	CorrectedRootCause string `json:"correctedRootCause,omitempty"`
	Note               string `json:"note,omitempty"`
	Reviewer           string `json:"reviewer"`
	ReviewedAt         string `json:"reviewedAt"`
}

type Analysis struct {
	ID                   string           `json:"id"`
	AlertID              string           `json:"alertId"`
	AlertTitle           string           `json:"alertTitle,omitempty"`
	Title                string           `json:"title"`
	Status               string           `json:"status"`
	Severity             string           `json:"severity"`
	Target               string           `json:"target"`
	RootCause            string           `json:"rootCause"`
	Confidence           int              `json:"confidence"`
	Summary              string           `json:"summary"`
	CreatedAt            string           `json:"createdAt"`
	UpdatedAt            string           `json:"updatedAt,omitempty"`
	Evidence             []Evidence       `json:"evidence"`
	Recommendations      []Recommendation `json:"recommendations"`
	RelatedAssets        []RelatedAsset   `json:"relatedAssets"`
	Impact               ImpactSummary    `json:"impact"`
	Timeline             []TimelineEvent  `json:"timeline"`
	Tickets              []RelatedTicket  `json:"tickets"`
	Releases             []RelatedRelease `json:"releases"`
	DataSources          []string         `json:"dataSources"`
	ConfidenceAdjustment int              `json:"confidenceAdjustment"`
	ConfidenceReason     string           `json:"confidenceReason,omitempty"`
	AppliedFeedbackCount int              `json:"appliedFeedbackCount"`
	Feedback             *Feedback        `json:"feedback,omitempty"`
}

type Knowledge struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Category string   `json:"category"`
	Summary  string   `json:"summary"`
	Tags     []string `json:"tags"`
	Helpful  int      `json:"helpful"`
}

type ChangePlanStep struct {
	Order  int    `json:"order"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type ChangePlan struct {
	TargetAssetID       string           `json:"targetAssetId,omitempty"`
	TargetAssetName     string           `json:"targetAssetName,omitempty"`
	Environment         string           `json:"environment,omitempty"`
	RunbookID           string           `json:"runbookId"`
	RunbookTitle        string           `json:"runbookTitle"`
	ExecutionMode       string           `json:"executionMode"`
	ChangeWindow        string           `json:"changeWindow,omitempty"`
	RollbackRunbookID   string           `json:"rollbackRunbookId,omitempty"`
	RollbackRunbookName string           `json:"rollbackRunbookName,omitempty"`
	SourceExecutionID   string           `json:"sourceExecutionId,omitempty"`
	Preconditions       []string         `json:"preconditions"`
	Steps               []ChangePlanStep `json:"steps"`
	Verification        []string         `json:"verification"`
	RollbackPlan        []string         `json:"rollbackPlan"`
	PlannedBy           string           `json:"plannedBy"`
	PlannedAt           string           `json:"plannedAt"`
}

type Execution struct {
	ID                  string     `json:"id"`
	AnalysisID          string     `json:"analysisId"`
	RunbookID           string     `json:"runbookId"`
	RunbookTitle        string     `json:"runbookTitle,omitempty"`
	Status              string     `json:"status"`
	RequestedBy         string     `json:"requestedBy"`
	RequestedAt         string     `json:"requestedAt"`
	ApprovalRequired    bool       `json:"approvalRequired"`
	TicketID            string     `json:"ticketId,omitempty"`
	JobID               string     `json:"jobId,omitempty"`
	ApprovedBy          string     `json:"approvedBy,omitempty"`
	ApprovedAt          string     `json:"approvedAt,omitempty"`
	FinishedAt          string     `json:"finishedAt,omitempty"`
	ResultSummary       string     `json:"resultSummary,omitempty"`
	Error               string     `json:"error,omitempty"`
	Simulated           bool       `json:"simulated"`
	Rollback            bool       `json:"rollback"`
	ParentExecutionID   string     `json:"parentExecutionId,omitempty"`
	ChangePlan          ChangePlan `json:"changePlan"`
	VerificationStatus  string     `json:"verificationStatus,omitempty"`
	VerificationSummary string     `json:"verificationSummary,omitempty"`
	VerifiedAt          string     `json:"verifiedAt,omitempty"`
}

type Summary struct {
	OpenAnomalies     int     `json:"openAnomalies"`
	AnalyzedToday     int     `json:"analyzedToday"`
	AverageConfidence float64 `json:"averageConfidence"`
	Accuracy          float64 `json:"accuracy"`
	KnowledgeCount    int     `json:"knowledgeCount"`
	PendingActions    int     `json:"pendingActions"`
	FeedbackCount     int     `json:"feedbackCount"`
	ConfirmedFeedback int     `json:"confirmedFeedback"`
	RejectedFeedback  int     `json:"rejectedFeedback"`
}

type AnalyzeInput struct {
	AlertID string `json:"alertId"`
}

type repository interface {
	LoadAnalyses(context.Context) ([]Analysis, error)
	SaveAnalysis(context.Context, Analysis) error
	LoadExecutions(context.Context) ([]Execution, error)
	SaveExecution(context.Context, Execution) error
	LoadRunbookPolicies(context.Context) ([]RunbookPolicy, error)
	SaveRunbookPolicy(context.Context, RunbookPolicy) error
	LoadChangeControl(context.Context) (ChangeControl, error)
	SaveChangeControl(context.Context, ChangeControl) error
	LoadCanaryValidations(context.Context) ([]CanaryValidation, error)
	SaveCanaryValidation(context.Context, CanaryValidation) error
}

type FeedbackInput struct {
	Status             string `json:"status"`
	CorrectedRootCause string `json:"correctedRootCause,omitempty"`
	Note               string `json:"note,omitempty"`
}

type RunbookPolicy struct {
	RunbookID           string   `json:"runbookId"`
	Title               string   `json:"title"`
	TemplateID          string   `json:"templateId"`
	Mode                string   `json:"mode"`
	ExecutionMode       string   `json:"executionMode"`
	Risk                string   `json:"risk"`
	ApprovalRequired    bool     `json:"approvalRequired"`
	Enabled             bool     `json:"enabled"`
	MaxTargets          int      `json:"maxTargets"`
	AllowedEnvironments []string `json:"allowedEnvironments"`
	ChangeWindow        string   `json:"changeWindow"`
	RollbackRunbookID   string   `json:"rollbackRunbookId,omitempty"`
	LiveDailyLimit      int      `json:"liveDailyLimit"`
	LiveCooldownMinutes int      `json:"liveCooldownMinutes"`
	RequireRollback     bool     `json:"requireRollback"`
	RequireCanary       bool     `json:"requireCanary"`
	UpdatedAt           string   `json:"updatedAt,omitempty"`
	UpdatedBy           string   `json:"updatedBy,omitempty"`
}

type RunbookExecutionStat struct {
	RunbookID    string  `json:"runbookId"`
	RunbookTitle string  `json:"runbookTitle"`
	Total        int     `json:"total"`
	Simulated    int     `json:"simulated"`
	Live         int     `json:"live"`
	Success      int     `json:"success"`
	Failed       int     `json:"failed"`
	InProgress   int     `json:"inProgress"`
	SuccessRate  float64 `json:"successRate"`
}

type CanaryValidation struct {
	ID          string   `json:"id"`
	RunbookID   string   `json:"runbookId"`
	AssetID     string   `json:"assetId"`
	AssetName   string   `json:"assetName"`
	Status      string   `json:"status"`
	JobID       string   `json:"jobId,omitempty"`
	Summary     string   `json:"summary"`
	Checks      []string `json:"checks"`
	RequestedBy string   `json:"requestedBy"`
	StartedAt   string   `json:"startedAt"`
	FinishedAt  string   `json:"finishedAt,omitempty"`
}

type ChangeControl struct {
	ID          string `json:"id"`
	Frozen      bool   `json:"frozen"`
	LiveEnabled bool   `json:"liveEnabled"`
	Reason      string `json:"reason,omitempty"`
	FreezeUntil string `json:"freezeUntil,omitempty"`
	UpdatedBy   string `json:"updatedBy,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

type RunbookPreflightCheck struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type RunbookPreflight struct {
	RunbookID     string                  `json:"runbookId"`
	AnalysisID    string                  `json:"analysisId,omitempty"`
	Ready         bool                    `json:"ready"`
	Checks        []RunbookPreflightCheck `json:"checks"`
	NextAllowedAt string                  `json:"nextAllowedAt,omitempty"`
	GeneratedAt   string                  `json:"generatedAt"`
}

type ExecutionStats struct {
	Total           int                    `json:"total"`
	Simulated       int                    `json:"simulated"`
	Live            int                    `json:"live"`
	PendingApproval int                    `json:"pendingApproval"`
	Running         int                    `json:"running"`
	Success         int                    `json:"success"`
	Failed          int                    `json:"failed"`
	Rejected        int                    `json:"rejected"`
	SuccessRate     float64                `json:"successRate"`
	ByRunbook       []RunbookExecutionStat `json:"byRunbook"`
}

type EvidenceInput struct {
	Tools []string `json:"tools,omitempty"`
}

type Report struct {
	Analysis    Analysis    `json:"analysis"`
	Executions  []Execution `json:"executions"`
	GeneratedAt string      `json:"generatedAt"`
	Markdown    string      `json:"markdown"`
}

type RootCauseFeedbackStat struct {
	RootCause  string  `json:"rootCause"`
	Confirmed  int     `json:"confirmed"`
	Rejected   int     `json:"rejected"`
	Partial    int     `json:"partial"`
	Accuracy   float64 `json:"accuracy"`
	Adjustment int     `json:"adjustment"`
}

type FeedbackStats struct {
	Total       int                     `json:"total"`
	Confirmed   int                     `json:"confirmed"`
	Rejected    int                     `json:"rejected"`
	Partial     int                     `json:"partial"`
	Accuracy    float64                 `json:"accuracy"`
	ByRootCause []RootCauseFeedbackStat `json:"byRootCause"`
}
