package httpapi

import (
	"strings"

	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/releases"
	"cmdb/gateway-bff/internal/tickets"
	"cmdb/gateway-bff/internal/topology"
)

type userScope struct {
	user   auth.User
	global bool
	ids    map[string]bool
	ips    map[string]bool
}

func newUserScope(cmdbService *cmdb.Service, user auth.User) userScope {
	scope := userScope{user: user, global: hasGlobalAssetScope(user), ids: map[string]bool{}, ips: map[string]bool{}}
	if scope.global {
		for _, asset := range cmdbService.InventoryAssets() {
			scope.ids[asset.ID] = true
			if asset.IP != "" {
				scope.ips[asset.IP] = true
			}
		}
		return scope
	}
	for _, asset := range cmdbService.InventoryAssetsFor(assetScopeForUser(user)) {
		scope.ids[asset.ID] = true
		if asset.IP != "" {
			scope.ips[asset.IP] = true
		}
	}
	return scope
}

func (s userScope) allowsAsset(asset cmdb.Asset) bool {
	return s.global || cmdb.AssetWithinScope(asset, assetScopeForUser(s.user))
}

func (s userScope) allowsIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && (s.ids[value] || s.ips[value])
}

func (s userScope) allowsProject(group string) bool {
	if s.global {
		return true
	}
	dataScope := strings.TrimSpace(s.user.DataScope)
	if dataScope == "groups" || dataScope == "" {
		for _, item := range s.user.ProjectGroups {
			if item == group {
				return true
			}
		}
	}
	return false
}

func filterKubernetesInventory(inventory cmdb.K8sInventory, scope userScope) cmdb.K8sInventory {
	if scope.global {
		return inventory
	}
	result := cmdb.K8sInventory{Clusters: []cmdb.K8sClusterInventory{}}
	for _, cluster := range inventory.Clusters {
		if scope.ids[cluster.ID] {
			result.Clusters = append(result.Clusters, cluster)
		}
	}
	return result
}

func filterNetworkDevices(items []cmdb.NetworkDeviceInventory, scope userScope) []cmdb.NetworkDeviceInventory {
	result := []cmdb.NetworkDeviceInventory{}
	for _, item := range items {
		if scope.allowsAsset(item.Asset) {
			result = append(result, item)
		}
	}
	return result
}

func filterNetworkLinks(items []cmdb.NetworkLink, scope userScope) []cmdb.NetworkLink {
	result := []cmdb.NetworkLink{}
	for _, item := range items {
		if scope.allowsIdentifier(item.SourceAssetID) && scope.allowsIdentifier(item.TargetAssetID) {
			result = append(result, item)
		}
	}
	return result
}

func filterVLANInventory(items []cmdb.VLANInventory, scope userScope) []cmdb.VLANInventory {
	if scope.global {
		return items
	}
	result := []cmdb.VLANInventory{}
	for _, item := range items {
		members := []cmdb.VLANMember{}
		for _, member := range item.Members {
			if scope.allowsIdentifier(member.DeviceID) || scope.allowsIdentifier(member.AssetID) {
				members = append(members, member)
			}
		}
		if len(members) == 0 {
			continue
		}
		item.Members = members
		item.MemberCount = len(members)
		item.UpMembers = 0
		item.LearnedMACCount = 0
		item.ARPEntryCount = 0
		for _, member := range members {
			if member.Status == "up" {
				item.UpMembers++
			}
			item.LearnedMACCount += len(member.MACAddresses)
			item.ARPEntryCount += member.ARPCount
		}
		result = append(result, item)
	}
	return result
}

func filterNetworkAnomalies(items []cmdb.NetworkAnomaly, scope userScope) []cmdb.NetworkAnomaly {
	result := []cmdb.NetworkAnomaly{}
	for _, item := range items {
		if scope.global || scope.allowsIdentifier(item.AssetID) || scope.allowsIdentifier(item.DeviceID) || scope.allowsIdentifier(item.IP) {
			result = append(result, item)
		}
	}
	return result
}

func filterTopologyGraph(graph topology.Graph, scope userScope) topology.Graph {
	if scope.global {
		return graph
	}
	kept := map[string]bool{}
	result := topology.Graph{UpdatedAt: graph.UpdatedAt, Nodes: []topology.Node{}, Edges: []topology.Edge{}}
	for _, node := range graph.Nodes {
		if scope.ids[node.ID] || scope.ips[node.IP] {
			kept[node.ID] = true
			result.Nodes = append(result.Nodes, node)
		}
	}
	for _, edge := range graph.Edges {
		if kept[edge.Source] && kept[edge.Target] {
			result.Edges = append(result.Edges, edge)
		}
	}
	return result
}

func filterMonitorAlerts(items []monitor.Alert, scope userScope) []monitor.Alert {
	result := []monitor.Alert{}
	for _, item := range items {
		if scope.global || scope.allowsIdentifier(item.TargetID) || scope.allowsIdentifier(item.Target) {
			result = append(result, item)
		}
	}
	return result
}

func filterMonitorMetrics(items []monitor.Metric, scope userScope) []monitor.Metric {
	result := []monitor.Metric{}
	for _, item := range items {
		if scope.global || scope.allowsIdentifier(item.Target) {
			result = append(result, item)
		}
	}
	return result
}

func filterJobs(items []jobs.Job, scope userScope) []jobs.Job {
	result := []jobs.Job{}
	for _, item := range items {
		if scope.global || allIdentifiersAllowed(item.Targets, scope) {
			result = append(result, item)
		}
	}
	return result
}

func filterSchedules(items []jobs.Schedule, scope userScope) []jobs.Schedule {
	result := []jobs.Schedule{}
	for _, item := range items {
		if scope.global || allIdentifiersAllowed(item.Targets, scope) {
			result = append(result, item)
		}
	}
	return result
}

func allIdentifiersAllowed(values []string, scope userScope) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if !scope.allowsIdentifier(value) {
			return false
		}
	}
	return true
}

func filterTickets(items []tickets.Ticket, cmdbService *cmdb.Service, scope userScope) []tickets.Ticket {
	result := []tickets.Ticket{}
	for _, item := range items {
		if scope.global {
			result = append(result, item)
			continue
		}
		if item.RelatedAsset != "" {
			asset, err := cmdbService.GetAsset(item.RelatedAsset)
			if err == nil && scope.allowsAsset(asset) {
				result = append(result, item)
				continue
			}
		}
		if scope.allowsProject(item.ProjectGroup) {
			result = append(result, item)
		}
	}
	return result
}

func filterReleases(items []releases.Release, ticketItems []tickets.Ticket, scope userScope) []releases.Release {
	if scope.global {
		return items
	}
	allowedTickets := map[string]bool{}
	for _, item := range ticketItems {
		allowedTickets[item.ID] = true
	}
	result := []releases.Release{}
	for _, item := range items {
		if item.TicketID != "" && allowedTickets[item.TicketID] {
			result = append(result, item)
		}
	}
	return result
}

func filterPhysicalGraph(graph topology.PhysicalGraph, scope userScope) topology.PhysicalGraph {
	if scope.global {
		return graph
	}
	kept := map[string]bool{}
	result := topology.PhysicalGraph{UpdatedAt: graph.UpdatedAt, Nodes: []topology.PhysicalNode{}, Edges: []topology.PhysicalEdge{}}
	for _, node := range graph.Nodes {
		if scope.ids[node.ID] || scope.ips[node.IP] {
			kept[node.ID] = true
			result.Nodes = append(result.Nodes, node)
		}
	}
	for _, edge := range graph.Edges {
		if kept[edge.Source] && kept[edge.Target] {
			result.Edges = append(result.Edges, edge)
		}
	}
	return result
}

func filterImpact(result topology.Impact, scope userScope) topology.Impact {
	if scope.global {
		return result
	}
	affected := []topology.ImpactNode{}
	for _, item := range result.Affected {
		if scope.ids[item.ID] || scope.ips[item.IP] {
			affected = append(affected, item)
		}
	}
	result.Affected = affected
	result.Paths = [][]string{}
	result.Summary = topology.ImpactSummary{Total: len(affected)}
	for _, item := range affected {
		switch item.Status {
		case "healthy":
			result.Summary.Healthy++
		case "warning":
			result.Summary.Warning++
		default:
			result.Summary.Critical++
		}
	}
	return result
}
