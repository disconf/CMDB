package projection

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestService(neo4jURL, openSearchURL string) *Service {
	service := NewFromEnv()
	service.neo4jURL = neo4jURL
	service.openSearchURL = openSearchURL
	return service
}

func TestProjectAssetsUsesBulkForSlashIDs(t *testing.T) {
	var bulkBody string
	indexCreated := false
	openSearch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodHead && r.URL.Path == "/cmdb-assets":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && r.URL.Path == "/cmdb-assets":
			indexCreated = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/_bulk":
			raw, _ := io.ReadAll(r.Body)
			bulkBody = string(raw)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errors":false,"items":[{"index":{"_id":"k8s-pod/ops-qa/gateway","status":201}}]}`))
		default:
			t.Fatalf("unexpected opensearch request %s %s", r.Method, r.URL.String())
		}
	}))
	defer openSearch.Close()

	assertStatement := ""
	neo4j := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/db/neo4j/tx/commit" {
			t.Fatalf("unexpected neo4j path %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		payload := string(raw)
		if strings.Contains(payload, "CREATE CONSTRAINT") {
			// Neo4j refuses schema changes inside a write transaction, so the
			// constraint must travel on its own.
			if strings.Contains(payload, "UNWIND") {
				t.Fatalf("constraint mixed into the write transaction: %s", payload)
			}
		} else if strings.Contains(payload, "UNWIND $assets AS row") {
			assertStatement = payload
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[],"errors":[]}`))
	}))
	defer neo4j.Close()

	service := newTestService(neo4j.URL, openSearch.URL)
	assets := []Asset{
		{ID: "k8s-pod/ops-qa/gateway", Name: "gateway", Type: "k8s-pod", Status: "online", Relations: []map[string]string{{"type": "runs-on", "targetId": "host-node01.jzq.com"}}},
		{ID: "host-node01.jzq.com", Name: "node01", Type: "host", Status: "online"},
	}
	neo4jCount, openSearchCount, err := service.ProjectAssets(context.Background(), assets)
	if err != nil {
		t.Fatalf("projection failed: %v", err)
	}
	if neo4jCount != 2 || openSearchCount != 2 {
		t.Fatalf("unexpected counts neo4j=%d opensearch=%d", neo4jCount, openSearchCount)
	}
	if !indexCreated {
		t.Fatal("expected the cmdb-assets index to be created")
	}
	if !strings.Contains(assertStatement, `"id":"k8s-pod/ops-qa/gateway"`) {
		t.Fatalf("neo4j payload lost the asset batch: %s", assertStatement)
	}
	if !strings.Contains(assertStatement, "RELATED") {
		t.Fatalf("neo4j payload missing relation statement: %s", assertStatement)
	}
	if !strings.Contains(bulkBody, `"k8s-pod/ops-qa/gateway"`) {
		t.Fatalf("bulk payload lost the asset id: %s", bulkBody)
	}
	if !strings.Contains(bulkBody, "/_bulk") && !strings.Contains(bulkBody, "{\"index\"") {
		t.Fatalf("bulk payload missing action line: %s", bulkBody)
	}
}

func TestProjectAssetsSurfacesOpenSearchItemErrors(t *testing.T) {
	openSearch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":true,"items":[{"index":{"_id":"host-1","status":400,"error":{"type":"mapper_parsing_exception","reason":"failed to parse field"}}}]}`))
	}))
	defer openSearch.Close()

	service := newTestService("", openSearch.URL)
	_, _, err := service.ProjectAssets(context.Background(), []Asset{{ID: "host-1", Name: "host-1"}})
	if err == nil || !strings.Contains(err.Error(), "mapper_parsing_exception") {
		t.Fatalf("expected a mapped opensearch error, got %v", err)
	}
}

func TestProjectAssetsSurfacesNeo4jErrors(t *testing.T) {
	neo4j := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[],"errors":[{"code":"Neo.ClientError.Statement.SyntaxError","message":"boom"}]}`))
	}))
	defer neo4j.Close()

	service := newTestService(neo4j.URL, "")
	_, _, err := service.ProjectAssets(context.Background(), []Asset{{ID: "host-1", Name: "host-1"}})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected a neo4j error, got %v", err)
	}
}

func TestInspectReportsCounts(t *testing.T) {
	neo4j := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[{"data":[{"row":[42]}]}],"errors":[]}`))
	}))
	defer neo4j.Close()
	openSearch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cmdb-assets/_count" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"count":41}`))
	}))
	defer openSearch.Close()

	service := newTestService(neo4j.URL, openSearch.URL)
	status := service.Inspect(context.Background())
	if status.Neo4jAssets != 42 || status.OpenSearchAssets != 41 {
		raw, _ := json.Marshal(status)
		t.Fatalf("unexpected status %s", string(raw))
	}
	if status.Detail != "" {
		t.Fatalf("unexpected detail %s", status.Detail)
	}
}
