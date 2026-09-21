package httpapi

import (
	"context"
	"net/http"
	"strings"

	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cicd"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/discovery"
)

type cicdMonitorTarget struct {
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	ResourceName string `json:"resourceName"`
	RelationType string `json:"relationType"`
	AssetID      string `json:"assetId"`
	AssetName    string `json:"assetName"`
	AssetType    string `json:"assetType"`
	Target       string `json:"target"`
	Port         string `json:"port"`
	Exporter     string `json:"exporter"`
	Status       string `json:"status"`
	Source       string `json:"source"`
	LastSeenAt   string `json:"lastSeenAt"`
}

func registerCICDMonitoringRoutes(mux *http.ServeMux, authService *auth.Service, service *cicd.Service, cmdbService *cmdb.Service, discoveryService *discovery.Service) {
	mux.HandleFunc("GET /api/v1/cicd/monitor-targets", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		targets, summary := cicdMonitorTargets(service, cmdbService, user)
		writeJSON(w, http.StatusOK, map[string]any{"summary": summary, "targets": targets})
	})

	mux.HandleFunc("GET /api/v1/monitor/service-discovery/cicd", func(w http.ResponseWriter, r *http.Request) {
		if !discoveryService.AuthorizeAgentToken(bearerToken(r)) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED"})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, cicdPrometheusTargetGroups(service, cmdbService))
	})
}

func cicdMonitorTargets(service *cicd.Service, cmdbService *cmdb.Service, user auth.User) ([]cicdMonitorTarget, map[string]int) {
	relations := service.ListRelations(context.Background(), "")
	scope := assetScopeForUser(user)
	targets := cmdbService.MonitorTargetsFor(scope)
	targetByAsset := make(map[string]cmdb.MonitorTarget, len(targets))
	for _, target := range targets {
		targetByAsset[target.AssetID] = target
	}
	result := make([]cicdMonitorTarget, 0, len(relations))
	summary := map[string]int{"total": 0, "configured": 0, "missing": 0}
	for _, relation := range relations {
		asset, err := cmdbService.GetAsset(relation.AssetID)
		if err != nil || !cmdb.AssetWithinScope(asset, scope) {
			continue
		}
		target, ok := targetByAsset[asset.ID]
		status := target.Status
		if !ok || target.Target == "" {
			status = "missing"
		}
		item := cicdMonitorTarget{ResourceType: relation.ResourceType, ResourceID: relation.ResourceID, ResourceName: relation.ResourceName, RelationType: relation.RelationType, AssetID: asset.ID, AssetName: asset.Name, AssetType: asset.Type, Target: target.Target, Port: target.Port, Exporter: target.Exporter, Status: status, Source: target.Source, LastSeenAt: target.LastSeenAt}
		result = append(result, item)
		summary["total"]++
		if status == "missing" {
			summary["missing"]++
		} else {
			summary["configured"]++
		}
	}
	return result, summary
}

func cicdPrometheusTargetGroups(service *cicd.Service, cmdbService *cmdb.Service) []cmdb.PrometheusTargetGroup {
	relations := service.ListRelations(context.Background(), "")
	targets := cmdbService.MonitorTargets()
	targetByAsset := make(map[string]cmdb.MonitorTarget, len(targets))
	for _, target := range targets {
		targetByAsset[target.AssetID] = target
	}
	groups := make([]cmdb.PrometheusTargetGroup, 0, len(relations))
	seen := make(map[string]bool)
	for _, relation := range relations {
		target, ok := targetByAsset[relation.AssetID]
		if !ok || strings.TrimSpace(target.Target) == "" || target.Exporter == "snmp_exporter" {
			continue
		}
		key := relation.ResourceType + "|" + relation.ResourceID + "|" + target.Target
		if seen[key] {
			continue
		}
		seen[key] = true
		groups = append(groups, cmdb.PrometheusTargetGroup{Targets: []string{target.Target}, Labels: map[string]string{"service": "cmdb-cicd", "asset_id": target.AssetID, "asset_name": target.AssetName, "asset_type": target.AssetType, "exporter": target.Exporter, "cicd_resource_type": relation.ResourceType, "cicd_resource_id": relation.ResourceID, "cicd_relation": relation.RelationType}})
	}
	return groups
}
