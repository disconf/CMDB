package httpapi

import (
	"errors"
	"net/http"

	"cmdb/gateway-bff/internal/assetgovernance"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
)

func registerAssetGovernanceRoutes(mux *http.ServeMux, authService *auth.Service, service *assetgovernance.Service) {
	mux.HandleFunc("GET /api/v1/cmdb/asset-governance/issues", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		scope, _, _ := assetQueryForRequest(r, authService)
		writeJSON(w, http.StatusOK, service.IssuesFor(scope))
	})
	mux.HandleFunc("GET /api/v1/cmdb/asset-governance/rules", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		writeJSON(w, http.StatusOK, service.GovernanceRules())
	})
	mux.HandleFunc("POST /api/v1/cmdb/asset-governance/rules", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input assetgovernance.GovernanceRuleInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "请求格式不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := service.CreateGovernanceRule(input, user.Username)
		if errors.Is(err, assetgovernance.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "规则名称、负责人和至少一个匹配条件不能为空"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "GOVERNANCE_RULE_CREATE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, item)
	})
	mux.HandleFunc("PUT /api/v1/cmdb/asset-governance/rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input assetgovernance.GovernanceRuleInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "请求格式不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := service.UpdateGovernanceRule(r.PathValue("id"), input, user.Username)
		if errors.Is(err, assetgovernance.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "GOVERNANCE_RULE_NOT_FOUND", "message": "治理规则不存在"})
			return
		}
		if errors.Is(err, assetgovernance.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "规则名称、负责人和至少一个匹配条件不能为空"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "GOVERNANCE_RULE_UPDATE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("DELETE /api/v1/cmdb/asset-governance/rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		if err := service.DeleteGovernanceRule(r.PathValue("id")); err != nil {
			if errors.Is(err, assetgovernance.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"code": "GOVERNANCE_RULE_NOT_FOUND", "message": "治理规则不存在"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "GOVERNANCE_RULE_DELETE_FAILED", "message": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/cmdb/asset-governance/rules/evaluate", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		writeJSON(w, http.StatusOK, service.EvaluateGovernanceRules(user.Username))
	})
	mux.HandleFunc("POST /api/v1/cmdb/asset-governance/assets/bulk", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input assetgovernance.AssetGovernanceBulkInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "请求格式不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := assetScopeForUser(user)
		for _, assetID := range input.AssetIDs {
			if !service.AssetWithinScope(assetID, scope) {
				writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN", "message": "批量操作包含无权限资产"})
				return
			}
		}
		result, err := service.BulkGovernance(input, user.Username)
		if errors.Is(err, assetgovernance.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "批量操作参数无效，单次最多处理 500 台资产"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "BULK_GOVERNANCE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/cmdb/asset-governance/evaluate", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		report, err := service.Evaluate(user.Username)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "ASSET_GOVERNANCE_EVALUATE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, report)
	})
	mux.HandleFunc("POST /api/v1/cmdb/asset-governance/assets/{id}/assign", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			Owner        string `json:"owner"`
			ProjectGroup string `json:"projectGroup"`
			Location     string `json:"location"`
			Note         string `json:"note"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "请求格式不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		if !service.AssetWithinScope(r.PathValue("id"), assetScopeForUser(user)) {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN", "message": "没有该资产所在项目组的数据权限"})
			return
		}
		asset, err := service.Assign(r.PathValue("id"), input.Owner, input.ProjectGroup, input.Location, user.Username, input.Note)
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND", "message": "资产不存在"})
			return
		}
		if errors.Is(err, assetgovernance.ErrValidation) || errors.Is(err, cmdb.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "负责人不能为空"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "ASSET_ASSIGN_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, asset)
	})
	mux.HandleFunc("POST /api/v1/cmdb/asset-governance/assets/{id}/confirm", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			Note string `json:"note"`
		}
		_ = decodeJSON(r, &input)
		user, _ := authService.CurrentUser(bearerToken(r))
		if !service.AssetWithinScope(r.PathValue("id"), assetScopeForUser(user)) {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN", "message": "没有该资产所在项目组的数据权限"})
			return
		}
		review, err := service.Confirm(r.PathValue("id"), user.Username, input.Note)
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND", "message": "资产不存在"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "ASSET_CONFIRM_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, review)
	})
	mux.HandleFunc("POST /api/v1/cmdb/asset-governance/assets/{id}/retire", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			Note string `json:"note"`
		}
		_ = decodeJSON(r, &input)
		user, _ := authService.CurrentUser(bearerToken(r))
		if !service.AssetWithinScope(r.PathValue("id"), assetScopeForUser(user)) {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN", "message": "没有该资产所在项目组的数据权限"})
			return
		}
		review, err := service.Retire(r.PathValue("id"), user.Username, input.Note)
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND", "message": "资产不存在"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "ASSET_RETIRE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, review)
	})
}
