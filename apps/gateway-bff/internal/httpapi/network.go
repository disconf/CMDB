package httpapi

import (
	"errors"
	"net/http"

	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/topology"
)

func registerPhysicalNetworkRoutes(mux *http.ServeMux, authService *auth.Service, cmdbService *cmdb.Service, topologyService *topology.Service) {
	mux.HandleFunc("GET /api/v1/cmdb/network-devices", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		writeJSON(w, http.StatusOK, cmdbService.NetworkDevices())
	})
	mux.HandleFunc("GET /api/v1/cmdb/network-devices/{id}/ports", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		ports, err := cmdbService.NetworkPorts(r.PathValue("id"))
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND", "message": "资产不存在"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "NETWORK_PORT_READ_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, ports)
	})
	mux.HandleFunc("GET /api/v1/cmdb/assets/{id}/interfaces", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		interfaces, err := cmdbService.NetworkInterfaces(r.PathValue("id"))
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND", "message": "资产不存在"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "NETWORK_INTERFACE_READ_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, interfaces)
	})
	mux.HandleFunc("GET /api/v1/cmdb/network-links", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		writeJSON(w, http.StatusOK, cmdbService.NetworkLinks())
	})
	mux.HandleFunc("POST /api/v1/cmdb/network-links", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input cmdb.CreateNetworkLinkInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "请求格式不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		link, err := cmdbService.CreateNetworkLink(input, user.Username)
		switch {
		case errors.Is(err, cmdb.ErrValidation):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": err.Error()})
		case errors.Is(err, cmdb.ErrConflict):
			writeJSON(w, http.StatusConflict, map[string]string{"code": "NETWORK_LINK_CONFLICT", "message": err.Error()})
		case errors.Is(err, cmdb.ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND", "message": "资产不存在"})
		case err != nil:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "NETWORK_LINK_CREATE_FAILED", "message": err.Error()})
		default:
			writeJSON(w, http.StatusCreated, link)
		}
	})
	mux.HandleFunc("DELETE /api/v1/cmdb/network-links/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		if err := cmdbService.DeleteNetworkLink(r.PathValue("id")); err != nil {
			if errors.Is(err, cmdb.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"code": "NETWORK_LINK_NOT_FOUND", "message": "物理链路不存在"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "NETWORK_LINK_DELETE_FAILED", "message": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/topology/physical", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "topology:view") {
			return
		}
		writeJSON(w, http.StatusOK, topologyService.PhysicalGraph(r.URL.Query().Get("environment")))
	})
}
