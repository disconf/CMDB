package tickets

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgresTicketLifecycle(t *testing.T) {
	url := os.Getenv("CMDB_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("CMDB_TEST_DATABASE_URL is not configured")
	}
	db, err := openPostgres(url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := &Service{db: db, next: 1}
	item, err := service.Create(CreateInput{
		Title: "integration-ticket-" + time.Now().Format("150405.000000"),
		Type:  "change", Priority: "high", Applicant: "integration-test",
		Description: "temporary ticket for PostgreSQL persistence verification",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM cmdb_tickets WHERE id=$1`, item.ID)
	})
	loaded, err := service.Get(item.ID)
	if err != nil || loaded.ID != item.ID || loaded.Status != StatusPending {
		t.Fatalf("load created ticket: %#v %v", loaded, err)
	}
	approved, err := service.Approve(item.ID, "integration-reviewer", "approved")
	if err != nil || approved.Status != StatusApproved {
		t.Fatalf("approve persisted ticket: %#v %v", approved, err)
	}
	closed, err := service.Close(item.ID, "integration-operator", "closed")
	if err != nil || closed.Status != StatusCompleted {
		t.Fatalf("close persisted ticket: %#v %v", closed, err)
	}
	items, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range items {
		if candidate.ID == item.ID && candidate.Status == StatusCompleted {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("completed ticket %s not found in list", item.ID)
	}
}
