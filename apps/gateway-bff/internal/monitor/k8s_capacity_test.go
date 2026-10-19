package monitor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKubernetesCapacity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		value := ""
		switch {
		case strings.Contains(query, `resource="pods"`):
			value = "200"
		case strings.Contains(query, "kube_pod_info"):
			value = "154"
		case strings.Contains(query, `resource="memory"`) && strings.Contains(query, "kube_node_status_allocatable"):
			value = "274877906944"
		case strings.Contains(query, "kube_pod_container_resource_requests") && strings.Contains(query, `resource="memory"`):
			value = "107374182400"
		case strings.Contains(query, `resource="cpu"`) && strings.Contains(query, "kube_node_status_allocatable"):
			value = "184"
		case strings.Contains(query, "kube_pod_container_resource_requests") && strings.Contains(query, `resource="cpu"`):
			value = "56.19"
		default:
			t.Fatalf("unexpected query: %s", query)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"success","data":{"result":[{"metric":{},"value":[1,"%s"]}]}}`, value)
	}))
	defer server.Close()

	service := &Service{prometheusURL: server.URL, httpClient: server.Client()}
	capacity := service.KubernetesCapacity("jzq-cluster")
	if !capacity.Available || !capacity.Pods.Available || !capacity.CPU.Available || !capacity.Memory.Available {
		t.Fatalf("expected all capacity groups to be available: %+v", capacity)
	}
	if capacity.Pods.Used != 154 || capacity.Pods.Capacity != 200 || capacity.Pods.UsagePercent != 77 {
		t.Fatalf("unexpected pod capacity: %+v", capacity.Pods)
	}
	if capacity.CPU.Requested != 56.19 || capacity.CPU.Capacity != 184 || capacity.CPU.UsagePercent != 30.54 {
		t.Fatalf("unexpected cpu capacity: %+v", capacity.CPU)
	}
	if capacity.Memory.Requested != 100 || capacity.Memory.Capacity != 256 || capacity.Memory.UsagePercent != 39.06 {
		t.Fatalf("unexpected memory capacity: %+v", capacity.Memory)
	}
}

func TestKubernetesCapacityUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"result":[]}}`)
	}))
	defer server.Close()

	service := &Service{prometheusURL: server.URL, httpClient: server.Client()}
	capacity := service.KubernetesCapacity("local")
	if capacity.Available {
		t.Fatalf("expected unavailable capacity: %+v", capacity)
	}
	if capacity.Message == "" {
		t.Fatal("expected an unavailable message")
	}
}
