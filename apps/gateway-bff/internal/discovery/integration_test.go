package discovery

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"github.com/segmentio/kafka-go"
)

func TestDiscoveredResourcePersistsAndReconcilesToCMDB(t *testing.T) {
	databaseURL := os.Getenv("CMDB_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CMDB_INTEGRATION_DATABASE_URL is not configured")
	}
	t.Setenv("DATABASE_URL", databaseURL)
	cmdbService := cmdb.NewService()
	service := NewServiceWithCMDB(cmdbService)
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	taskID, itemID := "integration-discovery-"+suffix, "integration-found-"+suffix
	payload, _ := json.Marshal(map[string]any{"eventId": "event-" + suffix, "eventType": "discovery.resource.found.v1", "data": map[string]any{"taskId": taskID, "id": itemID, "name": "integration-host", "ip": "10.255.255.1", "type": "virtual-machine", "confidence": 99}})
	t.Cleanup(func() {
		_, _ = service.db.ExecContext(context.Background(), "DELETE FROM event_outbox WHERE event_key=$1", itemID)
		_, _ = service.db.ExecContext(context.Background(), "DELETE FROM cmdb_assets WHERE id=$1", itemID)
		_, _ = service.db.ExecContext(context.Background(), "DELETE FROM discovery_tasks WHERE id=$1", taskID)
		_ = service.db.Close()
	})
	if err := service.consumeMessage(payload); err != nil {
		t.Fatalf("consume resource event: %v", err)
	}
	var staged int
	if err := service.db.QueryRow("SELECT count(*) FROM discovery_items WHERE id=$1 AND state='pending'", itemID).Scan(&staged); err != nil || staged != 1 {
		t.Fatalf("resource not staged: count=%d err=%v", staged, err)
	}
	if _, err := service.Reconcile(taskID); err != nil {
		t.Fatalf("reconcile resource: %v", err)
	}
	var assetCount, outboxCount int
	_ = service.db.QueryRow("SELECT count(*) FROM cmdb_assets WHERE id=$1", itemID).Scan(&assetCount)
	_ = service.db.QueryRow("SELECT count(*) FROM event_outbox WHERE event_key=$1", itemID).Scan(&outboxCount)
	if assetCount != 1 || outboxCount != 2 {
		t.Fatalf("expected reconciled asset and two events, got asset=%d outbox=%d", assetCount, outboxCount)
	}
}

func TestKafkaConsumerStagesDiscoveredResource(t *testing.T) {
	databaseURL, broker := os.Getenv("CMDB_INTEGRATION_DATABASE_URL"), os.Getenv("CMDB_INTEGRATION_KAFKA_BROKER")
	if databaseURL == "" || broker == "" {
		t.Skip("database and Kafka integration settings are required")
	}
	t.Setenv("DATABASE_URL", databaseURL)
	service := NewServiceWithCMDB(nil)
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	taskID, itemID := "kafka-integration-"+suffix, "kafka-found-"+suffix
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.consumeGroup(ctx, broker, "cmdb-discovery-integration-"+suffix)
	time.Sleep(2 * time.Second)
	payload, _ := json.Marshal(map[string]any{"eventId": "event-" + suffix, "eventType": "discovery.resource.found.v1", "data": map[string]any{"taskId": taskID, "id": itemID, "name": "kafka-host", "ip": "10.255.255.2", "type": "physical-server", "confidence": 98}})
	writer := &kafka.Writer{Addr: kafka.TCP(broker), Topic: "discovery.resource.found.v1", RequiredAcks: kafka.RequireAll}
	if err := writer.WriteMessages(ctx, kafka.Message{Key: []byte(itemID), Value: payload}); err != nil {
		t.Fatalf("publish discovery event: %v", err)
	}
	_ = writer.Close()
	t.Cleanup(func() {
		_, _ = service.db.Exec("DELETE FROM discovery_tasks WHERE id LIKE 'kafka-integration-%'")
		_ = service.db.Close()
	})
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		_ = service.db.QueryRow("SELECT count(*) FROM discovery_items WHERE id=$1", itemID).Scan(&count)
		if count == 1 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("Kafka discovery event was not staged before timeout")
}
