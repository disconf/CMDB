package events

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/segmentio/kafka-go"
)

type OutboxPublisher struct {
	db     *sql.DB
	writer *kafka.Writer
}

func NewOutboxPublisher(databaseURL, brokers string) (*OutboxPublisher, error) {
	if databaseURL == "" || brokers == "" {
		return nil, errors.New("DATABASE_URL and KAFKA_BROKERS are required")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect outbox database: %w", err)
	}
	writer := &kafka.Writer{Addr: kafka.TCP(strings.Split(brokers, ",")...), Balancer: &kafka.Hash{}, RequiredAcks: kafka.RequireAll, Async: false}
	return &OutboxPublisher{db: db, writer: writer}, nil
}

func (p *OutboxPublisher) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := p.publishBatch(ctx, 100); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("publish outbox event", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *OutboxPublisher) publishOne(ctx context.Context) error {
	return p.publishBatch(ctx, 1)
}

func (p *OutboxPublisher) publishBatch(ctx context.Context, limit int) error {
	if limit < 1 {
		limit = 1
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,topic,event_key,payload FROM event_outbox
		WHERE published_at IS NULL AND next_attempt_at <= now()
		ORDER BY created_at LIMIT $1 FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return err
	}
	type outboxEvent struct {
		id, topic, key string
		payload        []byte
	}
	events := make([]outboxEvent, 0, limit)
	for rows.Next() {
		var event outboxEvent
		if err = rows.Scan(&event.id, &event.topic, &event.key, &event.payload); err != nil {
			rows.Close()
			return err
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(events) == 0 {
		return nil
	}
	ids := make([]string, 0, len(events))
	messages := make([]kafka.Message, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.id)
		messages = append(messages, kafka.Message{
			Topic: event.topic, Key: []byte(event.key), Value: event.payload,
			Headers: []kafka.Header{{Key: "event-id", Value: []byte(event.id)}},
		})
	}
	publishCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = p.writer.WriteMessages(publishCtx, messages...)
	cancel()
	if err != nil {
		_, _ = tx.ExecContext(ctx, `UPDATE event_outbox SET attempts=attempts+1,
			next_attempt_at=now() + least(attempts+1, 6) * interval '5 seconds' WHERE id = ANY($1)`, ids)
		_ = tx.Commit()
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE event_outbox SET published_at=now() WHERE id = ANY($1)", ids); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *OutboxPublisher) Close() error {
	p.writer.Close()
	return p.db.Close()
}
