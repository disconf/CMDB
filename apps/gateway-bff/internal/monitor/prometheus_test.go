package monitor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPrometheusMetricsAndTargetSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/query_range" {
			fmt.Fprint(w, `{"status":"success","data":{"result":[{"values":[[1,"1.5"],[2,"2.5"]]}]}}`)
			return
		}
		value := "2"
		if r.URL.Query().Get("query") == "sum(up)" {
			value = "1"
		}
		fmt.Fprintf(w, `{"status":"success","data":{"result":[{"value":[1,%q]}]}}`, value)
	}))
	defer server.Close()
	t.Setenv("PROMETHEUS_URL", server.URL)
	service := NewService()
	metrics := service.Metrics()
	if len(metrics) != 12 {
		t.Fatalf("expected twelve Prometheus metrics, got %d", len(metrics))
	}
	if len(metrics[0].Trend) != 2 {
		t.Fatalf("expected trend samples, got %+v", metrics[0].Trend)
	}
	summary := service.Summary()
	if summary.Targets != 2 || summary.Healthy != 1 || summary.Availability != 50 {
		t.Fatalf("unexpected target summary %+v", summary)
	}
}

func TestHostMonitoringUsesCMDBAssetLabel(t *testing.T) {
	queries := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("query"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/query_range" {
			fmt.Fprint(w, `{"status":"success","data":{"result":[{"values":[[1,"10"],[2,"20"]]}]}}`)
			return
		}
		fmt.Fprint(w, `{"status":"success","data":{"result":[{"value":[1,"1"]}]}}`)
	}))
	defer server.Close()
	t.Setenv("PROMETHEUS_URL", server.URL)
	host := NewService().Host("agent-host-01")
	if !host.Monitored || !host.Up || len(host.Metrics) != 4 {
		t.Fatalf("unexpected host monitoring: %+v", host)
	}
	for _, query := range queries {
		if !strings.Contains(query, `cmdb_asset_id="agent-host-01"`) {
			t.Fatalf("query is not isolated to the asset: %s", query)
		}
	}
}

func TestCoverageClassifiesHealthyDownAndMissingHosts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"result":[{"metric":{"cmdb_asset_id":"a-1"},"value":[1,"1"]},{"metric":{"cmdb_asset_id":"a-2"},"value":[1,"0"]}]}}`)
	}))
	defer server.Close()
	t.Setenv("PROMETHEUS_URL", server.URL)
	coverage := NewService().Coverage([]ExpectedHost{{AssetID: "a-1"}, {AssetID: "a-2"}, {AssetID: "a-3"}})
	if !coverage.Available || coverage.Total != 3 || coverage.Monitored != 2 || coverage.Healthy != 1 || coverage.Down != 1 || coverage.Missing != 1 {
		t.Fatalf("unexpected coverage: %+v", coverage)
	}
	if coverage.Percentage != 66.67 {
		t.Fatalf("unexpected coverage percentage: %v", coverage.Percentage)
	}
}

func TestCoverageDoesNotInventDataWithoutPrometheus(t *testing.T) {
	t.Setenv("PROMETHEUS_URL", "")
	coverage := NewService().Coverage([]ExpectedHost{{AssetID: "a-1"}})
	if coverage.Available || coverage.Monitored != 0 || coverage.Missing != 1 || coverage.Hosts[0].MonitorStatus != "missing" {
		t.Fatalf("unexpected unavailable coverage: %+v", coverage)
	}
}

func TestRealPrometheusTargetSummary(t *testing.T) {
	prometheusURL := os.Getenv("CMDB_INTEGRATION_PROMETHEUS_URL")
	if prometheusURL == "" {
		t.Skip("CMDB_INTEGRATION_PROMETHEUS_URL is not configured")
	}
	t.Setenv("PROMETHEUS_URL", prometheusURL)
	service := NewService()
	summary := service.Summary()
	if summary.Targets < 2 || summary.Healthy < 2 {
		t.Fatalf("unexpected real Prometheus summary %+v", summary)
	}
}
