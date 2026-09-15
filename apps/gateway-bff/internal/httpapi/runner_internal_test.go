package httpapi

import (
	"cmdb/gateway-bff/internal/cmdb"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunnerAssetLookupRequiresToken(t *testing.T) {
	t.Setenv("ANSIBLE_RUNNER_TOKEN", "runner-test-token")
	server := NewServer()
	request := httptest.NewRequest(http.MethodGet, "/internal/runner/cmdb/assets/srv-prod-001", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
}

func TestRunnerAssetLookupReturnsInventoryAddress(t *testing.T) {
	t.Setenv("ANSIBLE_RUNNER_TOKEN", "runner-test-token")
	server := NewServer()
	request := httptest.NewRequest(http.MethodGet, "/internal/runner/cmdb/assets/srv-prod-001", nil)
	request.Header.Set("Authorization", "Bearer runner-test-token")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		ID      string `json:"id"`
		IP      string `json:"ip"`
		SSHPort int    `json:"sshPort"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID == "" || body.IP == "" || body.SSHPort != 22 {
		t.Fatalf("unexpected runner asset response: %#v", body)
	}
}
func TestRunnerAssetSSHPortUsesAttributeOverride(t *testing.T) {
	asset := cmdb.Asset{Attributes: []cmdb.Attribute{{Name: "SSH_PORT", Value: "19122"}}}
	if port := runnerAssetSSHPort(asset); port != 19122 {
		t.Fatalf("expected custom SSH port 19122, got %d", port)
	}
	asset.Attributes[0].Value = "70000"
	if port := runnerAssetSSHPort(asset); port != 22 {
		t.Fatalf("expected default SSH port for invalid override, got %d", port)
	}
}
