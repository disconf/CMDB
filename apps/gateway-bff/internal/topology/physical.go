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
	PortCount   int    `json:"portCount"`
	UpPortCount int    `json:"upPortCount"`
}

type PhysicalEdge struct {
	ID              string `json:"id"`
	Source          string `json:"source"`
	Target          string `json:"target"`
	SourceInterface string `json:"sourceInterface,omitempty"`
	TargetPort      string `json:"targetPort,omitempty"`
	Relation        string `json:"relation"`
	Status          string `json:"status"`
	SourceType      string `json:"sourceType"`
	LastSeenAt      string `json:"lastSeenAt,omitempty"`
}

type PhysicalGraph struct {
	Nodes     []PhysicalNode `json:"nodes"`
	Edges     []PhysicalEdge `json:"edges"`
	UpdatedAt string         `json:"updatedAt"`
}

func (s *Service) PhysicalGraph(environment string) PhysicalGraph {
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
		node := PhysicalNode{ID: asset.ID, Name: asset.Name, Kind: physicalKind(asset.Type), AssetType: asset.Type, Status: asset.Status, IP: asset.IP, Location: asset.Location, Owner: asset.Owner}
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
		addEdge(PhysicalEdge{ID: link.ID, Source: sourceID, Target: finalTargetID, SourceInterface: sourceInterface, TargetPort: targetPort, Relation: relation, Status: physicalLinkStatus(target.Status), SourceType: link.Source, LastSeenAt: link.LastSeenAt})
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
