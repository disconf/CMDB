package httpapi

import (
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

func TestHandleScheduledJobFailureCreatesAlertAndTicketAndRecoveryClosesThem(t *testing.T) {
	monitorService := monitor.NewService()
	ticketsService := tickets.NewService()
	cmdbService := cmdb.NewService()
	job := jobs.Job{ID: "job-failed-test", Name: "巡检失败验证", Operator: "scheduler", Targets: []string{"10.0.0.1"}, StartedAt: "2026-09-20 10:00:00"}
	job.Status = "failed"
	job.Logs = []jobs.Log{{Level: "error", Message: "host unreachable"}}
	handleJobTerminal(job, monitorService, ticketsService, cmdbService)
	alertFound := false
	for _, item := range monitorService.Alerts() {
		if item.ID == "job-failed-job-failed-test" && item.Status == "firing" {
			alertFound = true
		}
	}
	if !alertFound {
		t.Fatal("job failure alert was not created")
	}
	job.Status = "success"
	job.Logs = []jobs.Log{{Level: "success", Message: "recovered"}}
	handleJobTerminal(job, monitorService, ticketsService, cmdbService)
	alertResolved := false
	for _, item := range monitorService.Alerts() {
		if item.ID == "job-failed-job-failed-test" && item.Status == "resolved" {
			alertResolved = true
		}
	}
	if !alertResolved {
		t.Fatal("job failure alert was not resolved")
	}
	items, err := ticketsService.List()
	if err != nil {
		t.Fatal(err)
	}
	ticketFound := false
	for _, item := range items {
		if item.RelatedAlert == "job-failed-job-failed-test" && item.Status == tickets.StatusCompleted {
			ticketFound = true
		}
	}
	if !ticketFound {
		t.Fatal("job failure ticket was not completed")
	}
}
