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
		case r.Method == http.MethodGet && r.URL.Path == "/cmdb-assets/_mapping":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cmdb-assets":{"mappings":{"properties":{"ID":{"type":"keyword"}}}}}`))
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
		case r.Method == http.MethodPost && r.URL.Path == "/cmdb-assets/_delete_by_query":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"deleted":0,"failures":[]}`))
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

func TestUpsertAssetsOnlyRefreshesTheGivenNodes(t *testing.T) {
	var statements []map[string]any
	neo4j := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Statements []map[string]any `json:"statements"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &payload)
		if len(payload.Statements) > 0 {
			statements = payload.Statements
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[],"errors":[]}`))
	}))
	defer neo4j.Close()

	service := newTestService(neo4j.URL, "")
	_, _, err := service.UpsertAssets(context.Background(), []Asset{{ID: "host-2", Name: "host-2", Relations: []map[string]string{{"type": "runs-on", "targetId": "cluster-1"}}}})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	joined := ""
	for _, statement := range statements {
		text, _ := statement["statement"].(string)
		joined += text + "\n"
		if strings.Contains(text, "MATCH ()-[r:RELATED]->() DELETE r") {
			t.Fatalf("incremental upsert must not wipe every edge: %s", text)
		}
	}
	if !strings.Contains(joined, "UNWIND $ids AS id MATCH (a:Asset {id: id}) OPTIONAL MATCH (a)-[stale:RELATED]->() DELETE stale") {
		t.Fatalf("upsert must clear the stale edges of the changed asset: %s", joined)
	}
	if !strings.Contains(joined, "MERGE (a)-[r:RELATED {type: rel.type}]->(t)") {
		t.Fatalf("upsert must recreate the current relations: %s", joined)
	}
}

func TestDeleteAssetsRemovesNodesAndDocuments(t *testing.T) {
	var bulkPayload string
	openSearch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bulkPayload = string(raw)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":false,"items":[{"delete":{"_id":"k8s-pod/ops-qa/gateway","status":200}}]}`))
	}))
	defer openSearch.Close()

	var neo4jPayload string
	neo4j := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		neo4jPayload = string(raw)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[],"errors":[]}`))
	}))
	defer neo4j.Close()

	service := newTestService(neo4j.URL, openSearch.URL)
	if err := service.DeleteAssets(context.Background(), []string{"k8s-pod/ops-qa/gateway", "  "}); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if !strings.Contains(neo4jPayload, "DETACH DELETE a") || !strings.Contains(neo4jPayload, "k8s-pod/ops-qa/gateway") {
		t.Fatalf("neo4j delete payload unexpected: %s", neo4jPayload)
	}
	if !strings.Contains(bulkPayload, `"delete"`) || strings.Contains(bulkPayload, `"index"`) {
		t.Fatalf("opensearch delete payload unexpected: %s", bulkPayload)
	}
}

func TestProjectAssetsPrunesDeletedAssets(t *testing.T) {
	pruneBody := ""
	openSearch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/cmdb-assets/_mapping":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cmdb-assets":{"mappings":{"properties":{"ID":{"type":"keyword"}}}}}`))
		case r.Method == http.MethodHead:
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/cmdb-assets/_delete_by_query":
			raw, _ := io.ReadAll(r.Body)
			pruneBody = string(raw)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"deleted":2,"failures":[]}`))
		case r.URL.Path == "/_bulk":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errors":false,"items":[{"index":{"_id":"host-1","status":201}}]}`))
		default:
			t.Fatalf("unexpected opensearch request %s %s", r.Method, r.URL.String())
		}
	}))
	defer openSearch.Close()

	var neo4jBodies []string
	neo4j := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		neo4jBodies = append(neo4jBodies, string(raw))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[],"errors":[]}`))
	}))
	defer neo4j.Close()

	service := newTestService(neo4j.URL, openSearch.URL)
	if _, _, err := service.ProjectAssets(context.Background(), []Asset{{ID: "host-1", Name: "host-1"}}); err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}
	if !strings.Contains(pruneBody, `"must_not"`) || !strings.Contains(pruneBody, `"host-1"`) {
		t.Fatalf("opensearch prune query unexpected: %s", pruneBody)
	}
	pruned := false
	for _, body := range neo4jBodies {
		if strings.Contains(body, "DETACH DELETE a") {
			pruned = true
		}
	}
	if !pruned {
		t.Fatalf("neo4j rebuild must prune orphans, bodies: %v", neo4jBodies)
	}
}

func TestProjectAssetsPrunesEverythingWhenInventoryIsEmpty(t *testing.T) {
	pruneBody := ""
	openSearch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cmdb-assets/_mapping" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cmdb-assets":{"mappings":{"properties":{"ID":{"type":"keyword"}}}}}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		pruneBody = string(raw)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"deleted":5,"failures":[]}`))
	}))
	defer openSearch.Close()

	neo4j := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[],"errors":[]}`))
	}))
	defer neo4j.Close()

	service := newTestService(neo4j.URL, openSearch.URL)
	if _, _, err := service.ProjectAssets(context.Background(), nil); err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}
	if !strings.Contains(pruneBody, `"match_all"`) {
		t.Fatalf("empty inventory must prune everything: %s", pruneBody)
	}
}

func TestNormalizeOpenSearchIndexRebuildsDynamicallyMappedIndex(t *testing.T) {
	var deleted, created bool
	openSearch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/cmdb-assets/_mapping":
			// A previously auto-created index maps ID as text with a keyword sub field.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cmdb-assets":{"mappings":{"properties":{"ID":{"type":"text","fields":{"keyword":{"type":"keyword"}}}}}}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/cmdb-assets":
			deleted = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/cmdb-assets":
			created = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		case r.Method == http.MethodHead:
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/_bulk":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errors":false,"items":[]}`))
		case r.URL.Path == "/cmdb-assets/_delete_by_query":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"deleted":0,"failures":[]}`))
		default:
			t.Fatalf("unexpected opensearch request %s %s", r.Method, r.URL.String())
		}
	}))
	defer openSearch.Close()

	service := newTestService("", openSearch.URL)
	if _, _, err := service.ProjectAssets(context.Background(), []Asset{{ID: "host-1", Name: "host-1"}}); err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}
	if !deleted || !created {
		t.Fatalf("an incompatible mapping must be rebuilt, deleted=%v created=%v", deleted, created)
	}
}

func TestNormalizeOpenSearchIndexKeepsExactMapping(t *testing.T) {
	mutations := 0
	openSearch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/cmdb-assets/_mapping":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cmdb-assets":{"mappings":{"properties":{"ID":{"type":"keyword"}}}}}`))
		case r.Method == http.MethodDelete || r.Method == http.MethodPut:
			mutations++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		case r.Method == http.MethodHead:
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/_bulk":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errors":false,"items":[]}`))
		case r.URL.Path == "/cmdb-assets/_delete_by_query":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"deleted":0,"failures":[]}`))
		default:
			t.Fatalf("unexpected opensearch request %s %s", r.Method, r.URL.String())
		}
	}))
	defer openSearch.Close()

	service := newTestService("", openSearch.URL)
	if _, _, err := service.ProjectAssets(context.Background(), []Asset{{ID: "host-1", Name: "host-1"}}); err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}
	if mutations != 0 {
		t.Fatalf("an exact keyword mapping must be left alone, mutations=%d", mutations)
	}
}
