package monitor

import (
	"strings"
	"time"
)

type CMDBComplianceAlert struct {
	ID       string
	Title    string
	Severity string
	Target   string
	Message  string
	Owner    string
}

func (s *Service) SyncCMDBComplianceAlerts(items []CMDBComplianceAlert) (int, error) {
	payload := WebhookPayload{Status: "firing", GroupLabels: map[string]string{"source": "cmdb-compliance-expiry"}}
	current := map[string]bool{}
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		current[item.ID] = true
		payload.Alerts = append(payload.Alerts, struct {
			Status      string            `json:"status"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
			StartsAt    time.Time         `json:"startsAt"`
			EndsAt      time.Time         `json:"endsAt"`
			Fingerprint string            `json:"fingerprint"`
		}{
			Status:      "firing",
			Labels:      map[string]string{"alertname": "cmdb_compliance_expiry", "severity": firstMonitorValue(item.Severity, "warning"), "instance": item.Target, "owner": item.Owner, "source": "cmdb-compliance"},
			Annotations: map[string]string{"summary": item.Title, "description": item.Message},
			StartsAt:    time.Now(),
			Fingerprint: item.ID,
		})
	}
	for _, alert := range s.Alerts() {
		if alert.Rule != "cmdb_compliance_expiry" || alert.Status == "resolved" || current[alert.ID] {
			continue
		}
		payload.Alerts = append(payload.Alerts, struct {
			Status      string            `json:"status"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
			StartsAt    time.Time         `json:"startsAt"`
			EndsAt      time.Time         `json:"endsAt"`
			Fingerprint string            `json:"fingerprint"`
		}{
			Status:      "resolved",
			Labels:      map[string]string{"alertname": "cmdb_compliance_expiry", "severity": alert.Severity, "instance": alert.Target},
			Annotations: map[string]string{"summary": alert.Title, "description": "合规临时授权已到期或已撤销"},
			EndsAt:      time.Now(),
			Fingerprint: alert.ID,
		})
	}
	if len(payload.Alerts) == 0 {
		return 0, nil
	}
	return s.ReceiveWebhook(payload)
}
