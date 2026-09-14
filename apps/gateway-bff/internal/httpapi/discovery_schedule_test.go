package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCollectionScheduleCRUDAPI(t *testing.T) {
	server := NewServer()
	login := httptest.NewRecorder()
	server.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"admin123"}`)))
	var session struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil || session.Token == "" {
		t.Fatalf("login failed: %v", err)
	}
	payload := `{"name":"测试采集计划","kind":"ssh","cidrs":["10.10.0.0/30"],"port":22,"intervalMinutes":15,"enabled":true}`
	create := httptest.NewRequest(http.MethodPost, "/api/v1/discovery/schedules", bytes.NewBufferString(payload))
	create.Header.Set("Authorization", "Bearer "+session.Token)
	createRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRecorder, create)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create schedule: %d %s", createRecorder.Code, createRecorder.Body.String())
	}
	var schedule struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createRecorder.Body).Decode(&schedule); err != nil || schedule.ID == "" {
		t.Fatalf("decode schedule: %v", err)
	}
	list := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/schedules", nil)
	list.Header.Set("Authorization", "Bearer "+session.Token)
	listRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRecorder, list)
	if listRecorder.Code != http.StatusOK || !bytes.Contains(listRecorder.Body.Bytes(), []byte(schedule.ID)) {
		t.Fatalf("list schedule: %d %s", listRecorder.Code, listRecorder.Body.String())
	}
	remove := httptest.NewRequest(http.MethodDelete, "/api/v1/discovery/schedules/"+schedule.ID, nil)
	remove.Header.Set("Authorization", "Bearer "+session.Token)
	removeRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(removeRecorder, remove)
	if removeRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete schedule: %d %s", removeRecorder.Code, removeRecorder.Body.String())
	}
}
