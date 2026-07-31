package monitor

import (
	"testing"
	"time"
)

func TestSummaryAndAlertLifecycle(t *testing.T) {
	s := NewService()
	if s.Summary().Active < 2 {
		t.Fatal("expected active alerts")
	}
	a, err := s.Acknowledge("alert-001", "admin")
	if err != nil || a.Status != "acknowledged" {
		t.Fatalf("ack failed: %#v %v", a, err)
	}
	a, err = s.Resolve("alert-001", "admin")
	if err != nil || a.Status != "resolved" {
		t.Fatalf("resolve failed: %#v %v", a, err)
	}
}

func TestAlertmanagerWebhookUpsertsAndResolvesAlert(t *testing.T) {
	s := NewService()
	payload := WebhookPayload{Status: "firing"}
	payload.Alerts = append(payload.Alerts, struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		Fingerprint string            `json:"fingerprint"`
	}{Status: "firing", Labels: map[string]string{"alertname": "HostDown", "severity": "critical", "instance": "host-01"}, Annotations: map[string]string{"summary": "主机不可用", "description": "探测失败"}, StartsAt: time.Now(), Fingerprint: "fp-01"})
	if got, err := s.ReceiveWebhook(payload); err != nil || got != 1 {
		t.Fatalf("expected one alert, got %d", got)
	}
	payload.Status = "resolved"
	payload.Alerts[0].Status = "resolved"
	_, _ = s.ReceiveWebhook(payload)
	found := false
	for _, a := range s.Alerts() {
		if a.ID == "fp-01" {
			found = true
			if a.Status != "resolved" {
				t.Fatalf("expected resolved alert, got %s", a.Status)
			}
		}
	}
	if !found {
		t.Fatal("webhook alert not found")
	}
	events, err := s.Events("fp-01")
	if err != nil || len(events) != 2 || events[0].Type != "resolved" || events[1].Type != "firing" {
		t.Fatalf("unexpected alert timeline: %+v %v", events, err)
	}
}

func TestAlertmanagerUsesCMDBAssetIDForAssociation(t *testing.T) {
	s := NewService()
	payload := WebhookPayload{Status: "firing"}
	payload.Alerts = append(payload.Alerts, struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		Fingerprint string            `json:"fingerprint"`
	}{Status: "firing", Labels: map[string]string{"alertname": "HostDown", "instance": "host-a", "cmdb_asset_id": "asset-a"}, StartsAt: time.Now(), Fingerprint: "fp-asset"})
	if _, err := s.ReceiveWebhook(payload); err != nil {
		t.Fatalf("receive webhook: %v", err)
	}
	host := s.Host("asset-a")
	if len(host.Alerts) != 1 || host.Alerts[0].TargetID != "asset-a" || host.Alerts[0].Target != "host-a" {
		t.Fatalf("alert was not associated to CMDB asset: %+v", host.Alerts)
	}
}

func TestRepeatedWebhookIsDeduplicatedAndAcknowledgementIsPreserved(t *testing.T) {
	s := NewService()
	payload := WebhookPayload{Status: "firing"}
	payload.Alerts = append(payload.Alerts, struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		Fingerprint string            `json:"fingerprint"`
	}{Status: "firing", Labels: map[string]string{"alertname": "HighLoad", "instance": "host-02"}, StartsAt: time.Now(), Fingerprint: "fp-repeat"})
	_, _ = s.ReceiveWebhook(payload)
	_, _ = s.Acknowledge("fp-repeat", "admin")
	_, _ = s.ReceiveWebhook(payload)
	events, _ := s.Events("fp-repeat")
	if len(events) != 3 || events[0].Type != "repeated" {
		t.Fatalf("unexpected events: %+v", events)
	}
	for _, alert := range s.Alerts() {
		if alert.ID == "fp-repeat" && (alert.Status != "acknowledged" || alert.Owner != "admin") {
			t.Fatalf("acknowledgement was overwritten: %+v", alert)
		}
	}
}
func TestMissingAlert(t *testing.T) {
	if _, err := NewService().Acknowledge("missing", "admin"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAssignAndSilenceAlertAreAudited(t *testing.T) {
	s := NewService()
	alert, err := s.Assign("alert-001", "oncall-dba", "admin")
	if err != nil || alert.Owner != "oncall-dba" {
		t.Fatalf("assign failed: %+v %v", alert, err)
	}
	alert, err = s.Silence("alert-001", "admin")
	if err != nil || alert.Status != "silenced" {
		t.Fatalf("silence failed: %+v %v", alert, err)
	}
	events, _ := s.Events("alert-001")
	if len(events) != 2 || events[0].Type != "silenced" || events[1].Type != "assigned" {
		t.Fatalf("unexpected governance timeline: %+v", events)
	}
}
