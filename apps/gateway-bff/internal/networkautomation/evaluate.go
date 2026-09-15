package networkautomation

import (
	"context"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

func (s *Service) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.Evaluate("network-automation")
		}
	}
}

func (s *Service) Evaluate(operator string) (Report, error) {
	if operator = strings.TrimSpace(operator); operator == "" {
		operator = "network-automation"
	}
	anomalies := s.cmdb.NetworkAnomalies()
	policies := s.Policies()
	report := Report{Evaluated: len(anomalies), Policies: len(policies)}
	active := map[string]bool{}
	firing := []monitor.CMDBAnomalyAlert{}
	for _, anomaly := range anomalies {
		if anomaly.Status == "resolved" {
			continue
		}
		active[anomaly.ID] = true
		policy, ok := matchPolicy(policies, anomaly)
		if !ok {
			continue
		}
		life, err := s.ensureLifecycle(anomaly, policy)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
			continue
		}
		if policy.AutoTicket && life.TicketID == "" {
			ticket, _, ticketErr := s.tickets.SyncAlert(tickets.AlertInput{ID: anomaly.ID, Title: anomaly.Title, Severity: anomaly.Severity, Status: "firing", Target: anomalyTarget(anomaly), Message: anomaly.Detail, Owner: policy.Assignee})
			if ticketErr != nil {
				report.Errors = append(report.Errors, ticketErr.Error())
			} else {
				life.TicketID = ticket.ID
				if policy.Assignee != "" {
					_, _ = s.tickets.Assign(ticket.ID, policy.Assignee, operator)
				}
				report.AutoTickets++
			}
		}
		if policy.EscalationMinutes > 0 && life.EscalatedAt == "" && policy.EscalateTo != "" && anomalyAge(life.FirstSeenAt) >= time.Duration(policy.EscalationMinutes)*time.Minute {
			if life.TicketID != "" {
				_, _ = s.tickets.Assign(life.TicketID, policy.EscalateTo, operator)
				_, _ = s.tickets.Comment(life.TicketID, operator, "网络异常持续超过 "+time.Duration(policy.EscalationMinutes).String()+"，已升级给 "+policy.EscalateTo)
			}
			life.EscalatedAt = time.Now().Format("2006-01-02 15:04:05")
			report.Escalated++
		}
		life.LastSeenAt = time.Now().Format("2006-01-02 15:04:05")
		if err := s.saveLifecycle(life); err != nil {
			report.Errors = append(report.Errors, err.Error())
		}
		firing = append(firing, monitor.CMDBAnomalyAlert{ID: anomaly.ID, Title: anomaly.Title, Severity: anomaly.Severity, TargetID: anomaly.AssetID, Target: anomalyTarget(anomaly), Message: anomaly.Detail, Owner: policy.Assignee})
	}
	for _, life := range s.snapshotLifecycle() {
		if life.Status == "resolved" || active[life.AnomalyID] || life.LastSeenAt == "" {
			continue
		}
		policy, err := s.policyByID(life.PolicyID)
		if err != nil {
			policy = Policy{}
		}
		if life.TicketID != "" {
			_, _, _ = s.tickets.SyncAlert(tickets.AlertInput{ID: life.AnomalyID, Title: life.Title, Severity: life.Severity, Status: "resolved", Target: life.Target, Message: "网络异常已恢复", Owner: operator})
		}
		if policy.VerificationTemplate != "" && life.VerificationJobID == "" && life.Target != "" {
			job, jobErr := s.jobs.Create(jobs.CreateInput{TemplateID: policy.VerificationTemplate, Targets: []string{life.Target}, Operator: operator, TimeoutSeconds: 300})
			if jobErr == nil {
				if job.Status == "pending" {
					_, _ = s.jobs.Run(job.ID)
				}
				life.VerificationJobID = job.ID
				report.VerificationJobs++
			} else {
				report.Errors = append(report.Errors, jobErr.Error())
			}
		}
		life.Status = "resolved"
		life.ResolvedAt = time.Now().Format("2006-01-02 15:04:05")
		life.LastSeenAt = life.ResolvedAt
		if err := s.saveLifecycle(life); err != nil {
			report.Errors = append(report.Errors, err.Error())
		}
		report.Resolved++
	}
	count, err := s.monitor.SyncCMDBAnomalies(firing)
	report.AlertsSynced = count
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
	}
	return report, nil
}

func matchPolicy(policies []Policy, anomaly cmdb.NetworkAnomaly) (Policy, bool) {
	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}
		if policy.Severity != "" && policy.Severity != "*" && policy.Severity != anomaly.Severity {
			continue
		}
		if policy.AnomalyType != "" && policy.AnomalyType != "*" && policy.AnomalyType != anomaly.Type {
			continue
		}
		if policy.Environment != "" && policy.Environment != anomaly.Environment {
			continue
		}
		if policy.ProjectGroup != "" && policy.ProjectGroup != anomaly.ProjectGroup {
			continue
		}
		if policy.Location != "" && !strings.Contains(anomaly.Location, policy.Location) {
			continue
		}
		return policy, true
	}
	return Policy{}, false
}

func anomalyTarget(anomaly cmdb.NetworkAnomaly) string {
	return firstValue(anomaly.IP, anomaly.AssetName, anomaly.DeviceName, anomaly.AssetID)
}

func anomalyAge(firstSeen string) time.Duration {
	value, err := time.ParseInLocation("2006-01-02 15:04:05", firstSeen, time.Local)
	if err != nil {
		return 0
	}
	return time.Since(value)
}
