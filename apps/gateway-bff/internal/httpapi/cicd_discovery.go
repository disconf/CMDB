package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cicd"
	"cmdb/gateway-bff/internal/cmdb"
)

func registerCICDDiscoveryRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, service *cicd.Service, cmdbService *cmdb.Service) {
	mux.HandleFunc("POST /api/v1/cicd/relations/auto-discover", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		var input cicd.AutoDiscoveryInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "自动发现参数不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		assets := cmdbService.InventoryAssetsFor(assetScopeForUser(user))
		candidates := make([]cicd.AssetCandidate, 0, len(assets))
		for _, asset := range assets {
			candidates = append(candidates, cicd.AssetCandidate{ID: asset.ID, Name: asset.Name, Type: asset.Type, IP: asset.IP, ProjectGroup: asset.ProjectGroup})
		}
		result := service.AutoDiscoverRelations(r.Context(), input.ResourceTypes, candidates, user.Username, input.Apply)
		if input.Apply {
			auditService.Record(user.Username, "cicd.relation.auto_discover", "relations", "matched="+strconv.Itoa(result.Matched)+",applied="+strconv.Itoa(result.Applied))
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("GET /api/v1/cicd/relations/impact", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		resourceType := strings.TrimSpace(r.URL.Query().Get("resourceType"))
		resourceID := strings.TrimSpace(r.URL.Query().Get("resourceId"))
		if resourceType == "" || resourceID == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "IMPACT_QUERY_REQUIRED", "message": "resourceType 和 resourceId 不能为空"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := assetScopeForUser(user)
		relations := service.ListRelations(r.Context(), resourceType)
		direct := make([]map[string]any, 0)
		downstream := make([]map[string]any, 0)
		seenDownstream := make(map[string]bool)
		for _, relation := range relations {
			if relation.ResourceID != resourceID {
				continue
			}
			asset, err := cmdbService.GetAsset(relation.AssetID)
			if err != nil || !cmdb.AssetWithinScope(asset, scope) {
				continue
			}
			direct = append(direct, map[string]any{"assetId": asset.ID, "assetName": asset.Name, "assetType": asset.Type, "status": asset.Status, "relationType": relation.RelationType, "source": relation.Source})
			for _, assetRelation := range asset.Relations {
				if seenDownstream[assetRelation.TargetID] {
					continue
				}
				target, err := cmdbService.GetAsset(assetRelation.TargetID)
				if err != nil || !cmdb.AssetWithinScope(target, scope) {
					continue
				}
				seenDownstream[assetRelation.TargetID] = true
				downstream = append(downstream, map[string]any{"fromAssetId": asset.ID, "fromAssetName": asset.Name, "relationType": assetRelation.Type, "assetId": target.ID, "assetName": target.Name, "assetType": target.Type, "status": target.Status})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"resourceType": resourceType, "resourceId": resourceID, "directAssets": direct, "downstreamAssets": downstream, "impactCount": len(direct) + len(downstream)})
	})
}
