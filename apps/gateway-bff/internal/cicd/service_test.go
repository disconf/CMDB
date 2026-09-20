package cicd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHarborProbeUsesBasicAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2.0/systeminfo" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeTestJSON(w, map[string]string{"harbor_version": "v2.11.0", "auth_mode": "db_auth"})
	}))
	defer server.Close()

	service := NewServiceWithConfig(Config{
		HTTPTimeout:    3 * time.Second,
		HarborURL:      server.URL,
		HarborUsername: "admin",
		HarborPassword: "secret",
	})
	item, err := service.Test(context.Background(), IntegrationHarbor)
	if err != nil {
		t.Fatalf("probe Harbor: %v", err)
	}
	if item.Status != StatusConnected || item.Details["version"] != "v2.11.0" {
		t.Fatalf("unexpected integration: %#v", item)
	}
}

func TestArgoCDProbeLogsInAndReadsApplications(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			var input map[string]string
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatalf("decode login body: %v", err)
			}
			if input["username"] != "admin" || input["password"] != "secret" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			writeTestJSON(w, map[string]string{"token": "argo-token"})
		case "/api/v1/applications":
			if r.Header.Get("Authorization") != "Bearer argo-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			writeTestJSON(w, map[string]any{"items": []map[string]string{{"name": "cmdb"}}, "total": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := NewServiceWithConfig(Config{
		HTTPTimeout:    3 * time.Second,
		ArgoCDURL:      server.URL,
		ArgoCDUsername: "admin",
		ArgoCDPassword: "secret",
	})
	item, err := service.Test(context.Background(), IntegrationArgoCD)
	if err != nil {
		t.Fatalf("probe Argo CD: %v", err)
	}
	if item.Status != StatusConnected || item.Details["authMode"] != "session" || item.Details["applications"] != "1" {
		t.Fatalf("unexpected integration: %#v", item)
	}
}

func TestGitLabProbeUsesPrivateToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/version" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("PRIVATE-TOKEN") != "gitlab-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeTestJSON(w, map[string]string{"version": "17.4.0", "revision": "abc123"})
	}))
	defer server.Close()

	service := NewServiceWithConfig(Config{HTTPTimeout: 3 * time.Second, GitLabURL: server.URL, GitLabToken: "gitlab-token"})
	item, err := service.Test(context.Background(), IntegrationGitLab)
	if err != nil {
		t.Fatalf("probe GitLab: %v", err)
	}
	if item.Status != StatusConnected || item.Details["version"] != "17.4.0" {
		t.Fatalf("unexpected integration: %#v", item)
	}
}

func TestUnconfiguredIntegrationDoesNotCallNetwork(t *testing.T) {
	service := NewServiceWithConfig(Config{HTTPTimeout: time.Second})
	item, err := service.Test(context.Background(), IntegrationGitLab)
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
	if item.Status != StatusNotConfigured {
		t.Fatalf("unexpected status: %s", item.Status)
	}
}

func TestListDoesNotExposeCredentials(t *testing.T) {
	service := NewServiceWithConfig(Config{
		HarborURL:      "https://harbor.example.com",
		HarborUsername: "admin",
		HarborPassword: "harbor-secret",
		ArgoCDURL:      "https://argo.example.com",
		ArgoCDToken:    "argo-secret",
		GitLabURL:      "https://gitlab.example.com",
		GitLabToken:    "gitlab-secret",
	})
	payload, err := json.Marshal(service.List())
	if err != nil {
		t.Fatalf("marshal integrations: %v", err)
	}
	serialized := string(payload)
	for _, secret := range []string{"harbor-secret", "argo-secret", "gitlab-secret"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("credential leaked in API payload: %s", secret)
		}
	}
}

func writeTestJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
