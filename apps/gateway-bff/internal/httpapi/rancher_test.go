package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchRancherClusters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rancher-token" {
			t.Errorf("authorization header = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"c-abc","name":"prod","state":"active","kubernetesVersion":"v1.28.15","nodeCount":5}]}`))
	}))
	defer server.Close()
	t.Setenv("RANCHER_URL", server.URL)
	t.Setenv("RANCHER_TOKEN", "rancher-token")
	items, err := fetchRancherClusters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "prod" || items[0].NodeCount != 5 || items[0].DashboardURL != server.URL+"/dashboard/c/c-abc/explore" {
		t.Fatalf("unexpected clusters: %#v", items)
	}
	asset := rancherClusterAsset(items[0])
	if asset.ID != "rancher-cluster-c-abc" || asset.Type != "k8s-cluster" || asset.Source != "Rancher" {
		t.Fatalf("unexpected Rancher asset: %#v", asset)
	}
}

func TestFetchRancherClustersRequiresConfig(t *testing.T) {
	t.Setenv("RANCHER_URL", "")
	t.Setenv("RANCHER_TOKEN", "")
	if _, err := fetchRancherClusters(context.Background()); err == nil {
		t.Fatal("expected configuration error")
	}
}
