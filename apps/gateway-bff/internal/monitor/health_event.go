package monitor

import (
	"strings"
	"time"
)

type HealthEventInput struct {
	Source       string `json:"source"`
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	ResourceName string `json:"resourceName"`
	Status       string `json:"status"`
	Severity     string `json:"severity"`
	Summary      string `json:"summary"`
	Description  string `json:"description"`
	AssetID      string `json:"assetId"`
	Owner        string `json:"owner"`
	ProjectGroup string `json:"projectGroup"`
}

func BuildHealthEventPayload(input HealthEventInput) (string, WebhookPayload) {
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "cicd"
	}
	resourceType := strings.TrimSpace(input.ResourceType)
	resourceID := strings.TrimSpace(input.ResourceID)
	resourceName := strings.TrimSpace(input.ResourceName)
	status := strings.ToLower(strings.TrimSpace(input.Status))
	resolved := status == "resolved" || status == "success" || status == "healthy" || status == "recovered"
	if resolved {
		status = "resolved"
	} else {
		status = "firing"
	}
	severity := strings.ToLower(strings.TrimSpace(input.Severity))
	if severity == "" {
		severity = "warning"
	}
	summary := strings.TrimSpace(input.Summary)
	if summary == "" {
		summary = resourceName + " 健康状态异常"
	}
	description := strings.TrimSpace(input.Description)
	if description == "" {
		description = source + " 上报 " + resourceType + "/" + resourceID + " 状态为 " + status
	}
	fingerprint := "cicd-health-" + slug(source) + "-" + slug(resourceType) + "-" + slug(resourceID)
	now := time.Now()
	labels := map[string]string{
		"alertname":     "cicd-resource-health",
		"source":        source,
		"resource_type": resourceType,
		"resource_id":   resourceID,
		"resource_name": resourceName,
		"severity":      severity,
		"owner":         strings.TrimSpace(input.Owner),
		"project_group": strings.TrimSpace(input.ProjectGroup),
		"instance":      firstNonEmpty(input.AssetID, resourceID, resourceName),
	}
	if assetID := strings.TrimSpace(input.AssetID); assetID != "" {
		labels["cmdb_asset_id"] = assetID
	}
	payload := WebhookPayload{Receiver: "cmdb-cicd", Status: status, GroupKey: fingerprint}
	payload.Alerts = append(payload.Alerts, struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		EndsAt      time.Time         `json:"endsAt"`
		Fingerprint string            `json:"fingerprint"`
	}{Status: status, Labels: labels, Annotations: map[string]string{"summary": summary, "description": description}, StartsAt: now, EndsAt: now, Fingerprint: fingerprint})
	return fingerprint, payload
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("/", "-", "_", "-", " ", "-", ".", "-", ":", "-").Replace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
