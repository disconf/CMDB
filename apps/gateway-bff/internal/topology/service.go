package topology

import "errors"

var ErrNotFound = errors.New("topology node not found")

type Node struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Layer       int    `json:"layer"`
	Status      string `json:"status"`
	Environment string `json:"environment"`
	Owner       string `json:"owner"`
	IP          string `json:"ip,omitempty"`
}
type Edge struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	Relation string `json:"relation"`
	Status   string `json:"status"`
}
type Graph struct {
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
	UpdatedAt string `json:"updatedAt"`
}
type Impact struct {
	Root     Node       `json:"root"`
	Affected []Node     `json:"affected"`
	Paths    [][]string `json:"paths"`
}
type Service struct{ graph Graph }

func NewService() *Service {
	nodes := []Node{{"project-commerce", "电商业务群", "project", 0, "healthy", "production", "陈明", ""}, {"app-order", "订单中心", "application", 1, "healthy", "production", "王强", ""}, {"app-payment", "支付网关", "application", 1, "warning", "production", "周敏", ""}, {"service-order", "订单服务", "service", 2, "warning", "production", "王强", ""}, {"service-payment", "支付服务", "service", 2, "critical", "production", "周敏", ""}, {"vm-prod-041", "order-service-vm", "instance", 3, "healthy", "production", "王强", "10.21.4.41"}, {"vm-prod-052", "payment-gateway-vm", "instance", 3, "warning", "production", "周敏", "10.21.5.52"}, {"db-prod-001", "order-mysql-primary", "infrastructure", 4, "warning", "production", "吴涛", "10.23.2.31"}, {"redis-prod-01", "session-redis", "infrastructure", 4, "healthy", "production", "吴涛", "10.23.3.41"}, {"lb-prod-01", "public-api-lb", "infrastructure", 4, "healthy", "production", "孙磊", "10.20.0.10"}}
	pairs := [][3]string{{"project-commerce", "app-order", "包含"}, {"project-commerce", "app-payment", "包含"}, {"app-order", "service-order", "提供"}, {"app-payment", "service-payment", "提供"}, {"service-order", "vm-prod-041", "部署于"}, {"service-payment", "vm-prod-052", "部署于"}, {"service-order", "db-prod-001", "依赖"}, {"service-order", "redis-prod-01", "依赖"}, {"service-payment", "lb-prod-01", "接入"}}
	edges := make([]Edge, 0, len(pairs))
	for i, p := range pairs {
		edges = append(edges, Edge{ID: string(rune('a' + i)), Source: p[0], Target: p[1], Relation: p[2], Status: "healthy"})
	}
	return &Service{Graph{nodes, edges, "2026-07-15 14:40:00"}}
}
func (s *Service) Graph(environment string) Graph {
	if environment == "" {
		return s.graph
	}
	g := Graph{UpdatedAt: s.graph.UpdatedAt}
	kept := map[string]bool{}
	for _, n := range s.graph.Nodes {
		if n.Environment == environment {
			g.Nodes = append(g.Nodes, n)
			kept[n.ID] = true
		}
	}
	for _, e := range s.graph.Edges {
		if kept[e.Source] && kept[e.Target] {
			g.Edges = append(g.Edges, e)
		}
	}
	return g
}
func (s *Service) Impact(id string) (Impact, error) {
	byID := map[string]Node{}
	for _, n := range s.graph.Nodes {
		byID[n.ID] = n
	}
	root, ok := byID[id]
	if !ok {
		return Impact{}, ErrNotFound
	}
	seen := map[string]bool{id: true}
	queue := []string{id}
	result := Impact{Root: root}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, e := range s.graph.Edges {
			var next string
			if e.Source == current {
				next = e.Target
			} else if e.Target == current {
				next = e.Source
			}
			if next != "" && !seen[next] {
				seen[next] = true
				queue = append(queue, next)
				result.Affected = append(result.Affected, byID[next])
				result.Paths = append(result.Paths, []string{current, next})
			}
		}
	}
	return result, nil
}
