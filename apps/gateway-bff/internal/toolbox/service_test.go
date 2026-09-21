package toolbox

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"cmdb/gateway-bff/internal/topology"
)

func TestRunPrometheusAndHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"job":"test"},"value":[1,"1"]}]}}`))
	}))
	defer server.Close()
	t.Setenv("PROMETHEUS_URL", server.URL)
	t.Setenv("TOOLBOX_ALLOW_LOOPBACK", "true")
	s := NewService()
	result, err := s.Run(context.Background(), RunInput{ToolID: "prometheus", Query: "up", Operator: "admin"})
	if err != nil || result.Status != "success" || len(result.Output) < 3 {
		t.Fatalf("run: %#v %v", result, err)
	}
	history, err := s.History(context.Background(), 10)
	if err != nil || len(history) != 1 {
		t.Fatalf("history: %#v %v", history, err)
	}
	summary, err := s.Summary(context.Background())
	if err != nil || summary.ExecutionsToday != 1 {
		t.Fatalf("summary: %#v %v", summary, err)
	}
}

func TestValidationAndUnknownTool(t *testing.T) {
	s := NewService()
	if _, err := s.Run(context.Background(), RunInput{ToolID: "dns"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation, got %v", err)
	}
	if _, err := s.Run(context.Background(), RunInput{ToolID: "shell", Target: "host"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation, got %v", err)
	}
}

type fakeGraph struct{}

func (fakeGraph) Graph(string) topology.Graph {
	return topology.Graph{
		Nodes: []topology.Node{{ID: "app", Name: "应用"}, {ID: "db", Name: "数据库"}},
		Edges: []topology.Edge{{ID: "e1", Source: "app", Target: "db", Relation: "依赖"}},
	}
}

func TestTopologyPath(t *testing.T) {
	s := NewService()
	s.SetTopologyGraphProvider(fakeGraph{})
	result, err := s.Run(context.Background(), RunInput{ToolID: "topology-path", Target: "应用", Query: "数据库"})
	if err != nil || result.Status != "success" {
		t.Fatalf("path: %#v %v", result, err)
	}
	if len(result.Output) < 2 {
		t.Fatal("path details missing")
	}
}
