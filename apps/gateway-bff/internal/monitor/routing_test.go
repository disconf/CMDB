package monitor

import (
	"testing"
	"time"
)

func TestRoutingRuleAutomaticallyAssignsWebhookAlert(t *testing.T) {
	s := NewService()
	s.channels = append(s.channels, NotificationChannel{ID: "dba-webhook", Name: "DBA"})
	rule, err := s.CreateRoutingRule(CreateRoutingRuleInput{Name: "critical database", MatcherName: "service", MatcherValue: "postgresql", Team: "DBA", Owner: "dba-oncall", ChannelID: "dba-webhook"})
	if err != nil {
		t.Fatal(err)
	}
	if rule.ChannelID != "dba-webhook" {
		t.Fatalf("route channel was not stored: %+v", rule)
	}
	payload := WebhookPayload{Status: "firing"}
	payload.Alerts = append(payload.Alerts, struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		Fingerprint string            `json:"fingerprint"`
	}{Status: "firing", Labels: map[string]string{"alertname": "PostgresDown", "service": "postgresql", "instance": "db-01"}, StartsAt: time.Now(), Fingerprint: "routed-alert"})
	if _, err := s.ReceiveWebhook(payload); err != nil {
		t.Fatal(err)
	}
	for _, alert := range s.Alerts() {
		if alert.ID == "routed-alert" && alert.Owner != "dba-oncall" {
			t.Fatalf("unexpected owner: %+v", alert)
		}
	}
	if toggled, err := s.ToggleRoutingRule(rule.ID); err != nil || toggled.Enabled {
		t.Fatalf("toggle failed: %+v %v", toggled, err)
	}
	if err := s.DeleteRoutingRule(rule.ID); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitOwnerLabelOverridesRoutingRule(t *testing.T) {
	s := NewService()
	_, _ = s.CreateRoutingRule(CreateRoutingRuleInput{Name: "critical", MatcherName: "severity", MatcherValue: "critical", Team: "SRE", Owner: "sre-oncall"})
	payload := WebhookPayload{Status: "firing"}
	payload.Alerts = append(payload.Alerts, struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		Fingerprint string            `json:"fingerprint"`
	}{Status: "firing", Labels: map[string]string{"alertname": "HostDown", "severity": "critical", "owner": "explicit-owner"}, StartsAt: time.Now(), Fingerprint: "explicit-alert"})
	_, _ = s.ReceiveWebhook(payload)
	for _, alert := range s.Alerts() {
		if alert.ID == "explicit-alert" && alert.Owner != "explicit-owner" {
			t.Fatalf("explicit owner overwritten: %+v", alert)
		}
	}
}
