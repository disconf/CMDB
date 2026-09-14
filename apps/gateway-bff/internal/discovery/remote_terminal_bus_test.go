package discovery

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRemoteTerminalBusKeysKeepSessionInSameClusterSlot(t *testing.T) {
	control := remoteTerminalControlChannel("session-42")
	output := remoteTerminalOutputChannel("session-42")
	presence := remoteTerminalPresenceKey("session-42")
	sequence := remoteTerminalSequenceKey("session-42")
	for _, value := range []string{control, output, presence, sequence} {
		if !strings.Contains(value, "{session-42}") {
			t.Fatalf("key does not contain hash tag: %s", value)
		}
	}
}

func TestRemoteTerminalBusEventRoundTrip(t *testing.T) {
	event := remoteTerminalBusEvent{Kind: "input", Origin: "gateway-a", Data: "dXBUaW1lCg==", Cols: 120, Rows: 32}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRemoteTerminalBusEvent(string(payload))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != event.Kind || decoded.Origin != event.Origin {
		t.Fatalf("unexpected decoded event: %+v", decoded)
	}
	data, err := remoteTerminalEventData(decoded)
	if err != nil || string(data) != "upTime\n" {
		t.Fatalf("unexpected decoded data %q: %v", data, err)
	}
}

func TestAcquireRemoteSessionLeaseClaimsOwnerlessSession(t *testing.T) {
	service := NewService()
	service.instanceID = "gateway-a"
	service.remoteLeaseTTL = 30 * time.Second
	service.remoteSessions = []RemoteSession{{ID: "session-1", Status: "active", ExpiresAt: "2099-01-01 00:00:00"}}
	owner, err := service.AcquireRemoteSessionLease(context.Background(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if owner != "gateway-a" || service.remoteSessions[0].OwnerNode != "gateway-a" {
		t.Fatalf("unexpected owner %q / %q", owner, service.remoteSessions[0].OwnerNode)
	}
}

func TestRemoteTerminalBusRejectsInvalidEvent(t *testing.T) {
	if _, err := decodeRemoteTerminalBusEvent(`{"data":"eA=="}`); err == nil {
		t.Fatal("expected empty event kind to be rejected")
	}
	if _, err := remoteTerminalEventData(remoteTerminalBusEvent{Data: "not-base64"}); err == nil {
		t.Fatal("expected invalid base64 data to be rejected")
	}
}
func TestRemoteTerminalProxyCloseIsIdempotent(t *testing.T) {
	proxy := &RemoteTerminalProxy{
		subscribers:        map[uint64]chan []byte{1: make(chan []byte, 1)},
		subscriberAccess:   map[uint64]string{1: "control"},
		participantCancels: map[uint64]context.CancelFunc{},
	}
	done := make(chan struct{})
	go func() {
		_ = proxy.Close()
		_ = proxy.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("proxy close deadlocked")
	}
	if _, ok := proxy.Output(1); ok {
		t.Fatal("subscriber was not closed")
	}
}
