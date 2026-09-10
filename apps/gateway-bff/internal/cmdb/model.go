package cmdb

type Model struct {
	Code        string       `json:"code"`
	Name        string       `json:"name"`
	Category    string       `json:"category"`
	Icon        string       `json:"icon"`
	Count       int          `json:"count"`
	Description string       `json:"description"`
	Enabled     bool         `json:"enabled"`
	Fields      []ModelField `json:"fields"`
}
type ModelField struct {
	Name         string   `json:"name"`
	Label        string   `json:"label"`
	Type         string   `json:"type"`
	Required     bool     `json:"required"`
	DefaultValue string   `json:"defaultValue"`
	Options      []string `json:"options"`
}
type ModelInput struct {
	Code        string       `json:"code"`
	Name        string       `json:"name"`
	Category    string       `json:"category"`
	Icon        string       `json:"icon"`
	Description string       `json:"description"`
	Fields      []ModelField `json:"fields"`
}
type AgentAssetInput struct {
	ID             string
	Type           string
	Hostname       string
	IP             string
	OS             string
	Kernel         string
	Architecture   string
	CPUCount       int
	MemoryBytes    uint64
	DiskBytes      uint64
	BootTime       string
	AgentVersion   string
	Virtualization string
}
type Attribute struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Value string `json:"value"`
}
type Relation struct {
	Type       string `json:"type"`
	TargetID   string `json:"targetId"`
	TargetName string `json:"targetName"`
}
type Asset struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Type         string      `json:"type"`
	TypeName     string      `json:"typeName"`
	Status       string      `json:"status"`
	IP           string      `json:"ip"`
	Environment  string      `json:"environment"`
	ProjectGroup string      `json:"projectGroup"`
	Owner        string      `json:"owner"`
	Location     string      `json:"location"`
	Source       string      `json:"source"`
	LastSeenAt   string      `json:"lastSeenAt"`
	Tags         []string    `json:"tags"`
	Attributes   []Attribute `json:"attributes"`
	Relations    []Relation  `json:"relations"`
}
type AssetQuery struct {
	Search       string
	Type         string
	Status       string
	ProjectGroup string
	Page         int
	PageSize     int
}
type PageMeta struct {
	Total      int `json:"total"`
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	TotalPages int `json:"totalPages"`
}
type AssetPage struct {
	Data []Asset  `json:"data"`
	Meta PageMeta `json:"meta"`
}
type PrometheusTargetGroup struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}
type Summary struct {
	Total         int `json:"total"`
	Online        int `json:"online"`
	Warning       int `json:"warning"`
	Offline       int `json:"offline"`
	Models        int `json:"models"`
	ProjectGroups int `json:"projectGroups"`
}
type CreateAssetInput struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Type         string      `json:"type"`
	Status       string      `json:"status"`
	IP           string      `json:"ip"`
	Environment  string      `json:"environment"`
	ProjectGroup string      `json:"projectGroup"`
	Owner        string      `json:"owner"`
	Location     string      `json:"location"`
	Source       string      `json:"source"`
	Tags         []string    `json:"tags"`
	Attributes   []Attribute `json:"attributes"`
	Relations    []Relation  `json:"relations"`
}
type UpdateAssetInput struct {
	Name         string      `json:"name"`
	Status       string      `json:"status"`
	IP           string      `json:"ip"`
	Environment  string      `json:"environment"`
	ProjectGroup string      `json:"projectGroup"`
	Owner        string      `json:"owner"`
	Location     string      `json:"location"`
	Tags         []string    `json:"tags"`
	Attributes   []Attribute `json:"attributes"`
	Relations    []Relation  `json:"relations"`
}
type Change struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}
type HistoryEntry struct {
	ID         string   `json:"id"`
	AssetID    string   `json:"assetId"`
	Action     string   `json:"action"`
	Operator   string   `json:"operator"`
	OccurredAt string   `json:"occurredAt"`
	Changes    []Change `json:"changes"`
}
type ImportError struct {
	Row     int    `json:"row"`
	ID      string `json:"id"`
	Message string `json:"message"`
}
type ImportResult struct {
	Total   int           `json:"total"`
	Created int           `json:"created"`
	Updated int           `json:"updated"`
	Errors  []ImportError `json:"errors"`
}

type K8sNamespaceInventory struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Status    string         `json:"status"`
	Counts    map[string]int `json:"counts"`
	Resources []Asset        `json:"resources"`
}
type K8sClusterInventory struct {
	ID               string                  `json:"id"`
	Name             string                  `json:"name"`
	Status           string                  `json:"status"`
	Version          string                  `json:"version,omitempty"`
	NamespaceCount   int                     `json:"namespaceCount"`
	Counts           map[string]int          `json:"counts"`
	Namespaces       []K8sNamespaceInventory `json:"namespaces"`
	ClusterResources []Asset                 `json:"clusterResources"`
}
type K8sInventory struct {
	Clusters []K8sClusterInventory `json:"clusters"`
}

type Analytics struct {
	Status       Summary        `json:"status"`
	ByType       []TypeCount    `json:"byType"`
	ByGroup      []GroupCount   `json:"byGroup"`
	BySource     []SourceCount  `json:"bySource"`
	Completeness []Completeness `json:"completeness"`
}
type TypeCount struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}
type GroupCount struct {
	Group string `json:"group"`
	Count int    `json:"count"`
}
type SourceCount struct {
	Source string `json:"source"`
	Count  int    `json:"count"`
}
type Completeness struct {
	Field   string  `json:"field"`
	Label   string  `json:"label"`
	Missing int     `json:"missing"`
	Total   int     `json:"total"`
	Rate    float64 `json:"rate"`
}
