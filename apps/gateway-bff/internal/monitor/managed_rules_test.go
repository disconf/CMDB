package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmptyMonitorListsEncodeAsArrays(t *testing.T) {
	service := NewService()
	if service.RoutingRules() == nil {
		t.Fatal("routing rules must be an empty slice, not nil")
	}
	if service.NotificationChannels() == nil {
		t.Fatal("notification channels must be an empty slice, not nil")
	}
	if service.NotificationDeliveries() == nil {
		t.Fatal("notification deliveries must be an empty slice, not nil")
	}
}

func TestRenderManagedAlertRules(t *testing.T) {
	content := renderManagedAlertRules([]ManagedAlertRule{
		{ID: "rule-1", Group: "cmdb-business", Name: "BusinessServiceDown", Query: `up{job="business"} == 0`, Duration: "5m", Severity: "critical", Summary: "业务服务不可用", Enabled: true},
		{ID: "rule-2", Group: "cmdb-business", Name: "DisabledRule", Query: "vector(1)", Duration: "1m", Severity: "warning", Enabled: false},
	})
	if !strings.Contains(content, "BusinessServiceDown") || !strings.Contains(content, "cmdb-business") {
		t.Fatalf("enabled rule missing from rendered config: %s", content)
	}
	if strings.Contains(content, "DisabledRule") {
		t.Fatalf("disabled rule must not be rendered: %s", content)
	}
}

func TestReloadPrometheusRules(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/-/reload" {
			t.Fatalf("unexpected reload request %s %s", r.Method, r.URL.Path)
		}
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	service := NewService()
	service.prometheusURL = server.URL
	service.httpClient = server.Client()
	if err := service.ReloadPrometheusRules(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("Prometheus reload was not called")
	}
}
