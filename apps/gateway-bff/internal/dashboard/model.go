package dashboard

import "time"

type Overview struct {
	UpdatedAt   time.Time       `json:"updatedAt"`
	Metrics     Metrics         `json:"metrics"`
	Command     CommandCenter   `json:"command"`
	Assets      AssetSnapshot   `json:"assets"`
	Agents      AgentSnapshot   `json:"agents"`
	Monitoring  MonitorSnapshot `json:"monitoring"`
	Tasks       TaskSnapshot    `json:"tasks"`
	Tickets     TicketSnapshot  `json:"tickets"`
	Releases    ReleaseSnapshot `json:"releases"`
	Sources     []DataSource    `json:"sources"`
	Capacity    []CapacityItem  `json:"capacity"`
	Health      []HealthItem    `json:"health"`
	Topology    Topology        `json:"topology"`
	AlertTrend  []TrendPoint    `json:"alertTrend"`
	Alerts      []Alert         `json:"alerts"`
	Jobs        []Job           `json:"jobs"`
	Timeline    []TimelineEvent `json:"timeline"`
	Deployments []Deployment    `json:"deployments"`
}

type CommandCenter struct {
	PlatformStatus     string  `json:"platformStatus"`
	AssetHealth        float64 `json:"assetHealth"`
	AgentCoverage      float64 `json:"agentCoverage"`
	MonitorCoverage    float64 `json:"monitorCoverage"`
	TaskSuccessRate    float64 `json:"taskSuccessRate"`
	SLACompliance      float64 `json:"slaCompliance"`
	ReleaseSuccessRate float64 `json:"releaseSuccessRate"`
	ActiveIncidents    int     `json:"activeIncidents"`
	CriticalAlerts     int     `json:"criticalAlerts"`
}

type AssetSnapshot struct {
	Total         int `json:"total"`
	Online        int `json:"online"`
	Warning       int `json:"warning"`
	Offline       int `json:"offline"`
	Unknown       int `json:"unknown"`
	ProjectGroups int `json:"projectGroups"`
	Environments  int `json:"environments"`
	Sources       int `json:"sources"`
}

type AgentSnapshot struct {
	Total    int     `json:"total"`
	Online   int     `json:"online"`
	Offline  int     `json:"offline"`
	Pending  int     `json:"pending"`
	Coverage float64 `json:"coverage"`
}

type MonitorSnapshot struct {
	Targets      int     `json:"targets"`
	Healthy      int     `json:"healthy"`
	Coverage     float64 `json:"coverage"`
	Availability float64 `json:"availability"`
	Critical     int     `json:"critical"`
	Acknowledged int     `json:"acknowledged"`
}

type TaskSnapshot struct {
	Today           int     `json:"today"`
	Running         int     `json:"running"`
	Success         int     `json:"success"`
	Failed          int     `json:"failed"`
	PendingApproval int     `json:"pendingApproval"`
	Schedules       int     `json:"schedules"`
	SuccessRate     float64 `json:"successRate"`
}

type TicketSnapshot struct {
	Total         int     `json:"total"`
	Pending       int     `json:"pending"`
	Processing    int     `json:"processing"`
	ApprovedToday int     `json:"approvedToday"`
	SLAWarning    int     `json:"slaWarning"`
	SLACompliance float64 `json:"slaCompliance"`
}

type ReleaseSnapshot struct {
	Today       int          `json:"today"`
	Running     int          `json:"running"`
	Success     int          `json:"success"`
	Failed      int          `json:"failed"`
	SuccessRate float64      `json:"successRate"`
	Recent      []Deployment `json:"recent"`
}

type DataSource struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
	Count     int    `json:"count"`
	UpdatedAt string `json:"updatedAt"`
}

type Metrics struct {
	AssetTotal   int `json:"assetTotal"`
	OnlineAgents int `json:"onlineAgents"`
	ActiveAlerts int `json:"activeAlerts"`
	TodayJobs    int `json:"todayJobs"`
}

type CapacityItem struct {
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	Detail string  `json:"detail"`
}
type HealthItem struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}
type Topology struct {
	Layers []TopologyLayer `json:"layers"`
}
type TopologyLayer struct {
	Name  string         `json:"name"`
	Nodes []TopologyNode `json:"nodes"`
}
type TopologyNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}
type TrendPoint struct {
	Time     string `json:"time"`
	Total    int    `json:"total"`
	Critical int    `json:"critical"`
}
type Alert struct {
	ID         string `json:"id"`
	Level      string `json:"level"`
	Title      string `json:"title"`
	Target     string `json:"target"`
	OccurredAt string `json:"occurredAt"`
}
type Job struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Progress int    `json:"progress"`
	Status   string `json:"status"`
	Owner    string `json:"owner"`
}
type TimelineEvent struct {
	ID      string `json:"id"`
	Time    string `json:"time"`
	Type    string `json:"type"`
	Message string `json:"message"`
	Source  string `json:"source"`
}
type Deployment struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updatedAt"`
}
