package toolbox

import (
	"fmt"
	"strings"

	"cmdb/gateway-bff/internal/topology"
)

func (s *Service) runTopologyPath(in RunInput) ([]Line, string, string, error) {
	if s.topology == nil {
		return nil, "failed", "拓扑服务未配置", ErrNotConfigured
	}
	graph := s.topology.Graph(strings.TrimSpace(in.Environment))
	source, ok := resolveTopologyNode(graph, in.Target)
	if !ok {
		return nil, "failed", "源资产未找到", fmt.Errorf("拓扑中未找到源资产 %q", in.Target)
	}
	target, ok := resolveTopologyNode(graph, in.Query)
	if !ok {
		return nil, "failed", "目标资产未找到", fmt.Errorf("拓扑中未找到目标资产 %q", in.Query)
	}
	adjacency := map[string][]topology.Edge{}
	for _, edge := range graph.Edges {
		adjacency[edge.Source] = append(adjacency[edge.Source], edge)
		adjacency[edge.Target] = append(adjacency[edge.Target], edge)
	}
	parent := map[string]string{}
	parentEdge := map[string]topology.Edge{}
	queue := []string{source.ID}
	visited := map[string]bool{source.ID: true}
	found := false
	for len(queue) > 0 && !found {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range adjacency[current] {
			next := edge.Target
			if next == current {
				next = edge.Source
			}
			if visited[next] {
				continue
			}
			visited[next] = true
			parent[next] = current
			parentEdge[next] = edge
			if next == target.ID {
				found = true
				break
			}
			queue = append(queue, next)
		}
	}
	if !found {
		return []Line{outputLine("warning", fmt.Sprintf("未找到 %s 到 %s 的关联路径", source.Name, target.Name))}, "warning", "资产之间暂无关联路径", nil
	}
	ids := []string{target.ID}
	for ids[len(ids)-1] != source.ID {
		current := ids[len(ids)-1]
		parentID := parent[current]
		if parentID == "" {
			break
		}
		ids = append(ids, parentID)
	}
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i]
	}
	lines := []Line{outputLine("info", fmt.Sprintf("路径：%s", strings.Join(nodeNames(graph, ids), " -> ")))}
	for index := 1; index < len(ids); index++ {
		edge := parentEdge[ids[index]]
		lines = append(lines, outputLine("info", fmt.Sprintf("%s --[%s]--> %s", edge.Source, edge.Relation, edge.Target)))
	}
	return lines, "success", fmt.Sprintf("找到 %d 跳关联路径", len(ids)-1), nil
}

func resolveTopologyNode(graph topology.Graph, value string) (topology.Node, bool) {
	value = strings.TrimSpace(value)
	for _, node := range graph.Nodes {
		if node.ID == value {
			return node, true
		}
	}
	for _, node := range graph.Nodes {
		if strings.EqualFold(node.Name, value) {
			return node, true
		}
	}
	return topology.Node{}, false
}

func nodeNames(graph topology.Graph, ids []string) []string {
	byID := map[string]string{}
	for _, node := range graph.Nodes {
		byID[node.ID] = node.Name
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if name := byID[id]; name != "" {
			names = append(names, name)
		} else {
			names = append(names, id)
		}
	}
	return names
}
