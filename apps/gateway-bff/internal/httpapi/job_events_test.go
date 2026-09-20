package httpapi

import (
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

func TestHandleScheduledJobFailureCreatesAlertAndTicket(t *testing.T) {
	monitorService := monitor.NewService()
	ticketsService := tickets.NewService()
	handleJobTerminal(jobs.Job{ID: "job-failed-test", Name: "巡检失败验证", Status: "failed", Operator: "scheduler", Targets: []string{"10.0.0.1"}, StartedAt: "2026-09-20 10:00:00", Logs: []jobs.Log{{Level: "error", Message: "host unreachable"}}}, monitorService, ticketsService, cmdb.NewService())
	alertFound := false
	for _, item := range monitorService.Alerts() {
		if item.ID == "job-failed-job-failed-test" {
			alertFound = true
			break
		}
	}
	if !alertFound {
		t.Fatal("job failure alert was not created")
	}
	items, err := ticketsService.List()
	if err != nil {
		t.Fatal(err)
	}
	ticketFound := false
	for _, item := range items {
		if item.RelatedAlert == "job-failed-job-failed-test" {
			ticketFound = true
			break
		}
	}
	if !ticketFound {
		t.Fatal("job failure ticket was not created")
	}
}
