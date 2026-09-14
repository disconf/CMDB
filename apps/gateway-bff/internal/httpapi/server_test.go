package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	server := NewServer()
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected ok health status, got %q", body["status"])
	}
}

func TestReadinessEndpoint(t *testing.T) {
	server := NewServer()
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected readiness 200, got %d", recorder.Code)
	}
}

func TestMetricsAndAlertmanagerWebhookEndpoints(t *testing.T) {
	server := NewServer()
	metrics := httptest.NewRecorder()
	server.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metrics.Code != http.StatusOK || !bytes.Contains(metrics.Body.Bytes(), []byte("cmdb_assets_total")) {
		t.Fatalf("unexpected metrics response %d", metrics.Code)
	}
	login := httptest.NewRecorder()
	server.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"admin123"}`)))
	var session struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil || session.Token == "" {
		t.Fatalf("login failed: %v", err)
	}
	assetStatus := func() string {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/assets/srv-prod-001", nil)
		request.Header.Set("Authorization", "Bearer "+session.Token)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		var asset struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(response.Body).Decode(&asset); err != nil {
			t.Fatalf("decode asset: %v", err)
		}
		return asset.Status
	}
	body := `{"status":"firing","alerts":[{"status":"firing","labels":{"alertname":"GatewayDown","severity":"critical","instance":"gateway","cmdb_asset_id":"srv-prod-001"},"annotations":{"summary":"Gateway down"},"startsAt":"2026-07-21T01:00:00Z","generatorURL":"http://prometheus.example/graph","fingerprint":"webhook-test"}]}`
	webhook := httptest.NewRecorder()
	server.Handler().ServeHTTP(webhook, httptest.NewRequest(http.MethodPost, "/api/v1/monitor/alerts/webhook", bytes.NewBufferString(body)))
	if webhook.Code != http.StatusAccepted {
		t.Fatalf("expected webhook 202, got %d: %s", webhook.Code, webhook.Body.String())
	}
	if status := assetStatus(); status != "offline" {
		t.Fatalf("expected firing alert to set asset offline, got %s", status)
	}
	resolvedBody := `{"status":"resolved","alerts":[{"status":"resolved","labels":{"alertname":"GatewayDown","severity":"critical","instance":"gateway","cmdb_asset_id":"srv-prod-001"},"annotations":{"summary":"Gateway down"},"startsAt":"2026-07-21T01:00:00Z","endsAt":"2026-07-21T01:05:00Z","generatorURL":"http://prometheus.example/graph","fingerprint":"webhook-test"}]}`
	resolved := httptest.NewRecorder()
	server.Handler().ServeHTTP(resolved, httptest.NewRequest(http.MethodPost, "/api/v1/monitor/alerts/webhook", bytes.NewBufferString(resolvedBody)))
	if resolved.Code != http.StatusAccepted {
		t.Fatalf("expected resolved webhook 202, got %d: %s", resolved.Code, resolved.Body.String())
	}
	if status := assetStatus(); status != "online" {
		t.Fatalf("expected resolved alert to restore asset online, got %s", status)
	}
}
func TestPrometheusServiceDiscoveryEndpoint(t *testing.T) {
	t.Setenv("AGENT_SHARED_TOKEN", "prometheus-discovery-token")
	server := NewServer()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/service-discovery/node-exporter", nil)
	request.Header.Set("Authorization", "Bearer prometheus-discovery-token")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected service discovery 200, got %d", recorder.Code)
	}
	var groups []map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&groups); err != nil {
		t.Fatalf("decode service discovery response: %v", err)
	}
	if groups == nil {
		t.Fatal("service discovery response must be a JSON array")
	}
}

func TestPrometheusSNMPServiceDiscoveryEndpoint(t *testing.T) {
	t.Setenv("AGENT_SHARED_TOKEN", "prometheus-snmp-discovery-token")
	server := NewServer()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/service-discovery/snmp", nil)
	request.Header.Set("Authorization", "Bearer prometheus-snmp-discovery-token")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected SNMP service discovery 200, got %d", recorder.Code)
	}
	var groups []map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&groups); err != nil {
		t.Fatalf("decode SNMP service discovery response: %v", err)
	}
	if groups == nil {
		t.Fatal("SNMP service discovery response must be a JSON array")
	}
}
func TestHostMonitoringEndpointRequiresAuthAndValidAsset(t *testing.T) {
	server := NewServer()
	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/monitor/hosts/srv-prod-001", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauthorized.Code)
	}
	login := httptest.NewRecorder()
	server.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"admin123"}`)))
	var session struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(login.Body).Decode(&session)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/hosts/srv-prod-001", nil)
	request.Header.Set("Authorization", "Bearer "+session.Token)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected host monitoring 200, got %d", recorder.Code)
	}
}

func TestInstallExporterCreatesAuditedRunnerJob(t *testing.T) {
	t.Setenv("ANSIBLE_RUNNER_URL", "http://127.0.0.1:1")
	t.Setenv("AGENT_SHARED_TOKEN", "agent-test-token")
	server := NewServer()
	report := httptest.NewRequest(http.MethodPost, "/api/v1/agent/report", bytes.NewBufferString(`{"agentId":"agent-install-01","hostname":"install-01","ip":"10.90.0.11"}`))
	report.Header.Set("Authorization", "Bearer agent-test-token")
	reportRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(reportRecorder, report)
	if reportRecorder.Code != http.StatusOK {
		t.Fatalf("agent report failed: %d %s", reportRecorder.Code, reportRecorder.Body.String())
	}
	login := httptest.NewRecorder()
	server.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"admin123"}`)))
	var session struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(login.Body).Decode(&session)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/monitor/hosts/agent-install-01/install-exporter", nil)
	request.Header.Set("Authorization", "Bearer "+session.Token)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected install job 202, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestDashboardOverviewEndpoint(t *testing.T) {
	server := NewServer()
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/overview", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type %q", got)
	}
}

func TestUnknownAPIEndpointReturnsJSONNotFound(t *testing.T) {
	server := NewServer()
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", recorder.Code)
	}
}

func TestLoginAndCurrentUserFlow(t *testing.T) {
	server := NewServer()
	login := httptest.NewRecorder()
	server.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"admin123"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d", login.Code)
	}
	var session struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil {
		t.Fatalf("decode login: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.Header.Set("Authorization", "Bearer "+session.Token)
	current := httptest.NewRecorder()
	server.Handler().ServeHTTP(current, request)
	if current.Code != http.StatusOK {
		t.Fatalf("expected current user 200, got %d", current.Code)
	}
}

func TestProtectedEndpointRejectsMissingToken(t *testing.T) {
	server := NewServer()
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
}

func TestLoginRejectsInvalidCredentialsAndMalformedInput(t *testing.T) {
	server := NewServer()
	for _, body := range []string{`{"username":"admin","password":"wrong"}`, `{invalid`} {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body)))
		if recorder.Code != http.StatusUnauthorized && recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected client error, got %d", recorder.Code)
		}
	}
}

func TestModulesAndLogoutFlow(t *testing.T) {
	server := NewServer()
	login := httptest.NewRecorder()
	server.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"viewer","password":"viewer123"}`)))
	var session struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil {
		t.Fatalf("decode login: %v", err)
	}

	modulesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/modules", nil)
	modulesRequest.Header.Set("Authorization", "Bearer "+session.Token)
	modules := httptest.NewRecorder()
	server.Handler().ServeHTTP(modules, modulesRequest)
	if modules.Code != http.StatusOK {
		t.Fatalf("expected modules 200, got %d", modules.Code)
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutRequest.Header.Set("Authorization", "Bearer "+session.Token)
	logout := httptest.NewRecorder()
	server.Handler().ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("expected logout 204, got %d", logout.Code)
	}

	currentRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	currentRequest.Header.Set("Authorization", "Bearer "+session.Token)
	current := httptest.NewRecorder()
	server.Handler().ServeHTTP(current, currentRequest)
	if current.Code != http.StatusUnauthorized {
		t.Fatalf("expected expired session 401, got %d", current.Code)
	}
}

func TestCMDBEndpointsRequireAuthAndReturnCatalog(t *testing.T) {
	server := NewServer()
	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/assets", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauthorized.Code)
	}

	login := httptest.NewRecorder()
	server.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"admin123"}`)))
	var session struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	paths := []string{"/api/v1/cmdb/models", "/api/v1/cmdb/summary", "/api/v1/cmdb/assets?q=prod&type=physical-server&page=1&pageSize=10", "/api/v1/cmdb/assets/srv-prod-001"}
	for _, path := range paths {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+session.Token)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected %s to return 200, got %d", path, recorder.Code)
		}
	}

	missingRequest := httptest.NewRequest(http.MethodGet, "/api/v1/cmdb/assets/missing", nil)
	missingRequest.Header.Set("Authorization", "Bearer "+session.Token)
	missing := httptest.NewRecorder()
	server.Handler().ServeHTTP(missing, missingRequest)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("expected missing asset 404, got %d", missing.Code)
	}
}

func TestCMDBWriteHistoryAndImportFlow(t *testing.T) {
	server := NewServer()
	login := httptest.NewRecorder()
	server.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"admin123"}`)))
	var session struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(login.Body).Decode(&session)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		request.Header.Set("Authorization", "Bearer "+session.Token)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		return recorder
	}
	created := call(http.MethodPost, "/api/v1/cmdb/assets", `{"id":"vm-api-001","name":"api-vm","type":"virtual-machine","status":"online","ip":"10.1.1.8","environment":"测试","projectGroup":"研发效能组","owner":"李娜","location":"VMware","source":"manual","tags":["测试"]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", created.Code, created.Body.String())
	}
	updated := call(http.MethodPatch, "/api/v1/cmdb/assets/vm-api-001", `{"name":"api-vm","status":"warning","ip":"10.1.1.8","environment":"测试","projectGroup":"研发效能组","owner":"李娜","location":"VMware","tags":["测试","维护"]}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d", updated.Code)
	}
	history := call(http.MethodGet, "/api/v1/cmdb/assets/vm-api-001/history", "")
	if history.Code != http.StatusOK {
		t.Fatalf("expected history 200, got %d", history.Code)
	}
	imported := call(http.MethodPost, "/api/v1/cmdb/imports", `[{"id":"cloud-api-001","name":"cloud-api","type":"cloud-host","status":"online","environment":"测试","projectGroup":"数据平台组","owner":"陈明","location":"华东","source":"csv","tags":[]}]`)
	if imported.Code != http.StatusOK {
		t.Fatalf("expected import 200, got %d", imported.Code)
	}
	invalid := call(http.MethodPost, "/api/v1/cmdb/assets", `{"id":"bad"}`)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected validation 422, got %d", invalid.Code)
	}
	duplicate := call(http.MethodPost, "/api/v1/cmdb/assets", `{"id":"vm-api-001","name":"duplicate","type":"virtual-machine","status":"online","environment":"test","projectGroup":"dev","owner":"owner","source":"manual","tags":[]}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("expected conflict 409, got %d", duplicate.Code)
	}
}

func TestAgentVersionLedgerEndpoint(t *testing.T) {
	t.Setenv("CMDB_ENABLE_DEMO_DATA", "false")
	server := NewServer()
	login := httptest.NewRecorder()
	server.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"admin123"}`)))
	var session struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil || session.Token == "" {
		t.Fatalf("login failed: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/agent-versions", nil)
	request.Header.Set("Authorization", "Bearer "+session.Token)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected agent version ledger 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		CurrentVersion string `json:"currentVersion"`
		Agents         []any  `json:"agents"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode agent version ledger: %v", err)
	}
	if body.CurrentVersion == "" || body.Agents == nil {
		t.Fatalf("unexpected agent version ledger: %+v", body)
	}
}
