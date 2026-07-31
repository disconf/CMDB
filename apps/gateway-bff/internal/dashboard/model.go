package dashboard

import "time"

type Overview struct {
	UpdatedAt   time.Time       `json:"updatedAt"`
	Metrics     Metrics         `json:"metrics"`
	Capacity    []CapacityItem  `json:"capacity"`
	Health      []HealthItem    `json:"health"`
	Topology    Topology        `json:"topology"`
	AlertTrend  []TrendPoint    `json:"alertTrend"`
	Alerts      []Alert         `json:"alerts"`
	Jobs        []Job           `json:"jobs"`
	Timeline    []TimelineEvent `json:"timeline"`
	Deployments []Deployment    `json:"deployments"`
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
