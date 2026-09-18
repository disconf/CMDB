package topology

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

type PhysicalNode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	AssetType   string `json:"assetType"`
	Status      string `json:"status"`
	IP          string `json:"ip,omitempty"`
	Location    string `json:"location,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Role        string `json:"role,omitempty"`
	PortCount   int    `json:"portCount"`
	UpPortCount int    `json:"upPortCount"`
}

type PhysicalEdge struct {
	ID              string   `json:"id"`
	Source          string   `json:"source"`
	Target          string   `json:"target"`
	SourceInterface string   `json:"sourceInterface,omitempty"`
	TargetPort      string   `json:"targetPort,omitempty"`
	Relation        string   `json:"relation"`
	LinkKind        string   `json:"linkKind,omitempty"`
	Status          string   `json:"status"`
	SourceType      string   `json:"sourceType"`
	Verification    string   `json:"verification"`
	Confidence      int      `json:"confidence"`
	Evidence        []string `json:"evidence,omitempty"`
	LastSeenAt      string   `json:"lastSeenAt,omitempty"`
}

type PhysicalGraph struct {
	Nodes     []PhysicalNode `json:"nodes"`
	Edges     []PhysicalEdge `json:"edges"`
	UpdatedAt string         `json:"updatedAt"`
}

func (s *Service) PhysicalGraph(environment string) PhysicalGraph {
	return s.PhysicalGraphView(environment, false)
}

func (s *Service) PhysicalGraphView(environment string, linkedOnly bool) PhysicalGraph {
	if s.cmdb == nil {
		return PhysicalGraph{Nodes: []PhysicalNode{}, Edges: []PhysicalEdge{}, UpdatedAt: time.Now().Format("2006-01-02 15:04:05")}
	}
	assets := s.cmdb.InventoryAssets()
	byID := make(map[string]cmdb.Asset, len(assets))
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	devices := s.cmdb.NetworkDevices()
	portStats := make(map[string]cmdb.NetworkDeviceInventory, len(devices))
	for _, device := range devices {
		portStats[device.Asset.ID] = device
	}
	graph := PhysicalGraph{Nodes: []PhysicalNode{}, Edges: []PhysicalEdge{}, UpdatedAt: time.Now().Format("2006-01-02 15:04:05")}
	seenNode := make(map[string]bool)
	addNode := func(asset cmdb.Asset) {
		if seenNode[asset.ID] {
			return
		}
		if environment != "" && asset.Environment != environment {
			return
		}
		node := PhysicalNode{ID: asset.ID, Name: asset.Name, Kind: physicalKind(asset.Type), AssetType: asset.Type, Status: asset.Status, IP: asset.IP, Location: asset.Location, Owner: asset.Owner, Role: physicalRole(asset)}
		if inventory, ok := portStats[asset.ID]; ok {
			node.PortCount = inventory.TotalPorts
			node.UpPortCount = inventory.UpPorts
		}
		graph.Nodes = append(graph.Nodes, node)
		seenNode[asset.ID] = true
	}
	for _, asset := range assets {
		if isPhysicalGraphAsset(asset.Type) {
			addNode(asset)
		}
	}
	seenEdge := make(map[string]bool)
	addEdge := func(edge PhysicalEdge) {
		if edge.Source == "" || edge.Target == "" || edge.Source == edge.Target {
			return
		}
		if environment != "" {
			source, sourceOK := byID[edge.Source]
			target, targetOK := byID[edge.Target]
			if sourceOK && source.Environment != environment {
				return
			}
			if targetOK && target.Environment != environment {
				return
			}
		}
		if edge.ID == "" {
			edge.ID = fmt.Sprintf("physical-%d", len(graph.Edges)+1)
		}
		if edge.Status == "" {
			edge.Status = "healthy"
		}
		if edge.Relation == "" {
			edge.Relation = "物理链路"
		}
		if seenEdge[edge.ID] {
			return
		}
		seenEdge[edge.ID] = true
		graph.Edges = append(graph.Edges, edge)
	}
	for _, link := range s.cmdb.NetworkLinks() {
		targetID := link.TargetAssetID
		target, targetOK := byID[targetID]
		if targetID == "" || !targetOK {
			if link.TargetAssetName == "" {
				continue
			}
			targetID = "unmanaged-" + slug(link.TargetAssetName)
			if !seenNode[targetID] {
				graph.Nodes = append(graph.Nodes, PhysicalNode{ID: targetID, Name: link.TargetAssetName, Kind: "未纳管设备", AssetType: "unmanaged", Status: "unknown"})
				seenNode[targetID] = true
			}
			target = cmdb.Asset{ID: targetID, Name: link.TargetAssetName}
			targetOK = true
		}
		if !seenNode[link.SourceAssetID] || !seenNode[targetID] {
			continue
		}
		relation := "物理链路"
		if link.Source == "lldp" {
			relation = "LLDP"
		}
		sourceID, sourceInterface := link.SourceAssetID, link.SourceInterface
		finalTargetID, targetPort := targetID, link.TargetPort
		if networkSideAsset(byID[sourceID]) && hostSideAsset(target) {
			sourceID, finalTargetID = finalTargetID, sourceID
			sourceInterface, targetPort = targetPort, sourceInterface
		}
		addEdge(PhysicalEdge{ID: link.ID, Source: sourceID, Target: finalTargetID, SourceInterface: sourceInterface, TargetPort: targetPort, Relation: relation, LinkKind: physicalLinkKind(byID[sourceID].Type, byID[finalTargetID].Type), Status: physicalLinkStatus(target.Status), SourceType: link.Source, Verification: link.Verification, Confidence: link.Confidence, Evidence: append([]string(nil), link.Evidence...), LastSeenAt: link.LastSeenAt})
	}
	if linkedOnly {
		connected := make(map[string]bool, len(graph.Edges)*2)
		for _, edge := range graph.Edges {
			connected[edge.Source] = true
			connected[edge.Target] = true
		}
		nodes := make([]PhysicalNode, 0, len(connected))
		for _, node := range graph.Nodes {
			if connected[node.ID] {
				nodes = append(nodes, node)
			}
		}
		graph.Nodes = nodes
	}
	sort.Slice(graph.Nodes, func(i, j int) bool {
		if graph.Nodes[i].Kind == graph.Nodes[j].Kind {
			return strings.ToLower(graph.Nodes[i].Name) < strings.ToLower(graph.Nodes[j].Name)
		}
		return graph.Nodes[i].Kind < graph.Nodes[j].Kind
	})
	return graph
}

func isPhysicalGraphAsset(assetType string) bool {
	switch assetType {
	case "physical-server", "virtual-machine", "k8s-node", "network-device", "load-balancer":
		return true
	default:
		return false
	}
}

func physicalKind(assetType string) string {
	switch assetType {
	case "physical-server":
		return "物理服务器"
	case "virtual-machine":
		return "虚拟机"
	case "k8s-node":
		return "K8s 节点"
	case "network-device":
		return "交换机/网络设备"
	case "load-balancer":
		return "负载均衡"
	default:
		return assetType
	}
}

func physicalRole(asset cmdb.Asset) string {
	values := []string{asset.Name}
	for _, name := range []string{"network_role", "device_role", "switch_role", "role", "device_type"} {
		if value := physicalAttributeValue(asset.Attributes, name); value != "" {
			values = append(values, value)
		}
	}
	combined := strings.ToLower(strings.Join(values, " "))
	switch asset.Type {
	case "physical-server":
		return "服务器"
	case "virtual-machine":
		return "虚拟机"
	case "k8s-node":
		return "K8s 节点"
	case "load-balancer":
		return "负载均衡"
	case "network-device":
		switch {
		case strings.Contains(combined, "核心") || strings.Contains(combined, "core"):
			return "核心交换机"
		case strings.Contains(combined, "汇聚") || strings.Contains(combined, "aggregation") || strings.Contains(combined, "agg"):
			return "汇聚交换机"
		case strings.Contains(combined, "接入") || strings.Contains(combined, "access") || strings.Contains(combined, "acc"):
			return "接入交换机"
		case strings.Contains(combined, "防火墙") || strings.Contains(combined, "firewall"):
			return "防火墙"
		case strings.Contains(combined, "路由") || strings.Contains(combined, "router"):
			return "路由器"
		default:
			return "交换机"
		}
	default:
		return ""
	}
}

func physicalAttributeValue(attributes []cmdb.Attribute, name string) string {
	for _, attribute := range attributes {
		if attribute.Name == name {
			return strings.TrimSpace(attribute.Value)
		}
	}
	return ""
}
func physicalLinkKind(sourceType, targetType string) string {
	sourceNetwork := sourceType == "network-device" || sourceType == "load-balancer"
	targetNetwork := targetType == "network-device" || targetType == "load-balancer"
	sourceHost := sourceType == "physical-server" || sourceType == "virtual-machine" || sourceType == "k8s-node"
	targetHost := targetType == "physical-server" || targetType == "virtual-machine" || targetType == "k8s-node"
	if sourceNetwork && targetNetwork {
		return "switch-interconnect"
	}
	if (sourceHost && targetNetwork) || (sourceNetwork && targetHost) {
		return "host-access"
	}
	return "physical"
}
func physicalLinkStatus(status string) string {
	if status == "" {
		return "healthy"
	}
	return status
}

func networkSideAsset(asset cmdb.Asset) bool {
	return asset.Type == "network-device" || asset.Type == "load-balancer"
}

func hostSideAsset(asset cmdb.Asset) bool {
	return asset.Type == "physical-server" || asset.Type == "virtual-machine" || asset.Type == "k8s-node"
}
