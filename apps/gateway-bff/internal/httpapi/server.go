package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/aiops"
	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/credentials"
	"cmdb/gateway-bff/internal/dashboard"
	"cmdb/gateway-bff/internal/discovery"
	"cmdb/gateway-bff/internal/idc"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/k8s"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/releases"
	"cmdb/gateway-bff/internal/system"
	"cmdb/gateway-bff/internal/tickets"
	"cmdb/gateway-bff/internal/toolbox"
	"cmdb/gateway-bff/internal/topology"
)

type Server struct {
	handler   http.Handler
	discovery *discovery.Service
	monitor   *monitor.Service
}

func NewServer() *Server {
	mux := http.NewServeMux()
	cmdbService := cmdb.NewService()
	discoveryService := discovery.NewServiceWithCMDB(cmdbService)
	monitorService := monitor.NewService()
	service := dashboard.NewServiceWithRuntime(cmdbService, discoveryService, monitorService)
	platformMetrics := newMetrics(cmdbService, discoveryService, monitorService)
	jobsService := jobs.NewService()
	go jobsService.RunScheduler(context.Background())
	ticketsService := tickets.NewService()
	toolboxService := toolbox.NewService()
	systemService := system.NewService()
	aiopsService := aiops.NewService()
	releasesService := releases.NewService()
	topologyService := topology.NewServiceWithCMDB(cmdbService)
	authService := auth.NewService()
	idcService := idc.NewService()
	credentialsService := credentials.NewService()
	k8sService := k8s.NewService(cmdbService)
	auditService := audit.NewService()
	discoveryService.SetCredentialResolver(func(id string) (string, string, error) {
		m, err := credentialsService.Resolve(id)
		return m.Username, m.Secret, err
	})
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/ready", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.Handle("GET /metrics", platformMetrics.Handler())
	mux.HandleFunc("GET /api/v1/monitor/service-discovery/node-exporter", func(w http.ResponseWriter, r *http.Request) {
		if !discoveryService.AuthorizeAgentToken(bearerToken(r)) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED"})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, cmdbService.PrometheusTargetGroups(os.Getenv("CMDB_NODE_EXPORTER_PORT")))
	})
	mux.HandleFunc("GET /api/v1/monitor/service-discovery/snmp", func(w http.ResponseWriter, r *http.Request) {
		if !discoveryService.AuthorizeAgentToken(bearerToken(r)) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED"})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, cmdbService.PrometheusSNMPTargetGroups())
	})
	mux.HandleFunc("POST /api/v1/monitor/alerts/webhook", func(w http.ResponseWriter, r *http.Request) {
		var payload monitor.WebhookPayload
		if err := decodeJSONLenient(r, &payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_ALERTMANAGER_PAYLOAD"})
			return
		}
		accepted, err := monitorService.ReceiveWebhook(payload)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "ALERT_PERSISTENCE_FAILED"})
			return
		}
		syncAssetHealthFromAlerts(cmdbService, monitorService, payload)
		writeJSON(w, http.StatusAccepted, map[string]int{"accepted": accepted})
	})
	mux.HandleFunc("POST /api/v1/agent/report", func(w http.ResponseWriter, r *http.Request) {
		var in discovery.AgentReport
		if decodeJSON(r, &in) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REPORT"})
			return
		}
		asset, err := discoveryService.Report(bearerToken(r), in)
		if err != nil {
			if err.Error() == "unauthorized" {
				writeJSON(w, 401, map[string]string{"code": "INVALID_AGENT_TOKEN"})
				return
			}
			writeJSON(w, 422, map[string]string{"code": "INVALID_AGENT_REPORT"})
			return
		}
		writeJSON(w, 200, asset)
	})
	mux.HandleFunc("GET /api/v1/dashboard/overview", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, service.Overview()) })
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "请求格式不正确"})
			return
		}
		session, err := authService.Login(input.Username, input.Password)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "INVALID_CREDENTIALS", "message": "用户名或密码错误"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "登录失败"})
			return
		}
		writeJSON(w, http.StatusOK, session)
	})
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		user, err := authService.CurrentUser(bearerToken(r))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED", "message": "登录状态已失效"})
			return
		}
		writeJSON(w, http.StatusOK, user)
	})
	mux.HandleFunc("GET /api/v1/auth/modules", func(w http.ResponseWriter, r *http.Request) {
		modules, err := authService.Modules(bearerToken(r))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED", "message": "登录状态已失效"})
			return
		}
		writeJSON(w, http.StatusOK, modules)
	})
	mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		authService.Logout(bearerToken(r))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/cmdb/analytics", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		writeJSON(w, http.StatusOK, cmdbService.Analytics())
	})
	mux.HandleFunc("GET /api/v1/cmdb/models/{code}/template", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		header, err := cmdbService.ModelTemplateCSV(r.PathValue("code"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND"})
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		_, _ = w.Write([]byte(header))
	})
	mux.HandleFunc("GET /api/v1/cmdb/models", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		writeJSON(w, http.StatusOK, cmdbService.Models())
	})
	mux.HandleFunc("POST /api/v1/cmdb/models", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var in cmdb.ModelInput
		if decodeJSON(r, &in) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		item, err := cmdbService.CreateModel(in)
		if errors.Is(err, cmdb.ErrConflict) {
			writeJSON(w, 409, map[string]string{"code": "MODEL_CONFLICT", "message": "模型编码已存在"})
			return
		}
		if err != nil {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR", "message": "模型或字段定义无效"})
			return
		}
		writeJSON(w, 201, item)
	})
	mux.HandleFunc("PUT /api/v1/cmdb/models/{code}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var in cmdb.ModelInput
		if decodeJSON(r, &in) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		item, err := cmdbService.UpdateModel(r.PathValue("code"), in)
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "MODEL_NOT_FOUND"})
			return
		}
		if err != nil {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		writeJSON(w, 200, item)
	})
	mux.HandleFunc("POST /api/v1/cmdb/models/{code}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		item, err := cmdbService.ToggleModel(r.PathValue("code"))
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "MODEL_NOT_FOUND"})
			return
		}
		writeJSON(w, 200, item)
	})
	mux.HandleFunc("GET /api/v1/cmdb/kubernetes", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		writeJSON(w, http.StatusOK, cmdbService.KubernetesInventory())
	})
	mux.HandleFunc("GET /api/v1/cmdb/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		writeJSON(w, http.StatusOK, cmdbService.Summary())
	})
	mux.HandleFunc("GET /api/v1/cmdb/assets", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		query := r.URL.Query()
		page, _ := strconv.Atoi(query.Get("page"))
		pageSize, _ := strconv.Atoi(query.Get("pageSize"))
		writeJSON(w, http.StatusOK, cmdbService.ListAssets(cmdb.AssetQuery{Search: query.Get("q"), Type: query.Get("type"), Status: query.Get("status"), ProjectGroup: query.Get("projectGroup"), Page: page, PageSize: pageSize}))
	})
	mux.HandleFunc("POST /api/v1/cmdb/assets", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input cmdb.CreateAssetInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "请求格式不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		created, err := cmdbService.CreateAsset(input, user.Username)
		if errors.Is(err, cmdb.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "必填字段不完整或模型无效"})
			return
		}
		if errors.Is(err, cmdb.ErrConflict) {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "ASSET_CONFLICT", "message": "资产编号已存在"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "CREATE_FAILED", "message": err.Error()})
			return
		}
		w.Header().Set("Location", "/api/v1/cmdb/assets/"+created.ID)
		writeJSON(w, http.StatusCreated, created)
	})
	mux.HandleFunc("GET /api/v1/cmdb/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		asset, err := cmdbService.GetAsset(r.PathValue("id"))
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND", "message": "资产不存在"})
			return
		}
		writeJSON(w, http.StatusOK, asset)
	})
	mux.HandleFunc("PATCH /api/v1/cmdb/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input cmdb.UpdateAssetInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "请求格式不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		updated, err := cmdbService.UpdateAsset(r.PathValue("id"), input, user.Username)
		if errors.Is(err, cmdb.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "资产名称和负责人不能为空"})
			return
		}
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND", "message": "资产不存在"})
			return
		}
		if errors.Is(err, cmdb.ErrSlotTaken) {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "IDC_SLOT_TAKEN", "message": err.Error()})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "UPDATE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, updated)
	})
	mux.HandleFunc("GET /api/v1/cmdb/assets/{id}/history", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		entries, err := cmdbService.History(r.PathValue("id"))
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND", "message": "资产不存在"})
			return
		}
		writeJSON(w, http.StatusOK, entries)
	})
	mux.HandleFunc("POST /api/v1/cmdb/imports", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var rows []cmdb.CreateAssetInput
		if err := decodeJSON(r, &rows); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "导入数据格式不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		upsert := r.URL.Query().Get("mode") == "upsert"
		writeJSON(w, http.StatusOK, cmdbService.ImportAssets(rows, upsert, user.Username))
	})
	mux.HandleFunc("GET /api/v1/agent/install/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": discovery.AgentBundleVersion()})
	})
	mux.HandleFunc("GET /api/v1/agent/install/linux-amd64", func(w http.ResponseWriter, r *http.Request) {
		if !discoveryService.AuthorizeAgentToken(bearerToken(r)) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED"})
			return
		}
		path := os.Getenv("CMDB_AGENT_BUNDLE_FILE")
		if path == "" {
			path = "/cmdb-agent"
		}
		data, err := os.ReadFile(path)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "AGENT_BUNDLE_MISSING"})
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=cmdb-agent")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("GET /api/v1/agent/install/node-exporter/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": discovery.NodeExporterVersion()})
	})
	mux.HandleFunc("GET /api/v1/agent/install/node-exporter/linux-amd64", func(w http.ResponseWriter, r *http.Request) {
		if !discoveryService.AuthorizeAgentToken(bearerToken(r)) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED"})
			return
		}
		path := os.Getenv("CMDB_NODE_EXPORTER_BUNDLE_FILE")
		if path == "" {
			path = "/node_exporter"
		}
		data, err := os.ReadFile(path)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "NODE_EXPORTER_BUNDLE_MISSING"})
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=node_exporter")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("POST /api/v1/discovery/exporter-install", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var in discovery.ExporterInstallInput
		if err := decodeJSON(r, &in); err != nil || len(in.Hosts) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_INPUT"})
			return
		}
		result, err := discoveryService.ExporterBatchInstall(in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "EXPORTER_INSTALL_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/discovery/exporter-uninstall", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var in struct {
			Hosts []string `json:"hosts"`
			Port  int      `json:"port"`
		}
		if err := decodeJSON(r, &in); err != nil || len(in.Hosts) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_INPUT"})
			return
		}
		result, err := discoveryService.ExporterBatchUninstall(in.Hosts, in.Port)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "EXPORTER_UNINSTALL_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/discovery/agent-uninstall", func(w http.ResponseWriter, r *http.Request) {
		if !discoveryService.AuthorizeAgentToken(bearerToken(r)) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED"})
			return
		}
		var in discovery.AgentInstallInput
		if err := decodeJSON(r, &in); err != nil || len(in.Hosts) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_INPUT"})
			return
		}
		res, err := discoveryService.AgentBatchUninstall(in.Hosts, in.Port)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "UNINSTALL_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/v1/discovery/agent-install", func(w http.ResponseWriter, r *http.Request) {
		if !discoveryService.AuthorizeAgentToken(bearerToken(r)) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED"})
			return
		}
		var in discovery.AgentInstallInput
		if err := decodeJSON(r, &in); err != nil || len(in.Hosts) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_INPUT"})
			return
		}
		res, err := discoveryService.AgentBatchInstall(in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "INSTALL_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/v1/discovery/scan-snmp", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var in discovery.SNMPScanInput
		if err := decodeJSON(r, &in); err != nil || len(in.CIDRs) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_SCAN_INPUT"})
			return
		}
		res, err := discoveryService.ScanSNMP(r.Context(), in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "SCAN_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/v1/discovery/scan-ssh", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var in discovery.SSHScanInput
		if err := decodeJSON(r, &in); err != nil || len(in.CIDRs) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_SCAN_INPUT"})
			return
		}
		result, err := discoveryService.ScanSSH(r.Context(), in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "SCAN_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/discovery/scan-node-exporter", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var in discovery.NodeExporterScanInput
		if err := decodeJSON(r, &in); err != nil || len(in.CIDRs) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_SCAN_INPUT"})
			return
		}
		result, err := discoveryService.ScanNodeExporter(r.Context(), in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "SCAN_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("PATCH /api/v1/idc/racks/{rackId}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var in idc.UpdateRack
		if err := decodeJSON(r, &in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		rack, err := idcService.UpdateRack(r.PathValue("rackId"), in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "UPDATE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rack)
	})
	mux.HandleFunc("PATCH /api/v1/idc/modules/{moduleId}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var in idc.UpdateModule
		if err := decodeJSON(r, &in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		mod, err := idcService.UpdateModule(r.PathValue("moduleId"), in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "UPDATE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, mod)
	})
	mux.HandleFunc("DELETE /api/v1/idc/racks/{rackId}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		err := idcService.DeleteRack(r.PathValue("rackId"))
		if errors.Is(err, idc.ErrOccupied) {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "RACK_OCCUPIED", "message": "该机柜存在占用U位，不能删除"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/v1/idc/modules/{moduleId}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		if err := idcService.DeleteModule(r.PathValue("moduleId")); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/v1/idc/rooms/{roomId}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		if err := idcService.DeleteRoom(r.PathValue("roomId")); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/discovery/pending", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		items, err := discoveryService.PendingList()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "PENDING_LIST_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, items)
	})
	mux.HandleFunc("POST /api/v1/discovery/pending/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		status, err := discoveryService.ApprovePending(r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "APPROVE_FAILED", "message": err.Error()})
			return
		}
		auditService.Record(user.Username, "discovery.approve", r.PathValue("id"), "status="+status)
		writeJSON(w, http.StatusOK, map[string]string{"status": status})
	})
	mux.HandleFunc("POST /api/v1/discovery/pending/{id}/reject", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		if err := discoveryService.RejectPending(r.PathValue("id")); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "REJECT_FAILED", "message": err.Error()})
			return
		}
		auditService.Record(user.Username, "discovery.reject", r.PathValue("id"), "")
		writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
	})
	mux.HandleFunc("GET /api/v1/audit", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		entries, err := auditService.List(limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "AUDIT_ERROR", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, entries)
	})
	mux.HandleFunc("GET /api/v1/credentials", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		list, err := credentialsService.List()
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "VAULT_UNAVAILABLE", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("POST /api/v1/credentials", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var in struct {
			Name     string `json:"name"`
			Kind     string `json:"kind"`
			Username string `json:"username"`
			Secret   string `json:"secret"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		cred, err := credentialsService.Create(in.Name, in.Kind, in.Username, in.Secret)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "CREATE_FAILED", "message": err.Error()})
			return
		}
		if user, _ := authService.CurrentUser(bearerToken(r)); user.Username != "" {
			auditService.Record(user.Username, "credential.create", cred.ID, cred.Kind+"/"+cred.Name)
		}
		writeJSON(w, http.StatusCreated, cred)
	})
	mux.HandleFunc("DELETE /api/v1/credentials/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		if err := credentialsService.Delete(r.PathValue("id")); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "DELETE_FAILED", "message": err.Error()})
			return
		}
		if user, _ := authService.CurrentUser(bearerToken(r)); user.Username != "" {
			auditService.Record(user.Username, "credential.delete", r.PathValue("id"), "")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/idc/rooms", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		rooms, err := idcService.ListRooms()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "IDC_ERROR", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rooms)
	})
	mux.HandleFunc("POST /api/v1/idc/rooms", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var in idc.CreateRoom
		if err := decodeJSON(r, &in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		room, err := idcService.CreateRoom(in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "CREATE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, room)
	})
	mux.HandleFunc("POST /api/v1/idc/rooms/{roomId}/modules", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var in idc.CreateModule
		in.RoomID = r.PathValue("roomId")
		if err := decodeJSON(r, &in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		module, err := idcService.AddModule(in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "CREATE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, module)
	})
	mux.HandleFunc("POST /api/v1/idc/racks", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var in idc.CreateRack
		if err := decodeJSON(r, &in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		rack, err := idcService.AddRack(in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "CREATE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, rack)
	})
	mux.HandleFunc("POST /api/v1/discovery/ingest", func(w http.ResponseWriter, r *http.Request) {
		if !discoveryService.AuthorizeAgentToken(bearerToken(r)) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED"})
			return
		}
		var in discovery.IngestInput
		if err := decodeJSON(r, &in); err != nil || len(in.Items) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_DISCOVERY_PAYLOAD"})
			return
		}
		result, err := discoveryService.Ingest(in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "INGEST_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/discovery/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		writeJSON(w, http.StatusOK, discoveryService.Summary())
	})
	mux.HandleFunc("GET /api/v1/topology/graph", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "topology:view") {
			return
		}
		writeJSON(w, http.StatusOK, topologyService.Graph(r.URL.Query().Get("environment")))
	})
	mux.HandleFunc("GET /api/v1/monitor/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		writeJSON(w, 200, monitorService.Summary())
	})
	mux.HandleFunc("GET /api/v1/monitor/coverage", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		assets := cmdbService.MonitoringAssets()
		expected := make([]monitor.ExpectedHost, 0, len(assets))
		for _, asset := range assets {
			expected = append(expected, monitor.ExpectedHost{AssetID: asset.ID, Name: asset.Name, IP: asset.IP, Environment: asset.Environment, ProjectGroup: asset.ProjectGroup, AssetStatus: asset.Status})
		}
		writeJSON(w, http.StatusOK, monitorService.Coverage(expected))
	})
	mux.HandleFunc("GET /api/v1/monitor/metrics", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		writeJSON(w, 200, monitorService.Metrics())
	})
	mux.HandleFunc("GET /api/v1/monitor/hosts/{assetId}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		assetID := r.PathValue("assetId")
		if _, err := cmdbService.GetAsset(assetID); errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND"})
			return
		}
		writeJSON(w, http.StatusOK, monitorService.Host(assetID))
	})
	mux.HandleFunc("POST /api/v1/monitor/hosts/{assetId}/install-exporter", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		if info := jobsService.ExecutorInfo(); info.Mode != "ansible-runner" || !info.Ready {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "RUNNER_UNAVAILABLE", "message": "Ansible Runner尚未接入，不能执行真实安装"})
			return
		}
		asset, err := cmdbService.GetAsset(r.PathValue("assetId"))
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND"})
			return
		}
		if asset.Source != "linux-agent" || asset.Status != "online" || strings.TrimSpace(asset.IP) == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "ASSET_NOT_ELIGIBLE", "message": "仅允许对在线Linux Agent资产安装采集器"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		job, err := jobsService.Create(jobs.CreateInput{TemplateID: "tpl-node-exporter", Targets: []string{asset.ID}, Operator: user.Username, TimeoutSeconds: 300})
		if err == nil {
			job, err = jobsService.Run(job.ID)
		}
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "JOB_CREATE_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	})
	mux.HandleFunc("GET /api/v1/monitor/alerts", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		writeJSON(w, 200, monitorService.Alerts())
	})
	mux.HandleFunc("GET /api/v1/monitor/rules", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		rules, err := monitorService.Rules()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"message": "Prometheus rules unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, rules)
	})
	mux.HandleFunc("GET /api/v1/monitor/rules/managed", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		rules, err := monitorService.ManagedAlertRules()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rules)
	})
	mux.HandleFunc("POST /api/v1/monitor/rules/managed", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		var input monitor.CreateManagedAlertRuleInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid alert rule"})
			return
		}
		item, err := monitorService.CreateManagedAlertRule(r.Context(), input)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		if user, err := authService.CurrentUser(bearerToken(r)); err == nil {
			auditService.Record(user.Username, "monitor.rule.create", item.ID, item.Group+"/"+item.Name)
		}
		writeJSON(w, http.StatusCreated, item)
	})
	mux.HandleFunc("PUT /api/v1/monitor/rules/managed/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		var input monitor.CreateManagedAlertRuleInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid alert rule"})
			return
		}
		item, err := monitorService.UpdateManagedAlertRule(r.Context(), r.PathValue("id"), input)
		if errors.Is(err, monitor.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "alert rule not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		if user, err := authService.CurrentUser(bearerToken(r)); err == nil {
			auditService.Record(user.Username, "monitor.rule.update", item.ID, item.Group+"/"+item.Name)
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/monitor/rules/managed/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		item, err := monitorService.ToggleManagedAlertRule(r.Context(), r.PathValue("id"))
		if errors.Is(err, monitor.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "alert rule not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		if user, err := authService.CurrentUser(bearerToken(r)); err == nil {
			auditService.Record(user.Username, "monitor.rule.toggle", item.ID, "enabled="+strconv.FormatBool(item.Enabled))
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("DELETE /api/v1/monitor/rules/managed/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		if err := monitorService.DeleteManagedAlertRule(r.Context(), r.PathValue("id")); errors.Is(err, monitor.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "alert rule not found"})
			return
		} else if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		if user, err := authService.CurrentUser(bearerToken(r)); err == nil {
			auditService.Record(user.Username, "monitor.rule.delete", r.PathValue("id"), "")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/monitor/silences", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		silences, err := monitorService.Silences()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"message": "Alertmanager silences unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, silences)
	})
	mux.HandleFunc("POST /api/v1/monitor/silences", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		var input monitor.CreateSilenceInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid silence"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		input.CreatedBy = u.Username
		id, err := monitorService.CreateSilence(input)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
	})
	mux.HandleFunc("DELETE /api/v1/monitor/silences/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		if err := monitorService.ExpireSilence(r.PathValue("id")); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"message": "expire silence failed"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/monitor/routes", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		writeJSON(w, http.StatusOK, monitorService.RoutingRules())
	})
	mux.HandleFunc("GET /api/v1/monitor/escalations", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		writeJSON(w, http.StatusOK, monitorService.EscalationPolicies())
	})
	mux.HandleFunc("POST /api/v1/monitor/escalations", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		var input monitor.CreateEscalationPolicyInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid escalation policy"})
			return
		}
		item, err := monitorService.CreateEscalationPolicy(input)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, item)
	})
	mux.HandleFunc("POST /api/v1/monitor/escalations/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		item, err := monitorService.ToggleEscalationPolicy(r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "policy not found"})
			return
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("DELETE /api/v1/monitor/escalations/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		if err := monitorService.DeleteEscalationPolicy(r.PathValue("id")); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "policy not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/monitor/routes", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		var input monitor.CreateRoutingRuleInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid routing rule"})
			return
		}
		rule, err := monitorService.CreateRoutingRule(input)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, rule)
	})
	mux.HandleFunc("POST /api/v1/monitor/routes/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		rule, err := monitorService.ToggleRoutingRule(r.PathValue("id"))
		if errors.Is(err, monitor.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "route not found"})
			return
		}
		writeJSON(w, http.StatusOK, rule)
	})
	mux.HandleFunc("DELETE /api/v1/monitor/routes/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		if err := monitorService.DeleteRoutingRule(r.PathValue("id")); errors.Is(err, monitor.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "route not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/monitor/channels", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		writeJSON(w, http.StatusOK, monitorService.NotificationChannels())
	})
	mux.HandleFunc("GET /api/v1/monitor/deliveries", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		writeJSON(w, http.StatusOK, monitorService.NotificationDeliveries())
	})
	mux.HandleFunc("GET /api/v1/monitor/deliveries/stats", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		writeJSON(w, http.StatusOK, monitorService.NotificationDeliveryStats())
	})
	mux.HandleFunc("GET /api/v1/monitor/queue/status", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		writeJSON(w, http.StatusOK, monitorService.QueueStatus())
	})
	mux.HandleFunc("GET /api/v1/monitor/queue/dead-letters", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		writeJSON(w, http.StatusOK, monitorService.DeadLetters())
	})
	mux.HandleFunc("POST /api/v1/monitor/queue/dead-letters/{id}/replay", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		if err := monitorService.ReplayDeadLetter(r.PathValue("id")); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "dead letter replay failed"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /api/v1/monitor/queue/dead-letters/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		if err := monitorService.DeleteDeadLetter(r.PathValue("id")); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "dead letter not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/monitor/queue/dead-letters/bulk", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		var input struct {
			IDs    []string `json:"ids"`
			Action string   `json:"action"`
		}
		if decodeJSON(r, &input) != nil || len(input.IDs) == 0 || (input.Action != "replay" && input.Action != "delete") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid bulk request"})
			return
		}
		var success int
		var failed []string
		if input.Action == "replay" {
			success, failed = monitorService.ReplayDeadLetters(input.IDs)
		} else {
			success, failed = monitorService.DeleteDeadLetters(input.IDs)
		}
		writeJSON(w, http.StatusOK, map[string]any{"succeeded": success, "failed": failed})
	})
	mux.HandleFunc("POST /api/v1/monitor/channels", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		var input monitor.CreateChannelInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid channel"})
			return
		}
		channel, err := monitorService.CreateNotificationChannel(input)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, channel)
	})
	mux.HandleFunc("POST /api/v1/monitor/channels/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		channel, err := monitorService.ToggleNotificationChannel(r.PathValue("id"))
		if errors.Is(err, monitor.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "channel not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "channel update failed"})
			return
		}
		writeJSON(w, http.StatusOK, channel)
	})
	mux.HandleFunc("POST /api/v1/monitor/channels/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		if err := monitorService.TestNotificationChannel(r.PathValue("id")); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"message": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PATCH /api/v1/monitor/channels/{id}/template", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		var input struct {
			Template string `json:"template"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid template"})
			return
		}
		channel, err := monitorService.UpdateNotificationTemplate(r.PathValue("id"), input.Template)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, channel)
	})
	mux.HandleFunc("DELETE /api/v1/monitor/channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		if err := monitorService.DeleteNotificationChannel(r.PathValue("id")); errors.Is(err, monitor.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "channel not found"})
			return
		} else if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "channel delete failed"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/monitor/alerts/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		events, err := monitorService.Events(r.PathValue("id"))
		if errors.Is(err, monitor.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "alert not found"})
			return
		}
		writeJSON(w, http.StatusOK, events)
	})
	mux.HandleFunc("POST /api/v1/monitor/alerts/{id}/acknowledge", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		a, err := monitorService.Acknowledge(r.PathValue("id"), u.Username)
		if errors.Is(err, monitor.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "ALERT_NOT_FOUND"})
			return
		}
		writeJSON(w, 200, a)
	})
	mux.HandleFunc("POST /api/v1/monitor/alerts/{id}/resolve", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		a, err := monitorService.Resolve(r.PathValue("id"), u.Username)
		if errors.Is(err, monitor.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "ALERT_NOT_FOUND"})
			return
		}
		writeJSON(w, 200, a)
	})
	mux.HandleFunc("POST /api/v1/monitor/alerts/{id}/silence", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		a, err := monitorService.Silence(r.PathValue("id"), u.Username)
		if errors.Is(err, monitor.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "ALERT_NOT_FOUND"})
			return
		}
		writeJSON(w, http.StatusOK, a)
	})
	mux.HandleFunc("POST /api/v1/monitor/alerts/{id}/assign", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		var input struct {
			Owner string `json:"owner"`
		}
		if decodeJSON(r, &input) != nil || strings.TrimSpace(input.Owner) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "owner is required"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		a, err := monitorService.Assign(r.PathValue("id"), input.Owner, u.Username)
		if errors.Is(err, monitor.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "ALERT_NOT_FOUND"})
			return
		}
		writeJSON(w, http.StatusOK, a)
	})
	mux.HandleFunc("POST /api/v1/monitor/alerts/bulk", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:manage") {
			return
		}
		var input struct {
			IDs    []string `json:"ids"`
			Action string   `json:"action"`
		}
		if decodeJSON(r, &input) != nil || len(input.IDs) == 0 || (input.Action != "acknowledge" && input.Action != "resolve") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid bulk request"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		failed := make([]string, 0)
		succeeded := 0
		for _, id := range input.IDs {
			var err error
			if input.Action == "acknowledge" {
				_, err = monitorService.Acknowledge(id, u.Username)
			} else {
				_, err = monitorService.Resolve(id, u.Username)
			}
			if err != nil {
				failed = append(failed, id)
			} else {
				succeeded++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"succeeded": succeeded, "failed": failed})
	})
	mux.HandleFunc("GET /api/v1/topology/nodes/{id}/impact", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "topology:view") {
			return
		}
		result, err := topologyService.Impact(r.PathValue("id"))
		if errors.Is(err, topology.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "NODE_NOT_FOUND"})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/discovery/agents", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		writeJSON(w, http.StatusOK, discoveryService.AgentList(r.URL.Query().Get("q"), r.URL.Query().Get("status")))
	})
	mux.HandleFunc("GET /api/v1/discovery/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		agent, ok := discoveryService.AgentDetail(r.PathValue("id"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND"})
			return
		}
		writeJSON(w, http.StatusOK, agent)
	})
	mux.HandleFunc("GET /api/v1/k8s/status", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		writeJSON(w, http.StatusOK, k8sService.Status(r.Context()))
	})
	mux.HandleFunc("POST /api/v1/k8s/sync", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		result, err := k8sService.Sync(r.Context(), user.Username)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "K8S_SYNC_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-operations", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		writeJSON(w, http.StatusOK, discoveryService.RemoteOperations())
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-target-options", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		writeJSON(w, http.StatusOK, cmdbService.RemoteTargetOptions())
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-targets/resolve", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		var input struct {
			ProjectGroups []string `json:"projectGroups"`
			Tags          []string `json:"tags"`
			AssetIDs      []string `json:"assetIds"`
			IPs           []string `json:"ips"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid target selectors"})
			return
		}
		assets := cmdbService.ResolveRemoteTargets(input.ProjectGroups, input.Tags, input.AssetIDs, input.IPs)
		targets := make([]string, 0, len(assets))
		seen := map[string]bool{}
		for _, asset := range assets {
			if asset.IP != "" && !seen[asset.IP] {
				seen[asset.IP] = true
				targets = append(targets, asset.IP)
			}
		}
		if len(targets) == 0 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "没有匹配到可执行目标主机"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"targets": targets, "assets": assets})
	})
	mux.HandleFunc("GET /api/v1/discovery/access-grants", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		writeJSON(w, http.StatusOK, discoveryService.AccessGrants())
	})
	mux.HandleFunc("POST /api/v1/discovery/access-grants", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input discovery.AccessGrantInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, 400, map[string]string{"message": "invalid access grant"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.CreateAccessGrant(input, user.Username)
		if err != nil {
			writeJSON(w, 422, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.grant.create", item.ID, remoteGrantAuditDetail(item))
		writeJSON(w, 201, item)
	})
	mux.HandleFunc("POST /api/v1/discovery/access-grants/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.ToggleAccessGrant(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 404, map[string]string{"message": "access grant not found"})
			return
		}
		auditService.Record(user.Username, "remote.grant.toggle", item.ID, remoteGrantAuditDetail(item)+" | enabled="+strconv.FormatBool(item.Enabled))
		writeJSON(w, 200, item)
	})
	mux.HandleFunc("DELETE /api/v1/discovery/access-grants/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		if err := discoveryService.DeleteAccessGrant(r.PathValue("id")); err != nil {
			writeJSON(w, 404, map[string]string{"message": "access grant not found"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "remote.grant.delete", r.PathValue("id"), "")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-sessions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		writeJSON(w, http.StatusOK, discoveryService.RemoteSessions(user.Username, user.Roles))
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-sessions/history", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		writeJSON(w, http.StatusOK, discoveryService.RemoteSessionHistory(user.Username, user.Roles))
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-sessions/{id}/replay", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.RemoteSessionReplay(r.PathValue("id"), user.Username, user.Roles)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "remote session not found"})
			return
		}
		writeJSON(w, http.StatusOK, item)
	})
	registerRemoteTerminalRoutes(mux, authService, auditService, discoveryService)
	mux.HandleFunc("POST /api/v1/discovery/remote-sessions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input struct {
			AssetID      string `json:"assetId"`
			CredentialID string `json:"credentialId"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, 400, map[string]string{"message": "invalid remote session"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.CreateRemoteSession(input.AssetID, input.CredentialID, user.Username, user.Roles)
		if err != nil {
			writeJSON(w, 422, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.session.create", item.ID, item.AssetName)
		writeJSON(w, 201, item)
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-sessions/{id}/command", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input struct {
			Command string `json:"command"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, 400, map[string]string{"message": "invalid command"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.ExecuteRemoteSession(r.PathValue("id"), input.Command, user.Username, user.Roles)
		if err != nil {
			writeJSON(w, 422, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.session.command", r.PathValue("id"), input.Command)
		writeJSON(w, 200, item)
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-sessions/{id}/upload", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		if err := r.ParseMultipartForm(52 << 20); err != nil {
			writeJSON(w, 400, map[string]string{"message": "invalid upload"})
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, 400, map[string]string{"message": "file is required"})
			return
		}
		defer file.Close()
		remotePath := r.FormValue("path")
		user, _ := authService.CurrentUser(bearerToken(r))
		if err = discoveryService.UploadRemoteFile(r.PathValue("id"), remotePath, file, header.Size, user.Username, user.Roles); err != nil {
			writeJSON(w, 422, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.session.upload", r.PathValue("id"), remotePath)
		writeJSON(w, 200, map[string]string{"status": "ok", "path": remotePath})
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-sessions/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		remotePath := r.URL.Query().Get("path")
		content, err := discoveryService.DownloadRemoteFile(r.PathValue("id"), remotePath, user.Username, user.Roles)
		if err != nil {
			writeJSON(w, 422, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.session.download", r.PathValue("id"), remotePath)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(path.Base(remotePath)))
		_, _ = w.Write(content)
	})
	mux.HandleFunc("DELETE /api/v1/discovery/remote-sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		if err := discoveryService.CloseRemoteSession(r.PathValue("id"), user.Username, user.Roles); err != nil {
			writeJSON(w, 422, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.session.close", r.PathValue("id"), "")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-host-keys", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		writeJSON(w, http.StatusOK, discoveryService.RemoteHostKeys())
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-host-keys/probe", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input struct {
			AssetID      string `json:"assetId"`
			CredentialID string `json:"credentialId"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, 400, map[string]string{"message": "invalid host key probe"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		probe, err := discoveryService.ProbeRemoteHostKey(input.AssetID, input.CredentialID, user.Username, user.Roles)
		if err != nil {
			writeJSON(w, 422, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, 200, probe)
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-host-keys", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input discovery.RemoteHostKeyInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, 400, map[string]string{"message": "invalid host key"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.TrustRemoteHostKey(input, user.Username)
		if err != nil {
			writeJSON(w, 422, map[string]string{"message": err.Error()})
			return
		}
		auditService.Record(user.Username, "remote.hostkey.trust", item.AssetID, item.Fingerprint)
		writeJSON(w, 201, item)
	})
	mux.HandleFunc("DELETE /api/v1/discovery/remote-host-keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		if err := discoveryService.DeleteRemoteHostKey(r.PathValue("id")); err != nil {
			writeJSON(w, 404, map[string]string{"message": "host key not found"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "remote.hostkey.delete", r.PathValue("id"), "")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-executions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		writeJSON(w, http.StatusOK, discoveryService.RemoteExecutions())
	})
	mux.HandleFunc("GET /api/v1/discovery/remote-executions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		execution, err := discoveryService.RemoteExecution(r.PathValue("id"))
		if errors.Is(err, discovery.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "REMOTE_EXECUTION_NOT_FOUND"})
			return
		}
		writeJSON(w, http.StatusOK, execution)
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-executions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var in discovery.RemoteExecutionInput
		if err := decodeJSON(r, &in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REMOTE_EXECUTION"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		in.RequestedBy = user.Username
		execution, err := discoveryService.CreateRemoteExecution(in)
		if errors.Is(err, discovery.ErrRemoteValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "INVALID_REMOTE_EXECUTION", "message": "操作类型、目标主机或凭据无效"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "REMOTE_EXECUTION_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, execution)
	})
	mux.HandleFunc("POST /api/v1/discovery/remote-executions/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		execution, err := discoveryService.ApproveRemoteExecution(r.PathValue("id"), user.Username)
		if errors.Is(err, discovery.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "REMOTE_EXECUTION_NOT_FOUND"})
			return
		}
		if errors.Is(err, discovery.ErrRemoteValidation) {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "INVALID_REMOTE_EXECUTION_STATE"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "REMOTE_EXECUTION_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, execution)
	})
	mux.HandleFunc("GET /api/v1/discovery/tasks", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		writeJSON(w, http.StatusOK, discoveryService.Tasks())
	})
	mux.HandleFunc("GET /api/v1/discovery/tasks/{id}/results", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		detail, err := discoveryService.TaskDetail(r.PathValue("id"))
		if errors.Is(err, discovery.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "TASK_NOT_FOUND"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TASK_RESULT_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, detail)
	})
	mux.HandleFunc("POST /api/v1/discovery/tasks", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var input discovery.CreateTaskInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, 400, map[string]string{"message": "请求格式错误"})
			return
		}
		task, err := discoveryService.CreateTask(input)
		if err != nil {
			writeJSON(w, 422, map[string]string{"message": "名称和来源不能为空"})
			return
		}
		writeJSON(w, 201, task)
	})
	mux.HandleFunc("POST /api/v1/discovery/tasks/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		task, err := discoveryService.RunTask(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 404, map[string]string{"message": "任务不存在"})
			return
		}
		writeJSON(w, 200, task)
	})
	mux.HandleFunc("POST /api/v1/discovery/tasks/{id}/reconcile", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		task, err := discoveryService.Reconcile(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 404, map[string]string{"message": "任务不存在"})
			return
		}
		writeJSON(w, 200, task)
	})
	mux.HandleFunc("GET /api/v1/jobs/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		writeJSON(w, 200, jobsService.Summary())
	})
	mux.HandleFunc("GET /api/v1/jobs/executor", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		writeJSON(w, 200, jobsService.ExecutorInfo())
	})
	mux.HandleFunc("GET /api/v1/jobs/templates", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		writeJSON(w, 200, jobsService.Templates())
	})
	mux.HandleFunc("GET /api/v1/jobs/playbooks", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		writeJSON(w, 200, jobsService.Playbooks())
	})
	mux.HandleFunc("POST /api/v1/jobs/playbooks", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		var input jobs.PlaybookInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := jobsService.CreatePlaybook(input, user.Username)
		if err != nil {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR", "message": "Playbook 内容或变量无效"})
			return
		}
		auditService.Record(user.Username, "job.playbook.create", item.ID, item.Name)
		writeJSON(w, 201, item)
	})
	mux.HandleFunc("PUT /api/v1/jobs/playbooks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		var input jobs.PlaybookInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		item, err := jobsService.UpdatePlaybook(r.PathValue("id"), input)
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "PLAYBOOK_NOT_FOUND"})
			return
		}
		if err != nil {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR", "message": "Playbook 内容或变量无效"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "job.playbook.update", item.ID, item.Name)
		writeJSON(w, 200, item)
	})
	mux.HandleFunc("POST /api/v1/jobs/playbooks/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		item, err := jobsService.TogglePlaybook(r.PathValue("id"))
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "PLAYBOOK_NOT_FOUND"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "job.playbook.toggle", item.ID, strconv.FormatBool(item.Enabled))
		writeJSON(w, 200, item)
	})
	mux.HandleFunc("DELETE /api/v1/jobs/playbooks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		if err := jobsService.DeletePlaybook(r.PathValue("id")); errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "PLAYBOOK_NOT_FOUND"})
			return
		} else if err != nil {
			writeJSON(w, 500, map[string]string{"code": "PLAYBOOK_DELETE_FAILED"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "job.playbook.delete", r.PathValue("id"), "")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/jobs/templates", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		var in jobs.TemplateInput
		if decodeJSON(r, &in) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		item, err := jobsService.CreateTemplate(in)
		if errors.Is(err, jobs.ErrUnsafeCommand) {
			writeJSON(w, 422, map[string]string{"code": "UNSAFE_COMMAND", "message": "命令命中高危拦截规则"})
			return
		}
		if err != nil {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		writeJSON(w, 201, item)
	})
	mux.HandleFunc("POST /api/v1/jobs/templates/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		item, err := jobsService.ToggleTemplate(r.PathValue("id"))
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "TEMPLATE_NOT_FOUND"})
			return
		}
		writeJSON(w, 200, item)
	})
	mux.HandleFunc("GET /api/v1/jobs/executions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		writeJSON(w, 200, jobsService.Jobs())
	})
	mux.HandleFunc("GET /api/v1/jobs/executions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		j, err := jobsService.Get(r.PathValue("id"))
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "JOB_NOT_FOUND"})
			return
		}
		writeJSON(w, 200, j)
	})
	mux.HandleFunc("GET /api/v1/jobs/schedules", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		writeJSON(w, 200, jobsService.Schedules())
	})
	mux.HandleFunc("POST /api/v1/jobs/schedules", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		var input jobs.ScheduleInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := jobsService.CreateSchedule(input, user.Username)
		if errors.Is(err, jobs.ErrValidation) {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"code": "SCHEDULE_CREATE_FAILED"})
			return
		}
		auditService.Record(user.Username, "job.schedule.create", item.ID, item.Name)
		writeJSON(w, 201, item)
	})
	mux.HandleFunc("PUT /api/v1/jobs/schedules/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		var input jobs.ScheduleInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		item, err := jobsService.UpdateSchedule(r.PathValue("id"), input)
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "SCHEDULE_NOT_FOUND"})
			return
		}
		if errors.Is(err, jobs.ErrValidation) {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "job.schedule.update", item.ID, item.Name)
		writeJSON(w, 200, item)
	})
	mux.HandleFunc("POST /api/v1/jobs/schedules/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		item, err := jobsService.ToggleSchedule(r.PathValue("id"))
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "SCHEDULE_NOT_FOUND"})
			return
		}
		if err != nil {
			writeJSON(w, 422, map[string]string{"code": "SCHEDULE_UPDATE_FAILED"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "job.schedule.toggle", item.ID, "enabled="+strconv.FormatBool(item.Enabled))
		writeJSON(w, 200, item)
	})
	mux.HandleFunc("POST /api/v1/jobs/schedules/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		job, err := jobsService.RunSchedule(r.PathValue("id"), user.Username, false)
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "SCHEDULE_NOT_FOUND"})
			return
		}
		if errors.Is(err, jobs.ErrValidation) {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"code": "SCHEDULE_RUN_FAILED", "message": err.Error()})
			return
		}
		auditService.Record(user.Username, "job.schedule.run", r.PathValue("id"), job.ID)
		writeJSON(w, 200, job)
	})
	mux.HandleFunc("DELETE /api/v1/jobs/schedules/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		if err := jobsService.DeleteSchedule(r.PathValue("id")); errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "SCHEDULE_NOT_FOUND"})
			return
		} else if err != nil {
			writeJSON(w, 500, map[string]string{"code": "SCHEDULE_DELETE_FAILED"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "job.schedule.delete", r.PathValue("id"), "")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/jobs/executions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		var in jobs.CreateInput
		if decodeJSON(r, &in) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		in.Operator = u.Username
		j, err := jobsService.Create(in)
		if errors.Is(err, jobs.ErrValidation) {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		writeJSON(w, 201, j)
	})
	mux.HandleFunc("POST /api/v1/jobs/executions/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		j, err := jobsService.Run(r.PathValue("id"))
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "JOB_NOT_FOUND"})
			return
		}
		if errors.Is(err, jobs.ErrInvalidState) {
			writeJSON(w, 409, map[string]string{"code": "INVALID_JOB_STATE"})
			return
		}
		writeJSON(w, 200, j)
	})
	mux.HandleFunc("POST /api/v1/jobs/executions/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		j, err := jobsService.Retry(r.PathValue("id"))
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "JOB_NOT_FOUND"})
			return
		}
		if errors.Is(err, jobs.ErrInvalidState) {
			writeJSON(w, 409, map[string]string{"code": "INVALID_JOB_STATE"})
			return
		}
		writeJSON(w, 200, j)
	})
	mux.HandleFunc("POST /api/v1/jobs/executions/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		j, err := jobsService.Cancel(r.PathValue("id"))
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "JOB_NOT_FOUND"})
			return
		}
		if errors.Is(err, jobs.ErrInvalidState) {
			writeJSON(w, 409, map[string]string{"code": "INVALID_JOB_STATE"})
			return
		}
		writeJSON(w, 200, j)
	})
	mux.HandleFunc("POST /api/v1/jobs/executions/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		j, err := jobsService.Approve(r.PathValue("id"), u.Username)
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "JOB_NOT_FOUND"})
			return
		}
		if errors.Is(err, jobs.ErrInvalidState) {
			writeJSON(w, 409, map[string]string{"code": "INVALID_APPROVAL", "message": "申请人不能审批自己的高风险任务"})
			return
		}
		writeJSON(w, 200, j)
	})
	mux.HandleFunc("GET /api/v1/tickets/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "ticket:view") {
			return
		}
		writeJSON(w, 200, ticketsService.Summary())
	})
	mux.HandleFunc("GET /api/v1/tickets", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "ticket:view") {
			return
		}
		writeJSON(w, 200, ticketsService.List())
	})
	mux.HandleFunc("POST /api/v1/tickets", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "ticket:manage") {
			return
		}
		var in tickets.CreateInput
		if decodeJSON(r, &in) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		in.Applicant = u.Username
		x, err := ticketsService.Create(in)
		if errors.Is(err, tickets.ErrValidation) {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		writeJSON(w, 201, x)
	})
	mux.HandleFunc("POST /api/v1/tickets/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "ticket:manage") {
			return
		}
		var in struct {
			Comment string `json:"comment"`
		}
		_ = decodeJSON(r, &in)
		u, _ := authService.CurrentUser(bearerToken(r))
		x, err := ticketsService.Approve(r.PathValue("id"), u.Username, in.Comment)
		if errors.Is(err, tickets.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "TICKET_NOT_FOUND"})
			return
		}
		writeJSON(w, 200, x)
	})
	mux.HandleFunc("POST /api/v1/tickets/{id}/reject", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "ticket:manage") {
			return
		}
		var in struct {
			Comment string `json:"comment"`
		}
		_ = decodeJSON(r, &in)
		u, _ := authService.CurrentUser(bearerToken(r))
		x, err := ticketsService.Reject(r.PathValue("id"), u.Username, in.Comment)
		if errors.Is(err, tickets.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "TICKET_NOT_FOUND"})
			return
		}
		writeJSON(w, 200, x)
	})
	mux.HandleFunc("GET /api/v1/releases/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		writeJSON(w, 200, releasesService.Summary())
	})
	mux.HandleFunc("GET /api/v1/releases", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		writeJSON(w, 200, releasesService.List())
	})
	mux.HandleFunc("GET /api/v1/releases/artifacts", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		writeJSON(w, 200, releasesService.Artifacts())
	})
	mux.HandleFunc("POST /api/v1/releases", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		var in releases.StartInput
		if decodeJSON(r, &in) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		in.Operator = u.Username
		x, err := releasesService.Start(in)
		if errors.Is(err, releases.ErrValidation) {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		writeJSON(w, 201, x)
	})
	mux.HandleFunc("POST /api/v1/releases/{id}/rollback", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		x, err := releasesService.Rollback(r.PathValue("id"), u.Username)
		if errors.Is(err, releases.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "RELEASE_NOT_FOUND"})
			return
		}
		writeJSON(w, 200, x)
	})
	mux.HandleFunc("GET /api/v1/toolbox/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "toolbox:use") {
			return
		}
		writeJSON(w, 200, toolboxService.Summary())
	})
	mux.HandleFunc("GET /api/v1/toolbox/tools", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "toolbox:use") {
			return
		}
		writeJSON(w, 200, toolboxService.Tools())
	})
	mux.HandleFunc("GET /api/v1/toolbox/history", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "toolbox:use") {
			return
		}
		writeJSON(w, 200, toolboxService.History())
	})
	mux.HandleFunc("POST /api/v1/toolbox/executions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "toolbox:use") {
			return
		}
		var in toolbox.RunInput
		if decodeJSON(r, &in) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		in.Operator = u.Username
		x, err := toolboxService.Run(in)
		if errors.Is(err, toolbox.ErrValidation) {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		writeJSON(w, 201, x)
	})
	mux.HandleFunc("GET /api/v1/aiops/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "aiops:view") {
			return
		}
		writeJSON(w, 200, aiopsService.Summary())
	})
	mux.HandleFunc("GET /api/v1/aiops/analyses", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "aiops:view") {
			return
		}
		writeJSON(w, 200, aiopsService.Analyses())
	})
	mux.HandleFunc("GET /api/v1/aiops/knowledge", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "aiops:view") {
			return
		}
		writeJSON(w, 200, aiopsService.SearchKnowledge(r.URL.Query().Get("q")))
	})
	mux.HandleFunc("GET /api/v1/aiops/executions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "aiops:view") {
			return
		}
		writeJSON(w, 200, aiopsService.Executions())
	})
	mux.HandleFunc("POST /api/v1/aiops/analyses/{id}/execute", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "aiops:execute") {
			return
		}
		var in struct {
			RunbookID string `json:"runbookId"`
		}
		if decodeJSON(r, &in) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		x, err := aiopsService.Execute(r.PathValue("id"), in.RunbookID, u.Username)
		if errors.Is(err, aiops.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "ANALYSIS_NOT_FOUND"})
			return
		}
		writeJSON(w, 202, x)
	})
	mux.HandleFunc("GET /api/v1/system/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		writeJSON(w, 200, systemService.Summary())
	})
	mux.HandleFunc("GET /api/v1/system/users", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		writeJSON(w, 200, systemService.Users())
	})
	mux.HandleFunc("GET /api/v1/system/roles", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		writeJSON(w, 200, systemService.Roles())
	})
	mux.HandleFunc("GET /api/v1/system/modules", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		writeJSON(w, 200, systemService.Modules())
	})
	mux.HandleFunc("GET /api/v1/system/audits", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		writeJSON(w, 200, systemService.Audits())
	})
	mux.HandleFunc("GET /api/v1/system/settings", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		writeJSON(w, 200, systemService.Settings())
	})
	mux.HandleFunc("POST /api/v1/system/users/{id}/toggle", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		x, err := systemService.ToggleUser(r.PathValue("id"), u.Username)
		if errors.Is(err, system.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "USER_NOT_FOUND"})
			return
		}
		writeJSON(w, 200, x)
	})
	mux.HandleFunc("PUT /api/v1/system/roles/{id}/permissions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		var in struct {
			Permissions []string `json:"permissions"`
		}
		if decodeJSON(r, &in) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		x, err := systemService.UpdateRole(r.PathValue("id"), in.Permissions, u.Username)
		if errors.Is(err, system.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "ROLE_NOT_FOUND"})
			return
		}
		writeJSON(w, 200, x)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND", "message": "接口不存在"})
	})
	return &Server{handler: requestLogger(platformMetrics.Instrument(cors(auditTrail(mux, authService, auditService)))), discovery: discoveryService, monitor: monitorService}
}

func syncAssetHealthFromAlerts(cmdbService *cmdb.Service, monitorService *monitor.Service, payload monitor.WebhookPayload) {
	affected := map[string]bool{}
	for _, incoming := range payload.Alerts {
		if assetID := incoming.Labels["cmdb_asset_id"]; assetID != "" {
			affected[assetID] = true
		}
	}
	if len(affected) == 0 {
		return
	}
	active := monitorService.Alerts()
	for assetID := range affected {
		status := "online"
		score := 0
		for _, alert := range active {
			if alert.TargetID != assetID || alert.Status != "firing" {
				continue
			}
			alertScore := 1
			if alert.Severity == "warning" {
				alertScore = 2
			} else if alert.Severity == "critical" {
				alertScore = 3
			}
			if alertScore > score {
				score = alertScore
			}
		}
		if score == 3 {
			status = "offline"
		} else if score == 2 {
			status = "warning"
		}
		if err := cmdbService.ApplyMonitoringState(assetID, status, "alertmanager"); err != nil && !errors.Is(err, cmdb.ErrNotFound) {
			slog.Error("sync asset monitoring state", "asset_id", assetID, "status", status, "error", err)
		}
	}
}
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	value := r.Header.Get("Authorization")
	if len(value) <= len(prefix) || value[:len(prefix)] != prefix {
		return ""
	}
	return value[len(prefix):]
}

func decodeJSONLenient(r *http.Request, value any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(value)
}
func decodeJSON(r *http.Request, value any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func authorize(w http.ResponseWriter, r *http.Request, service *auth.Service, permission string) bool {
	user, err := service.CurrentUser(bearerToken(r))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED", "message": "登录状态已失效"})
		return false
	}
	for _, item := range user.Permissions {
		if item == permission {
			return true
		}
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"code": "FORBIDDEN", "message": "没有功能访问权限"})
	return false
}

func (s *Server) Handler() http.Handler                { return s.handler }
func (s *Server) DiscoveryService() *discovery.Service { return s.discovery }
func (s *Server) MonitorService() *monitor.Service     { return s.monitor }

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write response", "error", err)
	}
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
	})
}
