package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAlertmanagerSilenceLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/silences":
			json.NewEncoder(w).Encode([]Silence{{ID: "silence-1", Comment: "maintenance", Status: SilenceStatus{State: "active"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/silences":
			var input CreateSilenceInput
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.CreatedBy != "admin" || !input.Matchers[0].IsEqual {
				t.Fatalf("unexpected silence input: %+v %v", input, err)
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"silenceID": "silence-2"})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v2/silence/silence-2":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("ALERTMANAGER_URL", server.URL)
	service := NewService()
	silences, err := service.Silences()
	if err != nil || len(silences) != 1 {
		t.Fatalf("list silences: %+v %v", silences, err)
	}
	id, err := service.CreateSilence(CreateSilenceInput{Matchers: []SilenceMatcher{{Name: "instance", Value: "host-01"}}, EndsAt: time.Now().Add(time.Hour), CreatedBy: "admin", Comment: "maintenance"})
	if err != nil || id != "silence-2" {
		t.Fatalf("create silence: %s %v", id, err)
	}
	if err := service.ExpireSilence(id); err != nil {
		t.Fatalf("expire silence: %v", err)
	}
}
