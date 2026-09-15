package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
	"cmdb/gateway-bff/internal/topology"
)

func registerPhysicalNetworkRoutes(mux *http.ServeMux, authService *auth.Service, cmdbService *cmdb.Service, topologyService *topology.Service, monitorService *monitor.Service, ticketsService *tickets.Service) {
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
	mux.HandleFunc("GET /api/v1/cmdb/network-links/history", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		writeJSON(w, http.StatusOK, cmdbService.NetworkLinkHistory(limit))
	})
	mux.HandleFunc("GET /api/v1/cmdb/vlans", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		writeJSON(w, http.StatusOK, cmdbService.VLANInventory())
	})
	mux.HandleFunc("GET /api/v1/cmdb/network-anomalies", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		writeJSON(w, http.StatusOK, cmdbService.NetworkAnomalies())
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
			if source, readErr := cmdbService.GetAsset(input.SourceAssetID); readErr == nil {
				_, _ = topologyService.CaptureSnapshot("physical", source.Environment, user.Username)
			}
			writeJSON(w, http.StatusCreated, link)
		}
	})
	mux.HandleFunc("DELETE /api/v1/cmdb/network-links/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		linkID := r.PathValue("id")
		environment := ""
		for _, link := range cmdbService.NetworkLinks() {
			if link.ID == linkID {
				if source, readErr := cmdbService.GetAsset(link.SourceAssetID); readErr == nil {
					environment = source.Environment
				}
				break
			}
		}
		if err := cmdbService.DeleteNetworkLink(linkID); err != nil {
			if errors.Is(err, cmdb.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"code": "NETWORK_LINK_NOT_FOUND", "message": "物理链路不存在"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "NETWORK_LINK_DELETE_FAILED", "message": err.Error()})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		_, _ = topologyService.CaptureSnapshot("physical", environment, user.Username)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/cmdb/network-anomalies/{id}/acknowledge", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			Note string `json:"note"`
		}
		_ = decodeJSON(r, &input)
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := cmdbService.AcknowledgeNetworkAnomaly(r.PathValue("id"), user.Username, input.Note)
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ANOMALY_NOT_FOUND", "message": "网络异常不存在或已发生变化"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "ANOMALY_ACK_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/cmdb/network-anomalies/{id}/resolve", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			Note string `json:"note"`
		}
		_ = decodeJSON(r, &input)
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := cmdbService.ResolveNetworkAnomaly(r.PathValue("id"), user.Username, input.Note)
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ANOMALY_NOT_FOUND", "message": "网络异常不存在或已发生变化"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "ANOMALY_RESOLVE_FAILED", "message": err.Error()})
			return
		}
		_, _, _ = ticketsService.SyncAlert(tickets.AlertInput{ID: item.ID, Title: item.Title, Severity: item.Severity, Status: "resolved", Target: firstHTTPAPIValue(item.IP, item.DeviceName, item.AssetName), Message: item.Detail, Owner: user.Username})
		_, _ = syncNetworkAnomalyAlerts(cmdbService, monitorService)
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/cmdb/network-anomalies/{id}/ticket", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		item, ok := findNetworkAnomaly(cmdbService, r.PathValue("id"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ANOMALY_NOT_FOUND", "message": "网络异常不存在或已发生变化"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		ticket, created, err := ticketsService.SyncAlert(tickets.AlertInput{ID: item.ID, Title: item.Title, Severity: item.Severity, Status: "firing", Target: firstHTTPAPIValue(item.IP, item.DeviceName, item.AssetName), Message: item.Detail, Owner: user.Username})
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "ANOMALY_TICKET_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ticket": ticket, "created": created})
	})
	mux.HandleFunc("POST /api/v1/cmdb/network-anomalies/sync-alerts", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		count, err := syncNetworkAnomalyAlerts(cmdbService, monitorService)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "ALERT_SYNC_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"synced": count})
	})
	mux.HandleFunc("GET /api/v1/topology/physical", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "topology:view") {
			return
		}
		writeJSON(w, http.StatusOK, topologyService.PhysicalGraph(r.URL.Query().Get("environment")))
	})
}

func findNetworkAnomaly(service *cmdb.Service, id string) (cmdb.NetworkAnomaly, bool) {
	for _, item := range service.NetworkAnomalies() {
		if item.ID == id {
			return item, true
		}
	}
	return cmdb.NetworkAnomaly{}, false
}

func firstHTTPAPIValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func syncNetworkAnomalyAlerts(cmdbService *cmdb.Service, monitorService *monitor.Service) (int, error) {
	items := []monitor.CMDBAnomalyAlert{}
	for _, item := range cmdbService.NetworkAnomalies() {
		if item.Status == "resolved" {
			continue
		}
		items = append(items, monitor.CMDBAnomalyAlert{ID: item.ID, Title: item.Title, Severity: item.Severity, TargetID: item.AssetID, Target: firstHTTPAPIValue(item.IP, item.DeviceName, item.AssetName), Message: item.Detail, Owner: item.AcknowledgedBy})
	}
	return monitorService.SyncCMDBAnomalies(items)
}
