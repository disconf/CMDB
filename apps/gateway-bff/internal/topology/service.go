package topology

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

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
type ImpactNode struct {
	Node
	Distance  int      `json:"distance"`
	Direction string   `json:"direction"`
	Path      []string `json:"path"`
}
type ImpactSummary struct {
	Total    int `json:"total"`
	Healthy  int `json:"healthy"`
	Warning  int `json:"warning"`
	Critical int `json:"critical"`
}
type Impact struct {
	Root     Node          `json:"root"`
	Affected []ImpactNode  `json:"affected"`
	Paths    [][]string    `json:"paths"`
	Summary  ImpactSummary `json:"summary"`
}
type cachedGraph struct {
	graph   Graph
	expires time.Time
}
type Service struct {
	graph   Graph
	cmdb    *cmdb.Service
	cacheMu sync.RWMutex
	cache   map[string]cachedGraph
}

const topologyCacheTTL = 15 * time.Second

func NewService() *Service {
	return &Service{graph: demoGraph()}
}

func NewServiceWithCMDB(cmdbService *cmdb.Service) *Service {
	return &Service{graph: demoGraph(), cmdb: cmdbService, cache: map[string]cachedGraph{}}
}

func demoGraph() Graph {
	nodes := []Node{{"project-commerce", "电商业务群", "project", 0, "healthy", "production", "陈明", ""}, {"app-order", "订单中心", "application", 1, "healthy", "production", "王强", ""}, {"app-payment", "支付网关", "application", 1, "warning", "production", "周敏", ""}, {"service-order", "订单服务", "service", 2, "warning", "production", "王强", ""}, {"service-payment", "支付服务", "service", 2, "critical", "production", "周敏", ""}, {"vm-prod-041", "order-service-vm", "instance", 3, "healthy", "production", "王强", "10.21.4.41"}, {"vm-prod-052", "payment-gateway-vm", "instance", 3, "warning", "production", "周敏", "10.21.5.52"}, {"db-prod-001", "order-mysql-primary", "infrastructure", 4, "warning", "production", "吴涛", "10.23.2.31"}, {"redis-prod-01", "session-redis", "infrastructure", 4, "healthy", "production", "吴涛", "10.23.3.41"}, {"lb-prod-01", "public-api-lb", "infrastructure", 4, "healthy", "production", "孙磊", "10.20.0.10"}}
	pairs := [][3]string{{"project-commerce", "app-order", "包含"}, {"project-commerce", "app-payment", "包含"}, {"app-order", "service-order", "提供"}, {"app-payment", "service-payment", "提供"}, {"service-order", "vm-prod-041", "部署于"}, {"service-payment", "vm-prod-052", "部署于"}, {"service-order", "db-prod-001", "依赖"}, {"service-order", "redis-prod-01", "依赖"}, {"service-payment", "lb-prod-01", "接入"}}
	edges := make([]Edge, 0, len(pairs))
	for i, p := range pairs {
		edges = append(edges, Edge{ID: fmt.Sprintf("demo-%d", i+1), Source: p[0], Target: p[1], Relation: p[2], Status: "healthy"})
	}
	return Graph{nodes, edges, "2026-07-15 14:40:00"}
}

func (s *Service) Graph(environment string) Graph {
	if s.cmdb == nil {
		return filterDemoGraph(s.graph, environment)
	}
	if graph, ok := s.cachedGraph(environment); ok {
		return graph
	}
	graph := s.cmdbGraph(environment)
	s.cacheMu.Lock()
	if s.cache == nil {
		s.cache = map[string]cachedGraph{}
	}
	s.cache[environment] = cachedGraph{graph: graph, expires: time.Now().Add(topologyCacheTTL)}
	s.cacheMu.Unlock()
	return graph
}

// Invalidate forces the next topology read to rebuild from CMDB.
// Asset event consumers call this after refreshing their in-process CMDB state.
func (s *Service) Invalidate() {
	s.cacheMu.Lock()
	s.cache = map[string]cachedGraph{}
	s.cacheMu.Unlock()
}

func (s *Service) cachedGraph(environment string) (Graph, bool) {
	s.cacheMu.RLock()
	cached, ok := s.cache[environment]
	s.cacheMu.RUnlock()
	if !ok || time.Now().After(cached.expires) {
		return Graph{}, false
	}
	return cached.graph, true
}

func filterDemoGraph(graph Graph, environment string) Graph {
	if environment == "" {
		return graph
	}
	g := Graph{UpdatedAt: graph.UpdatedAt}
	kept := map[string]bool{}
	for _, n := range graph.Nodes {
		if n.Environment == environment {
			g.Nodes = append(g.Nodes, n)
			kept[n.ID] = true
		}
	}
	for _, e := range graph.Edges {
		if kept[e.Source] && kept[e.Target] {
			g.Edges = append(g.Edges, e)
		}
	}
	return g
}

type graphBuilder struct {
	graph        Graph
	byID         map[string]Node
	byName       map[string]string
	assetsByID   map[string]cmdb.Asset
	assetsByName map[string]string
	edgeSet      map[string]bool
}

func (s *Service) cmdbGraph(environment string) Graph {
	assets := make([]cmdb.Asset, 0)
	for page := 1; ; page++ {
		result := s.cmdb.ListAssets(cmdb.AssetQuery{Page: page, PageSize: 100})
		assets = append(assets, result.Data...)
		if result.Meta.TotalPages == 0 || page >= result.Meta.TotalPages {
			break
		}
	}
	builder := &graphBuilder{graph: Graph{Nodes: make([]Node, 0, len(assets)), Edges: make([]Edge, 0), UpdatedAt: time.Now().Format("2006-01-02 15:04:05")}, byID: map[string]Node{}, byName: map[string]string{}, assetsByID: map[string]cmdb.Asset{}, assetsByName: map[string]string{}, edgeSet: map[string]bool{}}
	for _, asset := range assets {
		builder.assetsByID[asset.ID] = asset
		builder.assetsByName[normalizeName(asset.Name)] = asset.ID
	}
	for _, asset := range assets {
		if !shouldIncludeTopologyAsset(asset) || (environment != "" && asset.Environment != environment) {
			continue
		}
		node := assetNode(asset)
		builder.addNode(node)
	}
	for _, asset := range assets {
		if !shouldIncludeTopologyAsset(asset) || (environment != "" && asset.Environment != environment) {
			continue
		}
		for _, relation := range asset.Relations {
			target := builder.resolve(relation.TargetID, relation.TargetName)
			if target == "" {
				continue
			}
			builder.addEdge(asset.ID, target, relation.Type, "healthy")
		}
		builder.addLLDPEdges(asset)
	}
	return builder.graph
}

func shouldIncludeTopologyAsset(asset cmdb.Asset) bool {
	if strings.HasPrefix(asset.Type, "k8s-") && asset.Type != "k8s-node" && asset.Type != "k8s-cluster" {
		return false
	}
	switch asset.Type {
	case "project", "application", "service", "network-device", "k8s-cluster", "k8s-node":
		return true
	}
	return len(asset.Relations) > 0
}
func assetNode(asset cmdb.Asset) Node {
	return Node{ID: asset.ID, Name: asset.Name, Kind: asset.TypeName, Layer: assetLayer(asset), Status: assetStatus(asset.Status), Environment: asset.Environment, Owner: asset.Owner, IP: asset.IP}
}

func assetLayer(asset cmdb.Asset) int {
	switch asset.Type {
	case "project":
		return 0
	case "application":
		return 1
	case "service":
		return 2
	case "network-device", "database", "storage", "middleware":
		return 4
	default:
		return 3
	}
}

func assetStatus(status string) string {
	switch status {
	case "online", "healthy":
		return "healthy"
	case "warning":
		return "warning"
	case "offline", "critical":
		return "critical"
	default:
		return "warning"
	}
}

func (b *graphBuilder) addNode(node Node) {
	if _, exists := b.byID[node.ID]; exists {
		return
	}
	b.graph.Nodes = append(b.graph.Nodes, node)
	b.byID[node.ID] = node
	if node.Name != "" {
		b.byName[normalizeName(node.Name)] = node.ID
	}
}

func (b *graphBuilder) resolve(targetID, targetName string) string {
	if targetID != "" {
		if asset, ok := b.assetsByID[targetID]; ok {
			b.addNode(assetNode(asset))
			return targetID
		}
	}
	if targetName != "" {
		if id, ok := b.assetsByName[normalizeName(targetName)]; ok {
			b.addNode(assetNode(b.assetsByID[id]))
			return id
		}
	}
	return ""
}

func (b *graphBuilder) addEdge(source, target, relation, status string) {
	if source == "" || target == "" || source == target {
		return
	}
	key := source + "|" + target + "|" + relation
	if b.edgeSet[key] {
		return
	}
	b.edgeSet[key] = true
	b.graph.Edges = append(b.graph.Edges, Edge{ID: fmt.Sprintf("edge-%d", len(b.graph.Edges)+1), Source: source, Target: target, Relation: relation, Status: status})
}

type lldpNeighbor struct {
	LocalPort      string `json:"localPort"`
	RemoteSysName  string `json:"remoteSysName"`
	RemoteChassis  string `json:"remoteChassisId"`
	RemotePortID   string `json:"remotePortId"`
	RemotePortDesc string `json:"remotePortDesc"`
}

func (b *graphBuilder) addLLDPEdges(asset cmdb.Asset) {
	var neighbors []lldpNeighbor
	raw := attributeValue(asset.Attributes, "lldp_neighbors")
	if raw == "" || json.Unmarshal([]byte(raw), &neighbors) != nil {
		return
	}
	for _, neighbor := range neighbors {
		target := ""
		for _, candidate := range []string{neighbor.RemoteSysName, neighbor.RemoteChassis} {
			if id := b.resolve("", candidate); id != "" {
				target = id
				break
			}
		}
		if target == "" && neighbor.RemoteSysName != "" {
			target = "lldp-" + slug(neighbor.RemoteSysName)
			b.addNode(Node{ID: target, Name: neighbor.RemoteSysName, Kind: "网络设备", Layer: 4, Status: "healthy", Environment: asset.Environment, Owner: "自动发现", IP: neighbor.RemoteChassis})
		}
		if target != "" {
			relation := "LLDP"
			if neighbor.LocalPort != "" {
				relation += ":" + neighbor.LocalPort
			}
			b.addEdge(asset.ID, target, relation, "healthy")
		}
	}
}

func attributeValue(attributes []cmdb.Attribute, name string) string {
	for _, attribute := range attributes {
		if attribute.Name == name {
			return attribute.Value
		}
	}
	return ""
}

func normalizeName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func (s *Service) Impact(id string) (Impact, error) {
	graph := s.Graph("")
	byID := map[string]Node{}
	for _, node := range graph.Nodes {
		byID[node.ID] = node
	}
	root, ok := byID[id]
	if !ok {
		return Impact{}, ErrNotFound
	}
	type queueNode struct {
		id       string
		distance int
		path     []string
	}
	seen := map[string]bool{id: true}
	queue := []queueNode{{id: id, path: []string{id}}}
	result := Impact{Root: root}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range graph.Edges {
			next, direction := "", ""
			switch {
			case edge.Source == current.id:
				next, direction = edge.Target, "downstream"
			case edge.Target == current.id:
				next, direction = edge.Source, "upstream"
			}
			if next == "" || seen[next] {
				continue
			}
			node, exists := byID[next]
			if !exists {
				continue
			}
			seen[next] = true
			path := append(append([]string(nil), current.path...), next)
			result.Affected = append(result.Affected, ImpactNode{
				Node: node, Distance: current.distance + 1, Direction: direction, Path: path,
			})
			queue = append(queue, queueNode{id: next, distance: current.distance + 1, path: path})
		}
	}
	sort.SliceStable(result.Affected, func(i, j int) bool {
		left, right := result.Affected[i], result.Affected[j]
		if statusPriority(left.Status) != statusPriority(right.Status) {
			return statusPriority(left.Status) < statusPriority(right.Status)
		}
		if left.Distance != right.Distance {
			return left.Distance < right.Distance
		}
		return strings.ToLower(left.Name) < strings.ToLower(right.Name)
	})
	result.Summary.Total = len(result.Affected)
	for _, item := range result.Affected {
		switch item.Status {
		case "healthy":
			result.Summary.Healthy++
		case "warning":
			result.Summary.Warning++
		case "critical":
			result.Summary.Critical++
		}
		result.Paths = append(result.Paths, append([]string(nil), item.Path...))
	}
	return result, nil
}

func statusPriority(status string) int {
	switch strings.ToLower(status) {
	case "critical":
		return 0
	case "warning":
		return 1
	case "healthy":
		return 2
	default:
		return 3
	}
}
