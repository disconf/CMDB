package toolbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const toolboxSchema = `
CREATE TABLE IF NOT EXISTS toolbox_executions (
  id text PRIMARY KEY,
  tool_id text NOT NULL,
  tool_name text NOT NULL,
  category text NOT NULL,
  target text NOT NULL DEFAULT '',
  status text NOT NULL,
  operator_name text NOT NULL DEFAULT '',
  started_at timestamptz NOT NULL,
  finished_at timestamptz NOT NULL,
  duration_ms bigint NOT NULL DEFAULT 0,
  summary text NOT NULL DEFAULT '',
  error_message text NOT NULL DEFAULT '',
  output jsonb NOT NULL DEFAULT '[]',
  parameters jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_toolbox_executions_started_at ON toolbox_executions(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_toolbox_executions_status ON toolbox_executions(status, started_at DESC);
`

type postgresStore struct {
	db *sql.DB
}

func openPostgres(databaseURL string) (*postgresStore, error) {
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
	if _, err = db.ExecContext(ctx, toolboxSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate toolbox schema: %w", err)
	}
	return &postgresStore{db: db}, nil
}

func (s *postgresStore) Save(ctx context.Context, result Result) error {
	output, _ := json.Marshal(result.Output)
	parameters, _ := json.Marshal(result.Parameters)
	started, err := parseDatabaseTime(result.StartedAt)
	if err != nil {
		return err
	}
	finished, err := parseDatabaseTime(result.FinishedAt)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO toolbox_executions(id,tool_id,tool_name,category,target,status,operator_name,started_at,finished_at,duration_ms,summary,error_message,output,parameters)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT(id) DO UPDATE SET status=excluded.status,finished_at=excluded.finished_at,duration_ms=excluded.duration_ms,
summary=excluded.summary,error_message=excluded.error_message,output=excluded.output,parameters=excluded.parameters`,
		result.ID, result.ToolID, result.ToolName, result.Category, result.Target, result.Status, result.Operator,
		started, finished, result.DurationMs, result.Summary, result.Error, output, parameters)
	return err
}

func parseDatabaseTime(value string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q", value)
}

func (s *postgresStore) List(ctx context.Context, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id,tool_id,tool_name,category,target,status,operator_name,started_at,finished_at,duration_ms,summary,error_message,output,parameters
FROM toolbox_executions ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Result, 0, limit)
	for rows.Next() {
		var item Result
		var started, finished time.Time
		var output, parameters []byte
		if err = rows.Scan(&item.ID, &item.ToolID, &item.ToolName, &item.Category, &item.Target, &item.Status,
			&item.Operator, &started, &finished, &item.DurationMs, &item.Summary, &item.Error, &output, &parameters); err != nil {
			return nil, err
		}
		item.StartedAt = started.Local().Format("2006-01-02 15:04:05")
		item.FinishedAt = finished.Local().Format("2006-01-02 15:04:05")
		item.Duration = formatDuration(item.DurationMs)
		_ = json.Unmarshal(output, &item.Output)
		_ = json.Unmarshal(parameters, &item.Parameters)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *postgresStore) Summary(ctx context.Context) (Summary, error) {
	var summary Summary
	var completed int
	var average float64
	err := s.db.QueryRowContext(ctx, `
SELECT
  count(*) FILTER (WHERE started_at >= current_date),
  count(*) FILTER (WHERE started_at >= current_date AND status IN ('success','warning')),
  count(*) FILTER (WHERE started_at >= current_date AND status = 'failed'),
  COALESCE(avg(duration_ms) FILTER (WHERE started_at >= current_date),0)
FROM toolbox_executions`).Scan(&summary.ExecutionsToday, &completed, &summary.FailedToday, &average)
	if err != nil {
		return Summary{}, err
	}
	if summary.ExecutionsToday > 0 {
		summary.SuccessRate = math.Round(float64(completed)/float64(summary.ExecutionsToday)*10000) / 100
	}
	summary.AvgDuration = formatDuration(int64(average))
	return summary, nil
}

type memoryStore struct {
	mu      sync.RWMutex
	results []Result
}

func newMemoryStore() *memoryStore { return &memoryStore{results: []Result{}} }

func (s *memoryStore) Save(_ context.Context, result Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.results {
		if s.results[i].ID == result.ID {
			s.results[i] = result
			return nil
		}
	}
	s.results = append([]Result{result}, s.results...)
	if len(s.results) > 200 {
		s.results = s.results[:200]
	}
	return nil
}

func (s *memoryStore) List(_ context.Context, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit > len(s.results) {
		limit = len(s.results)
	}
	return append([]Result(nil), s.results[:limit]...), nil
}

func (s *memoryStore) Summary(_ context.Context) (Summary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	today := time.Now().Format("2006-01-02")
	summary := Summary{}
	var totalDuration int64
	for _, item := range s.results {
		if !strings.HasPrefix(item.StartedAt, today) {
			continue
		}
		summary.ExecutionsToday++
		totalDuration += item.DurationMs
		if item.Status == "failed" {
			summary.FailedToday++
		} else {
			summary.SuccessRate++
		}
	}
	if summary.ExecutionsToday > 0 {
		summary.SuccessRate = math.Round(summary.SuccessRate/float64(summary.ExecutionsToday)*10000) / 100
		summary.AvgDuration = formatDuration(totalDuration / int64(summary.ExecutionsToday))
	}
	return summary, nil
}

func formatDuration(milliseconds int64) string {
	if milliseconds <= 0 {
		return "--"
	}
	if milliseconds < 1000 {
		return fmt.Sprintf("%dms", milliseconds)
	}
	return fmt.Sprintf("%.2fs", float64(milliseconds)/1000)
}
