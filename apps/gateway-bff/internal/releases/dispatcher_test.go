package releases

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPDispatcher(t *testing.T) {
	var received DispatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer release-token" {
			t.Errorf("authorization header = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode dispatch request: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	t.Setenv("RELEASE_EXECUTOR_URL", server.URL)
	t.Setenv("RELEASE_EXECUTOR_TOKEN", "release-token")
	dispatcher := newDispatcherFromEnv()
	service := &Service{dispatcher: dispatcher}
	err := service.Dispatch(context.Background(), Release{
		ID: "rel-1", Application: "payment", Version: "v1.0.0", PreviousVersion: "v0.9.0",
		Environment: "qa", TicketID: "TKT-1", Operator: "operator",
	}, "deploy", "one-time-token")
	if err != nil {
		t.Fatal(err)
	}
	if received.Action != "deploy" || received.ReleaseID != "rel-1" || received.AuthorizationToken != "one-time-token" {
		t.Fatalf("unexpected dispatch request: %#v", received)
	}
	if info := service.ExecutorInfo(); info.Mode != "http" || !info.Ready {
		t.Fatalf("unexpected executor info: %#v", info)
	}
}

func TestManualDispatcher(t *testing.T) {
	t.Setenv("RELEASE_EXECUTOR_URL", "")
	service := &Service{dispatcher: newDispatcherFromEnv()}
	if info := service.ExecutorInfo(); info.Mode != "manual" || !info.Ready {
		t.Fatalf("unexpected manual executor info: %#v", info)
	}
	if err := service.Dispatch(context.Background(), Release{ID: "rel-1"}, "deploy", "token"); err != nil {
		t.Fatal(err)
	}
}
