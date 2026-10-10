package aiops

import (
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/releases"
	"cmdb/gateway-bff/internal/tickets"
	"cmdb/gateway-bff/internal/topology"
)

type AlertProvider interface {
	Alerts() []monitor.Alert
	Events(string) ([]monitor.AlertEvent, error)
	Host(string) monitor.HostMonitoring
	ActiveCount() int
}

type AssetProvider interface {
	GetAsset(string) (cmdb.Asset, error)
	FindHostByIP(string, string) string
	ListAssets(cmdb.AssetQuery) cmdb.AssetPage
}

type ImpactProvider interface {
	Impact(string) (topology.Impact, error)
}

type TicketProvider interface {
	List() ([]tickets.Ticket, error)
	Create(tickets.CreateInput) (tickets.Ticket, error)
	ConsumeTaskAuthorization(string, string, string) (tickets.Ticket, error)
}

type JobProvider interface {
	Create(jobs.CreateInput) (jobs.Job, error)
	Run(string) (jobs.Job, error)
}

type ReleaseProvider interface {
	List() ([]releases.Release, error)
}
