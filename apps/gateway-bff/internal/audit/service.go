package audit

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Entry struct {
	ID         string `json:"id"`
	Actor      string `json:"actor"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	Detail     string `json:"detail"`
	OccurredAt string `json:"occurredAt"`
}

type Service struct{ db *sql.DB }

func NewService() *Service {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return &Service{}
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return &Service{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return &Service{}
	}
	_, _ = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS audit_log (
		id text PRIMARY KEY, actor text NOT NULL, action text NOT NULL,
		target text NOT NULL DEFAULT '', detail text NOT NULL DEFAULT '',
		occurred_at timestamptz NOT NULL DEFAULT now())`)
	return &Service{db: db}
}

func (s *Service) Record(actor, action, target, detail string) {
	if s.db == nil {
		return
	}
	_, _ = s.db.Exec(`INSERT INTO audit_log(id,actor,action,target,detail) VALUES($1,$2,$3,$4,$5)`,
		fmt.Sprintf("audit-%d", time.Now().UnixNano()), actor, action, target, detail)
}

func (s *Service) List(limit int) ([]Entry, error) {
	if s.db == nil {
		return []Entry{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id,actor,action,target,detail,occurred_at FROM audit_log ORDER BY occurred_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var at time.Time
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Target, &e.Detail, &at); err != nil {
			return nil, err
		}
		e.OccurredAt = at.Local().Format("2006-01-02 15:04:05")
		out = append(out, e)
	}
	return out, rows.Err()
}
