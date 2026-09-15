package assetgovernance

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgresGovernanceReviewPersistence(t *testing.T) {
	databaseURL := os.Getenv("CMDB_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CMDB_INTEGRATION_DATABASE_URL is not configured")
	}
	db, err := openPostgres(databaseURL)
	if err != nil {
		t.Fatalf("open governance postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now().Format("2006-01-02 15:04:05")
	review := Review{AssetID: "integration-governance-review", Status: "confirmed", Owner: "integration-owner", ProjectGroup: "integration-group", Location: "integration-room", ReviewedAt: now, ReviewedBy: "integration", Note: "integration-test", TicketID: "integration-ticket", UpdatedAt: now}
	if err = upsertReview(context.Background(), db, review); err != nil {
		t.Fatalf("persist governance review: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM cmdb_asset_governance_reviews WHERE asset_id=$1", review.AssetID)
	})
	reviews, err := loadReviews(context.Background(), db)
	if err != nil {
		t.Fatalf("load governance reviews: %v", err)
	}
	loaded, ok := reviews[review.AssetID]
	if !ok || loaded.Status != "confirmed" || loaded.Owner != review.Owner || loaded.TicketID != review.TicketID {
		t.Fatalf("unexpected governance review: %#v", loaded)
	}
}
