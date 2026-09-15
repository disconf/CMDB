package topology

import (
	"encoding/json"
	"strings"

	"cmdb/gateway-bff/internal/cmdb"
)

func (s *Service) CaptureSnapshot(kind, environment, operator string) (cmdb.TopologySnapshot, error) {
	if s.cmdb == nil {
		return cmdb.TopologySnapshot{}, cmdb.ErrValidation
	}
	kind = strings.TrimSpace(kind)
	environment = strings.TrimSpace(environment)
	if kind == "" {
		kind = "physical"
	}
	var payload []byte
	var nodeCount, edgeCount int
	switch kind {
	case "business":
		graph := s.Graph(environment)
		payload, _ = json.Marshal(graph)
		nodeCount, edgeCount = len(graph.Nodes), len(graph.Edges)
	default:
		kind = "physical"
		graph := s.PhysicalGraph(environment)
		payload, _ = json.Marshal(graph)
		nodeCount, edgeCount = len(graph.Nodes), len(graph.Edges)
	}
	return s.cmdb.SaveTopologySnapshot(cmdb.TopologySnapshotInput{Kind: kind, Environment: environment, NodeCount: nodeCount, EdgeCount: edgeCount, Payload: payload, Operator: operator})
}
