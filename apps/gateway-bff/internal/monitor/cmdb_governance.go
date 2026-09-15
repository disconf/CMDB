package monitor

import (
	"strings"
	"time"
)

type CMDBGovernanceAlert struct {
	ID       string
	Title    string
	Severity string
	TargetID string
	Target   string
	Message  string
	Owner    string
}

func (s *Service) SyncCMDBGovernanceAlerts(items []CMDBGovernanceAlert) (int, error) {
	payload := WebhookPayload{Status: "firing", GroupLabels: map[string]string{"source": "cmdb-asset-governance"}}
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
			Status: "firing",
			Labels: map[string]string{
				"alertname":     "cmdb_asset_governance",
				"severity":      firstMonitorValue(item.Severity, "warning"),
				"cmdb_asset_id": item.TargetID,
				"instance":      firstMonitorValue(item.Target, item.TargetID),
				"owner":         item.Owner,
				"source":        "cmdb-asset-governance",
			},
			Annotations: map[string]string{"summary": item.Title, "description": item.Message},
			StartsAt:    time.Now(),
			Fingerprint: item.ID,
		})
	}
	for _, alert := range s.Alerts() {
		if alert.Rule != "cmdb_asset_governance" || alert.Status == "resolved" || current[alert.ID] {
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
			Labels:      map[string]string{"alertname": "cmdb_asset_governance", "severity": alert.Severity, "instance": alert.Target},
			Annotations: map[string]string{"summary": alert.Title, "description": "资产责任治理问题已确认或已退役"},
			EndsAt:      time.Now(),
			Fingerprint: alert.ID,
		})
	}
	if len(payload.Alerts) == 0 {
		return 0, nil
	}
	return s.ReceiveWebhook(payload)
}
