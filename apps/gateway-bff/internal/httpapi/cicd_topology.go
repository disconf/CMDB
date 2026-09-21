package httpapi

import (
	"net/http"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cicd"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/topology"
)

func registerCICDTopologyRoutes(mux *http.ServeMux, authService *auth.Service, service *cicd.Service, cmdbService *cmdb.Service) {
	mux.HandleFunc("GET /api/v1/topology/cicd", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		graph := buildCICDTopology(r, authService, service, cmdbService)
		writeJSON(w, http.StatusOK, graph)
	})
}

func buildCICDTopology(r *http.Request, authService *auth.Service, service *cicd.Service, cmdbService *cmdb.Service) topology.Graph {
	overview := service.Resources(r.Context(), false)
	relations := service.ListRelations(r.Context(), "")
	nodes := make([]topology.Node, 0)
	edges := make([]topology.Edge, 0)
	nodeIndex := make(map[string]int)
	addNode := func(node topology.Node) {
		if _, ok := nodeIndex[node.ID]; ok {
			return
		}
		nodeIndex[node.ID] = len(nodes)
		nodes = append(nodes, node)
	}
	addResource := func(resourceType, resourceID, name, status string) {
		addNode(topology.Node{ID: "cicd|" + resourceType + "|" + resourceID, Name: name, Kind: resourceKindLabel(resourceType), Layer: 0, Status: topologyStatus(status), Owner: "CI/CD"})
	}
	for _, repository := range overview.Harbor.RecentRepositories {
		addResource("harbor-repository", strings.Trim(repository.Project+"/"+repository.Name, "/"), repository.Name, "healthy")
	}
	for _, application := range overview.ArgoCD.Items {
		addResource("argocd-application", strings.Trim(application.Namespace+"/"+application.Name, "/"), application.Name, application.HealthStatus)
	}
	for _, project := range overview.GitLab.RecentProjects {
		addResource("gitlab-project", project.Path, project.Name, "healthy")
	}

	user, _ := authService.CurrentUser(bearerToken(r))
	scope := assetScopeForUser(user)
	for _, relation := range relations {
		resourceNodeID := "cicd|" + relation.ResourceType + "|" + relation.ResourceID
		addResource(relation.ResourceType, relation.ResourceID, relation.ResourceName, "healthy")
		asset, err := cmdbService.GetAsset(relation.AssetID)
		if err != nil || !cmdb.AssetWithinScope(asset, scope) {
			continue
		}
		assetNodeID := "asset|" + asset.ID
		addNode(topology.Node{ID: assetNodeID, Name: asset.Name, Kind: asset.TypeName, Layer: 1, Status: topologyStatus(asset.Status), Environment: asset.Environment, Owner: asset.Owner, IP: asset.IP})
		edges = append(edges, topology.Edge{ID: "cicd-edge|" + relation.ID, Source: resourceNodeID, Target: assetNodeID, Relation: relationTypeLabel(relation.RelationType), Status: topologyStatus(asset.Status)})
		for _, assetRelation := range asset.Relations {
			target, err := cmdbService.GetAsset(assetRelation.TargetID)
			if err != nil || !cmdb.AssetWithinScope(target, scope) {
				continue
			}
			targetNodeID := "asset|" + target.ID
			addNode(topology.Node{ID: targetNodeID, Name: target.Name, Kind: target.TypeName, Layer: 2, Status: topologyStatus(target.Status), Environment: target.Environment, Owner: target.Owner, IP: target.IP})
			edges = append(edges, topology.Edge{ID: "asset-edge|" + asset.ID + "|" + target.ID + "|" + assetRelation.Type, Source: assetNodeID, Target: targetNodeID, Relation: relationTypeLabel(assetRelation.Type), Status: topologyStatus(target.Status)})
		}
	}
	return topology.Graph{Nodes: nodes, Edges: edges, UpdatedAt: time.Now().Format("2006-01-02 15:04:05")}
}

func resourceKindLabel(resourceType string) string {
	return map[string]string{"harbor-repository": "Harbor 镜像仓库", "argocd-application": "Argo CD 应用", "gitlab-project": "GitLab 项目"}[resourceType]
}

func relationTypeLabel(value string) string {
	return map[string]string{"deployed-to": "部署到", "runs-on": "运行于", "belongs-to": "属于", "built-from": "构建来源", "monitored-by": "监控关联", "related-to": "关联"}[value]
}

func topologyStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "healthy", "online", "success", "synced", "connected":
		return "healthy"
	case "warning", "progressing", "running", "pending", "outofsync":
		return "warning"
	case "critical", "offline", "failed", "degraded", "error":
		return "critical"
	default:
		return "healthy"
	}
}
