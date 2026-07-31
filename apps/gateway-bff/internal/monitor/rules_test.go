package monitor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrometheusAlertRules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/rules" || r.URL.Query().Get("type") != "alert" {
			t.Fatalf("unexpected request %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"groups":[{"name":"cmdb","rules":[{"type":"alerting","name":"GatewayDown","query":"up{job=\"cmdb-gateway\"} == 0","duration":120,"health":"ok","state":"firing","lastError":"","labels":{"severity":"critical"},"alerts":[{}]},{"type":"recording","name":"ignored"}]}]}}`)
	}))
	defer server.Close()
	t.Setenv("PROMETHEUS_URL", server.URL)
	rules, err := NewService().Rules()
	if err != nil || len(rules) != 1 {
		t.Fatalf("unexpected rules: %+v %v", rules, err)
	}
	if rules[0].Name != "GatewayDown" || rules[0].FiringCount != 1 || rules[0].Severity != "critical" {
		t.Fatalf("unexpected rule: %+v", rules[0])
	}
}
