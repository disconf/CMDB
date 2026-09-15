package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/topology"
)

func registerTopologySnapshotRoutes(mux *http.ServeMux, authService *auth.Service, cmdbService *cmdb.Service, topologyService *topology.Service) {
	mux.HandleFunc("POST /api/v1/topology/snapshots", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			Kind        string `json:"kind"`
			Environment string `json:"environment"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "请求格式不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		snapshot, err := topologyService.CaptureSnapshot(input.Kind, input.Environment, user.Username)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "SNAPSHOT_CAPTURE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, snapshot)
	})
	mux.HandleFunc("GET /api/v1/topology/snapshots", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "topology:view") {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		writeJSON(w, http.StatusOK, cmdbService.TopologySnapshots(r.URL.Query().Get("kind"), r.URL.Query().Get("environment"), limit, false))
	})
	mux.HandleFunc("GET /api/v1/topology/snapshots/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "topology:view") {
			return
		}
		snapshot, err := cmdbService.TopologySnapshot(r.PathValue("id"))
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "SNAPSHOT_NOT_FOUND", "message": "拓扑快照不存在"})
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	})
}
