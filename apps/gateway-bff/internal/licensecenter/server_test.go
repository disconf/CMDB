package licensecenter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"cmdb/gateway-bff/internal/license"
)

func TestIssueAndDownloadLicense(t *testing.T) {
	publicKey, privateKey, err := license.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privatePEM, err := license.PrivateKeyPEM(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LICENSE_CENTER_ADMIN_USERNAME", "license-admin")
	t.Setenv("LICENSE_CENTER_ADMIN_PASSWORD", "test-password")
	t.Setenv("LICENSE_SIGNING_KEY", string(privatePEM))
	t.Setenv("LICENSE_CENTER_DATA_FILE", filepath.Join(t.TempDir(), "licenses.json"))
	center, err := New()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(center.Handler())
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	loginBody := bytes.NewBufferString(`{"username":"license-admin","password":"test-password"}`)
	response, err := client.Post(server.URL+"/api/login", "application/json", loginBody)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %v %v", response, err)
	}
	_ = response.Body.Close()

	issueBody := bytes.NewBufferString(`{"customer":"测试客户","deploymentId":"deploy-test-001","edition":"enterprise","maxAssets":500,"validityDays":365,"features":["cmdb","monitoring"],"notes":"test"}`)
	response, err = client.Post(server.URL+"/api/licenses", "application/json", issueBody)
	if err != nil || response.StatusCode != http.StatusCreated {
		t.Fatalf("issue failed: %v %v", response, err)
	}
	var record Record
	if err := json.NewDecoder(response.Body).Decode(&record); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if record.MaxAssets != 500 || record.LicenseKey == "" {
		t.Fatalf("unexpected issued record: %+v", record)
	}
	claims, err := license.Verify(publicKey, record.LicenseKey)
	if err != nil || claims.DeploymentID != "deploy-test-001" {
		t.Fatalf("issued license cannot be verified: %+v %v", claims, err)
	}
	response, err = client.Get(server.URL + "/api/licenses/" + record.ID + "/download")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("download failed: %v %v", response, err)
	}
	_ = response.Body.Close()
}

func TestRejectsLicenseBelowCommercialThreshold(t *testing.T) {
	_, privateKey, _ := license.GenerateKeyPair()
	privatePEM, _ := license.PrivateKeyPEM(privateKey)
	t.Setenv("LICENSE_CENTER_ADMIN_USERNAME", "admin")
	t.Setenv("LICENSE_CENTER_ADMIN_PASSWORD", "password")
	t.Setenv("LICENSE_SIGNING_KEY", string(privatePEM))
	t.Setenv("LICENSE_CENTER_DATA_FILE", "")
	center, err := New()
	if err != nil {
		t.Fatal(err)
	}
	handler := center.Handler()
	login := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBufferString(`{"username":"admin","password":"password"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	cookies := loginResponse.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected login cookie")
	}
	issue := httptest.NewRequest(http.MethodPost, "/api/licenses", bytes.NewBufferString(`{"customer":"客户","deploymentId":"deploy-test-001","maxAssets":200,"validityDays":365}`))
	issue.Header.Set("Content-Type", "application/json")
	issue.AddCookie(cookies[0])
	issueResponse := httptest.NewRecorder()
	handler.ServeHTTP(issueResponse, issue)
	if issueResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected validation failure, got %d: %s", issueResponse.Code, issueResponse.Body.String())
	}
}
