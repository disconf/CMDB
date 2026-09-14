package discovery

import (
	"context"
	"testing"
	"time"
)

func TestRenewRemoteSessionLeaseOwnership(t *testing.T) {
	service := NewService()
	service.instanceID = "gateway-a"
	service.remoteLeaseTTL = 30 * time.Second
	future := time.Now().Add(time.Minute).Format("2006-01-02 15:04:05")
	service.remoteSessions = []RemoteSession{{
		ID:             "session-1",
		Status:         "active",
		OwnerNode:      "gateway-a",
		LeaseExpiresAt: future,
	}}

	if err := service.RenewRemoteSessionLease(context.Background(), "session-1"); err != nil {
		t.Fatalf("renew own lease: %v", err)
	}
	if service.remoteSessions[0].LeaseExpiresAt == future {
		t.Fatal("lease expiry was not refreshed")
	}

	service.remoteSessions[0].OwnerNode = "gateway-b"
	service.remoteSessions[0].LeaseExpiresAt = time.Now().Add(time.Minute).Format("2006-01-02 15:04:05")
	if err := service.RenewRemoteSessionLease(context.Background(), "session-1"); err != ErrRemoteForbidden {
		t.Fatalf("expected foreign live lease to be rejected, got %v", err)
	}

	service.remoteSessions[0].LeaseExpiresAt = time.Now().Add(-time.Minute).Format("2006-01-02 15:04:05")
	if err := service.RenewRemoteSessionLease(context.Background(), "session-1"); err != nil {
		t.Fatalf("renew expired foreign lease: %v", err)
	}
	if service.remoteSessions[0].OwnerNode != "gateway-a" {
		t.Fatalf("lease owner was not transferred: %q", service.remoteSessions[0].OwnerNode)
	}
}
