package networkautomation

type Policy struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Enabled              bool   `json:"enabled"`
	Priority             int    `json:"priority"`
	Severity             string `json:"severity"`
	AnomalyType          string `json:"anomalyType"`
	Environment          string `json:"environment"`
	ProjectGroup         string `json:"projectGroup"`
	Location             string `json:"location"`
	AutoTicket           bool   `json:"autoTicket"`
	Assignee             string `json:"assignee"`
	EscalationMinutes    int    `json:"escalationMinutes"`
	EscalateTo           string `json:"escalateTo"`
	VerificationTemplate string `json:"verificationTemplate"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
}

type PolicyInput struct {
	Name                 string `json:"name"`
	Enabled              *bool  `json:"enabled"`
	Priority             int    `json:"priority"`
	Severity             string `json:"severity"`
	AnomalyType          string `json:"anomalyType"`
	Environment          string `json:"environment"`
	ProjectGroup         string `json:"projectGroup"`
	Location             string `json:"location"`
	AutoTicket           bool   `json:"autoTicket"`
	Assignee             string `json:"assignee"`
	EscalationMinutes    int    `json:"escalationMinutes"`
	EscalateTo           string `json:"escalateTo"`
	VerificationTemplate string `json:"verificationTemplate"`
}

type Lifecycle struct {
	AnomalyID         string `json:"anomalyId"`
	PolicyID          string `json:"policyId"`
	Status            string `json:"status"`
	Title             string `json:"title,omitempty"`
	Severity          string `json:"severity,omitempty"`
	Target            string `json:"target,omitempty"`
	FirstSeenAt       string `json:"firstSeenAt"`
	LastSeenAt        string `json:"lastSeenAt"`
	ResolvedAt        string `json:"resolvedAt,omitempty"`
	TicketID          string `json:"ticketId,omitempty"`
	EscalatedAt       string `json:"escalatedAt,omitempty"`
	VerificationJobID string `json:"verificationJobId,omitempty"`
}

type Report struct {
	Evaluated        int      `json:"evaluated"`
	Policies         int      `json:"policies"`
	AutoTickets      int      `json:"autoTickets"`
	Escalated        int      `json:"escalated"`
	Resolved         int      `json:"resolved"`
	VerificationJobs int      `json:"verificationJobs"`
	AlertsSynced     int      `json:"alertsSynced"`
	Snapshots        int      `json:"snapshots"`
	Errors           []string `json:"errors,omitempty"`
}
