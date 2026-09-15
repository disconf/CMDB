package releases

import (
	"cmdb/gateway-bff/internal/demo"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("release not found")
var ErrValidation = errors.New("validation")
var ErrInvalidState = errors.New("invalid state")
var ErrUnauthorized = errors.New("unauthorized")
var ErrExpired = errors.New("authorization expired")

const (
	StatusAwaitingApproval = "awaiting_approval"
	StatusApproved         = "approved"
	StatusRejected         = "rejected"
	StatusPending          = "pending"
	StatusRunning          = "running"
	StatusSuccess          = "success"
	StatusFailed           = "failed"
	StatusRollingBack      = "rolling-back"
	StatusCancelled        = "cancelled"
)

type Stage struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Duration string `json:"duration"`
	Message  string `json:"message"`
}

type Release struct {
	ID                     string  `json:"id"`
	Application            string  `json:"application"`
	Version                string  `json:"version"`
	PreviousVersion        string  `json:"previousVersion"`
	Environment            string  `json:"environment"`
	Status                 string  `json:"status"`
	Progress               int     `json:"progress"`
	Operator               string  `json:"operator"`
	StartedAt              string  `json:"startedAt"`
	Strategy               string  `json:"strategy"`
	Approval               string  `json:"approval"`
	TicketID               string  `json:"ticketId"`
	Source                 string  `json:"source"`
	ExternalID             string  `json:"externalId"`
	Commit                 string  `json:"commit"`
	ApprovedBy             string  `json:"approvedBy,omitempty"`
	ApprovedAt             string  `json:"approvedAt,omitempty"`
	ApprovalComment        string  `json:"approvalComment,omitempty"`
	AuthorizationExpiresAt string  `json:"authorizationExpiresAt,omitempty"`
	AuthorizationUsedAt    string  `json:"authorizationUsedAt,omitempty"`
	Stages                 []Stage `json:"stages"`

	startedAt              time.Time
	updatedAt              time.Time
	approvedAt             time.Time
	authorizationExpiresAt time.Time
	authorizationUsedAt    time.Time
	authorizationHash      string
}

type Artifact struct {
	ID          string `json:"id"`
	Application string `json:"application"`
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	Size        string `json:"size"`
	CreatedAt   string `json:"createdAt"`
	Security    string `json:"security"`
	Source      string `json:"source"`
	URL         string `json:"url"`

	createdAt time.Time
}

type StartInput struct {
	Application string `json:"application"`
	Version     string `json:"version"`
	Environment string `json:"environment"`
	Operator    string `json:"operator"`
}

type EventInput struct {
	Source      string `json:"source"`
	ExternalID  string `json:"externalId"`
	Type        string `json:"type"`
	Application string `json:"application"`
	Version     string `json:"version"`
	Environment string `json:"environment"`
	Status      string `json:"status"`
	Progress    int    `json:"progress"`
	Stage       string `json:"stage"`
	Message     string `json:"message"`
	Duration    string `json:"duration"`
	Commit      string `json:"commit"`
	Size        string `json:"size"`
	Security    string `json:"security"`
	URL         string `json:"url"`
	TicketID    string `json:"ticketId"`
	Operator    string `json:"operator"`
}

type Summary struct {
	Today       int     `json:"today"`
	Running     int     `json:"running"`
	Success     int     `json:"success"`
	Failed      int     `json:"failed"`
	SuccessRate float64 `json:"successRate"`
}

type Service struct {
	mu        sync.RWMutex
	releases  []Release
	artifacts []Artifact
	next      int
	db        *sql.DB
}

func approvalStages() []Stage {
	return []Stage{
		{ID: "build", Name: "构建制品", Status: StatusSuccess, Message: "制品已就绪"},
		{ID: "scan", Name: "安全扫描", Status: StatusSuccess, Message: "安全扫描通过"},
		{ID: "approve", Name: "发布门禁", Status: StatusPending, Message: "等待审批人确认"},
		{ID: "deploy", Name: "部署应用", Status: StatusPending, Message: "审批通过后签发执行授权"},
		{ID: "verify", Name: "指标验证", Status: StatusPending, Message: "等待部署完成"},
	}
}
func stages(running bool) []Stage {
	return []Stage{
		{ID: "build", Name: "构建制品", Status: StatusSuccess, Duration: "01:42", Message: "镜像构建完成"},
		{ID: "scan", Name: "安全扫描", Status: StatusSuccess, Duration: "00:38", Message: "未发现高危漏洞"},
		{ID: "approve", Name: "发布门禁", Status: StatusSuccess, Duration: "00:12", Message: "审批与变更窗口已校验"},
		{ID: "deploy", Name: "灰度部署", Status: map[bool]string{true: StatusRunning, false: StatusSuccess}[running], Duration: "02:16", Message: "部署 2/4 实例"},
		{ID: "verify", Name: "指标验证", Status: map[bool]string{true: StatusPending, false: StatusSuccess}[running], Message: "等待执行"},
	}
}

func NewService() *Service {
	s := &Service{next: 1}
	if demo.Enabled() {
		now := time.Now()
		s.releases = []Release{
			demoRelease("rel-001", "订单服务", "v2.7.4", "v2.7.3", "production", StatusRunning, 72, "王强", "灰度发布", "approved", "TKT-20260715-001", stages(true), now.Add(-45*time.Minute)),
			demoRelease("rel-002", "数据分析 Worker", "v1.9.2", "v1.9.1", "production", StatusSuccess, 100, "陈明", "滚动发布", "approved", "TKT-20260715-006", stages(false), now.Add(-2*time.Hour)),
			demoRelease("rel-003", "支付网关", "v4.1.0", "v4.0.8", "production", StatusFailed, 64, "周敏", "蓝绿发布", "approved", "TKT-20260715-003", []Stage{{ID: "build", Name: "构建制品", Status: StatusSuccess, Duration: "01:10", Message: "完成"}, {ID: "scan", Name: "安全扫描", Status: StatusSuccess, Duration: "00:31", Message: "通过"}, {ID: "deploy", Name: "部署新版本", Status: StatusFailed, Duration: "01:24", Message: "健康检查失败"}}, now.Add(-3*time.Hour)),
		}
		s.artifacts = []Artifact{
			demoArtifact("art-01", "订单服务", "v2.7.4", "8ac91f2", "286 MB", "passed", now.Add(-50*time.Minute)),
			demoArtifact("art-02", "支付网关", "v4.1.0", "31bd808", "194 MB", "passed", now.Add(-3*time.Hour)),
			demoArtifact("art-03", "数据分析 Worker", "v1.9.2", "fa209cb", "412 MB", "passed", now.Add(-2*time.Hour)),
		}
		s.next = 4
	}
	if url := strings.TrimSpace(os.Getenv("DATABASE_URL")); url != "" {
		db, err := openPostgres(url)
		if err != nil {
			panic(fmt.Sprintf("initialize releases repository: %v", err))
		}
		s.db = db
	}
	return s
}

func demoRelease(id, application, version, previous, environment, status string, progress int, operator, strategy, approval, ticket string, pipeline []Stage, started time.Time) Release {
	item := Release{ID: id, Application: application, Version: version, PreviousVersion: previous, Environment: environment, Status: status, Progress: progress, Operator: operator, Strategy: strategy, Approval: approval, TicketID: ticket, Source: "demo", Stages: pipeline, startedAt: started, updatedAt: started}
	applyReleaseDerived(&item)
	return item
}

func demoArtifact(id, application, version, commit, size, security string, created time.Time) Artifact {
	item := Artifact{ID: id, Application: application, Version: version, Commit: commit, Size: size, Security: security, Source: "demo", createdAt: created}
	applyArtifactDerived(&item)
	return item
}
func (s *Service) List() ([]Release, error) {
	if s.db != nil {
		ctx, cancel := contextWithTimeout(8 * time.Second)
		defer cancel()
		return loadReleases(ctx, s.db)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Release, len(s.releases))
	for i := range s.releases {
		items[i] = cloneRelease(s.releases[i])
		applyReleaseDerived(&items[i])
	}
	return items, nil
}

func (s *Service) Artifacts() ([]Artifact, error) {
	if s.db != nil {
		ctx, cancel := contextWithTimeout(8 * time.Second)
		defer cancel()
		return loadArtifacts(ctx, s.db)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Artifact, len(s.artifacts))
	for i := range s.artifacts {
		items[i] = cloneArtifact(s.artifacts[i])
		applyArtifactDerived(&items[i])
	}
	return items, nil
}

func (s *Service) Summary() (Summary, error) {
	items, err := s.List()
	if err != nil {
		return Summary{}, err
	}
	return summarize(items), nil
}

func summarize(items []Release) Summary {
	now := time.Now()
	result := Summary{}
	finished := 0
	success := 0
	for _, item := range items {
		if item.startedAt.In(now.Location()).Format("2006-01-02") == now.Format("2006-01-02") {
			result.Today++
		}
		switch item.Status {
		case StatusRunning, StatusRollingBack:
			result.Running++
		case StatusSuccess:
			result.Success++
			success++
			finished++
		case StatusFailed:
			result.Failed++
			finished++
		}
	}
	if finished > 0 {
		result.SuccessRate = float64(success) * 100 / float64(finished)
	} else if demo.Enabled() {
		result.SuccessRate = 96.8
	}
	return result
}
func (s *Service) Start(in StartInput) (Release, error) {
	in.Application = strings.TrimSpace(in.Application)
	in.Version = strings.TrimSpace(in.Version)
	in.Environment = strings.TrimSpace(in.Environment)
	in.Operator = strings.TrimSpace(in.Operator)
	if in.Application == "" || in.Version == "" || in.Environment == "" {
		return Release{}, ErrValidation
	}
	now := time.Now()
	item := Release{
		ID: fmt.Sprintf("rel-manual-%d", now.UnixNano()), Application: in.Application, Version: in.Version,
		PreviousVersion: "未提供", Environment: in.Environment, Status: StatusAwaitingApproval, Progress: 10,
		Operator: in.Operator, Strategy: "灰度发布", Approval: "pending", Source: "manual",
		Stages: approvalStages(), startedAt: now, updatedAt: now,
	}
	applyReleaseDerived(&item)
	if s.db != nil {
		ctx, cancel := contextWithTimeout(8 * time.Second)
		defer cancel()
		if err := upsertRelease(ctx, s.db, item); err != nil {
			return Release{}, err
		}
		return cloneRelease(item), nil
	}
	s.mu.Lock()
	s.releases = append([]Release{item}, s.releases...)
	s.mu.Unlock()
	return cloneRelease(item), nil
}
func (s *Service) Approve(id, actor, comment string) (Release, string, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return Release{}, "", ErrValidation
	}
	token, hash, expiresAt, err := newAuthorization()
	if err != nil {
		return Release{}, "", err
	}
	item, err := s.mutateRelease(id, func(current *Release, now time.Time) error {
		if current.Status != StatusAwaitingApproval || current.Approval != "pending" {
			return ErrInvalidState
		}
		if actor == current.Operator {
			return ErrUnauthorized
		}
		current.Status = StatusApproved
		current.Progress = 20
		current.Approval = "approved"
		current.ApprovedBy = actor
		current.ApprovedAt = now.Format("2006-01-02 15:04")
		current.ApprovalComment = strings.TrimSpace(comment)
		current.authorizationHash = hash
		current.authorizationExpiresAt = expiresAt
		current.authorizationUsedAt = time.Time{}
		current.Stages = upsertStage(current.Stages, Stage{ID: "approve", Name: "发布门禁", Status: StatusSuccess, Message: "审批通过，已签发一次性执行授权"})
		current.Stages = upsertStage(current.Stages, Stage{ID: "deploy", Name: "部署应用", Status: StatusPending, Message: "执行授权有效，等待消费"})
		current.updatedAt = now
		applyReleaseDerived(current)
		return nil
	})
	if err != nil {
		return Release{}, "", err
	}
	return item, token, nil
}

func (s *Service) Reject(id, actor, comment string) (Release, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return Release{}, ErrValidation
	}
	return s.mutateRelease(id, func(current *Release, now time.Time) error {
		if current.Status != StatusAwaitingApproval || current.Approval != "pending" {
			return ErrInvalidState
		}
		current.Status = StatusRejected
		current.Progress = 10
		current.Approval = "rejected"
		current.ApprovedBy = actor
		current.ApprovedAt = now.Format("2006-01-02 15:04")
		current.ApprovalComment = strings.TrimSpace(comment)
		current.Stages = upsertStage(current.Stages, Stage{ID: "approve", Name: "发布门禁", Status: StatusFailed, Message: "审批未通过"})
		current.updatedAt = now
		applyReleaseDerived(current)
		return nil
	})
}

func (s *Service) AuthorizeExecution(id, token, actor string) (Release, error) {
	token = strings.TrimSpace(token)
	actor = strings.TrimSpace(actor)
	if token == "" || actor == "" {
		return Release{}, ErrValidation
	}
	hash := hashAuthorization(token)
	now := time.Now()
	return s.mutateRelease(id, func(current *Release, changeTime time.Time) error {
		if current.Status != StatusApproved || current.Approval != "approved" {
			return ErrInvalidState
		}
		if current.authorizationHash == "" || subtle.ConstantTimeCompare([]byte(hash), []byte(current.authorizationHash)) != 1 {
			return ErrUnauthorized
		}
		if !current.authorizationUsedAt.IsZero() {
			return ErrInvalidState
		}
		if current.authorizationExpiresAt.IsZero() || !now.Before(current.authorizationExpiresAt) {
			return ErrExpired
		}
		current.authorizationUsedAt = changeTime
		current.Status = StatusRunning
		current.Progress = 25
		current.Stages = upsertStage(current.Stages, Stage{ID: "deploy", Name: "部署应用", Status: StatusRunning, Message: "一次性执行授权已消费，等待流水线继续部署"})
		current.updatedAt = changeTime
		applyReleaseDerived(current)
		return nil
	})
}

func (s *Service) mutateRelease(id string, change func(*Release, time.Time) error) (Release, error) {
	now := time.Now()
	if s.db != nil {
		ctx, cancel := contextWithTimeout(8 * time.Second)
		defer cancel()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return Release{}, err
		}
		defer tx.Rollback()

		item, err := loadReleaseByIDForUpdate(ctx, tx, id)
		if err != nil {
			return Release{}, err
		}
		if err := change(&item, now); err != nil {
			return Release{}, err
		}
		item.updatedAt = now
		applyReleaseDerived(&item)
		if err := upsertRelease(ctx, tx, item); err != nil {
			return Release{}, err
		}
		if err := tx.Commit(); err != nil {
			return Release{}, err
		}
		return cloneRelease(item), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.releases {
		if s.releases[i].ID != id {
			continue
		}
		item := cloneRelease(s.releases[i])
		if err := change(&item, now); err != nil {
			return Release{}, err
		}
		item.updatedAt = now
		applyReleaseDerived(&item)
		s.releases[i] = item
		return cloneRelease(item), nil
	}
	return Release{}, ErrNotFound
}

func newAuthorization() (string, string, time.Time, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", "", time.Time{}, err
	}
	token := hex.EncodeToString(value)
	return token, hashAuthorization(token), time.Now().Add(10 * time.Minute), nil
}

func hashAuthorization(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (s *Service) Rollback(id, operator string) (Release, error) {
	operator = strings.TrimSpace(operator)
	if operator == "" {
		return Release{}, ErrValidation
	}
	now := time.Now()
	if s.db != nil {
		ctx, cancel := contextWithTimeout(8 * time.Second)
		defer cancel()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return Release{}, err
		}
		defer tx.Rollback()

		item, err := loadReleaseByIDForUpdate(ctx, tx, id)
		if err != nil {
			return Release{}, err
		}
		item.Status = StatusRollingBack
		item.Operator = operator
		item.Progress = 15
		item.Stages = append(item.Stages, Stage{ID: "rollback", Name: "回滚至 " + item.PreviousVersion, Status: StatusRunning, Message: "正在恢复流量"})
		item.updatedAt = now
		applyReleaseDerived(&item)
		if err := upsertRelease(ctx, tx, item); err != nil {
			return Release{}, err
		}
		if err := tx.Commit(); err != nil {
			return Release{}, err
		}
		return cloneRelease(item), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.releases {
		if s.releases[i].ID == id {
			s.releases[i].Status = StatusRollingBack
			s.releases[i].Operator = operator
			s.releases[i].Progress = 15
			s.releases[i].Stages = append(s.releases[i].Stages, Stage{ID: "rollback", Name: "回滚至 " + s.releases[i].PreviousVersion, Status: StatusRunning, Message: "正在恢复流量"})
			s.releases[i].updatedAt = now
			applyReleaseDerived(&s.releases[i])
			return cloneRelease(s.releases[i]), nil
		}
	}
	return Release{}, ErrNotFound
}
func (s *Service) ApplyEvent(in EventInput) (Release, error) {
	normalizeEventInput(&in)
	if in.Application == "" || in.Version == "" || in.Environment == "" || in.Source == "" {
		return Release{}, ErrValidation
	}
	now := time.Now()
	id := releaseIDForEvent(in, now)
	if s.db != nil {
		ctx, cancel := contextWithTimeout(8 * time.Second)
		defer cancel()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return Release{}, err
		}
		defer tx.Rollback()
		if _, pendingErr := findPendingManualRelease(ctx, tx, in); pendingErr == nil {
			return Release{}, ErrInvalidState
		} else if !errors.Is(pendingErr, ErrNotFound) {
			return Release{}, pendingErr
		}
		item, err := loadReleaseByIDForUpdate(ctx, tx, id)
		if errors.Is(err, ErrNotFound) {
			item = newEventRelease(id, in, now)
		} else if err != nil {
			return Release{}, err
		}
		if item.Source == "manual" && item.Approval == "pending" {
			return Release{}, ErrInvalidState
		}
		applyEvent(&item, in, now)
		if err := upsertRelease(ctx, tx, item); err != nil {
			return Release{}, err
		}
		if artifact, ok := artifactFromEvent(in, now); ok {
			if err := upsertArtifact(ctx, tx, artifact); err != nil {
				return Release{}, err
			}
		}
		if err := tx.Commit(); err != nil {
			return Release{}, err
		}
		return cloneRelease(item), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.releases {
		if s.releases[i].Source == "manual" && s.releases[i].Approval == "pending" && s.releases[i].Application == in.Application && s.releases[i].Version == in.Version && s.releases[i].Environment == in.Environment {
			return Release{}, ErrInvalidState
		}
	}
	index := -1
	for i := range s.releases {
		if s.releases[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		item := newEventRelease(id, in, now)
		applyEvent(&item, in, now)
		s.releases = append([]Release{item}, s.releases...)
		if artifact, ok := artifactFromEvent(in, now); ok {
			s.artifacts = upsertArtifactMemory(s.artifacts, artifact)
		}
		return cloneRelease(item), nil
	}
	if s.releases[index].Source == "manual" && s.releases[index].Approval == "pending" {
		return Release{}, ErrInvalidState
	}
	applyEvent(&s.releases[index], in, now)
	if artifact, ok := artifactFromEvent(in, now); ok {
		s.artifacts = upsertArtifactMemory(s.artifacts, artifact)
	}
	return cloneRelease(s.releases[index]), nil
}
func normalizeEventInput(in *EventInput) {
	in.Source = strings.ToLower(strings.TrimSpace(in.Source))
	in.ExternalID = strings.TrimSpace(in.ExternalID)
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	in.Application = strings.TrimSpace(in.Application)
	in.Version = strings.TrimSpace(in.Version)
	in.Environment = strings.TrimSpace(in.Environment)
	in.Status = normalizeStatus(in.Status)
	in.Stage = strings.TrimSpace(in.Stage)
	in.Message = strings.TrimSpace(in.Message)
	in.Duration = strings.TrimSpace(in.Duration)
	in.Commit = strings.TrimSpace(in.Commit)
	in.Size = strings.TrimSpace(in.Size)
	in.Security = strings.TrimSpace(in.Security)
	in.URL = strings.TrimSpace(in.URL)
	in.TicketID = strings.TrimSpace(in.TicketID)
	in.Operator = strings.TrimSpace(in.Operator)
	if in.Source == "" {
		in.Source = "external"
	}
	if in.Type == "" {
		in.Type = "deployment"
	}
	if in.Status == "" {
		in.Status = StatusRunning
	}
	if in.Operator == "" {
		in.Operator = in.Source
	}
}

func newEventRelease(id string, in EventInput, now time.Time) Release {
	item := Release{
		ID: id, Application: in.Application, Version: in.Version, PreviousVersion: "未提供",
		Environment: in.Environment, Status: in.Status, Operator: in.Operator,
		Strategy: "外部流水线发布", Approval: "approved", TicketID: in.TicketID,
		Source: in.Source, ExternalID: in.ExternalID, Commit: in.Commit,
		startedAt: now, updatedAt: now,
	}
	return item
}

func applyEvent(item *Release, in EventInput, now time.Time) {
	item.Source = in.Source
	item.ExternalID = in.ExternalID
	item.Status = in.Status
	item.Operator = in.Operator
	item.TicketID = firstNonEmpty(in.TicketID, item.TicketID)
	item.Commit = firstNonEmpty(in.Commit, item.Commit)
	if in.Progress > 0 {
		item.Progress = clampProgress(in.Progress)
	} else {
		item.Progress = progressForStatus(in.Status, item.Progress)
	}
	stageID := in.Stage
	if stageID == "" {
		stageID = stageIDForEvent(in)
	}
	stage := Stage{ID: stageID, Name: stageName(stageID, in.Type), Status: in.Status, Duration: in.Duration, Message: in.Message}
	if stage.Message == "" {
		stage.Message = stageName(stageID, in.Type)
	}
	item.Stages = upsertStage(item.Stages, stage)
	item.updatedAt = now
	if item.startedAt.IsZero() {
		item.startedAt = now
	}
	applyReleaseDerived(item)
}

func upsertStage(items []Stage, incoming Stage) []Stage {
	for i := range items {
		if items[i].ID == incoming.ID {
			items[i] = incoming
			return items
		}
	}
	return append(items, incoming)
}
func artifactFromEvent(in EventInput, now time.Time) (Artifact, bool) {
	if in.Type != "artifact" && in.Commit == "" && in.Size == "" && in.Security == "" && in.URL == "" {
		return Artifact{}, false
	}
	key := in.Application + "|" + in.Version + "|" + in.Commit
	if in.Commit == "" {
		key += "|" + in.Size + "|" + in.URL + "|" + in.Source + "|" + in.ExternalID
	}
	id := "art-" + digest(key)[:16]
	item := Artifact{ID: id, Application: in.Application, Version: in.Version, Commit: in.Commit, Size: in.Size, Security: firstNonEmpty(in.Security, "unknown"), Source: in.Source, URL: in.URL, createdAt: now}
	applyArtifactDerived(&item)
	return item, true
}

func upsertArtifactMemory(items []Artifact, incoming Artifact) []Artifact {
	for i := range items {
		if items[i].ID == incoming.ID {
			items[i] = incoming
			return items
		}
	}
	return append([]Artifact{incoming}, items...)
}

func releaseIDForEvent(in EventInput, now time.Time) string {
	external := in.ExternalID
	if external == "" {
		external = now.Format(time.RFC3339Nano)
	}
	key := strings.Join([]string{external, in.Application, in.Version, in.Environment}, "|")
	return "rel-" + digest(key)[:20]
}

func digest(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func stageIDForEvent(in EventInput) string {
	if in.Type == "pipeline" {
		return "pipeline"
	}
	if in.Type == "scan" {
		return "scan"
	}
	if in.Type == "artifact" {
		return "artifact"
	}
	switch in.Status {
	case StatusSuccess:
		return "verify"
	case StatusFailed:
		return "deploy"
	default:
		return "deploy"
	}
}

func stageName(id, kind string) string {
	if id == "" {
		id = kind
	}
	switch id {
	case "build":
		return "构建制品"
	case "artifact":
		return "制品推送"
	case "scan":
		return "安全扫描"
	case "approve":
		return "发布门禁"
	case "deploy":
		return "部署应用"
	case "verify":
		return "指标验证"
	case "rollback":
		return "版本回滚"
	case "pipeline":
		return "CI 流水线"
	default:
		return id
	}
}
func normalizeStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "succeeded", "success", "passed", "healthy", "completed", "complete":
		return StatusSuccess
	case "failed", "failure", "error", "degraded", "unhealthy":
		return StatusFailed
	case "running", "in_progress", "progressing", "deploying", "syncing":
		return StatusRunning
	case "rolling-back", "rollback", "rolling_back":
		return StatusRollingBack
	case "cancelled", "canceled", "skipped":
		return StatusCancelled
	case "pending", "queued", "waiting":
		return StatusPending
	default:
		if value == "" {
			return StatusRunning
		}
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func progressForStatus(status string, current int) int {
	switch status {
	case StatusSuccess, StatusFailed:
		return 100
	case StatusRollingBack:
		return minPositive(current, 15)
	case StatusRunning:
		if current < 20 {
			return 20
		}
		return current
	case StatusPending:
		return 10
	default:
		return current
	}
}

func minPositive(value, fallback int) int {
	if value > 0 && value < fallback {
		return value
	}
	return fallback
}

func clampProgress(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func applyReleaseDerived(item *Release) {
	if item.startedAt.IsZero() {
		item.startedAt = time.Now()
	}
	if item.updatedAt.IsZero() {
		item.updatedAt = item.startedAt
	}
	item.StartedAt = item.startedAt.Local().Format("2006-01-02 15:04")
	if !item.approvedAt.IsZero() {
		item.ApprovedAt = item.approvedAt.Local().Format("2006-01-02 15:04")
	}
	if !item.authorizationExpiresAt.IsZero() {
		item.AuthorizationExpiresAt = item.authorizationExpiresAt.Local().Format(time.RFC3339)
	}
	if !item.authorizationUsedAt.IsZero() {
		item.AuthorizationUsedAt = item.authorizationUsedAt.Local().Format(time.RFC3339)
	}
	if item.Stages == nil {
		item.Stages = []Stage{}
	}
	if item.Progress == 0 {
		item.Progress = progressForStatus(item.Status, 0)
	}
}

func applyArtifactDerived(item *Artifact) {
	if item.createdAt.IsZero() {
		item.createdAt = time.Now()
	}
	item.CreatedAt = item.createdAt.Local().Format("2006-01-02 15:04")
}

func cloneRelease(item Release) Release {
	item.Stages = append([]Stage(nil), item.Stages...)
	return item
}

func cloneArtifact(item Artifact) Artifact {
	return item
}
