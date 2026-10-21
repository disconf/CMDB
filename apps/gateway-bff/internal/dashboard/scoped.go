package dashboard

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/discovery"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/releases"
	"cmdb/gateway-bff/internal/tickets"
)

// BuildScopedOverview builds the command center exclusively from the source
// records visible to the current user. It never supplements missing source
// data with demo values or platform-wide totals.
func BuildScopedOverview(
	now time.Time,
	assets []cmdb.Asset,
	agents []discovery.Agent,
	coverage monitor.Coverage,
	alerts []monitor.Alert,
	jobItems []jobs.Job,
	scheduleItems []jobs.Schedule,
	ticketItems []tickets.Ticket,
	releaseItems []releases.Release,
) Overview {
	assetSnapshot := buildAssetSnapshot(assets)
	agentSnapshot := buildAgentSnapshotFromItems(agents)
	monitorSnapshot := buildMonitorSnapshotFromCoverage(coverage)
	applyAlertSummary(&monitorSnapshot, alerts)
	taskSnapshot := buildTaskSnapshotFromItems(jobItems, len(scheduleItems), now)
	ticketSnapshot := buildTicketSnapshotFromItems(ticketItems, now)
	releaseSnapshot := buildReleaseSnapshotFromItems(releaseItems, now)
	command := buildCommandCenter(assetSnapshot, agentSnapshot, monitorSnapshot, taskSnapshot, ticketSnapshot, releaseSnapshot)
	alertList := buildAlertList(alerts, 10)
	jobList := buildActiveJobs(jobItems, 8)
	deployments := releaseSnapshot.Recent

	return Overview{
		UpdatedAt: now,
		Metrics: Metrics{
			AssetTotal:   assetSnapshot.Total,
			OnlineAgents: agentSnapshot.Online,
			ActiveAlerts: countActiveAlerts(alerts),
			TodayJobs:    countJobsToday(jobItems, now),
		},
		Command:     command,
		Assets:      assetSnapshot,
		Agents:      agentSnapshot,
		Monitoring:  monitorSnapshot,
		Tasks:       taskSnapshot,
		Tickets:     ticketSnapshot,
		Releases:    releaseSnapshot,
		Sources:     buildDataSources(now, assetSnapshot, agentSnapshot, monitorSnapshot, taskSnapshot, ticketSnapshot, releaseSnapshot),
		Capacity:    buildCapacity(assets),
		Health:      buildHealth(assets),
		Topology:    buildTopology(assets),
		AlertTrend:  buildAlertTrend(alerts, now),
		Alerts:      alertList,
		Jobs:        jobList,
		Timeline:    []TimelineEvent{},
		Deployments: deployments,
	}
}

func buildAgentSnapshotFromItems(items []discovery.Agent) AgentSnapshot {
	result := AgentSnapshot{Total: len(items)}
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Status)) {
		case "online":
			result.Online++
		case "pending":
			result.Pending++
		default:
			result.Offline++
		}
	}
	if result.Total > 0 {
		result.Coverage = percentage(result.Online, result.Total)
	}
	return result
}

func buildMonitorSnapshotFromCoverage(coverage monitor.Coverage) MonitorSnapshot {
	result := MonitorSnapshot{
		Targets:      coverage.Total,
		Healthy:      coverage.Healthy,
		Coverage:     coverage.Percentage,
		Acknowledged: 0,
	}
	if coverage.Total > 0 {
		result.Availability = percentage(coverage.Healthy, coverage.Total)
	}
	return result
}

func applyAlertSummary(result *MonitorSnapshot, alerts []monitor.Alert) {
	for _, alert := range alerts {
		if alert.Status == "acknowledged" {
			result.Acknowledged++
		}
		if alert.Status != "resolved" && strings.EqualFold(alert.Severity, "critical") {
			result.Critical++
		}
	}
}

func buildTaskSnapshotFromItems(items []jobs.Job, schedules int, now time.Time) TaskSnapshot {
	result := TaskSnapshot{Schedules: schedules}
	for _, item := range items {
		switch item.Status {
		case "running":
			result.Running++
		case "awaiting_approval", "waiting-approval", "pending-approval":
			result.PendingApproval++
		}
		startedAt, err := parseSourceTime(item.StartedAt)
		if err != nil || startedAt.In(now.Location()).Format("2006-01-02") != now.Format("2006-01-02") {
			continue
		}
		result.Today++
		switch item.Status {
		case "success":
			result.Success++
		case "failed":
			result.Failed++
		}
	}
	finished := result.Success + result.Failed
	if finished > 0 {
		result.SuccessRate = percentage(result.Success, finished)
	}
	return result
}

func buildTicketSnapshotFromItems(items []tickets.Ticket, now time.Time) TicketSnapshot {
	result := TicketSnapshot{Total: len(items), SLACompliance: 100}
	for _, item := range items {
		switch item.Status {
		case tickets.StatusPending:
			result.Pending++
		case tickets.StatusApproved, tickets.StatusCompleted:
			result.Processing++
		}
		if item.Status == tickets.StatusPending || item.Status == tickets.StatusApproved {
			due, err := time.Parse(time.RFC3339, item.SLADueAt)
			if err == nil && due.Before(now.Add(30*time.Minute)) {
				result.SLAWarning++
			}
		}
		if item.UpdatedAt == "" {
			continue
		}
		updatedAt, err := parseSourceTime(item.UpdatedAt)
		if err == nil && updatedAt.In(now.Location()).Format("2006-01-02") == now.Format("2006-01-02") &&
			item.Status != tickets.StatusPending && item.Status != tickets.StatusRejected && item.Status != tickets.StatusCancelled {
			result.ApprovedToday++
		}
	}
	if result.Total > 0 {
		result.SLACompliance = percentage(result.Total-result.SLAWarning, result.Total)
	}
	return result
}

func buildReleaseSnapshotFromItems(items []releases.Release, now time.Time) ReleaseSnapshot {
	result := ReleaseSnapshot{Recent: []Deployment{}}
	finished, success := 0, 0
	for _, item := range items {
		if startedAt, err := parseSourceTime(item.StartedAt); err == nil &&
			startedAt.In(now.Location()).Format("2006-01-02") == now.Format("2006-01-02") {
			result.Today++
		}
		switch item.Status {
		case releases.StatusRunning, releases.StatusRollingBack:
			result.Running++
		case releases.StatusSuccess:
			result.Success++
			success++
			finished++
		case releases.StatusFailed:
			result.Failed++
			finished++
		}
	}
	if finished > 0 {
		result.SuccessRate = percentage(success, finished)
	}
	sorted := append([]releases.Release(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, leftErr := parseSourceTime(sorted[i].StartedAt)
		right, rightErr := parseSourceTime(sorted[j].StartedAt)
		if leftErr != nil {
			return false
		}
		if rightErr != nil {
			return true
		}
		return left.After(right)
	})
	for _, item := range sorted {
		if len(result.Recent) >= 6 {
			break
		}
		result.Recent = append(result.Recent, Deployment{
			Name: item.Application, Version: item.Version, Status: item.Status, UpdatedAt: item.StartedAt,
		})
	}
	return result
}

func buildActiveJobs(items []jobs.Job, limit int) []Job {
	result := make([]Job, 0, limit)
	for _, item := range items {
		switch item.Status {
		case "running", "queued", "pending", "awaiting_approval", "waiting-approval":
		default:
			continue
		}
		kind := item.TemplateID
		if kind == "" {
			kind = "任务"
		}
		result = append(result, Job{
			ID: item.ID, Name: item.Name, Type: kind, Progress: item.Progress,
			Status: item.Status, Owner: item.Operator,
		})
		if len(result) >= limit {
			break
		}
	}
	return result
}

func countActiveAlerts(items []monitor.Alert) int {
	count := 0
	for _, item := range items {
		if item.Status != "resolved" {
			count++
		}
	}
	return count
}

func countJobsToday(items []jobs.Job, now time.Time) int {
	count := 0
	for _, item := range items {
		startedAt, err := parseSourceTime(item.StartedAt)
		if err == nil && startedAt.In(now.Location()).Format("2006-01-02") == now.Format("2006-01-02") {
			count++
		}
	}
	return count
}

func parseSourceTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty source timestamp")
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		time.RFC3339,
	} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid source timestamp %q", value)
}
