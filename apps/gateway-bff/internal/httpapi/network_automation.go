package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/networkautomation"
)

func registerNetworkAutomationRoutes(mux *http.ServeMux, authService *auth.Service, service *networkautomation.Service) {
	mux.HandleFunc("GET /api/v1/cmdb/network-automation-policies", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		writeJSON(w, http.StatusOK, service.Policies())
	})
	mux.HandleFunc("POST /api/v1/cmdb/network-automation-policies", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input networkautomation.PolicyInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "请求格式不正确"})
			return
		}
		item, err := service.CreatePolicy(input)
		if errors.Is(err, networkautomation.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "自动化策略参数无效"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "POLICY_CREATE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, item)
	})
	mux.HandleFunc("PUT /api/v1/cmdb/network-automation-policies/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input networkautomation.PolicyInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "请求格式不正确"})
			return
		}
		item, err := service.UpdatePolicy(r.PathValue("id"), input)
		if errors.Is(err, networkautomation.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "POLICY_NOT_FOUND", "message": "自动化策略不存在"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "POLICY_UPDATE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("DELETE /api/v1/cmdb/network-automation-policies/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		if err := service.DeletePolicy(r.PathValue("id")); err != nil {
			if errors.Is(err, networkautomation.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"code": "POLICY_NOT_FOUND", "message": "自动化策略不存在"})
				return
			}
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "POLICY_DELETE_FAILED", "message": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/cmdb/network-automation-policies/evaluate", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		report, err := service.Evaluate(user.Username)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "AUTOMATION_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, report)
	})
	mux.HandleFunc("POST /api/v1/cmdb/network-topology-snapshots", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		snapshot, err := service.CaptureNetworkSnapshot(user.Username)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "SNAPSHOT_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, snapshot)
	})
	mux.HandleFunc("GET /api/v1/cmdb/network-topology-snapshots", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		writeJSON(w, http.StatusOK, service.NetworkSnapshots(limit))
	})
	mux.HandleFunc("GET /api/v1/cmdb/network-topology-diff", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		diff, err := service.NetworkDiff(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
		if errors.Is(err, networkautomation.ErrSnapshotNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "SNAPSHOT_NOT_FOUND", "message": "网络快照不存在"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "DIFF_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, diff)
	})
	mux.HandleFunc("GET /api/v1/cmdb/network-automation-lifecycle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		writeJSON(w, http.StatusOK, service.Lifecycle())
	})
}
