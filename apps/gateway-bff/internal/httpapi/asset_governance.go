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
		writeJSON(w, http.StatusOK, service.Issues())
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
