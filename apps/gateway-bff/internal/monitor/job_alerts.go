package monitor

import "time"

type JobFailureInput struct {
	JobID        string
	JobName      string
	Target       string
	TargetName   string
	Message      string
	Severity     string
	Owner        string
	ProjectGroup string
	StartedAt    string
}

func (s *Service) ReceiveJobFailure(input JobFailureInput) (int, error) {
	if input.JobID == "" {
		input.JobID = "job-unknown-" + time.Now().Format("20060102150405")
	}
	if input.JobName == "" {
		input.JobName = "自动化任务"
	}
	if input.Severity == "" {
		input.Severity = "warning"
	}
	if input.Message == "" {
		input.Message = "自动化任务执行失败，请检查执行日志和主机状态"
	}
	labels := map[string]string{
		"alertname":     "job-failure",
		"instance":      input.Target,
		"cmdb_asset_id": input.Target,
		"severity":      input.Severity,
		"owner":         input.Owner,
		"project_group": input.ProjectGroup,
		"job_id":        input.JobID,
		"job_name":      input.JobName,
	}
	annotations := map[string]string{
		"summary":     input.JobName + " 执行失败",
		"description": input.Message,
	}
	payload := WebhookPayload{
		Receiver: "cmdb-jobs",
		Status:   "firing",
		GroupKey: "job:" + input.JobID,
		Alerts: []struct {
			Status      string            `json:"status"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
			StartsAt    time.Time         `json:"startsAt"`
			EndsAt      time.Time         `json:"endsAt"`
			Fingerprint string            `json:"fingerprint"`
		}{
			{
				Status:      "firing",
				Labels:      labels,
				Annotations: annotations,
				StartsAt:    time.Now(),
				Fingerprint: "job-failed-" + input.JobID,
			},
		},
	}
	return s.ReceiveWebhook(payload)
}
