package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/credentials"
)

var cloudSyncProviders = []string{"aliyun", "tencent", "aws", "huawei", "openstack", "vmware"}

type cloudSyncScheduler struct {
	cmdbService        *cmdb.Service
	credentialsService *credentials.Service
	auditService       *audit.Service
	running            sync.Mutex
}

func newCloudSyncScheduler(cmdbService *cmdb.Service, credentialsService *credentials.Service, auditService *audit.Service) *cloudSyncScheduler {
	return &cloudSyncScheduler{cmdbService: cmdbService, credentialsService: credentialsService, auditService: auditService}
}

func (s *cloudSyncScheduler) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDue(ctx)
		}
	}
}

func (s *cloudSyncScheduler) runDue(ctx context.Context) {
	if !s.running.TryLock() {
		return
	}
	defer s.running.Unlock()
	now := time.Now()
	for _, config := range s.cmdbService.IntegrationConfigs() {
		if !config.AutoSyncEnabled || !isCloudSyncProvider(config.Provider) {
			continue
		}
		due, err := integrationTime(config.NextSyncAt)
		if err != nil {
			due = now
		}
		if due.After(now) {
			continue
		}
		s.runAndRecord(ctx, config.Provider, "system-cloud-sync")
	}
}

func (s *cloudSyncScheduler) runAndRecord(ctx context.Context, provider, operator string) (cloudSyncResult, error) {
	config, ok := s.cmdbService.IntegrationConfig(provider)
	if !ok || !config.Enabled {
		return cloudSyncResult{}, errors.New("云平台集成未启用")
	}
	result, err := s.execute(ctx, provider, operator)
	nextSync := time.Now().Add(time.Duration(config.SyncIntervalMinutes) * time.Minute)
	status := "success"
	message := fmt.Sprintf("同步完成，共处理 %d 个资源", result.Instances)
	if err != nil {
		status = "failed"
		message = err.Error()
	}
	if _, saveErr := s.cmdbService.SaveIntegrationSyncResult(provider, status, message, result.Instances, nextSync); saveErr != nil {
		if err == nil {
			err = saveErr
		}
		message = message + "; 保存同步状态失败: " + saveErr.Error()
	}
	s.auditService.Record(operator, "cmdb.cloud_sync."+status, provider, message)
	return result, err
}

func (s *cloudSyncScheduler) execute(ctx context.Context, provider, operator string) (cloudSyncResult, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "aliyun":
		client, err := newConfiguredAliyunClient(s.cmdbService, s.credentialsService)
		if err != nil {
			return cloudSyncResult{}, err
		}
		return syncAliyunInventory(ctx, s.cmdbService, client, operator)
	case "tencent":
		client, err := newConfiguredTencentClient(s.cmdbService, s.credentialsService)
		if err != nil {
			return cloudSyncResult{}, err
		}
		return syncTencentInventory(ctx, s.cmdbService, client, operator)
	case "aws":
		client, err := newConfiguredAWSClient(s.cmdbService, s.credentialsService)
		if err != nil {
			return cloudSyncResult{}, err
		}
		return syncAWSInventory(ctx, s.cmdbService, client, operator)
	case "huawei":
		client, err := newConfiguredHuaweiClient(s.cmdbService, s.credentialsService)
		if err != nil {
			return cloudSyncResult{}, err
		}
		return syncHuaweiInventory(ctx, s.cmdbService, client, operator)
	case "openstack":
		client, err := newConfiguredOpenStackClient(s.cmdbService, s.credentialsService)
		if err != nil {
			return cloudSyncResult{}, err
		}
		return syncOpenStackInventory(ctx, s.cmdbService, client, operator)
	case "vmware":
		client, err := newConfiguredVMwareClient(ctx, s.cmdbService, s.credentialsService)
		if err != nil {
			return cloudSyncResult{}, err
		}
		result, err := syncVMwareInventory(ctx, s.cmdbService, client, operator)
		return cloudSyncResult{Provider: "vmware", Instances: result.Clusters + result.Hosts + result.VMs}, err
	default:
		return cloudSyncResult{}, errors.New("不支持的云平台")
	}
}

func registerCloudSyncRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, cmdbService *cmdb.Service, scheduler *cloudSyncScheduler) {
	mux.HandleFunc("GET /api/v1/cmdb/cloud-sync/schedules", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		configs := cmdbService.IntegrationConfigs()
		items := make([]cmdb.IntegrationConfig, 0, len(configs))
		for _, config := range configs {
			if isCloudSyncProvider(config.Provider) {
				items = append(items, config)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "supportedProviders": cloudSyncProviders})
	})
	mux.HandleFunc("PUT /api/v1/cmdb/cloud-sync/schedules/{provider}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		provider := strings.ToLower(strings.TrimSpace(r.PathValue("provider")))
		if !isCloudSyncProvider(provider) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "UNSUPPORTED_PROVIDER"})
			return
		}
		var input struct {
			Enabled         bool `json:"enabled"`
			IntervalMinutes int  `json:"intervalMinutes"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		item, err := cmdbService.SaveIntegrationSchedule(provider, cmdb.IntegrationScheduleInput{Enabled: input.Enabled, IntervalMinutes: input.IntervalMinutes}, "api")
		if errors.Is(err, cmdb.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "INTEGRATION_NOT_CONFIGURED", "message": "请先配置云平台连接"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "SCHEDULE_SAVE_FAILED", "message": err.Error()})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "cmdb.cloud_schedule.update", provider, fmt.Sprintf("enabled=%t interval=%d", item.AutoSyncEnabled, item.SyncIntervalMinutes))
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/v1/cmdb/cloud-sync/run/{provider}", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		provider := strings.ToLower(strings.TrimSpace(r.PathValue("provider")))
		if !isCloudSyncProvider(provider) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "UNSUPPORTED_PROVIDER"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		result, err := scheduler.runAndRecord(r.Context(), provider, user.Username)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "CLOUD_SYNC_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func isCloudSyncProvider(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	for _, item := range cloudSyncProviders {
		if item == provider {
			return true
		}
	}
	return false
}

func integrationTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, errors.New("next sync time is empty")
	}
	return time.ParseInLocation("2006-01-02 15:04:05", value, time.Local)
}
