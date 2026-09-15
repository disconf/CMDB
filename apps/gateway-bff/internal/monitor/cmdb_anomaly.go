package monitor

import (
	"strings"
	"time"
)

type CMDBAnomalyAlert struct {
	ID       string
	Title    string
	Severity string
	TargetID string
	Target   string
	Message  string
	Owner    string
}

func (s *Service) SyncCMDBAnomalies(items []CMDBAnomalyAlert) (int, error) {
	payload := WebhookPayload{Status: "firing", GroupLabels: map[string]string{"source": "cmdb-network-anomaly"}}
	current := map[string]bool{}
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		current[item.ID] = true
		status := "firing"
		targetID := firstMonitorValue(item.TargetID, item.Target)
		payload.Alerts = append(payload.Alerts, struct {
			Status      string            `json:"status"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
			StartsAt    time.Time         `json:"startsAt"`
			EndsAt      time.Time         `json:"endsAt"`
			Fingerprint string            `json:"fingerprint"`
		}{
			Status: status,
			Labels: map[string]string{
				"alertname":     "cmdb_network_anomaly",
				"severity":      firstMonitorValue(item.Severity, "warning"),
				"cmdb_asset_id": item.TargetID,
				"instance":      firstMonitorValue(item.Target, item.TargetID),
				"owner":         item.Owner,
				"source":        "cmdb-network-anomaly",
			},
			Annotations: map[string]string{"summary": item.Title, "description": item.Message},
			StartsAt:    time.Now(),
			Fingerprint: item.ID,
		})
		_ = targetID
	}
	for _, alert := range s.Alerts() {
		if alert.Rule != "cmdb_network_anomaly" || alert.Status == "resolved" || current[alert.ID] {
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
			Labels:      map[string]string{"alertname": "cmdb_network_anomaly", "severity": alert.Severity, "instance": alert.Target},
			Annotations: map[string]string{"summary": alert.Title, "description": "CMDB 网络异常已消除"},
			EndsAt:      time.Now(),
			Fingerprint: alert.ID,
		})
	}
	if len(payload.Alerts) == 0 {
		return 0, nil
	}
	return s.ReceiveWebhook(payload)
}

func firstMonitorValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
