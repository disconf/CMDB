package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPExecutorSendsAuthenticatedRequestAndEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing token")
		}
		var in ExecutionRequest
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.JobID != "job-1" {
			t.Error("bad request")
		}
		_ = json.NewEncoder(w).Encode(ExecutionResult{Status: "success", Message: "done", Events: []ExecutionEvent{{Progress: 50, Level: "info", Message: "half"}}})
	}))
	defer server.Close()
	executor := &httpExecutor{endpoint: server.URL, token: "secret", client: server.Client()}
	events := 0
	result := executor.Execute(context.Background(), ExecutionRequest{JobID: "job-1"}, func(event ExecutionEvent) {
		events++
		if event.Progress != 50 {
			t.Errorf("event %#v", event)
		}
	})
	if result.Status != "success" || events != 1 {
		t.Fatalf("result %#v events %d", result, events)
	}
}
func TestExecutorDefaultsToSimulation(t *testing.T) {
	t.Setenv("ANSIBLE_RUNNER_URL", "")
	if info := newExecutorFromEnv().Info(); info.Mode != "simulated" || !info.Ready {
		t.Fatalf("info %#v", info)
	}
}

func TestHTTPExecutorCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/capabilities" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(RunnerCapabilities{Status: "ok", Version: "1.0.7", Profiles: []string{"health"}, MaxTargets: 100})
	}))
	defer server.Close()
	executor := &httpExecutor{endpoint: server.URL, token: "secret", client: server.Client()}
	value, err := executor.Capabilities(context.Background())
	if err != nil || value.Version != "1.0.7" || !value.Ready || value.Mode != "ansible-runner" {
		t.Fatalf("capabilities %#v err=%v", value, err)
	}
}
