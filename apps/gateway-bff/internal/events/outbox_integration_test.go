package events

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

func TestKafkaPublishesEvent(t *testing.T) {
	broker := os.Getenv("CMDB_INTEGRATION_KAFKA_BROKER")
	if broker == "" {
		t.Skip("CMDB_INTEGRATION_KAFKA_BROKER is not configured")
	}
	topic := "cmdb.asset.changed.v1"
	eventID := "integration-" + time.Now().UTC().Format("20060102150405.000000000")
	payload, _ := json.Marshal(map[string]any{"eventId": eventID, "eventType": topic, "data": map[string]string{"assetId": "integration-check"}})
	writer := &kafka.Writer{Addr: kafka.TCP(broker), Topic: topic, RequiredAcks: kafka.RequireAll}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := writer.WriteMessages(ctx, kafka.Message{Key: []byte("integration-check"), Value: payload}); err != nil {
		t.Fatalf("publish Kafka event: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close Kafka writer: %v", err)
	}
}

func TestOutboxPublisherMarksEventPublished(t *testing.T) {
	databaseURL := os.Getenv("CMDB_INTEGRATION_DATABASE_URL")
	broker := os.Getenv("CMDB_INTEGRATION_KAFKA_BROKER")
	if databaseURL == "" || broker == "" {
		t.Skip("database and Kafka integration settings are required")
	}
	publisher, err := NewOutboxPublisher(databaseURL, broker)
	if err != nil {
		t.Fatalf("create outbox publisher: %v", err)
	}
	id := "integration-outbox-" + time.Now().UTC().Format("20060102150405.000000000")
	payload, _ := json.Marshal(map[string]any{"eventId": id, "eventType": "cmdb.asset.changed.v1", "data": map[string]string{"assetId": "integration-check"}})
	if _, err = publisher.db.Exec(`INSERT INTO event_outbox(id,topic,event_key,payload) VALUES($1,$2,$3,$4)`, id, "cmdb.asset.changed.v1", "integration-check", payload); err != nil {
		t.Fatalf("insert outbox event: %v", err)
	}
	t.Cleanup(func() {
		_, _ = publisher.db.Exec("DELETE FROM event_outbox WHERE id=$1", id)
		_ = publisher.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = publisher.publishOne(ctx); err != nil {
		t.Fatalf("publish outbox event: %v", err)
	}
	var publishedAt *time.Time
	if err = publisher.db.QueryRow("SELECT published_at FROM event_outbox WHERE id=$1", id).Scan(&publishedAt); err != nil {
		t.Fatalf("read published state: %v", err)
	}
	if publishedAt == nil {
		t.Fatal("expected outbox event to be marked published")
	}
}
