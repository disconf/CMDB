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
	db, assets, history, snapshots, err := openPostgres(databaseURL, seed.assets)
	if err != nil {
		t.Fatalf("open postgres repository: %v", err)
	}
	service := &Service{db: db, assets: assets, models: seed.models, history: history, snapshots: snapshots}
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
	snapshot, err := service.SaveTopologySnapshot(TopologySnapshotInput{Kind: "physical", Environment: "集成测试", NodeCount: 1, EdgeCount: 1, Payload: []byte(`{"nodes":[],"edges":[]}`), Operator: "integration-test"})
	if err != nil {
		t.Fatalf("save persisted topology snapshot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM cmdb_topology_snapshots WHERE id=$1", snapshot.ID)
	})
	loadedSnapshot, err := service.TopologySnapshot(snapshot.ID)
	if err != nil || loadedSnapshot.ID != snapshot.ID || len(loadedSnapshot.Payload) == 0 {
		t.Fatalf("load persisted topology snapshot: %#v %v", loadedSnapshot, err)
	}
	state := NetworkAnomalyState{AnomalyID: "integration-network-anomaly", Status: "acknowledged", Operator: "integration-test", Note: "checked"}
	if err := upsertNetworkAnomalyState(context.Background(), db, state); err != nil {
		t.Fatalf("save network anomaly state: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM cmdb_network_anomaly_states WHERE anomaly_id=$1", state.AnomalyID)
	})
	states, err := loadNetworkAnomalyStates(context.Background(), db)
	if err != nil || states[state.AnomalyID].Status != "acknowledged" {
		t.Fatalf("load network anomaly state: %#v %v", states, err)
	}
	var assetCount, historyCount, outboxCount, statusOutboxCount int
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
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM event_outbox WHERE event_key=$1 AND topic='cmdb.asset.status.v1'", created.ID).Scan(&statusOutboxCount); err != nil {
		t.Fatalf("query status outbox events: %v", err)
	}
	if assetCount != 1 || historyCount != 1 || outboxCount != 3 || statusOutboxCount != 1 {
		t.Fatalf("expected one asset, one history, three outbox rows and one status event, got asset=%d history=%d outbox=%d status=%d", assetCount, historyCount, outboxCount, statusOutboxCount)
	}
}
