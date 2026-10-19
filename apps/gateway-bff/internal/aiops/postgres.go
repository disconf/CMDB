package aiops

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const aiopsSchema = `
CREATE TABLE IF NOT EXISTS aiops_analyses (
  id text PRIMARY KEY,
  alert_id text NOT NULL,
  alert_title text NOT NULL DEFAULT '',
  title text NOT NULL,
  status text NOT NULL,
  severity text NOT NULL DEFAULT '',
  target text NOT NULL DEFAULT '',
  root_cause text NOT NULL DEFAULT '',
  confidence integer NOT NULL DEFAULT 0,
  confidence_adjustment integer NOT NULL DEFAULT 0,
  confidence_reason text NOT NULL DEFAULT '',
  applied_feedback_count integer NOT NULL DEFAULT 0,
  summary text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  evidence jsonb NOT NULL DEFAULT '[]',
  recommendations jsonb NOT NULL DEFAULT '[]',
  knowledge_suggestions jsonb NOT NULL DEFAULT '[]',
  related_assets jsonb NOT NULL DEFAULT '[]',
  impact jsonb NOT NULL DEFAULT '{}',
  timeline jsonb NOT NULL DEFAULT '[]',
  tickets jsonb NOT NULL DEFAULT '[]',
  releases jsonb NOT NULL DEFAULT '[]',
  data_sources jsonb NOT NULL DEFAULT '[]',
  feedback jsonb NOT NULL DEFAULT '{}'
);
ALTER TABLE aiops_analyses ADD COLUMN IF NOT EXISTS feedback jsonb NOT NULL DEFAULT '{}';
ALTER TABLE aiops_analyses ADD COLUMN IF NOT EXISTS confidence_adjustment integer NOT NULL DEFAULT 0;
ALTER TABLE aiops_analyses ADD COLUMN IF NOT EXISTS confidence_reason text NOT NULL DEFAULT '';
ALTER TABLE aiops_analyses ADD COLUMN IF NOT EXISTS applied_feedback_count integer NOT NULL DEFAULT 0;
ALTER TABLE aiops_analyses ADD COLUMN IF NOT EXISTS knowledge_suggestions jsonb NOT NULL DEFAULT '[]';
CREATE UNIQUE INDEX IF NOT EXISTS idx_aiops_analyses_alert_id ON aiops_analyses(alert_id);
CREATE INDEX IF NOT EXISTS idx_aiops_analyses_created_at ON aiops_analyses(created_at DESC);
CREATE TABLE IF NOT EXISTS aiops_knowledge (
  id text PRIMARY KEY,
  title text NOT NULL,
  category text NOT NULL DEFAULT '',
  summary text NOT NULL DEFAULT '',
  content text NOT NULL DEFAULT '',
  tags jsonb NOT NULL DEFAULT '[]',
  helpful integer NOT NULL DEFAULT 0,
  views integer NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'published',
  source text NOT NULL DEFAULT 'manual',
  source_id text NOT NULL DEFAULT '',
  author text NOT NULL DEFAULT '',
  related jsonb NOT NULL DEFAULT '[]',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS content text NOT NULL DEFAULT '';
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS views integer NOT NULL DEFAULT 0;
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'published';
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'manual';
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS source_id text NOT NULL DEFAULT '';
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS author text NOT NULL DEFAULT '';
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS related jsonb NOT NULL DEFAULT '[]';
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS space_id text NOT NULL DEFAULT '';
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS folder_id text NOT NULL DEFAULT '';
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS content_format text NOT NULL DEFAULT 'markdown';
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS owner text NOT NULL DEFAULT '';
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS visibility text NOT NULL DEFAULT 'organization';
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS doc_version integer NOT NULL DEFAULT 1;
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS published_version integer NOT NULL DEFAULT 0;
ALTER TABLE aiops_knowledge ADD COLUMN IF NOT EXISTS deleted_at timestamptz;
CREATE INDEX IF NOT EXISTS idx_aiops_knowledge_updated_at ON aiops_knowledge(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_aiops_knowledge_category ON aiops_knowledge(category);
CREATE INDEX IF NOT EXISTS idx_aiops_knowledge_space_folder ON aiops_knowledge(space_id, folder_id);
CREATE TABLE IF NOT EXISTS knowledge_spaces (
  id text PRIMARY KEY,
  name text NOT NULL,
  code text NOT NULL UNIQUE,
  description text NOT NULL DEFAULT '',
  kind text NOT NULL DEFAULT 'organization',
  visibility text NOT NULL DEFAULT 'organization',
  owner text NOT NULL DEFAULT '',
  created_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE TABLE IF NOT EXISTS knowledge_folders (
  id text PRIMARY KEY,
  space_id text NOT NULL REFERENCES knowledge_spaces(id) ON DELETE CASCADE,
  parent_id text NOT NULL DEFAULT '',
  name text NOT NULL,
  path text NOT NULL DEFAULT '',
  description text NOT NULL DEFAULT '',
  owner text NOT NULL DEFAULT '',
  sort_order integer NOT NULL DEFAULT 0,
  created_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_knowledge_folders_space_parent ON knowledge_folders(space_id, parent_id, sort_order);
CREATE TABLE IF NOT EXISTS knowledge_acl (
  id text PRIMARY KEY,
  resource_type text NOT NULL,
  resource_id text NOT NULL,
  subject_type text NOT NULL,
  subject_id text NOT NULL,
  permission text NOT NULL,
  effect text NOT NULL DEFAULT 'allow',
  inherit boolean NOT NULL DEFAULT true,
  created_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_acl_unique ON knowledge_acl(resource_type, resource_id, subject_type, subject_id);
CREATE INDEX IF NOT EXISTS idx_knowledge_acl_resource ON knowledge_acl(resource_type, resource_id);
CREATE TABLE IF NOT EXISTS aiops_knowledge_versions (
  id text PRIMARY KEY,
  knowledge_id text NOT NULL REFERENCES aiops_knowledge(id) ON DELETE CASCADE,
  version integer NOT NULL,
  snapshot jsonb NOT NULL,
  changed_by text NOT NULL DEFAULT '',
  change_summary text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(knowledge_id, version)
);
CREATE INDEX IF NOT EXISTS idx_aiops_knowledge_versions_knowledge ON aiops_knowledge_versions(knowledge_id, version DESC);
CREATE TABLE IF NOT EXISTS aiops_knowledge_attachments (
  id text PRIMARY KEY,
  knowledge_id text NOT NULL REFERENCES aiops_knowledge(id) ON DELETE CASCADE,
  filename text NOT NULL,
  content_type text NOT NULL DEFAULT 'application/octet-stream',
  size bigint NOT NULL DEFAULT 0,
  sha256 text NOT NULL DEFAULT '',
  object_key text NOT NULL,
  uploaded_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_aiops_knowledge_attachments_knowledge ON aiops_knowledge_attachments(knowledge_id, created_at DESC);
CREATE TABLE IF NOT EXISTS aiops_knowledge_embeddings (
  knowledge_id text PRIMARY KEY REFERENCES aiops_knowledge(id) ON DELETE CASCADE,
  model text NOT NULL,
  dimensions integer NOT NULL,
  embedding vector(256) NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_aiops_knowledge_embeddings_hnsw ON aiops_knowledge_embeddings USING hnsw (embedding vector_cosine_ops);
CREATE TABLE IF NOT EXISTS aiops_knowledge_answers (
  id text PRIMARY KEY, question text NOT NULL, answer text NOT NULL,
  citations jsonb NOT NULL DEFAULT '[]', model text NOT NULL DEFAULT '',
  created_by text NOT NULL DEFAULT '', generated_at timestamptz NOT NULL,
  feedback jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_aiops_knowledge_answers_time ON aiops_knowledge_answers(generated_at DESC);
CREATE TABLE IF NOT EXISTS aiops_execution_requests (
  id text PRIMARY KEY,
  analysis_id text NOT NULL,
  runbook_id text NOT NULL,
  runbook_title text NOT NULL DEFAULT '',
  status text NOT NULL,
  requested_by text NOT NULL DEFAULT '',
  requested_at timestamptz NOT NULL,
  approval_required boolean NOT NULL DEFAULT true,
  simulated boolean NOT NULL DEFAULT false,
  rollback boolean NOT NULL DEFAULT false,
  rehearsal boolean NOT NULL DEFAULT false,
  parent_execution_id text NOT NULL DEFAULT '',
  ticket_id text NOT NULL DEFAULT '',
  job_id text NOT NULL DEFAULT '',
  approved_by text NOT NULL DEFAULT '',
  approved_at timestamptz,
  finished_at timestamptz,
  result_summary text NOT NULL DEFAULT '',
  error_message text NOT NULL DEFAULT '',
  change_plan jsonb NOT NULL DEFAULT '{}',
  verification_status text NOT NULL DEFAULT '',
  verification_summary text NOT NULL DEFAULT '',
  verified_at timestamptz
);
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS job_id text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS approved_by text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS approved_at timestamptz;
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS finished_at timestamptz;
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS result_summary text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS error_message text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS simulated boolean NOT NULL DEFAULT false;
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS rollback boolean NOT NULL DEFAULT false;
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS rehearsal boolean NOT NULL DEFAULT false;
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS parent_execution_id text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS change_plan jsonb NOT NULL DEFAULT '{}';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS verification_status text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS verification_summary text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS verified_at timestamptz;
CREATE TABLE IF NOT EXISTS aiops_runbook_policies (
  runbook_id text PRIMARY KEY,
  title text NOT NULL,
  template_id text NOT NULL,
  mode text NOT NULL,
  execution_mode text NOT NULL DEFAULT 'simulate',
  risk text NOT NULL,
  approval_required boolean NOT NULL DEFAULT true,
  enabled boolean NOT NULL DEFAULT false,
  max_targets integer NOT NULL DEFAULT 1,
  allowed_environments jsonb NOT NULL DEFAULT '[]',
  change_window text NOT NULL DEFAULT '*',
  rollback_runbook_id text NOT NULL DEFAULT '',
  live_daily_limit integer NOT NULL DEFAULT 1,
  live_cooldown_minutes integer NOT NULL DEFAULT 30,
  require_rollback boolean NOT NULL DEFAULT true,
  require_canary boolean NOT NULL DEFAULT true,
  updated_by text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE aiops_runbook_policies ADD COLUMN IF NOT EXISTS execution_mode text NOT NULL DEFAULT 'simulate';
ALTER TABLE aiops_runbook_policies ADD COLUMN IF NOT EXISTS rollback_runbook_id text NOT NULL DEFAULT '';
ALTER TABLE aiops_runbook_policies ADD COLUMN IF NOT EXISTS live_daily_limit integer NOT NULL DEFAULT 1;
ALTER TABLE aiops_runbook_policies ADD COLUMN IF NOT EXISTS live_cooldown_minutes integer NOT NULL DEFAULT 30;
ALTER TABLE aiops_runbook_policies ADD COLUMN IF NOT EXISTS require_rollback boolean NOT NULL DEFAULT true;
ALTER TABLE aiops_runbook_policies ADD COLUMN IF NOT EXISTS require_canary boolean NOT NULL DEFAULT true;
CREATE INDEX IF NOT EXISTS idx_aiops_execution_requests_time ON aiops_execution_requests(requested_at DESC);
CREATE TABLE IF NOT EXISTS aiops_change_control (
  id text PRIMARY KEY,
  frozen boolean NOT NULL DEFAULT false,
  live_enabled boolean NOT NULL DEFAULT true,
  reason text NOT NULL DEFAULT '',
  freeze_until timestamptz,
  updated_by text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO aiops_change_control(id,frozen,live_enabled,reason,updated_by) VALUES('default',false,true,'','system') ON CONFLICT(id) DO NOTHING;
CREATE TABLE IF NOT EXISTS aiops_canary_validations (
  id text PRIMARY KEY,
  runbook_id text NOT NULL,
  asset_id text NOT NULL,
  asset_name text NOT NULL DEFAULT '',
  status text NOT NULL,
  job_id text NOT NULL DEFAULT '',
  summary text NOT NULL DEFAULT '',
  checks jsonb NOT NULL DEFAULT '[]',
  host_up boolean NOT NULL DEFAULT false,
  baseline jsonb NOT NULL DEFAULT '[]',
  requested_by text NOT NULL DEFAULT '',
  started_at timestamptz NOT NULL,
  finished_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE aiops_canary_validations ADD COLUMN IF NOT EXISTS host_up boolean NOT NULL DEFAULT false;
ALTER TABLE aiops_canary_validations ADD COLUMN IF NOT EXISTS baseline jsonb NOT NULL DEFAULT '[]';
CREATE INDEX IF NOT EXISTS idx_aiops_canary_validations_asset ON aiops_canary_validations(runbook_id,asset_id,started_at DESC);
`

type postgresRepository struct {
	db *sql.DB
}

func openPostgres(databaseURL string) (*postgresRepository, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err = db.ExecContext(ctx, aiopsSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate aiops schema: %w", err)
	}
	return &postgresRepository{db: db}, nil
}

func (r *postgresRepository) SaveAnalysis(ctx context.Context, item Analysis) error {
	evidence, _ := json.Marshal(item.Evidence)
	recommendations, _ := json.Marshal(item.Recommendations)
	knowledgeSuggestions, _ := json.Marshal(item.KnowledgeSuggestions)
	assets, _ := json.Marshal(item.RelatedAssets)
	impact, _ := json.Marshal(item.Impact)
	timeline, _ := json.Marshal(item.Timeline)
	tickets, _ := json.Marshal(item.Tickets)
	releases, _ := json.Marshal(item.Releases)
	sources, _ := json.Marshal(item.DataSources)
	feedback, _ := json.Marshal(item.Feedback)
	created, err := parseAITime(item.CreatedAt)
	if err != nil {
		return err
	}
	updated := created
	if item.UpdatedAt != "" {
		updated, err = parseAITime(item.UpdatedAt)
		if err != nil {
			return err
		}
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO aiops_analyses(id,alert_id,alert_title,title,status,severity,target,root_cause,confidence,confidence_adjustment,confidence_reason,applied_feedback_count,summary,created_at,updated_at,evidence,recommendations,knowledge_suggestions,related_assets,impact,timeline,tickets,releases,data_sources,feedback)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)
ON CONFLICT(id) DO UPDATE SET alert_title=excluded.alert_title,title=excluded.title,status=excluded.status,severity=excluded.severity,target=excluded.target,root_cause=excluded.root_cause,confidence=excluded.confidence,confidence_adjustment=excluded.confidence_adjustment,confidence_reason=excluded.confidence_reason,applied_feedback_count=excluded.applied_feedback_count,summary=excluded.summary,updated_at=excluded.updated_at,evidence=excluded.evidence,recommendations=excluded.recommendations,knowledge_suggestions=excluded.knowledge_suggestions,related_assets=excluded.related_assets,impact=excluded.impact,timeline=excluded.timeline,tickets=excluded.tickets,releases=excluded.releases,data_sources=excluded.data_sources,feedback=excluded.feedback`,
		item.ID, item.AlertID, item.AlertTitle, item.Title, item.Status, item.Severity, item.Target, item.RootCause, item.Confidence, item.ConfidenceAdjustment, item.ConfidenceReason, item.AppliedFeedbackCount, item.Summary, created, updated, evidence, recommendations, knowledgeSuggestions, assets, impact, timeline, tickets, releases, sources, feedback)
	return err
}
func (r *postgresRepository) LoadAnalyses(ctx context.Context) ([]Analysis, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id,alert_id,alert_title,title,status,severity,target,root_cause,confidence,confidence_adjustment,confidence_reason,applied_feedback_count,summary,created_at,updated_at,evidence,recommendations,knowledge_suggestions,related_assets,impact,timeline,tickets,releases,data_sources,feedback
FROM aiops_analyses ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Analysis{}
	for rows.Next() {
		var item Analysis
		var created, updated time.Time
		var evidence, recommendations, knowledgeSuggestions, assets, impact, timeline, tickets, releases, sources, feedback []byte
		if err = rows.Scan(&item.ID, &item.AlertID, &item.AlertTitle, &item.Title, &item.Status, &item.Severity, &item.Target, &item.RootCause, &item.Confidence, &item.ConfidenceAdjustment, &item.ConfidenceReason, &item.AppliedFeedbackCount, &item.Summary, &created, &updated, &evidence, &recommendations, &knowledgeSuggestions, &assets, &impact, &timeline, &tickets, &releases, &sources, &feedback); err != nil {
			return nil, err
		}
		item.CreatedAt = created.Local().Format("2006-01-02 15:04:05")
		item.UpdatedAt = updated.Local().Format("2006-01-02 15:04:05")
		_ = json.Unmarshal(evidence, &item.Evidence)
		_ = json.Unmarshal(recommendations, &item.Recommendations)
		_ = json.Unmarshal(knowledgeSuggestions, &item.KnowledgeSuggestions)
		_ = json.Unmarshal(assets, &item.RelatedAssets)
		_ = json.Unmarshal(impact, &item.Impact)
		_ = json.Unmarshal(timeline, &item.Timeline)
		_ = json.Unmarshal(tickets, &item.Tickets)
		_ = json.Unmarshal(releases, &item.Releases)
		_ = json.Unmarshal(sources, &item.DataSources)
		if string(feedback) != "" && string(feedback) != "{}" && string(feedback) != "null" {
			var value Feedback
			_ = json.Unmarshal(feedback, &value)
			if value.Status != "" {
				item.Feedback = &value
			}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) SaveKnowledge(ctx context.Context, item Knowledge) error {
	tags, _ := json.Marshal(item.Tags)
	related, _ := json.Marshal(item.Related)
	createdAt, err := parseAITime(item.CreatedAt)
	if err != nil {
		createdAt = time.Now()
	}
	updatedAt, err := parseAITime(item.UpdatedAt)
	if err != nil {
		updatedAt = time.Now()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO aiops_knowledge(id,space_id,folder_id,title,category,summary,content,content_format,tags,related,helpful,views,status,source,source_id,author,owner,visibility,doc_version,published_version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22) ON CONFLICT(id) DO UPDATE SET space_id=excluded.space_id,folder_id=excluded.folder_id,title=excluded.title,category=excluded.category,summary=excluded.summary,content=excluded.content,content_format=excluded.content_format,tags=excluded.tags,related=excluded.related,helpful=excluded.helpful,views=excluded.views,status=excluded.status,source=excluded.source,source_id=excluded.source_id,author=excluded.author,owner=excluded.owner,visibility=excluded.visibility,doc_version=excluded.doc_version,published_version=excluded.published_version,updated_at=excluded.updated_at`,
		item.ID, item.SpaceID, item.FolderID, item.Title, item.Category, item.Summary, item.Content, item.ContentFormat, tags, related, item.Helpful, item.Views, item.Status, item.Source, item.SourceID, item.Author, item.Owner, item.Visibility, item.Version, item.PublishedVersion, createdAt, updatedAt)
	return err
}

func (r *postgresRepository) LoadKnowledge(ctx context.Context) ([]Knowledge, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,space_id,folder_id,title,category,summary,content,content_format,tags,related,helpful,views,status,source,source_id,author,owner,visibility,doc_version,published_version,created_at,updated_at FROM aiops_knowledge ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Knowledge{}
	for rows.Next() {
		var item Knowledge
		var tags, related []byte
		var createdAt, updatedAt time.Time
		if err = rows.Scan(&item.ID, &item.SpaceID, &item.FolderID, &item.Title, &item.Category, &item.Summary, &item.Content, &item.ContentFormat, &tags, &related, &item.Helpful, &item.Views, &item.Status, &item.Source, &item.SourceID, &item.Author, &item.Owner, &item.Visibility, &item.Version, &item.PublishedVersion, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(tags, &item.Tags)
		_ = json.Unmarshal(related, &item.Related)
		item.CreatedAt = createdAt.Local().Format("2006-01-02 15:04:05")
		item.UpdatedAt = updatedAt.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) DeleteKnowledge(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM aiops_knowledge WHERE id=$1`, id)
	return err
}

func (r *postgresRepository) LoadKnowledgeSpaces(ctx context.Context) ([]KnowledgeSpace, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,code,description,kind,visibility,owner,created_by,created_at,updated_at FROM knowledge_spaces WHERE deleted_at IS NULL ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []KnowledgeSpace{}
	for rows.Next() {
		var item KnowledgeSpace
		var createdAt, updatedAt time.Time
		if err = rows.Scan(&item.ID, &item.Name, &item.Code, &item.Description, &item.Kind, &item.Visibility, &item.Owner, &item.CreatedBy, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = createdAt.Local().Format("2006-01-02 15:04:05")
		item.UpdatedAt = updatedAt.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) SaveKnowledgeSpace(ctx context.Context, item KnowledgeSpace) error {
	createdAt, err := parseAITime(item.CreatedAt)
	if err != nil {
		createdAt = time.Now()
	}
	updatedAt, err := parseAITime(item.UpdatedAt)
	if err != nil {
		updatedAt = time.Now()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO knowledge_spaces(id,name,code,description,kind,visibility,owner,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO UPDATE SET name=excluded.name,code=excluded.code,description=excluded.description,kind=excluded.kind,visibility=excluded.visibility,owner=excluded.owner,updated_at=excluded.updated_at`,
		item.ID, item.Name, item.Code, item.Description, item.Kind, item.Visibility, item.Owner, item.CreatedBy, createdAt, updatedAt)
	return err
}

func (r *postgresRepository) DeleteKnowledgeSpace(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE knowledge_spaces SET deleted_at=now(), updated_at=now() WHERE id=$1`, id)
	return err
}

func (r *postgresRepository) LoadKnowledgeFolders(ctx context.Context, spaceID string) ([]KnowledgeFolder, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,space_id,parent_id,name,path,description,owner,sort_order,created_at,updated_at FROM knowledge_folders WHERE deleted_at IS NULL AND ($1='' OR space_id=$1) ORDER BY sort_order,name`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []KnowledgeFolder{}
	for rows.Next() {
		var item KnowledgeFolder
		var createdAt, updatedAt time.Time
		if err = rows.Scan(&item.ID, &item.SpaceID, &item.ParentID, &item.Name, &item.Path, &item.Description, &item.Owner, &item.SortOrder, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = createdAt.Local().Format("2006-01-02 15:04:05")
		item.UpdatedAt = updatedAt.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) SaveKnowledgeFolder(ctx context.Context, item KnowledgeFolder) error {
	createdAt, err := parseAITime(item.CreatedAt)
	if err != nil {
		createdAt = time.Now()
	}
	updatedAt, err := parseAITime(item.UpdatedAt)
	if err != nil {
		updatedAt = time.Now()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO knowledge_folders(id,space_id,parent_id,name,path,description,owner,sort_order,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO UPDATE SET space_id=excluded.space_id,parent_id=excluded.parent_id,name=excluded.name,path=excluded.path,description=excluded.description,owner=excluded.owner,sort_order=excluded.sort_order,updated_at=excluded.updated_at`,
		item.ID, item.SpaceID, item.ParentID, item.Name, item.Path, item.Description, item.Owner, item.SortOrder, createdAt, updatedAt)
	return err
}

func (r *postgresRepository) DeleteKnowledgeFolder(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE knowledge_folders SET deleted_at=now(), updated_at=now() WHERE id=$1`, id)
	return err
}

func (r *postgresRepository) LoadKnowledgeACL(ctx context.Context, resourceType, resourceID string) ([]KnowledgeACL, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,resource_type,resource_id,subject_type,subject_id,permission,effect,inherit,created_by,created_at FROM knowledge_acl WHERE resource_type=$1 AND ($2='' OR resource_id=$2) ORDER BY created_at`, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []KnowledgeACL{}
	for rows.Next() {
		var item KnowledgeACL
		var createdAt time.Time
		if err = rows.Scan(&item.ID, &item.ResourceType, &item.ResourceID, &item.SubjectType, &item.SubjectID, &item.Permission, &item.Effect, &item.Inherit, &item.CreatedBy, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = createdAt.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) SaveKnowledgeACL(ctx context.Context, item KnowledgeACL) error {
	createdAt, err := parseAITime(item.CreatedAt)
	if err != nil {
		createdAt = time.Now()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO knowledge_acl(id,resource_type,resource_id,subject_type,subject_id,permission,effect,inherit,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(resource_type,resource_id,subject_type,subject_id) DO UPDATE SET permission=excluded.permission,effect=excluded.effect,inherit=excluded.inherit,created_by=excluded.created_by,created_at=excluded.created_at`,
		item.ID, item.ResourceType, item.ResourceID, item.SubjectType, item.SubjectID, item.Permission, item.Effect, item.Inherit, item.CreatedBy, createdAt)
	return err
}

func (r *postgresRepository) DeleteKnowledgeACL(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM knowledge_acl WHERE id=$1`, id)
	return err
}

func (r *postgresRepository) SaveKnowledgeVersion(ctx context.Context, item KnowledgeVersion) error {
	snapshot, _ := json.Marshal(item.Snapshot)
	createdAt, err := parseAITime(item.CreatedAt)
	if err != nil {
		createdAt = time.Now()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO aiops_knowledge_versions(id,knowledge_id,version,snapshot,changed_by,change_summary,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET snapshot=excluded.snapshot,changed_by=excluded.changed_by,change_summary=excluded.change_summary`,
		item.ID, item.KnowledgeID, item.Version, snapshot, item.ChangedBy, item.ChangeSummary, createdAt)
	return err
}

func (r *postgresRepository) LoadKnowledgeVersions(ctx context.Context, knowledgeID string) ([]KnowledgeVersion, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,knowledge_id,version,snapshot,changed_by,change_summary,created_at FROM aiops_knowledge_versions WHERE knowledge_id=$1 ORDER BY version DESC`, knowledgeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []KnowledgeVersion{}
	for rows.Next() {
		var item KnowledgeVersion
		var snapshot []byte
		var createdAt time.Time
		if err = rows.Scan(&item.ID, &item.KnowledgeID, &item.Version, &snapshot, &item.ChangedBy, &item.ChangeSummary, &createdAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(snapshot, &item.Snapshot)
		item.CreatedAt = createdAt.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) SaveKnowledgeAttachment(ctx context.Context, item KnowledgeAttachment) error {
	createdAt, err := parseAITime(item.CreatedAt)
	if err != nil {
		createdAt = time.Now()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO aiops_knowledge_attachments(id,knowledge_id,filename,content_type,size,sha256,object_key,uploaded_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO UPDATE SET filename=excluded.filename,content_type=excluded.content_type,size=excluded.size,sha256=excluded.sha256,object_key=excluded.object_key,uploaded_by=excluded.uploaded_by`,
		item.ID, item.KnowledgeID, item.Filename, item.ContentType, item.Size, item.SHA256, item.Key, item.UploadedBy, createdAt)
	return err
}

func (r *postgresRepository) LoadKnowledgeAttachments(ctx context.Context, knowledgeID string) ([]KnowledgeAttachment, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,knowledge_id,filename,content_type,size,sha256,object_key,uploaded_by,created_at FROM aiops_knowledge_attachments WHERE knowledge_id=$1 ORDER BY created_at DESC`, knowledgeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []KnowledgeAttachment{}
	for rows.Next() {
		var item KnowledgeAttachment
		var createdAt time.Time
		if err = rows.Scan(&item.ID, &item.KnowledgeID, &item.Filename, &item.ContentType, &item.Size, &item.SHA256, &item.Key, &item.UploadedBy, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = createdAt.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) DeleteKnowledgeAttachment(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM aiops_knowledge_attachments WHERE id=$1`, id)
	return err
}

func (r *postgresRepository) SaveKnowledgeEmbedding(ctx context.Context, knowledgeID, model string, dimensions int, vector []float32) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO aiops_knowledge_embeddings(knowledge_id,model,dimensions,embedding,updated_at) VALUES($1,$2,$3,$4::vector,now()) ON CONFLICT(knowledge_id) DO UPDATE SET model=excluded.model,dimensions=excluded.dimensions,embedding=excluded.embedding,updated_at=now()`, knowledgeID, model, dimensions, formatKnowledgeVector(vector))
	return err
}

func (r *postgresRepository) LoadKnowledgeEmbedding(ctx context.Context, knowledgeID string) ([]float32, error) {
	var raw string
	if err := r.db.QueryRowContext(ctx, `SELECT embedding::text FROM aiops_knowledge_embeddings WHERE knowledge_id=$1`, knowledgeID).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return parseKnowledgeVector(raw), nil
}

func (r *postgresRepository) SearchKnowledgeVector(ctx context.Context, vector []float32, limit int) ([]KnowledgeVectorMatch, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.db.QueryContext(ctx, `SELECT knowledge_id,1-(embedding <=> $1::vector) AS score FROM aiops_knowledge_embeddings ORDER BY embedding <=> $1::vector LIMIT $2`, formatKnowledgeVector(vector), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []KnowledgeVectorMatch{}
	for rows.Next() {
		var item KnowledgeVectorMatch
		if err = rows.Scan(&item.KnowledgeID, &item.Score); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func formatKnowledgeVector(vector []float32) string {
	parts := make([]string, 0, len(vector))
	for _, value := range vector {
		parts = append(parts, strconv.FormatFloat(float64(value), 'f', 6, 32))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func parseKnowledgeVector(value string) []float32 {
	value = strings.Trim(strings.TrimSpace(value), "[]")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]float32, 0, len(parts))
	for _, part := range parts {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(part), 32)
		if err == nil {
			result = append(result, float32(parsed))
		}
	}
	return result
}

func (r *postgresRepository) SaveKnowledgeAnswer(ctx context.Context, item KnowledgeAnswer) error {
	citations, _ := json.Marshal(item.Citations)
	feedback, _ := json.Marshal(item.Feedback)
	generated, err := parseAITime(item.GeneratedAt)
	if err != nil {
		generated = time.Now()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO aiops_knowledge_answers(id,question,answer,citations,model,created_by,generated_at,feedback) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET answer=excluded.answer,citations=excluded.citations,model=excluded.model,feedback=excluded.feedback`, item.ID, item.Question, item.Answer, citations, item.Model, item.CreatedBy, generated, feedback)
	return err
}
func (r *postgresRepository) GetKnowledgeAnswer(ctx context.Context, id string) (KnowledgeAnswer, error) {
	var item KnowledgeAnswer
	var citations, feedback []byte
	var generated time.Time
	err := r.db.QueryRowContext(ctx, `SELECT id,question,answer,citations,model,created_by,generated_at,feedback FROM aiops_knowledge_answers WHERE id=$1`, id).Scan(&item.ID, &item.Question, &item.Answer, &citations, &item.Model, &item.CreatedBy, &generated, &feedback)
	if err == sql.ErrNoRows {
		return KnowledgeAnswer{}, ErrNotFound
	}
	if err != nil {
		return KnowledgeAnswer{}, err
	}
	_ = json.Unmarshal(citations, &item.Citations)
	if len(feedback) > 0 && string(feedback) != "{}" {
		var value KnowledgeAnswerFeedback
		_ = json.Unmarshal(feedback, &value)
		if value.Status != "" {
			item.Feedback = &value
		}
	}
	item.GeneratedAt = generated.Local().Format("2006-01-02 15:04:05")
	return item, nil
}
func (r *postgresRepository) LoadKnowledgeAnswers(ctx context.Context, limit int) ([]KnowledgeAnswer, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,question,answer,citations,model,created_by,generated_at,feedback FROM aiops_knowledge_answers ORDER BY generated_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []KnowledgeAnswer{}
	for rows.Next() {
		var item KnowledgeAnswer
		var citations, feedback []byte
		var generated time.Time
		if err = rows.Scan(&item.ID, &item.Question, &item.Answer, &citations, &item.Model, &item.CreatedBy, &generated, &feedback); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(citations, &item.Citations)
		if len(feedback) > 0 && string(feedback) != "{}" {
			var value KnowledgeAnswerFeedback
			_ = json.Unmarshal(feedback, &value)
			if value.Status != "" {
				item.Feedback = &value
			}
		}
		item.GeneratedAt = generated.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) SaveExecution(ctx context.Context, item Execution) error {
	requestedAt, err := parseAITime(item.RequestedAt)
	if err != nil {
		return err
	}
	var approvedAt any
	if item.ApprovedAt != "" {
		approvedAt, _ = parseAITime(item.ApprovedAt)
	}
	var finishedAt any
	if item.FinishedAt != "" {
		finishedAt, _ = parseAITime(item.FinishedAt)
	}
	var verifiedAt any
	if item.VerifiedAt != "" {
		verifiedAt, _ = parseAITime(item.VerifiedAt)
	}
	changePlan, _ := json.Marshal(item.ChangePlan)
	_, err = r.db.ExecContext(ctx, `
INSERT INTO aiops_execution_requests(id,analysis_id,runbook_id,runbook_title,status,requested_by,requested_at,approval_required,simulated,rollback,rehearsal,parent_execution_id,ticket_id,job_id,approved_by,approved_at,finished_at,result_summary,error_message,change_plan,verification_status,verification_summary,verified_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
ON CONFLICT(id) DO UPDATE SET status=excluded.status,simulated=excluded.simulated,rollback=excluded.rollback,rehearsal=excluded.rehearsal,parent_execution_id=excluded.parent_execution_id,ticket_id=excluded.ticket_id,job_id=excluded.job_id,approved_by=excluded.approved_by,approved_at=excluded.approved_at,finished_at=excluded.finished_at,result_summary=excluded.result_summary,error_message=excluded.error_message,change_plan=excluded.change_plan,verification_status=excluded.verification_status,verification_summary=excluded.verification_summary,verified_at=excluded.verified_at`,
		item.ID, item.AnalysisID, item.RunbookID, item.RunbookTitle, item.Status, item.RequestedBy, requestedAt, item.ApprovalRequired, item.Simulated, item.Rollback, item.Rehearsal, item.ParentExecutionID, item.TicketID, item.JobID, item.ApprovedBy, approvedAt, finishedAt, item.ResultSummary, item.Error, changePlan, item.VerificationStatus, item.VerificationSummary, verifiedAt)
	return err
}

func (r *postgresRepository) LoadExecutions(ctx context.Context) ([]Execution, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id,analysis_id,runbook_id,runbook_title,status,requested_by,requested_at,approval_required,simulated,rollback,rehearsal,parent_execution_id,ticket_id,job_id,approved_by,approved_at,finished_at,result_summary,error_message,change_plan,verification_status,verification_summary,verified_at
FROM aiops_execution_requests ORDER BY requested_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Execution{}
	for rows.Next() {
		var item Execution
		var requestedAt time.Time
		var approvedAt, finishedAt, verifiedAt sql.NullTime
		var changePlan []byte
		if err = rows.Scan(&item.ID, &item.AnalysisID, &item.RunbookID, &item.RunbookTitle, &item.Status, &item.RequestedBy, &requestedAt, &item.ApprovalRequired, &item.Simulated, &item.Rollback, &item.Rehearsal, &item.ParentExecutionID, &item.TicketID, &item.JobID, &item.ApprovedBy, &approvedAt, &finishedAt, &item.ResultSummary, &item.Error, &changePlan, &item.VerificationStatus, &item.VerificationSummary, &verifiedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(changePlan, &item.ChangePlan)
		item.RequestedAt = requestedAt.Local().Format("2006-01-02 15:04:05")
		if approvedAt.Valid {
			item.ApprovedAt = approvedAt.Time.Local().Format("2006-01-02 15:04:05")
		}
		if finishedAt.Valid {
			item.FinishedAt = finishedAt.Time.Local().Format("2006-01-02 15:04:05")
		}
		if verifiedAt.Valid {
			item.VerifiedAt = verifiedAt.Time.Local().Format("2006-01-02 15:04:05")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func parseAITime(value string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid aiops time %q", value)
}

func (r *postgresRepository) LoadRunbookPolicies(ctx context.Context) ([]RunbookPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT runbook_id,title,template_id,mode,execution_mode,risk,approval_required,enabled,max_targets,allowed_environments,change_window,rollback_runbook_id,live_daily_limit,live_cooldown_minutes,require_rollback,require_canary,updated_by,updated_at FROM aiops_runbook_policies ORDER BY mode,runbook_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RunbookPolicy{}
	for rows.Next() {
		var item RunbookPolicy
		var environments []byte
		var updatedAt time.Time
		if err = rows.Scan(&item.RunbookID, &item.Title, &item.TemplateID, &item.Mode, &item.ExecutionMode, &item.Risk, &item.ApprovalRequired, &item.Enabled, &item.MaxTargets, &environments, &item.ChangeWindow, &item.RollbackRunbookID, &item.LiveDailyLimit, &item.LiveCooldownMinutes, &item.RequireRollback, &item.RequireCanary, &item.UpdatedBy, &updatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(environments, &item.AllowedEnvironments)
		item.UpdatedAt = updatedAt.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) LoadCanaryValidations(ctx context.Context) ([]CanaryValidation, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,runbook_id,asset_id,asset_name,status,job_id,summary,checks,host_up,baseline,requested_by,started_at,finished_at FROM aiops_canary_validations ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CanaryValidation{}
	for rows.Next() {
		var item CanaryValidation
		var checks, baseline []byte
		var startedAt time.Time
		var finishedAt sql.NullTime
		if err = rows.Scan(&item.ID, &item.RunbookID, &item.AssetID, &item.AssetName, &item.Status, &item.JobID, &item.Summary, &checks, &item.HostUp, &baseline, &item.RequestedBy, &startedAt, &finishedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(checks, &item.Checks)
		_ = json.Unmarshal(baseline, &item.Baseline)
		item.StartedAt = startedAt.Local().Format("2006-01-02 15:04:05")
		if finishedAt.Valid {
			item.FinishedAt = finishedAt.Time.Local().Format("2006-01-02 15:04:05")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) SaveCanaryValidation(ctx context.Context, item CanaryValidation) error {
	startedAt, err := parseAITime(item.StartedAt)
	if err != nil {
		return err
	}
	var finishedAt any
	if item.FinishedAt != "" {
		finishedAt, _ = parseAITime(item.FinishedAt)
	}
	checks, _ := json.Marshal(item.Checks)
	baseline, _ := json.Marshal(item.Baseline)
	_, err = r.db.ExecContext(ctx, `INSERT INTO aiops_canary_validations(id,runbook_id,asset_id,asset_name,status,job_id,summary,checks,host_up,baseline,requested_by,started_at,finished_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now()) ON CONFLICT(id) DO UPDATE SET status=excluded.status,job_id=excluded.job_id,summary=excluded.summary,checks=excluded.checks,host_up=excluded.host_up,baseline=excluded.baseline,finished_at=excluded.finished_at,updated_at=now()`, item.ID, item.RunbookID, item.AssetID, item.AssetName, item.Status, item.JobID, item.Summary, checks, item.HostUp, baseline, item.RequestedBy, startedAt, finishedAt)
	return err
}

func (r *postgresRepository) LoadChangeControl(ctx context.Context) (ChangeControl, error) {
	var item ChangeControl
	var freezeUntil sql.NullTime
	var updatedAt time.Time
	err := r.db.QueryRowContext(ctx, `SELECT id,frozen,live_enabled,reason,freeze_until,updated_by,updated_at FROM aiops_change_control WHERE id='default'`).Scan(&item.ID, &item.Frozen, &item.LiveEnabled, &item.Reason, &freezeUntil, &item.UpdatedBy, &updatedAt)
	if err != nil {
		return ChangeControl{}, err
	}
	if freezeUntil.Valid {
		item.FreezeUntil = freezeUntil.Time.Local().Format("2006-01-02 15:04:05")
	}
	item.UpdatedAt = updatedAt.Local().Format("2006-01-02 15:04:05")
	return item, nil
}

func (r *postgresRepository) SaveChangeControl(ctx context.Context, item ChangeControl) error {
	var freezeUntil any
	if item.FreezeUntil != "" {
		parsed, err := parseAITime(item.FreezeUntil)
		if err != nil {
			return err
		}
		freezeUntil = parsed
	}
	updatedAt := time.Now()
	if item.UpdatedAt != "" {
		if parsed, err := parseAITime(item.UpdatedAt); err == nil {
			updatedAt = parsed
		}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO aiops_change_control(id,frozen,live_enabled,reason,freeze_until,updated_by,updated_at) VALUES('default',$1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET frozen=excluded.frozen,live_enabled=excluded.live_enabled,reason=excluded.reason,freeze_until=excluded.freeze_until,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, item.Frozen, item.LiveEnabled, item.Reason, freezeUntil, item.UpdatedBy, updatedAt)
	return err
}

func (r *postgresRepository) SaveRunbookPolicy(ctx context.Context, item RunbookPolicy) error {
	environments, _ := json.Marshal(item.AllowedEnvironments)
	updatedAt, err := parseAITime(item.UpdatedAt)
	if err != nil {
		updatedAt = time.Now()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO aiops_runbook_policies(runbook_id,title,template_id,mode,execution_mode,risk,approval_required,enabled,max_targets,allowed_environments,change_window,rollback_runbook_id,live_daily_limit,live_cooldown_minutes,require_rollback,require_canary,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) ON CONFLICT(runbook_id) DO UPDATE SET title=excluded.title,template_id=excluded.template_id,mode=excluded.mode,execution_mode=excluded.execution_mode,risk=excluded.risk,approval_required=excluded.approval_required,enabled=excluded.enabled,max_targets=excluded.max_targets,allowed_environments=excluded.allowed_environments,change_window=excluded.change_window,rollback_runbook_id=excluded.rollback_runbook_id,live_daily_limit=excluded.live_daily_limit,live_cooldown_minutes=excluded.live_cooldown_minutes,require_rollback=excluded.require_rollback,require_canary=excluded.require_canary,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, item.RunbookID, item.Title, item.TemplateID, item.Mode, item.ExecutionMode, item.Risk, item.ApprovalRequired, item.Enabled, item.MaxTargets, environments, item.ChangeWindow, item.RollbackRunbookID, item.LiveDailyLimit, item.LiveCooldownMinutes, item.RequireRollback, item.RequireCanary, item.UpdatedBy, updatedAt)
	return err
}
