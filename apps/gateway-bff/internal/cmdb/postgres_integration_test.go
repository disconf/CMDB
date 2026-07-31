package cmdb

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgresPersistsAssetAndHistory(t *testing.T) {
	databaseURL := os.Getenv("CMDB_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CMDB_INTEGRATION_DATABASE_URL is not configured")
	}
	seed := NewService()
	db, assets, history, err := openPostgres(databaseURL, seed.assets)
	if err != nil {
		t.Fatalf("open postgres repository: %v", err)
	}
	service := &Service{db: db, assets: assets, models: seed.models, history: history}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM event_outbox WHERE event_key=$1", "integration-persistence-check")
		_, _ = db.ExecContext(context.Background(), "DELETE FROM cmdb_assets WHERE id=$1", "integration-persistence-check")
		_ = db.Close()
	})
	_, _ = db.ExecContext(context.Background(), "DELETE FROM cmdb_assets WHERE id=$1", "integration-persistence-check")
	_, _ = db.ExecContext(context.Background(), "DELETE FROM event_outbox WHERE event_key=$1", "integration-persistence-check")

	created, err := service.CreateAsset(CreateAssetInput{
		ID: "integration-persistence-check", Name: "postgres-check", Type: "virtual-machine",
		Status: "online", Environment: "集成测试", ProjectGroup: "研发效能组",
		Owner: "integration-test", Source: "test",
	}, "integration-test")
	if err != nil {
		t.Fatalf("create persisted asset: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created asset has no ID")
	}
	var assetCount, historyCount, outboxCount int
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM cmdb_assets WHERE id=$1", created.ID).Scan(&assetCount); err != nil {
		t.Fatalf("query persisted asset: %v", err)
	}
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM cmdb_asset_history WHERE asset_id=$1", created.ID).Scan(&historyCount); err != nil {
		t.Fatalf("query persisted history: %v", err)
	}
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM event_outbox WHERE event_key=$1", created.ID).Scan(&outboxCount); err != nil {
		t.Fatalf("query outbox events: %v", err)
	}
	if assetCount != 1 || historyCount != 1 || outboxCount != 2 {
		t.Fatalf("expected one asset, one history and two outbox rows, got asset=%d history=%d outbox=%d", assetCount, historyCount, outboxCount)
	}
}
