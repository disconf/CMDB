package httpapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/credentials"
)

type vmwareCluster struct {
	ID         string `json:"cluster"`
	Name       string `json:"name"`
	DRSEnabled bool   `json:"drs_enabled"`
	HAEnabled  bool   `json:"ha_enabled"`
}
type vmwareHost struct {
	ID              string `json:"host"`
	Name            string `json:"name"`
	ConnectionState string `json:"connection_state"`
	PowerState      string `json:"power_state"`
}
type vmwareVM struct {
	ID            string `json:"vm"`
	Name          string `json:"name"`
	PowerState    string `json:"power_state"`
	CPUCount      int    `json:"cpu_count"`
	MemorySizeMiB int    `json:"memory_size_MiB"`
}
type vmwareInventory struct {
	Configured bool            `json:"configured"`
	URL        string          `json:"url,omitempty"`
	Clusters   []vmwareCluster `json:"clusters"`
	Hosts      []vmwareHost    `json:"hosts"`
	VMs        []vmwareVM      `json:"vms"`
	FetchedAt  string          `json:"fetchedAt"`
}
type vmwareSyncResult struct {
	Clusters int `json:"clusters"`
	Hosts    int `json:"hosts"`
	VMs      int `json:"vms"`
}
type vmwareClient struct {
	baseURL, username, password, sessionID string
	client                                 *http.Client
}

func registerVMwareRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, cmdbService *cmdb.Service, credentialsService *credentials.Service) {
	mux.HandleFunc("GET /api/v1/cmdb/vmware/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		config, ok := cmdbService.IntegrationConfig("vmware")
		if !ok {
			config = cmdb.IntegrationConfig{Provider: "vmware", AuthType: "password"}
		}
		writeJSON(w, http.StatusOK, vmwareConfigResponse(config))
	})
	mux.HandleFunc("PUT /api/v1/cmdb/vmware/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			URL      string `json:"url"`
			Username string `json:"username"`
			Secret   string `json:"secret"`
			Insecure bool   `json:"insecure"`
			Enabled  bool   `json:"enabled"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		existing, _ := cmdbService.IntegrationConfig("vmware")
		credentialID := existing.CredentialID
		if strings.TrimSpace(input.Secret) != "" {
			credential, err := credentialsService.Create(credentials.CreateInput{Name: "VMware vCenter 集成凭据", Kind: "vcenter", Username: input.Username, Secret: input.Secret, Group: "integrations", Description: "VMware vCenter API 登录凭据"})
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "VAULT_UNAVAILABLE", "message": err.Error()})
				return
			}
			credentialID = credential.ID
		}
		config, err := cmdbService.SaveIntegrationConfig(cmdb.IntegrationConfigInput{Provider: "vmware", URL: input.URL, Username: input.Username, CredentialID: credentialID, AuthType: "password", Insecure: input.Insecure, Enabled: input.Enabled}, "api")
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "VMware vCenter 地址不能为空"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "cmdb.vmware_config.update", "vmware", config.URL)
		writeJSON(w, http.StatusOK, vmwareConfigResponse(config))
	})
	mux.HandleFunc("POST /api/v1/cmdb/vmware/config/test", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			URL      string `json:"url"`
			Username string `json:"username"`
			Secret   string `json:"secret"`
			Insecure bool   `json:"insecure"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		client, err := buildVMwareClient(r.Context(), input.URL, input.Username, input.Secret, input.Insecure)
		if err == nil {
			_, err = client.Clusters(r.Context())
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "VMWARE_TEST_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "VMware vCenter API 连接成功"})
	})
	mux.HandleFunc("GET /api/v1/cmdb/vmware/inventory", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		client, err := newConfiguredVMwareClient(r.Context(), cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "VMWARE_NOT_READY", "message": err.Error()})
			return
		}
		inventory, err := client.Inventory(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "VMWARE_INVENTORY_FAILED", "message": err.Error()})
			return
		}
		inventory.Configured, inventory.URL, inventory.FetchedAt = true, client.baseURL, time.Now().Format("2006-01-02 15:04:05")
		writeJSON(w, http.StatusOK, inventory)
	})
	mux.HandleFunc("POST /api/v1/cmdb/vmware/sync", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		client, err := newConfiguredVMwareClient(r.Context(), cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "VMWARE_NOT_READY", "message": err.Error()})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		result, err := syncVMwareInventory(r.Context(), cmdbService, client, user.Username)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "VMWARE_SYNC_FAILED", "message": err.Error()})
			return
		}
		auditService.Record(user.Username, "cmdb.vmware_sync", "vmware", fmt.Sprintf("clusters=%d hosts=%d vms=%d", result.Clusters, result.Hosts, result.VMs))
		writeJSON(w, http.StatusOK, result)
	})
}

func vmwareConfigResponse(config cmdb.IntegrationConfig) map[string]any {
	return map[string]any{"configured": config.URL != "" && config.Enabled, "provider": "vmware", "url": config.URL, "username": config.Username, "insecure": config.Insecure, "enabled": config.Enabled, "credentialId": config.CredentialID, "updatedAt": config.UpdatedAt}
}

func newConfiguredVMwareClient(ctx context.Context, cmdbService *cmdb.Service, credentialsService *credentials.Service) (*vmwareClient, error) {
	config, ok := cmdbService.IntegrationConfig("vmware")
	if !ok || !config.Enabled || strings.TrimSpace(config.URL) == "" {
		return nil, errors.New("VMware vCenter 未配置")
	}
	username, secret := config.Username, ""
	if config.CredentialID != "" {
		material, err := credentialsService.Resolve(config.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("读取 VMware vCenter 凭据失败: %w", err)
		}
		if username == "" {
			username = material.Username
		}
		secret = material.Secret
	}
	return buildVMwareClient(ctx, config.URL, username, secret, config.Insecure)
}

func buildVMwareClient(ctx context.Context, baseURL, username, password string, insecure bool) (*vmwareClient, error) {
	baseURL, username, password = strings.TrimRight(strings.TrimSpace(baseURL), "/"), strings.TrimSpace(username), strings.TrimSpace(password)
	if baseURL == "" || username == "" || password == "" {
		return nil, errors.New("VMware vCenter 地址、用户名和密码不能为空")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure}}
	client := &vmwareClient{baseURL: baseURL, username: username, password: password, client: &http.Client{Timeout: 45 * time.Second, Transport: transport}}
	if err := client.login(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *vmwareClient) login(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/session", nil)
	if err != nil {
		return err
	}
	request.SetBasicAuth(c.username, c.password)
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("VMware vCenter 登录返回 %s", response.Status)
	}
	c.sessionID = strings.TrimSpace(response.Header.Get("vmware-api-session-id"))
	if c.sessionID == "" {
		return errors.New("VMware vCenter 登录未返回 session id")
	}
	return nil
}

func (c *vmwareClient) getJSON(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("vmware-api-session-id", c.sessionID)
	request.Header.Set("Accept", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("VMware API %s returned %s", path, response.Status)
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func (c *vmwareClient) Clusters(ctx context.Context) ([]vmwareCluster, error) {
	items := []vmwareCluster{}
	if err := c.getJSON(ctx, "/api/vcenter/cluster", &items); err != nil {
		return nil, err
	}
	return items, nil
}
func (c *vmwareClient) Hosts(ctx context.Context) ([]vmwareHost, error) {
	items := []vmwareHost{}
	if err := c.getJSON(ctx, "/api/vcenter/host", &items); err != nil {
		return nil, err
	}
	return items, nil
}
func (c *vmwareClient) VMs(ctx context.Context) ([]vmwareVM, error) {
	items := []vmwareVM{}
	if err := c.getJSON(ctx, "/api/vcenter/vm", &items); err != nil {
		return nil, err
	}
	return items, nil
}
func (c *vmwareClient) Inventory(ctx context.Context) (vmwareInventory, error) {
	clusters, err := c.Clusters(ctx)
	if err != nil {
		return vmwareInventory{}, err
	}
	hosts, err := c.Hosts(ctx)
	if err != nil {
		return vmwareInventory{}, err
	}
	vms, err := c.VMs(ctx)
	if err != nil {
		return vmwareInventory{}, err
	}
	return vmwareInventory{Clusters: clusters, Hosts: hosts, VMs: vms}, nil
}

func syncVMwareInventory(ctx context.Context, cmdbService *cmdb.Service, client *vmwareClient, operator string) (vmwareSyncResult, error) {
	inventory, err := client.Inventory(ctx)
	if err != nil {
		return vmwareSyncResult{}, err
	}
	result := vmwareSyncResult{}
	for _, cluster := range inventory.Clusters {
		input := cmdb.CreateAssetInput{ID: "vmware-cluster-" + safeAssetID(cluster.ID), Name: cluster.Name, Type: "virtualization-cluster", Status: "online", Environment: "虚拟化平台", ProjectGroup: "虚拟化平台组", Owner: "VMware", Location: "vCenter / " + cluster.Name, Source: "vCenter", Tags: []string{"VMware", "vCenter", "虚拟化"}, Attributes: []cmdb.Attribute{{Name: "vmware_cluster_id", Label: "vCenter 集群 ID", Value: cluster.ID}, {Name: "drs_enabled", Label: "DRS", Value: fmt.Sprintf("%t", cluster.DRSEnabled)}, {Name: "ha_enabled", Label: "HA", Value: fmt.Sprintf("%t", cluster.HAEnabled)}}}
		if _, err = cmdbService.UpsertExternalAsset(input, operator); err != nil {
			return result, err
		}
		result.Clusters++
	}
	for _, host := range inventory.Hosts {
		input := cmdb.CreateAssetInput{ID: "vmware-host-" + safeAssetID(host.ID), Name: host.Name, Type: "physical-server", Status: vmwareHostStatus(host.ConnectionState), Environment: "虚拟化平台", ProjectGroup: "虚拟化平台组", Owner: "VMware", Location: "vCenter / ESXi", Source: "vCenter", Tags: []string{"VMware", "ESXi", "虚拟化宿主"}, Attributes: []cmdb.Attribute{{Name: "vmware_host_id", Label: "vCenter 主机 ID", Value: host.ID}, {Name: "connection_state", Label: "连接状态", Value: host.ConnectionState}, {Name: "power_state", Label: "电源状态", Value: host.PowerState}, {Name: "hypervisor_vendor", Label: "虚拟化厂商", Value: "VMware"}}}
		if _, err = cmdbService.UpsertExternalAsset(input, operator); err != nil {
			return result, err
		}
		result.Hosts++
	}
	for _, vm := range inventory.VMs {
		status := "offline"
		if strings.EqualFold(vm.PowerState, "POWERED_ON") {
			status = "online"
		}
		input := cmdb.CreateAssetInput{ID: "vmware-vm-" + safeAssetID(vm.ID), Name: vm.Name, Type: "virtual-machine", Status: status, Environment: "虚拟化平台", ProjectGroup: "虚拟化平台组", Owner: "VMware", Location: "vCenter / VM", Source: "vCenter", Tags: []string{"VMware", "vCenter", "虚拟机"}, Attributes: []cmdb.Attribute{{Name: "vmware_vm_id", Label: "vCenter 虚拟机 ID", Value: vm.ID}, {Name: "power_state", Label: "电源状态", Value: vm.PowerState}, {Name: "cpu_count", Label: "vCPU", Value: fmt.Sprintf("%d", vm.CPUCount)}, {Name: "memory_mib", Label: "内存(MiB)", Value: fmt.Sprintf("%d", vm.MemorySizeMiB)}, {Name: "virtualization_type", Label: "虚拟化类型", Value: "VMware"}}}
		if _, err = cmdbService.UpsertExternalAsset(input, operator); err != nil {
			return result, err
		}
		result.VMs++
	}
	return result, nil
}

func vmwareHostStatus(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "CONNECTED":
		return "online"
	case "DISCONNECTED", "NOT_RESPONDING":
		return "offline"
	default:
		return "warning"
	}
}
