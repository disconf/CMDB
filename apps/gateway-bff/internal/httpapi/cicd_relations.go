package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cicd"
	"cmdb/gateway-bff/internal/cmdb"
)

func registerCICDRelationRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, service *cicd.Service, cmdbService *cmdb.Service) {
	mux.HandleFunc("GET /api/v1/cicd/relations", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, service.ListRelations(r.Context(), strings.TrimSpace(r.URL.Query().Get("resourceType"))))
	})

	mux.HandleFunc("POST /api/v1/cicd/relations", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		var input cicd.RelationInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "关联信息格式不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := service.CreateRelation(r.Context(), input, user.Username)
		if errors.Is(err, cicd.ErrRelationValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "RELATION_VALIDATION_ERROR", "message": "资源、资产和关联类型不能为空"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "RELATION_CREATE_FAILED", "message": "关联关系保存失败"})
			return
		}
		auditService.Record(user.Username, "cicd.relation.save", item.ResourceType+":"+item.ResourceID, item.AssetID)
		writeJSON(w, http.StatusCreated, item)
	})

	mux.HandleFunc("DELETE /api/v1/cicd/relations/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		if err := service.DeleteRelation(r.Context(), r.PathValue("id")); errors.Is(err, cicd.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "RELATION_NOT_FOUND"})
			return
		} else if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "RELATION_DELETE_FAILED"})
			return
		}
		if user, err := authService.CurrentUser(bearerToken(r)); err == nil {
			auditService.Record(user.Username, "cicd.relation.delete", r.PathValue("id"), "")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /api/v1/cicd/relations/suggestions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		assets := cmdbService.InventoryAssetsFor(assetScopeForUser(user))
		candidates := make([]cicd.AssetCandidate, 0, len(assets))
		for _, asset := range assets {
			candidates = append(candidates, cicd.AssetCandidate{ID: asset.ID, Name: asset.Name, Type: asset.Type, IP: asset.IP, ProjectGroup: asset.ProjectGroup})
		}
		writeJSON(w, http.StatusOK, service.SuggestRelations(r.URL.Query().Get("resourceType"), r.URL.Query().Get("resourceId"), r.URL.Query().Get("resourceName"), candidates, 10))
	})
}
