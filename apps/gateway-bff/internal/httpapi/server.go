package httpapi

import (
	"context"
	"crypto/subtle"
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
	"cmdb/gateway-bff/internal/assetgovernance"
	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/credentials"
	"cmdb/gateway-bff/internal/dashboard"
	"cmdb/gateway-bff/internal/discovery"
	"cmdb/gateway-bff/internal/events"
	"cmdb/gateway-bff/internal/idc"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/k8s"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/networkautomation"
	"cmdb/gateway-bff/internal/releases"
	"cmdb/gateway-bff/internal/system"
	"cmdb/gateway-bff/internal/tickets"
	"cmdb/gateway-bff/internal/toolbox"
	"cmdb/gateway-bff/internal/topology"
)

type Server struct {
	handler     http.Handler
	discovery   *discovery.Service
	monitor     *monitor.Service
	assetEvents *events.AssetEventConsumer
}

func NewServer() *Server {
	mux := http.NewServeMux()
	cmdbService := cmdb.NewService()
	discoveryService := discovery.NewServiceWithCMDB(cmdbService)
	monitorService := monitor.NewService()
	topologyService := topology.NewServiceWithCMDB(cmdbService)
	var assetEventConsumer *events.AssetEventConsumer
	if brokers := os.Getenv("KAFKA_BROKERS"); strings.TrimSpace(brokers) != "" {
		var consumerErr error
		assetEventConsumer, consumerErr = events.NewAssetEventConsumer(brokers, func(ctx context.Context, _ string, _ []byte) error {
			if err := cmdbService.ReloadFromDatabase(ctx); err != nil {
				return err
			}
			topologyService.Invalidate()
			return nil
		})
		if consumerErr != nil {
			panic("initialize CMDB asset event consumer: " + consumerErr.Error())
		}
	}
	service := dashboard.NewServiceWithRuntime(cmdbService, discoveryService, monitorService)
	platformMetrics := newMetrics(cmdbService, discoveryService, monitorService, assetEventConsumer)
	jobsService := jobs.NewService()
	go jobsService.RunScheduler(context.Background())
	ticketsService := tickets.NewService()
	toolboxService := toolbox.NewService()
	systemService := system.NewService()
	aiopsService := aiops.NewService()
	releasesService := releases.NewService()
	networkAutomationService := networkautomation.NewService(cmdbService, ticketsService, monitorService, jobsService)
	go networkAutomationService.Run(context.Background(), time.Minute)
	assetGovernanceService := assetgovernance.NewService(cmdbService, ticketsService, monitorService)
	go assetGovernanceService.Run(context.Background(), 5*time.Minute)
	authService := auth.NewService()
	authService.SetAccessProfileResolver(func(username string) (auth.AccessProfile, error) {
		profile, err := systemService.AccessProfile(username)
		if err != nil {
			return auth.AccessProfile{}, err
		}
		return auth.AccessProfile{DataScope: profile.DataScope, ProjectGroups: profile.ProjectGroups, ScopeTags: profile.ScopeTags, Permissions: profile.Permissions}, nil
	})
	idcService := idc.NewService()
	credentialsService := credentials.NewService()
	k8sService := k8s.NewService(cmdbService)
	auditService := audit.NewService()
	registerRancherRoutes(mux, authService, auditService, cmdbService, credentialsService)
	registerPhysicalNetworkRoutes(mux, authService, cmdbService, topologyService, monitorService, ticketsService)
	registerNetworkAutomationRoutes(mux, authService, networkAutomationService)
	registerAssetGovernanceRoutes(mux, authService, assetGovernanceService)
	registerTopologySnapshotRoutes(mux, authService, cmdbService, topologyService)
	discoveryService.SetCredentialResolver(func(id string) (string, string, error) {
		m, err := credentialsService.Resolve(id)
		return m.Username, m.Secret, err
	})
	registerCollectionScheduleRoutes(mux, authService, auditService, discoveryService)
	registerDiscoveryQualityRoute(mux, authService, discoveryService, cmdbService)
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/ready", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /internal/runner/cmdb/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeRunnerToken(w, r) {
			return
		}
		asset, err := cmdbService.GetAsset(r.PathValue("id"))
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND", "message": "资产不存在"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "ASSET_READ_FAILED"})
			return
		}
		payload := map[string]any{"id": asset.ID, "ip": asset.IP, "sshPort": runnerAssetSSHPort(asset)}
		credentialID := runnerAssetCredentialID(asset, r.URL.Query().Get("credentialId"))
		if credentialID != "" {
			material, err := credentialsService.Resolve(credentialID)
			if err != nil || strings.TrimSpace(material.Username) == "" || strings.TrimSpace(material.Secret) == "" {
				writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "CREDENTIAL_RESOLVE_FAILED", "message": "SSH 凭据不可用"})
				return
			}
			payload["credentialId"] = credentialID
			payload["sshUser"] = material.Username
			payload["sshPassword"] = material.Secret
			payload["authType"] = "password"
		}
		writeJSON(w, http.StatusOK, payload)
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
	mux.HandleFunc("GET /api/v1/monitor/service-discovery/exporters", func(w http.ResponseWriter, r *http.Request) {
		if !discoveryService.AuthorizeAgentToken(bearerToken(r)) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED"})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, cmdbService.PrometheusExporterTargetGroups())
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
		syncTicketsFromAlerts(ticketsService, auditService, payload)
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
	mux.HandleFunc("GET /api/v1/dashboard/overview", func(w http.ResponseWriter, r *http.Request) {
		overview := service.Overview()
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		if !scope.global {
			summary := cmdbService.SummaryFor(assetScopeForUser(user))
			alerts := filterMonitorAlerts(monitorService.Alerts(), scope)
			overview.Metrics.AssetTotal = summary.Total
			overview.Metrics.OnlineAgents = summary.Online
			overview.Metrics.TodayJobs = 0
			overview.Metrics.ActiveAlerts = 0
			overview.Alerts = []dashboard.Alert{}
			for _, item := range alerts {
				if item.Status == "resolved" {
					continue
				}
				overview.Metrics.ActiveAlerts++
				if len(overview.Alerts) < 10 {
					overview.Alerts = append(overview.Alerts, dashboard.Alert{ID: item.ID, Level: item.Severity, Title: item.Title, Target: item.Target, OccurredAt: item.StartedAt})
				}
			}
			overview.Topology = dashboard.Topology{Layers: []dashboard.TopologyLayer{}}
			overview.Jobs = []dashboard.Job{}
			overview.Timeline = []dashboard.TimelineEvent{}
			overview.Deployments = []dashboard.Deployment{}
		}
		writeJSON(w, http.StatusOK, overview)
	})
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
	mux.HandleFunc("GET /api/v1/cmdb/monitor-targets", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		scope, _, _ := assetQueryForRequest(r, authService)
		writeJSON(w, http.StatusOK, cmdbService.MonitorTargetsFor(scope))
	})
	mux.HandleFunc("GET /api/v1/cmdb/analytics", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		scope, _, _ := assetQueryForRequest(r, authService)
		writeJSON(w, http.StatusOK, cmdbService.AnalyticsFor(scope))
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
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		writeJSON(w, http.StatusOK, filterKubernetesInventory(cmdbService.KubernetesInventory(), scope))
	})
	mux.HandleFunc("GET /api/v1/cmdb/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		scope, _, _ := assetQueryForRequest(r, authService)
		writeJSON(w, http.StatusOK, cmdbService.SummaryFor(scope))
	})
	mux.HandleFunc("GET /api/v1/cmdb/assets", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		query := r.URL.Query()
		page, _ := strconv.Atoi(query.Get("page"))
		pageSize, _ := strconv.Atoi(query.Get("pageSize"))
		scope, _, _ := assetQueryForRequest(r, authService)
		assetQuery := cmdb.AssetQuery{Search: query.Get("q"), Type: query.Get("type"), Status: query.Get("status"), ProjectGroup: query.Get("projectGroup"), Page: page, PageSize: pageSize, ScopeAll: scope.ScopeAll, ScopeProjectGroups: scope.ScopeProjectGroups, ScopeTags: scope.ScopeTags, ScopeEmpty: scope.ScopeEmpty}
		writeJSON(w, http.StatusOK, cmdbService.ListAssets(assetQuery))
	})
	mux.HandleFunc("GET /api/v1/cmdb/asset-options", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := assetScopeForUser(user)
		types := map[string]bool{}
		for _, value := range strings.Split(r.URL.Query().Get("types"), ",") {
			if value = strings.TrimSpace(value); value != "" {
				types[value] = true
			}
		}
		items := cmdbService.InventoryAssetsFor(scope)
		options := make([]cmdb.Asset, 0, len(items))
		for _, item := range items {
			if len(types) > 0 && !types[item.Type] {
				continue
			}
			options = append(options, item)
		}
		writeJSON(w, http.StatusOK, options)
	})
	mux.HandleFunc("POST /api/v1/cmdb/assets/batch-update", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input cmdb.BatchUpdateAssetInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "请求格式不正确"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := assetScopeForUser(user)
		for _, assetID := range input.AssetIDs {
			asset, err := cmdbService.GetAsset(assetID)
			if err != nil || !cmdb.AssetWithinScope(asset, scope) {
				writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN", "message": "批量配置包含无权限资产"})
				return
			}
		}
		result, err := cmdbService.BatchUpdateAssets(input, user.Username)
		if errors.Is(err, cmdb.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "请选择 1 到 200 台服务器，并检查宿主机类型"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "BATCH_UPDATE_FAILED", "message": err.Error()})
			return
		}
		auditService.Record(user.Username, "cmdb.asset.batch_update", "assets", "updated="+strconv.Itoa(result.Succeeded))
		writeJSON(w, http.StatusOK, result)
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
		if !canAccessAsset(user, cmdb.Asset{ProjectGroup: input.ProjectGroup, Tags: input.Tags}) {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN", "message": "没有该资产所在项目组的数据权限"})
			return
		}
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
		if _, ok := authorizeAssetAccess(w, r, authService, asset); !ok {
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
		existing, existingErr := cmdbService.GetAsset(r.PathValue("id"))
		if existingErr == nil && !canAccessAsset(user, existing) {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN", "message": "没有该资产所在项目组的数据权限"})
			return
		}
		if !canAccessAsset(user, cmdb.Asset{ProjectGroup: input.ProjectGroup, Tags: input.Tags}) {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN", "message": "不能将资产移动到无权限的项目组或标签范围"})
			return
		}
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
		asset, assetErr := cmdbService.GetAsset(r.PathValue("id"))
		if assetErr == nil {
			if _, ok := authorizeAssetAccess(w, r, authService, asset); !ok {
				return
			}
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
		for index, row := range rows {
			if !canAccessAsset(user, cmdb.Asset{ProjectGroup: row.ProjectGroup, Tags: row.Tags}) {
				writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN", "message": "导入数据第 " + strconv.Itoa(index+2) + " 行超出当前项目组或标签权限"})
				return
			}
			if existing, getErr := cmdbService.GetAsset(row.ID); getErr == nil && row.ID != "" && !canAccessAsset(user, existing) {
				writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN", "message": "导入数据第 " + strconv.Itoa(index+2) + " 行对应资产超出当前项目组或标签权限"})
				return
			}
		}
		upsert := r.URL.Query().Get("mode") == "upsert"
		writeJSON(w, http.StatusOK, cmdbService.ImportAssets(rows, upsert, user.Username))
	})
	mux.HandleFunc("GET /api/v1/agent/install/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": discovery.AgentBundleVersion()})
	})
	mux.HandleFunc("GET /api/v1/discovery/agent-versions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		writeJSON(w, http.StatusOK, discoveryService.AgentVersionReport())
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
			Hosts        []string `json:"hosts"`
			Port         int      `json:"port"`
			CredentialID string   `json:"credentialId"`
		}
		if err := decodeJSON(r, &in); err != nil || len(in.Hosts) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_INPUT"})
			return
		}
		result, err := discoveryService.ExporterBatchUninstall(in.Hosts, in.Port, in.CredentialID)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "EXPORTER_UNINSTALL_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/discovery/agent-uninstall", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var in discovery.AgentInstallInput
		if err := decodeJSON(r, &in); err != nil || len(in.Hosts) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_INPUT"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.RunAgentDeployment(discovery.AgentInstallInput{Hosts: in.Hosts, Port: in.Port, CredentialID: in.CredentialID}, "uninstall", user.Username)
		res := item.Results
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "UNINSTALL_FAILED", "message": err.Error()})
			return
		}
		if user.Username != "" {
			auditService.Record(user.Username, "discovery.agent.uninstall", strings.Join(in.Hosts, ","), "hosts="+strconv.Itoa(len(in.Hosts))+" deployment="+item.ID)
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/v1/discovery/agent-install", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var in discovery.AgentInstallInput
		if err := decodeJSON(r, &in); err != nil || len(in.Hosts) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_INPUT"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.RunAgentDeployment(in, "install", user.Username)
		res := item.Results
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "INSTALL_FAILED", "message": err.Error()})
			return
		}
		if user.Username != "" {
			auditService.Record(user.Username, "discovery.agent.install", strings.Join(in.Hosts, ","), "hosts="+strconv.Itoa(len(in.Hosts))+" version="+discovery.AgentBundleVersion()+" deployment="+item.ID)
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/v1/discovery/agent-upgrade", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var in discovery.AgentInstallInput
		if err := decodeJSON(r, &in); err != nil || len(in.Hosts) == 0 || strings.TrimSpace(in.TargetVersion) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_INPUT"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.RunAgentDeployment(in, "upgrade", user.Username)
		res := item.Results
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "UPGRADE_FAILED", "message": err.Error()})
			return
		}
		if user.Username != "" {
			auditService.Record(user.Username, "discovery.agent.upgrade", strings.Join(in.Hosts, ","), "hosts="+strconv.Itoa(len(in.Hosts))+" target="+in.TargetVersion+" deployment="+item.ID)
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("GET /api/v1/discovery/agent-deployments", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		limit := 50
		if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
			if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		items, err := discoveryService.AgentDeployments(limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "DEPLOYMENTS_LOAD_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, items)
	})
	mux.HandleFunc("GET /api/v1/discovery/agent-deployments/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		item, err := discoveryService.AgentDeployment(r.PathValue("id"))
		if errors.Is(err, discovery.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "DEPLOYMENT_NOT_FOUND"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "DEPLOYMENT_LOAD_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/discovery/agent-deployments/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.RetryAgentDeployment(r.PathValue("id"), user.Username)
		if errors.Is(err, discovery.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "DEPLOYMENT_NOT_FOUND"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "RETRY_FAILED", "message": err.Error()})
			return
		}
		if user.Username != "" {
			auditService.Record(user.Username, "discovery.agent.retry", item.ID, "retryOf="+item.RetryOf+" hosts="+strconv.Itoa(item.Total))
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/discovery/agent-health-checks", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var in discovery.AgentHealthInput
		if err := decodeJSON(r, &in); err != nil || len(in.Hosts) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_INPUT"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.RunAgentHealthCheck(in, user.Username)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "HEALTH_CHECK_FAILED", "message": err.Error()})
			return
		}
		if user.Username != "" {
			auditService.Record(user.Username, "discovery.agent.health_check", strings.Join(in.Hosts, ","), "hosts="+strconv.Itoa(len(in.Hosts))+" check="+item.ID+" status="+item.Status)
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("GET /api/v1/discovery/agent-health-checks", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		limit := 50
		if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
			if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		items, err := discoveryService.AgentHealthChecks(limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "HEALTH_CHECKS_LOAD_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, items)
	})
	mux.HandleFunc("GET /api/v1/discovery/agent-health-checks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		item, err := discoveryService.AgentHealthCheck(r.PathValue("id"))
		if errors.Is(err, discovery.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "HEALTH_CHECK_NOT_FOUND"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "HEALTH_CHECK_LOAD_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/discovery/agent-health-checks/{id}/remediate", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var in discovery.AgentRemediationInput
		if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Action) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_INPUT"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := discoveryService.RemediateAgentHealth(r.PathValue("id"), in, user.Username)
		if errors.Is(err, discovery.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "HEALTH_CHECK_NOT_FOUND"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "REMEDIATION_FAILED", "message": err.Error()})
			return
		}
		if user.Username != "" {
			auditService.Record(user.Username, "discovery.agent.remediate", r.PathValue("id"), "action="+in.Action+" hosts="+strconv.Itoa(len(in.Hosts)))
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/discovery/agent-precheck", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var in discovery.AgentPrecheckInput
		if err := decodeJSON(r, &in); err != nil || len(in.Hosts) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_INPUT"})
			return
		}
		results, err := discoveryService.AgentPrecheck(in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "PRECHECK_FAILED", "message": err.Error()})
			return
		}
		if user, _ := authService.CurrentUser(bearerToken(r)); user.Username != "" {
			auditService.Record(user.Username, "discovery.agent.precheck", strings.Join(in.Hosts, ","), "hosts="+strconv.Itoa(len(in.Hosts)))
		}
		writeJSON(w, http.StatusOK, results)
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
	mux.HandleFunc("POST /api/v1/discovery/scan-services", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:manage") {
			return
		}
		var in discovery.ServiceScanInput
		if err := decodeJSON(r, &in); err != nil || len(in.CIDRs) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_SCAN_INPUT"})
			return
		}
		result, err := discoveryService.ScanServices(r.Context(), in)
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
		var in credentials.CreateInput
		if err := decodeJSON(r, &in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		cred, err := credentialsService.Create(in)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "CREATE_FAILED", "message": err.Error()})
			return
		}
		if user, _ := authService.CurrentUser(bearerToken(r)); user.Username != "" {
			detail := cred.Kind + "/" + cred.Name
			if cred.Group != "" {
				detail += " group=" + cred.Group
			}
			auditService.Record(user.Username, "credential.create", cred.ID, detail)
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
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		writeJSON(w, http.StatusOK, filterTopologyGraph(topologyService.Graph(r.URL.Query().Get("environment")), scope))
	})
	mux.HandleFunc("GET /api/v1/monitor/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		summary := monitorService.Summary()
		if !scope.global {
			alerts := filterMonitorAlerts(monitorService.Alerts(), scope)
			summary.Active, summary.Critical, summary.Acknowledged = 0, 0, 0
			for _, alert := range alerts {
				if alert.Status != "resolved" {
					summary.Active++
				}
				if alert.Severity == "critical" && alert.Status != "resolved" {
					summary.Critical++
				}
				if alert.Status == "acknowledged" {
					summary.Acknowledged++
				}
			}
			summary.Targets = len(scope.ids)
		}
		writeJSON(w, 200, summary)
	})
	mux.HandleFunc("GET /api/v1/monitor/coverage", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		assets := cmdbService.MonitoringAssets()
		if !scope.global {
			filtered := []cmdb.Asset{}
			for _, asset := range assets {
				if scope.allowsAsset(asset) {
					filtered = append(filtered, asset)
				}
			}
			assets = filtered
		}
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
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		writeJSON(w, 200, filterMonitorMetrics(monitorService.Metrics(), scope))
	})
	mux.HandleFunc("GET /api/v1/monitor/hosts/{assetId}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "monitor:view") {
			return
		}
		assetID := r.PathValue("assetId")
		asset, err := cmdbService.GetAsset(assetID)
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "ASSET_NOT_FOUND"})
			return
		}
		if _, ok := authorizeAssetAccess(w, r, authService, asset); !ok {
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
		if _, ok := authorizeAssetAccess(w, r, authService, asset); !ok {
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
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		writeJSON(w, 200, filterMonitorAlerts(monitorService.Alerts(), scope))
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
		status := monitorService.QueueStatus()
		if assetEventConsumer != nil {
			status.AssetConsumer = assetEventConsumer.Status()
			status.AssetEventsProcessed = assetEventConsumer.Processed()
			status.AssetEventLastAt = assetEventConsumer.LastEventAt()
		}
		writeJSON(w, http.StatusOK, status)
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
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		if !scope.global && scope.ids[result.Root.ID] == false && scope.ips[result.Root.IP] == false {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN"})
			return
		}
		writeJSON(w, http.StatusOK, filterImpact(result, scope))
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
		user, _ := authService.CurrentUser(bearerToken(r))
		writeJSON(w, http.StatusOK, cmdbService.RemoteTargetOptionsFor(assetScopeForUser(user)))
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
		user, _ := authService.CurrentUser(bearerToken(r))
		assets := cmdbService.ResolveRemoteTargetsFor(assetScopeForUser(user), input.ProjectGroups, input.Tags, input.AssetIDs, input.IPs)
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
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		summary := jobsService.Summary()
		if !scope.global {
			jobs := filterJobs(jobsService.Jobs(), scope)
			schedules := filterSchedules(jobsService.Schedules(), scope)
			summary.Running, summary.SuccessToday, summary.FailedToday = 0, 0, 0
			for _, job := range jobs {
				switch job.Status {
				case "running":
					summary.Running++
				case "success":
					summary.SuccessToday++
				case "failed":
					summary.FailedToday++
				}
			}
			summary.Schedules = len(schedules)
		}
		writeJSON(w, 200, summary)
	})
	mux.HandleFunc("GET /api/v1/jobs/executor", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		writeJSON(w, 200, jobsService.ExecutorInfo())
	})
	mux.HandleFunc("GET /api/v1/jobs/runner", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		writeJSON(w, 200, jobsService.RunnerOverview(r.Context()))
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
	mux.HandleFunc("PUT /api/v1/jobs/templates/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		var in jobs.TemplateInput
		if decodeJSON(r, &in) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		item, err := jobsService.UpdateTemplate(r.PathValue("id"), in)
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "TEMPLATE_NOT_FOUND"})
			return
		}
		if errors.Is(err, jobs.ErrUnsafeCommand) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "UNSAFE_COMMAND", "message": "命令命中高危拦截规则"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "job.template.update", item.ID, item.Name)
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/jobs/templates/{id}/duplicate", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		item, err := jobsService.DuplicateTemplate(r.PathValue("id"), user.Username)
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "TEMPLATE_NOT_FOUND"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "TEMPLATE_DUPLICATE_FAILED"})
			return
		}
		auditService.Record(user.Username, "job.template.duplicate", item.ID, item.Name)
		writeJSON(w, http.StatusCreated, item)
	})
	mux.HandleFunc("DELETE /api/v1/jobs/templates/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		if err := jobsService.DeleteTemplate(r.PathValue("id")); errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "TEMPLATE_NOT_FOUND"})
			return
		} else if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TEMPLATE_DELETE_FAILED"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "job.template.delete", r.PathValue("id"), "")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/jobs/target-options", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		writeJSON(w, http.StatusOK, cmdbService.RemoteTargetOptionsFor(assetScopeForUser(user)))
	})
	mux.HandleFunc("POST /api/v1/jobs/targets/resolve", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
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
		user, _ := authService.CurrentUser(bearerToken(r))
		assets := cmdbService.ResolveRemoteTargetsFor(assetScopeForUser(user), input.ProjectGroups, input.Tags, input.AssetIDs, input.IPs)
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
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		writeJSON(w, 200, filterJobs(jobsService.Jobs(), scope))
	})
	mux.HandleFunc("GET /api/v1/jobs/executions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		j, err := jobsService.Get(r.PathValue("id"))
		if err == nil {
			user, _ := authService.CurrentUser(bearerToken(r))
			scope := newUserScope(cmdbService, user)
			if !scope.global && !allIdentifiersAllowed(j.Targets, scope) {
				writeJSON(w, 403, map[string]string{"code": "DATA_SCOPE_FORBIDDEN"})
				return
			}
		}
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "JOB_NOT_FOUND"})
			return
		}
		writeJSON(w, 200, j)
	})
	mux.HandleFunc("GET /api/v1/jobs/executions/{id}/artifacts/{index}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		job, err := jobsService.Get(r.PathValue("id"))
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "JOB_NOT_FOUND"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		if !scope.global && !allIdentifiersAllowed(job.Targets, scope) {
			writeJSON(w, 403, map[string]string{"code": "DATA_SCOPE_FORBIDDEN"})
			return
		}
		index, parseErr := strconv.Atoi(r.PathValue("index"))
		if parseErr != nil || index < 0 || index >= len(job.ArtifactKeys) {
			writeJSON(w, 404, map[string]string{"code": "ARTIFACT_NOT_FOUND"})
			return
		}
		body, downloadErr := jobsService.DownloadArtifact(r.Context(), job.ArtifactKeys[index])
		if downloadErr != nil {
			writeJSON(w, 502, map[string]string{"code": "ARTIFACT_DOWNLOAD_FAILED", "message": "执行归档暂不可用"})
			return
		}
		defer body.Close()
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+job.ID+`-artifact-`+strconv.Itoa(index+1)+`.zip"`)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, body)
	})
	mux.HandleFunc("GET /api/v1/jobs/schedules", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		writeJSON(w, 200, filterSchedules(jobsService.Schedules(), scope))
	})
	mux.HandleFunc("POST /api/v1/jobs/schedules/preview", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:view") {
			return
		}
		var input struct {
			Cron     string `json:"cron"`
			Timezone string `json:"timezone"`
			Count    int    `json:"count"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		nextRuns, err := jobsService.PreviewSchedule(input.Cron, input.Timezone, input.Count)
		if errors.Is(err, jobs.ErrValidation) {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"code": "SCHEDULE_PREVIEW_FAILED"})
			return
		}
		writeJSON(w, 200, map[string]any{"nextRuns": nextRuns})
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
		scope := newUserScope(cmdbService, user)
		if !scope.global && !allIdentifiersAllowed(input.Targets, scope) {
			writeJSON(w, 403, map[string]string{"code": "DATA_SCOPE_FORBIDDEN"})
			return
		}
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
		if errors.Is(err, jobs.ErrScheduleConflict) {
			writeJSON(w, 409, map[string]string{"code": "SCHEDULE_CONFLICT", "message": "上一次任务仍在执行，已按并发策略阻止本次调度"})
			return
		}
		if errors.Is(err, jobs.ErrInvalidState) {
			writeJSON(w, 409, map[string]string{"code": "SCHEDULE_NOT_DUE", "message": "计划未到执行时间或已被其他调度器处理"})
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
		scope := newUserScope(cmdbService, u)
		if !scope.global && !allIdentifiersAllowed(in.Targets, scope) {
			writeJSON(w, 403, map[string]string{"code": "DATA_SCOPE_FORBIDDEN"})
			return
		}
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
		var input struct {
			Targets []string `json:"targets"`
		}
		if r.Body != nil && r.ContentLength != 0 {
			if decodeJSON(r, &input) != nil {
				writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
				return
			}
		}
		j, err := jobsService.RetryTargets(r.PathValue("id"), input.Targets)
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "JOB_NOT_FOUND"})
			return
		}
		if errors.Is(err, jobs.ErrValidation) {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR", "message": "重试目标不属于原任务"})
			return
		}
		if errors.Is(err, jobs.ErrInvalidState) {
			writeJSON(w, 409, map[string]string{"code": "INVALID_JOB_STATE"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "job.execution.retry", j.ID, strings.Join(input.Targets, ","))
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
	mux.HandleFunc("POST /api/v1/jobs/executions/{id}/review", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "job:manage") {
			return
		}
		var input jobs.ApprovalInput
		if decodeJSON(r, &input) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		j, err := jobsService.Review(r.PathValue("id"), u.Username, input)
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "JOB_NOT_FOUND"})
			return
		}
		if errors.Is(err, jobs.ErrValidation) {
			writeJSON(w, 422, map[string]string{"code": "INVALID_APPROVAL", "message": "审批决策或意见不合法"})
			return
		}
		if errors.Is(err, jobs.ErrInvalidState) {
			writeJSON(w, 409, map[string]string{"code": "INVALID_APPROVAL", "message": "申请人不能审批自己的高风险任务"})
			return
		}
		auditService.Record(u.Username, "job.execution.review."+strings.TrimSpace(input.Decision), j.ID, strings.TrimSpace(input.Comment))
		writeJSON(w, 200, j)
	})
	mux.HandleFunc("GET /api/v1/tickets/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "ticket:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		if scope.global {
			result, err := ticketsService.Summary()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TICKET_SUMMARY_FAILED"})
				return
			}
			writeJSON(w, http.StatusOK, result)
			return
		}
		items, err := ticketsService.List()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TICKET_SUMMARY_FAILED"})
			return
		}
		result := tickets.Summary{}
		for _, item := range filterTickets(items, cmdbService, scope) {
			result.Total++
			if item.Status == tickets.StatusPending {
				result.Pending++
			}
			if item.Status == tickets.StatusApproved || item.Status == tickets.StatusCompleted {
				result.Processing++
			}
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/tickets", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "ticket:view") {
			return
		}
		items, err := ticketsService.List()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TICKET_LIST_FAILED"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		writeJSON(w, http.StatusOK, filterTickets(items, cmdbService, scope))
	})
	mux.HandleFunc("GET /api/v1/tickets/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "ticket:view") {
			return
		}
		item, err := ticketsService.Get(r.PathValue("id"))
		if errors.Is(err, tickets.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "TICKET_NOT_FOUND"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TICKET_READ_FAILED"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		if len(filterTickets([]tickets.Ticket{item}, cmdbService, scope)) == 0 {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN"})
			return
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/tickets", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "ticket:manage") {
			return
		}
		var in tickets.CreateInput
		if decodeJSON(r, &in) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		in.Applicant = u.Username
		item, err := ticketsService.Create(in)
		if errors.Is(err, tickets.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "工单标题、类型或必填字段无效"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TICKET_CREATE_FAILED"})
			return
		}
		auditService.Record(u.Username, "ticket.create", item.ID, item.Title)
		writeJSON(w, http.StatusCreated, item)
	})
	decideTicket := func(action string, decision func(string, string, string) (tickets.Ticket, error)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !authorize(w, r, authService, "ticket:manage") {
				return
			}
			var in struct {
				Comment string `json:"comment"`
			}
			_ = decodeJSON(r, &in)
			u, _ := authService.CurrentUser(bearerToken(r))
			item, err := decision(r.PathValue("id"), u.Username, in.Comment)
			if errors.Is(err, tickets.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"code": "TICKET_NOT_FOUND"})
				return
			}
			if errors.Is(err, tickets.ErrInvalidState) {
				writeJSON(w, http.StatusConflict, map[string]string{"code": "TICKET_INVALID_STATE", "message": "当前工单状态不允许该操作"})
				return
			}
			if errors.Is(err, tickets.ErrValidation) {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR"})
				return
			}
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TICKET_UPDATE_FAILED"})
				return
			}
			auditService.Record(u.Username, "ticket."+action, item.ID, in.Comment)
			writeJSON(w, http.StatusOK, item)
		}
	}
	mux.HandleFunc("POST /api/v1/tickets/{id}/approve", decideTicket("approve", ticketsService.Approve))
	mux.HandleFunc("POST /api/v1/tickets/{id}/reject", decideTicket("reject", ticketsService.Reject))
	mux.HandleFunc("POST /api/v1/tickets/{id}/comment", decideTicket("comment", ticketsService.Comment))
	mux.HandleFunc("POST /api/v1/tickets/{id}/close", decideTicket("close", ticketsService.Close))
	mux.HandleFunc("POST /api/v1/tickets/{id}/cancel", decideTicket("cancel", ticketsService.Cancel))
	mux.HandleFunc("POST /api/v1/tickets/{id}/assign", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "ticket:manage") {
			return
		}
		var in struct {
			Assignee string `json:"assignee"`
		}
		if decodeJSON(r, &in) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		item, err := ticketsService.Assign(r.PathValue("id"), in.Assignee, u.Username)
		if errors.Is(err, tickets.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "TICKET_NOT_FOUND"})
			return
		}
		if errors.Is(err, tickets.ErrInvalidState) {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "TICKET_INVALID_STATE"})
			return
		}
		if errors.Is(err, tickets.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "TICKET_UPDATE_FAILED"})
			return
		}
		auditService.Record(u.Username, "ticket.assign", item.ID, item.Assignee)
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("GET /api/v1/releases/summary", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		if !hasGlobalAssetScope(user) {
			writeJSON(w, http.StatusOK, releases.Summary{})
			return
		}
		result, err := releasesService.Summary()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "RELEASE_SUMMARY_FAILED"})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/releases", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		items, err := releasesService.List()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "RELEASE_LIST_FAILED"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		scope := newUserScope(cmdbService, user)
		if !scope.global {
			ticketItems, _ := ticketsService.List()
			items = filterReleases(items, filterTickets(ticketItems, cmdbService, scope), scope)
		}
		writeJSON(w, http.StatusOK, items)
	})
	mux.HandleFunc("GET /api/v1/releases/artifacts", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		items, err := releasesService.Artifacts()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "RELEASE_ARTIFACTS_FAILED"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		if !hasGlobalAssetScope(user) {
			items = []releases.Artifact{}
		}
		writeJSON(w, http.StatusOK, items)
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
		auditService.Record(u.Username, "release.start", x.ID, x.Application+" "+x.Version)
		writeJSON(w, 201, x)
	})
	mux.HandleFunc("POST /api/v1/releases/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		var in struct {
			Comment string `json:"comment"`
		}
		_ = decodeJSON(r, &in)
		u, _ := authService.CurrentUser(bearerToken(r))
		item, token, err := releasesService.Approve(r.PathValue("id"), u.Username, in.Comment)
		if errors.Is(err, releases.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "RELEASE_NOT_FOUND"})
			return
		}
		if errors.Is(err, releases.ErrInvalidState) {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "RELEASE_INVALID_STATE", "message": "当前发布状态不允许审批"})
			return
		}
		if errors.Is(err, releases.ErrUnauthorized) {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "RELEASE_SELF_APPROVAL_FORBIDDEN", "message": "发布申请人不能审批自己的发布"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "RELEASE_APPROVAL_FAILED"})
			return
		}
		auditService.Record(u.Username, "release.approve", item.ID, item.Application+" "+item.Version)
		writeJSON(w, http.StatusOK, map[string]any{"release": item, "authorizationToken": token, "expiresAt": item.AuthorizationExpiresAt})
	})
	mux.HandleFunc("POST /api/v1/releases/{id}/reject", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		var in struct {
			Comment string `json:"comment"`
		}
		_ = decodeJSON(r, &in)
		u, _ := authService.CurrentUser(bearerToken(r))
		item, err := releasesService.Reject(r.PathValue("id"), u.Username, in.Comment)
		if errors.Is(err, releases.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "RELEASE_NOT_FOUND"})
			return
		}
		if errors.Is(err, releases.ErrInvalidState) {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "RELEASE_INVALID_STATE"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "RELEASE_REJECT_FAILED"})
			return
		}
		auditService.Record(u.Username, "release.reject", item.ID, item.ApprovalComment)
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("GET /api/v1/releases/executor", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:view") {
			return
		}
		writeJSON(w, http.StatusOK, releasesService.ExecutorInfo())
	})
	mux.HandleFunc("POST /api/v1/releases/{id}/execute", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "release:manage") {
			return
		}
		var in struct {
			AuthorizationToken string `json:"authorizationToken"`
		}
		if decodeJSON(r, &in) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		item, err := releasesService.AuthorizeExecution(r.PathValue("id"), in.AuthorizationToken, u.Username)
		if errors.Is(err, releases.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "RELEASE_NOT_FOUND"})
			return
		}
		if errors.Is(err, releases.ErrUnauthorized) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "RELEASE_AUTHORIZATION_INVALID"})
			return
		}
		if errors.Is(err, releases.ErrExpired) {
			writeJSON(w, http.StatusGone, map[string]string{"code": "RELEASE_AUTHORIZATION_EXPIRED"})
			return
		}
		if errors.Is(err, releases.ErrInvalidState) {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "RELEASE_AUTHORIZATION_USED"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "RELEASE_EXECUTE_FAILED"})
			return
		}
		action := "deploy"
		if item.Status == releases.StatusRollingBack {
			action = "rollback"
		}
		if err := releasesService.Dispatch(r.Context(), item, action, in.AuthorizationToken); err != nil {
			auditService.Record(u.Username, "release.dispatch_failed", item.ID, err.Error())
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "RELEASE_DISPATCH_FAILED", "message": "发布执行器调用失败"})
			return
		}
		auditService.Record(u.Username, "release.execute_authorize", item.ID, action)
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/releases/events", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeReleaseWebhook(w, r) {
			return
		}
		var in releases.EventInput
		if decodeJSON(r, &in) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		item, err := releasesService.ApplyEvent(in)
		if errors.Is(err, releases.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR"})
			return
		}
		if errors.Is(err, releases.ErrInvalidState) {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "RELEASE_APPROVAL_REQUIRED", "message": "存在待审批的手工发布，外部事件不能绕过审批"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "RELEASE_EVENT_FAILED"})
			return
		}
		auditService.Record(item.Source, "release.event", item.ID, item.Application+" "+item.Version+" "+item.Status)
		writeJSON(w, http.StatusAccepted, item)
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
		auditService.Record(u.Username, "release.rollback", x.ID, x.Application+" "+x.Version)
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
	mux.HandleFunc("PUT /api/v1/system/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		var in system.UserUpdateInput
		if decodeJSON(r, &in) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		x, err := systemService.UpdateUser(r.PathValue("id"), in, u.Username)
		if errors.Is(err, system.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "USER_NOT_FOUND"})
			return
		}
		if errors.Is(err, system.ErrValidation) {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR", "message": "用户数据范围无效"})
			return
		}
		writeJSON(w, 200, x)
	})
	mux.HandleFunc("PUT /api/v1/system/roles/{id}/permissions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		var in system.RoleUpdateInput
		if decodeJSON(r, &in) != nil {
			writeJSON(w, 400, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		x, err := systemService.UpdateRoleProfile(r.PathValue("id"), in, u.Username)
		if errors.Is(err, system.ErrNotFound) {
			writeJSON(w, 404, map[string]string{"code": "ROLE_NOT_FOUND"})
			return
		}
		if errors.Is(err, system.ErrValidation) {
			writeJSON(w, 422, map[string]string{"code": "VALIDATION_ERROR", "message": "角色权限或数据范围无效"})
			return
		}
		writeJSON(w, 200, x)
	})
	mux.HandleFunc("POST /api/v1/system/sync/keycloak", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "system:manage") {
			return
		}
		report, err := systemService.SyncKeycloak(r.Context())
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "KEYCLOAK_SYNC_FAILED", "message": err.Error()})
			return
		}
		u, _ := authService.CurrentUser(bearerToken(r))
		systemService.RecordAudit(u.Username, "同步 Keycloak 组织", "系统管理", "用户与用户组已同步")
		writeJSON(w, 200, report)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND", "message": "接口不存在"})
	})
	return &Server{handler: requestLogger(platformMetrics.Instrument(cors(auditTrail(mux, authService, auditService)))), discovery: discoveryService, monitor: monitorService, assetEvents: assetEventConsumer}
}

func (s *Server) RunAssetEventConsumer(ctx context.Context) {
	if s != nil && s.assetEvents != nil {
		s.assetEvents.Run(ctx)
	}
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
func syncTicketsFromAlerts(ticketService *tickets.Service, auditService *audit.Service, payload monitor.WebhookPayload) {
	for _, incoming := range payload.Alerts {
		status := incoming.Status
		if payload.Status == "resolved" {
			status = "resolved"
		}
		id := incoming.Fingerprint
		if id == "" {
			id = "alertmanager-" + incoming.StartsAt.UTC().Format("20060102T150405Z")
		}
		title := incoming.Annotations["summary"]
		if title == "" {
			title = incoming.Labels["alertname"]
		}
		item, created, err := ticketService.SyncAlert(tickets.AlertInput{
			ID: id, Title: title, Severity: incoming.Labels["severity"], Status: status,
			Target:  firstNonEmpty(incoming.Labels["cmdb_asset_id"], incoming.Labels["instance"]),
			Message: firstNonEmpty(incoming.Annotations["description"], incoming.Annotations["summary"]),
			Owner:   incoming.Labels["owner"],
		})
		if err != nil {
			slog.Error("sync ticket from alert", "alert_id", id, "error", err)
			continue
		}
		if item.ID == "" {
			continue
		}
		if created {
			auditService.Record("alertmanager", "ticket.auto_create", item.ID, title)
		}
		if status == "resolved" {
			auditService.Record("alertmanager", "ticket.auto_close", item.ID, "告警恢复")
		}
	}
}

func authorizeReleaseWebhook(w http.ResponseWriter, r *http.Request) bool {
	expected := strings.TrimSpace(os.Getenv("RELEASE_WEBHOOK_TOKEN"))
	provided := strings.TrimSpace(r.Header.Get("X-Release-Token"))
	if provided == "" {
		provided = bearerToken(r)
	}
	if expected == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED", "message": "发布事件令牌无效"})
		return false
	}
	return true
}
func authorizeRunnerToken(w http.ResponseWriter, r *http.Request) bool {
	expected := strings.TrimSpace(os.Getenv("ANSIBLE_RUNNER_TOKEN"))
	provided := bearerToken(r)
	if provided == "" {
		provided = strings.TrimSpace(r.Header.Get("X-Runner-Token"))
	}
	if expected == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED", "message": "Runner 认证失败"})
		return false
	}
	return true
}

func runnerAssetCredentialID(asset cmdb.Asset, override string) string {
	if value := strings.TrimSpace(override); value != "" {
		return value
	}
	for _, attr := range asset.Attributes {
		name := strings.ToLower(strings.TrimSpace(attr.Name))
		if name == "ssh_credential_id" || name == "sshcredentialid" || name == "ssh credential id" {
			return strings.TrimSpace(attr.Value)
		}
	}
	return ""
}

func runnerAssetSSHPort(asset cmdb.Asset) int {
	for _, attr := range asset.Attributes {
		name := strings.ToLower(strings.TrimSpace(attr.Name))
		if name != "ssh_port" && name != "sshport" && name != "ssh port" {
			continue
		}
		if port, err := strconv.Atoi(strings.TrimSpace(attr.Value)); err == nil && port > 0 && port <= 65535 {
			return port
		}
	}
	return 22
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
		if item == permission || item == "*" {
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
