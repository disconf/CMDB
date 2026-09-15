package networkautomation

type OnboardingRequest struct {
	ID           string `json:"id"`
	AnomalyID    string `json:"anomalyId"`
	Name         string `json:"name"`
	IP           string `json:"ip"`
	MACAddress   string `json:"macAddress"`
	VLAN         string `json:"vlan"`
	AssetType    string `json:"assetType"`
	Environment  string `json:"environment"`
	ProjectGroup string `json:"projectGroup"`
	Owner        string `json:"owner"`
	Location     string `json:"location"`
	Status       string `json:"status"`
	Note         string `json:"note"`
	TicketID     string `json:"ticketId,omitempty"`
	AssetID      string `json:"assetId,omitempty"`
	RuleID       string `json:"ruleId,omitempty"`
	CreatedBy    string `json:"createdBy"`
	ApprovedBy   string `json:"approvedBy,omitempty"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type OnboardingInput struct {
	AnomalyID    string `json:"anomalyId"`
	Name         string `json:"name"`
	AssetType    string `json:"assetType"`
	Environment  string `json:"environment"`
	ProjectGroup string `json:"projectGroup"`
	Owner        string `json:"owner"`
	Location     string `json:"location"`
	Note         string `json:"note"`
}
