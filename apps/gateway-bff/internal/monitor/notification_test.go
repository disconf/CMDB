package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestWebhookNotificationChannelLifecycle(t *testing.T) {
	received := false
	renderedText := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		received = true
		renderedText, _ = payload["text"].(string)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	t.Setenv("WEBHOOK_ALLOWED_HOSTS", parsed.Hostname())
	service := NewService()
	channel, err := service.CreateNotificationChannel(CreateChannelInput{Name: "test webhook", Endpoint: server.URL + "/notify?token=secret"})
	if err != nil {
		t.Fatal(err)
	}
	if channel.Endpoint != "" || channel.DisplayURL != server.URL+"/notify" {
		t.Fatalf("endpoint leaked or masked incorrectly: %+v", channel)
	}
	if err := service.TestNotificationChannel(channel.ID); err != nil || !received {
		t.Fatalf("test notification failed: %v", err)
	}
	if renderedText == "" {
		t.Fatal("expected rendered notification text")
	}
	listed := service.NotificationChannels()
	if listed[0].LastStatus != "success" || listed[0].DisplayURL != server.URL+"/notify" {
		t.Fatalf("unexpected channel status: %+v", listed[0])
	}
	if toggled, err := service.ToggleNotificationChannel(channel.ID); err != nil || toggled.Enabled {
		t.Fatalf("toggle failed: %+v %v", toggled, err)
	}
	if err := service.DeleteNotificationChannel(channel.ID); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationTemplateRenderingAndUpdate(t *testing.T) {
	service := NewService()
	service.channels = []NotificationChannel{{ID: "channel-1", Template: "{{severity}} {{title}} -> {{owner}}"}}
	rendered := renderNotificationTemplate(service.channels[0].Template, Alert{Severity: "critical", Title: "Database down", Owner: "dba"})
	if rendered != "critical Database down -> dba" {
		t.Fatalf("unexpected render %q", rendered)
	}
	updated, err := service.UpdateNotificationTemplate("channel-1", "{{status}}: {{message}}")
	if err != nil || updated.Template != "{{status}}: {{message}}" {
		t.Fatalf("update failed: %+v %v", updated, err)
	}
}

func TestWebhookHostMustBeAllowlisted(t *testing.T) {
	t.Setenv("WEBHOOK_ALLOWED_HOSTS", "hooks.example.com")
	service := NewService()
	if _, err := service.CreateNotificationChannel(CreateChannelInput{Name: "blocked", Endpoint: "http://127.0.0.1/internal"}); err == nil {
		t.Fatal("expected non-allowlisted host to be rejected")
	}
}

func TestNotificationCanTargetSingleChannel(t *testing.T) {
	firstCalls, secondCalls := 0, 0
	first := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { firstCalls++ }))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { secondCalls++ }))
	defer second.Close()
	parsed, _ := url.Parse(first.URL)
	t.Setenv("WEBHOOK_ALLOWED_HOSTS", parsed.Hostname())
	service := NewService()
	firstChannel, _ := service.CreateNotificationChannel(CreateChannelInput{Name: "first", Endpoint: first.URL})
	_, _ = service.CreateNotificationChannel(CreateChannelInput{Name: "second", Endpoint: second.URL})
	service.notify(Alert{Title: "routed"}, firstChannel.ID)
	if firstCalls != 1 || secondCalls != 0 {
		t.Fatalf("unexpected deliveries first=%d second=%d", firstCalls, secondCalls)
	}
}

func TestNotificationRetriesAndRecordsEveryAttempt(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	t.Setenv("WEBHOOK_ALLOWED_HOSTS", parsed.Hostname())
	service := NewService()
	_, _ = service.CreateNotificationChannel(CreateChannelInput{Name: "retry", Endpoint: server.URL})
	if err := service.sendNotification(service.channels[0], Alert{ID: "alert-retry", Title: "Retry alert"}); err != nil {
		t.Fatal(err)
	}
	deliveries := service.NotificationDeliveries()
	stats := service.NotificationDeliveryStats()
	if calls != 3 || len(deliveries) != 3 || deliveries[0].Status != "success" || deliveries[0].Attempt != 3 {
		t.Fatalf("unexpected retry deliveries: calls=%d %+v", calls, deliveries)
	}
	if stats.Total != 3 || stats.Successful != 1 || stats.Failed != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}
