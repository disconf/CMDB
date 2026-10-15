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

func TestResourcesParsesHarborAndArgoCD(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2.0/projects":
			if username, password, ok := r.BasicAuth(); !ok || username != "admin" || password != "secret" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			writeTestJSON(w, []map[string]any{{"name": "cmdb", "repo_count": 3, "update_time": "2026-09-21T00:00:00Z"}})
		case "/api/v2.0/repositories":
			writeTestJSON(w, []map[string]any{{"name": "cmdb/gateway-bff", "artifact_count": 12, "pull_count": 30, "update_time": "2026-09-21T01:00:00Z"}})
		case "/api/v1/session":
			writeTestJSON(w, map[string]string{"token": "argo-token"})
		case "/api/v1/applications":
			if r.Header.Get("Authorization") != "Bearer argo-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			writeTestJSON(w, map[string]any{"items": []map[string]any{
				{"metadata": map[string]string{"name": "cmdb", "namespace": "argocd"}, "spec": map[string]any{"project": "default", "destination": map[string]string{"server": "https://kubernetes.default.svc", "namespace": "ops-qa"}}, "status": map[string]any{"sync": map[string]string{"status": "Synced", "revision": "abc123"}, "health": map[string]string{"status": "Healthy"}, "operationState": map[string]string{"phase": "Succeeded", "finishedAt": "2026-09-21T01:00:00Z"}}},
				{"metadata": map[string]string{"name": "legacy", "namespace": "argocd"}, "spec": map[string]any{"project": "default", "destination": map[string]string{"server": "https://kubernetes.default.svc", "namespace": "legacy"}}, "status": map[string]any{"sync": map[string]string{"status": "OutOfSync"}, "health": map[string]string{"status": "Degraded"}}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := NewServiceWithConfig(Config{
		HTTPTimeout:    3 * time.Second,
		HarborURL:      server.URL,
		HarborUsername: "admin",
		HarborPassword: "secret",
		ArgoCDURL:      server.URL,
		ArgoCDUsername: "admin",
		ArgoCDPassword: "secret",
	})
	overview := service.Resources(context.Background(), true)
	if !overview.Harbor.Available || overview.Harbor.Projects != 1 || overview.Harbor.Repositories != 3 || len(overview.Harbor.RecentRepositories) != 1 {
		t.Fatalf("unexpected Harbor resources: %#v", overview.Harbor)
	}
	if !overview.ArgoCD.Available || overview.ArgoCD.Applications != 2 || overview.ArgoCD.Healthy != 1 || overview.ArgoCD.Degraded != 1 || overview.ArgoCD.Synced != 1 || overview.ArgoCD.OutOfSync != 1 {
		t.Fatalf("unexpected Argo CD resources: %#v", overview.ArgoCD)
	}
	if overview.GitLab.Status != StatusNotConfigured {
		t.Fatalf("unexpected GitLab resources: %#v", overview.GitLab)
	}
}

func TestResourcesCachesOverview(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2.0/projects" {
			calls++
		}
		switch r.URL.Path {
		case "/api/v2.0/projects":
			writeTestJSON(w, []map[string]any{})
		case "/api/v2.0/repositories":
			writeTestJSON(w, []map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service := NewServiceWithConfig(Config{HTTPTimeout: time.Second, HarborURL: server.URL, HarborUsername: "admin", HarborPassword: "secret"})
	_ = service.Resources(context.Background(), true)
	_ = service.Resources(context.Background(), false)
	if calls != 1 {
		t.Fatalf("expected cached resource read, calls=%d", calls)
	}
}

func TestResourcesParsesGitLabProjectsAndPipeline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "gitlab-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v4/projects":
			writeTestJSON(w, []map[string]any{{"id": 42, "name": "cmdb-web", "path_with_namespace": "ops/cmdb-web", "visibility": "private", "default_branch": "main", "last_activity_at": "2026-09-21T01:00:00Z", "web_url": "https://gitlab.example.com/ops/cmdb-web"}})
		case "/api/v4/projects/42/pipelines":
			writeTestJSON(w, []map[string]any{{"status": "success", "ref": "main", "sha": "abc123", "updated_at": "2026-09-21T01:10:00Z", "web_url": "https://gitlab.example.com/ops/cmdb-web/-/pipelines/9"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := NewServiceWithConfig(Config{HTTPTimeout: 3 * time.Second, GitLabURL: server.URL, GitLabToken: "gitlab-token"})
	overview := service.Resources(context.Background(), true)
	if !overview.GitLab.Available || overview.GitLab.Projects != 1 || len(overview.GitLab.RecentProjects) != 1 {
		t.Fatalf("unexpected GitLab resources: %#v", overview.GitLab)
	}
	project := overview.GitLab.RecentProjects[0]
	if project.LatestPipeline == nil || project.LatestPipeline.Status != "success" || project.LatestPipeline.SHA != "abc123" {
		t.Fatalf("unexpected GitLab pipeline: %#v", project.LatestPipeline)
	}
}

func TestResourceRelationsLifecycle(t *testing.T) {
	service := NewServiceWithConfig(Config{HTTPTimeout: time.Second})
	input := RelationInput{ResourceType: "argocd-application", ResourceID: "ops-qa/cmdb", ResourceName: "cmdb", AssetID: "k8s-jzq", AssetName: "jzq-cluster", AssetType: "k8s-cluster", RelationType: "deployed-to"}
	item, err := service.CreateRelation(context.Background(), input, "tester")
	if err != nil {
		t.Fatalf("create relation: %v", err)
	}
	if item.ID == "" || item.Source != "manual" || item.Actor != "tester" {
		t.Fatalf("unexpected relation: %#v", item)
	}
	updated, err := service.CreateRelation(context.Background(), input, "tester-2")
	if err != nil || updated.ID != item.ID {
		t.Fatalf("relation upsert failed: %#v %v", updated, err)
	}
	if relations := service.ListRelations(context.Background(), "argocd-application"); len(relations) != 1 {
		t.Fatalf("unexpected relation list: %#v", relations)
	}
	if err := service.DeleteRelation(context.Background(), item.ID); err != nil {
		t.Fatalf("delete relation: %v", err)
	}
	if relations := service.ListRelations(context.Background(), ""); len(relations) != 0 {
		t.Fatalf("relation was not deleted: %#v", relations)
	}
}

func TestRelationSuggestionsRankMatchingAssets(t *testing.T) {
	service := NewServiceWithConfig(Config{HTTPTimeout: time.Second})
	suggestions := service.SuggestRelations("argocd-application", "ops-qa/cmdb", "cmdb", []AssetCandidate{
		{ID: "k8s-jzq", Name: "jzq-cluster", Type: "k8s-cluster"},
		{ID: "cmdb", Name: "cmdb", Type: "k8s-cluster"},
	}, 10)
	if len(suggestions) != 2 || suggestions[0].AssetID != "cmdb" || suggestions[0].Score <= suggestions[1].Score {
		t.Fatalf("unexpected suggestions: %#v", suggestions)
	}
}

func TestBuildAutoResourcesIncludesEnabledToolTypes(t *testing.T) {
	overview := ResourceOverview{
		Harbor: HarborResources{Available: true, RecentRepositories: []HarborRepository{{Project: "cmdb", Name: "gateway-bff"}}},
		ArgoCD: ArgoCDResources{Available: true, Items: []ArgoApplication{{Name: "cmdb", Namespace: "argocd"}}},
		GitLab: GitLabResources{Available: true, RecentProjects: []GitLabProject{{ID: 42, Name: "cmdb-web"}}},
	}
	resources := buildAutoResources(overview, map[string]bool{"harbor-repository": true, "argocd-application": true, "gitlab-project": true})
	if len(resources) != 3 {
		t.Fatalf("unexpected auto resources: %#v", resources)
	}
	if resources[0].ResourceID != "cmdb/gateway-bff" || resources[1].ResourceID != "argocd/cmdb" || resources[2].ResourceID != "42" {
		t.Fatalf("unexpected auto resource ids: %#v", resources)
	}
}

func TestCloneResourceOverviewKeepsEmptySlicesNonNull(t *testing.T) {
	cloned := cloneResourceOverview(ResourceOverview{ArgoCD: ArgoCDResources{Available: true, Status: StatusConnected}})
	if cloned.ArgoCD.Items == nil {
		t.Fatal("empty Argo CD items must stay a non-nil slice so JSON encodes [] instead of null")
	}
	if cloned.Harbor.RecentRepositories == nil {
		t.Fatal("empty Harbor repositories must stay a non-nil slice")
	}
	if cloned.GitLab.RecentProjects == nil {
		t.Fatal("empty GitLab projects must stay a non-nil slice")
	}
	payload, err := json.Marshal(cloned)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(payload), `"items":null`) {
		t.Fatalf("argo items must not be encoded as null: %s", string(payload))
	}
}
