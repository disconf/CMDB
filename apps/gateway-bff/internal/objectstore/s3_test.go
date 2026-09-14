package objectstore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func testStore(t *testing.T, handler http.HandlerFunc) *Store {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &Store{endpoint: endpoint, bucket: "cmdb-test", region: "us-east-1", accessKey: "test", secretKey: "test", client: server.Client()}
}

func TestStoreDeleteIsIdempotent(t *testing.T) {
	requests := []string{}
	store := testStore(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodDelete && r.URL.Path == "/cmdb-test/terminal-recordings/missing.jsonl.gz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := store.Delete(context.Background(), "terminal-recordings/session-1.jsonl.gz"); err != nil {
		t.Fatalf("delete existing object: %v", err)
	}
	if err := store.Delete(context.Background(), "terminal-recordings/missing.jsonl.gz"); err != nil {
		t.Fatalf("delete missing object: %v", err)
	}
	if len(requests) < 3 {
		t.Fatalf("unexpected request count: %v", requests)
	}
}

func TestStoreDeleteRejectsMissingKey(t *testing.T) {
	store := testStore(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	if err := store.Delete(context.Background(), " "); err == nil {
		t.Fatal("expected empty object key to be rejected")
	}
}
