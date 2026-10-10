package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVMwareClientInventory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/session":
			if username, password, ok := r.BasicAuth(); !ok || username != "admin" || password != "secret" {
				t.Fatalf("unexpected basic auth")
			}
			w.Header().Set("vmware-api-session-id", "session-1")
			w.WriteHeader(http.StatusCreated)
		case "/api/vcenter/cluster":
			if r.Header.Get("vmware-api-session-id") != "session-1" {
				t.Fatalf("missing session header")
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"cluster": "cluster-1", "name": "Cluster A", "drs_enabled": true, "ha_enabled": true}})
		case "/api/vcenter/host":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"host": "host-1", "name": "esxi-1", "cluster": "cluster-1", "connection_state": "CONNECTED", "power_state": "POWERED_ON"}})
		case "/api/vcenter/vm":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"vm": "vm-1", "name": "app-1", "host": "host-1", "power_state": "POWERED_ON", "cpu_count": 4, "memory_size_MiB": 8192}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := buildVMwareClient(context.Background(), server.URL, "admin", "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := client.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Clusters) != 1 || len(inventory.Hosts) != 1 || len(inventory.VMs) != 1 || inventory.VMs[0].CPUCount != 4 || inventory.Hosts[0].Cluster != "cluster-1" || inventory.VMs[0].Host != "host-1" {
		t.Fatalf("unexpected inventory: %#v", inventory)
	}
}
