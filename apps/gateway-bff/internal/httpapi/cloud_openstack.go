package httpapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/credentials"
)

type openStackClient struct {
	authURL, region, projectName, userDomain, projectDomain, username, password string
	computeURL, token                                                           string
	client                                                                      *http.Client
}
type openStackAddress struct {
	Address string `json:"addr"`
	Type    string `json:"OS-EXT-IPS:type"`
	Version int    `json:"version"`
}
type openStackServer struct {
	ID        string                        `json:"id"`
	Name      string                        `json:"name"`
	Status    string                        `json:"status"`
	Addresses map[string][]openStackAddress `json:"addresses"`
	Flavor    struct {
		ID string `json:"id"`
	} `json:"flavor"`
	Image struct {
		ID string `json:"id"`
	} `json:"image"`
	AZ string `json:"OS-EXT-AZ:availability_zone"`
}

func registerOpenStackRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, cmdbService *cmdb.Service, credentialsService *credentials.Service) {
	mux.HandleFunc("GET /api/v1/cmdb/cloud-openstack/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		config, ok := cmdbService.IntegrationConfig("openstack")
		if !ok {
			config = cmdb.IntegrationConfig{Provider: "openstack", AuthType: "password"}
		}
		writeJSON(w, http.StatusOK, openStackConfigResponse(config))
	})
	mux.HandleFunc("PUT /api/v1/cmdb/cloud-openstack/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			AuthURL       string `json:"authUrl"`
			RegionID      string `json:"regionId"`
			ProjectName   string `json:"projectName"`
			UserDomain    string `json:"userDomain"`
			ProjectDomain string `json:"projectDomain"`
			Username      string `json:"username"`
			Password      string `json:"password"`
			Insecure      bool   `json:"insecure"`
			Enabled       bool   `json:"enabled"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		existing, _ := cmdbService.IntegrationConfig("openstack")
		credentialID := existing.CredentialID
		if strings.TrimSpace(input.Password) != "" {
			credential, err := credentialsService.Create(credentials.CreateInput{Name: "OpenStack 集成凭据", Kind: "openstack", Username: input.Username, Secret: input.Password, Group: "integrations", Description: "OpenStack Keystone 登录凭据"})
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "VAULT_UNAVAILABLE", "message": err.Error()})
				return
			}
			credentialID = credential.ID
		}
		authURL := normalizeOpenStackAuthURL(input.AuthURL)
		if authURL == "" || strings.TrimSpace(input.ProjectName) == "" || strings.TrimSpace(input.Username) == "" || (credentialID == "" && strings.TrimSpace(input.Password) == "") {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "OpenStack 认证地址、项目、用户名和密码不能为空"})
			return
		}
		fragment := url.Values{"region": {strings.TrimSpace(input.RegionID)}, "project": {strings.TrimSpace(input.ProjectName)}, "user_domain": {strings.TrimSpace(input.UserDomain)}, "project_domain": {strings.TrimSpace(input.ProjectDomain)}}.Encode()
		config, err := cmdbService.SaveIntegrationConfig(cmdb.IntegrationConfigInput{Provider: "openstack", URL: authURL + "#" + fragment, Username: strings.TrimSpace(input.Username), CredentialID: credentialID, AuthType: "password", Insecure: input.Insecure, Enabled: input.Enabled}, "api")
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "OpenStack 认证地址、项目、用户名和密码不能为空"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "cmdb.openstack_config.update", "openstack", config.URL)
		response := openStackConfigResponse(config)
		response["regionId"] = strings.TrimSpace(input.RegionID)
		response["projectName"] = strings.TrimSpace(input.ProjectName)
		response["userDomain"] = strings.TrimSpace(input.UserDomain)
		response["projectDomain"] = strings.TrimSpace(input.ProjectDomain)
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("POST /api/v1/cmdb/cloud-openstack/config/test", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			AuthURL       string `json:"authUrl"`
			RegionID      string `json:"regionId"`
			ProjectName   string `json:"projectName"`
			UserDomain    string `json:"userDomain"`
			ProjectDomain string `json:"projectDomain"`
			Username      string `json:"username"`
			Password      string `json:"password"`
			Insecure      bool   `json:"insecure"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		client, err := buildOpenStackClient(input.AuthURL, input.RegionID, input.ProjectName, input.UserDomain, input.ProjectDomain, input.Username, input.Password, input.Insecure)
		if err == nil {
			_, err = client.Instances(r.Context())
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "OPENSTACK_TEST_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "OpenStack API 连接成功"})
	})
	mux.HandleFunc("GET /api/v1/cmdb/cloud-openstack/inventory", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		client, err := newConfiguredOpenStackClient(cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "OPENSTACK_NOT_READY", "message": err.Error()})
			return
		}
		instances, err := client.Instances(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "OPENSTACK_INVENTORY_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cloudInventory{Configured: true, Provider: "openstack", RegionID: client.region, Instances: instances, FetchedAt: time.Now().Format("2006-01-02 15:04:05")})
	})
	mux.HandleFunc("POST /api/v1/cmdb/cloud-openstack/sync", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		client, err := newConfiguredOpenStackClient(cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "OPENSTACK_NOT_READY", "message": err.Error()})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		result, err := syncOpenStackInventory(r.Context(), cmdbService, client, user.Username)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "OPENSTACK_SYNC_FAILED", "message": err.Error()})
			return
		}
		auditService.Record(user.Username, "cmdb.openstack_sync", "openstack", fmt.Sprintf("region=%s project=%s instances=%d", result.RegionID, client.projectName, result.Instances))
		writeJSON(w, http.StatusOK, result)
	})
}

func openStackConfigResponse(config cmdb.IntegrationConfig) map[string]any {
	authURL, region, projectName, userDomain, projectDomain := parseOpenStackURL(config.URL)
	return map[string]any{"configured": config.URL != "" && config.Enabled, "provider": "openstack", "authUrl": authURL, "regionId": region, "projectName": projectName, "userDomain": userDomain, "projectDomain": projectDomain, "username": config.Username, "insecure": config.Insecure, "enabled": config.Enabled, "credentialId": config.CredentialID, "updatedAt": config.UpdatedAt}
}
func parseOpenStackURL(value string) (string, string, string, string, string) {
	authURL := value
	region, projectName, userDomain, projectDomain := "", "", "", ""
	if index := strings.Index(value, "#"); index >= 0 {
		authURL = value[:index]
		values, _ := url.ParseQuery(value[index+1:])
		region = values.Get("region")
		projectName = values.Get("project")
		userDomain = values.Get("user_domain")
		projectDomain = values.Get("project_domain")
	}
	return authURL, region, projectName, userDomain, projectDomain
}
func normalizeOpenStackAuthURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") {
		value = "https://" + value
	}
	return strings.TrimRight(value, "/")
}
func openStackTokenURL(authURL string) string {
	authURL = strings.TrimRight(normalizeOpenStackAuthURL(authURL), "/")
	if !strings.HasSuffix(authURL, "/v3") {
		authURL += "/v3"
	}
	return authURL + "/auth/tokens"
}
func newConfiguredOpenStackClient(cmdbService *cmdb.Service, credentialsService *credentials.Service) (*openStackClient, error) {
	config, ok := cmdbService.IntegrationConfig("openstack")
	if !ok || !config.Enabled {
		return nil, errors.New("OpenStack 集成未配置")
	}
	authURL, region, projectName, userDomain, projectDomain := parseOpenStackURL(config.URL)
	username, password := config.Username, ""
	if config.CredentialID != "" {
		material, err := credentialsService.Resolve(config.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("读取 OpenStack 凭据失败: %w", err)
		}
		if username == "" {
			username = material.Username
		}
		password = material.Secret
	}
	return buildOpenStackClient(authURL, region, projectName, userDomain, projectDomain, username, password, config.Insecure)
}
func buildOpenStackClient(authURL, region, projectName, userDomain, projectDomain, username, password string, insecure bool) (*openStackClient, error) {
	authURL = normalizeOpenStackAuthURL(authURL)
	region = strings.TrimSpace(region)
	projectName = strings.TrimSpace(projectName)
	userDomain = strings.TrimSpace(userDomain)
	projectDomain = strings.TrimSpace(projectDomain)
	username = strings.TrimSpace(username)
	if authURL == "" || projectName == "" || username == "" || password == "" {
		return nil, errors.New("OpenStack 认证地址、项目、用户名和密码不能为空")
	}
	if userDomain == "" {
		userDomain = "Default"
	}
	if projectDomain == "" {
		projectDomain = userDomain
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure}}
	return &openStackClient{authURL: authURL, region: region, projectName: projectName, userDomain: userDomain, projectDomain: projectDomain, username: username, password: password, client: &http.Client{Timeout: 45 * time.Second, Transport: transport}}, nil
}

func (c *openStackClient) authenticate(ctx context.Context) error {
	payload := map[string]any{"auth": map[string]any{"identity": map[string]any{"methods": []string{"password"}, "password": map[string]any{"user": map[string]any{"name": c.username, "domain": map[string]string{"name": c.userDomain}, "password": c.password}}}, "scope": map[string]any{"project": map[string]any{"name": c.projectName, "domain": map[string]string{"name": c.projectDomain}}}}}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, openStackTokenURL(c.authURL), strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusCreated {
		return fmt.Errorf("OpenStack Keystone 返回 %s: %s", response.Status, string(raw))
	}
	token := response.Header.Get("X-Subject-Token")
	if token == "" {
		return errors.New("OpenStack Keystone 未返回 X-Subject-Token")
	}
	var authResponse struct {
		Token struct {
			Catalog []struct {
				Type      string `json:"type"`
				Endpoints []struct {
					Interface string `json:"interface"`
					Region    string `json:"region"`
					URL       string `json:"url"`
				} `json:"endpoints"`
			} `json:"catalog"`
		} `json:"token"`
	}
	if err = json.Unmarshal(raw, &authResponse); err != nil {
		return err
	}
	for _, service := range authResponse.Token.Catalog {
		if !strings.EqualFold(service.Type, "compute") {
			continue
		}
		for _, endpoint := range service.Endpoints {
			if endpoint.URL == "" || (!strings.EqualFold(endpoint.Interface, "public") && !strings.EqualFold(endpoint.Interface, "internal")) {
				continue
			}
			if c.region != "" && endpoint.Region != "" && !strings.EqualFold(endpoint.Region, c.region) {
				continue
			}
			c.computeURL = strings.TrimRight(endpoint.URL, "/")
			break
		}
		if c.computeURL != "" {
			break
		}
	}
	if c.computeURL == "" {
		return errors.New("OpenStack Keystone 未返回可用的 Compute 服务地址")
	}
	c.token = token
	return nil
}

func (c *openStackClient) Instances(ctx context.Context) ([]cloudInstance, error) {
	if err := c.authenticate(ctx); err != nil {
		return nil, err
	}
	const pageSize = 100
	result := []cloudInstance{}
	nextURL := c.computeURL + "/servers/detail?limit=" + strconv.Itoa(pageSize)
	for nextURL != "" {
		servers, next, err := c.servers(ctx, nextURL)
		if err != nil {
			return nil, err
		}
		for _, server := range servers {
			publicIP, privateIP := openStackServerIPs(server.Addresses)
			result = append(result, cloudInstance{InstanceID: server.ID, Name: server.Name, Status: server.Status, PublicIP: publicIP, PrivateIP: privateIP, ZoneID: server.AZ, InstanceType: server.Flavor.ID, OSName: server.Image.ID, RegionID: c.region})
		}
		if next == "" && len(servers) == pageSize {
			next = fmt.Sprintf("%s/servers/detail?limit=%d&marker=%s", c.computeURL, pageSize, url.QueryEscape(servers[len(servers)-1].ID))
		}
		nextURL = resolveOpenStackURL(c.computeURL, next)
	}
	return result, nil
}
func (c *openStackClient) servers(ctx context.Context, requestURL string) ([]openStackServer, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("X-Auth-Token", c.token)
	request.Header.Set("Accept", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("OpenStack Nova 返回 %s: %s", response.Status, string(raw))
	}
	var payload struct {
		Servers      []openStackServer `json:"servers"`
		ServersLinks []struct {
			Rel  string `json:"rel"`
			Href string `json:"href"`
		} `json:"servers_links"`
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return nil, "", err
	}
	next := ""
	for _, link := range payload.ServersLinks {
		if strings.EqualFold(link.Rel, "next") {
			next = link.Href
			break
		}
	}
	return payload.Servers, next, nil
}
func resolveOpenStackURL(baseURL, value string) string {
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.IsAbs() {
		return value
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return value
	}
	reference, err := url.Parse(value)
	if err != nil {
		return value
	}
	return base.ResolveReference(reference).String()
}
func openStackServerIPs(addresses map[string][]openStackAddress) (string, string) {
	publicIP, privateIP := "", ""
	for _, items := range addresses {
		for _, item := range items {
			if item.Version != 4 || item.Address == "" {
				continue
			}
			if strings.EqualFold(item.Type, "floating") && publicIP == "" {
				publicIP = item.Address
			}
			if strings.EqualFold(item.Type, "fixed") && privateIP == "" {
				privateIP = item.Address
			}
		}
	}
	return publicIP, privateIP
}
func openStackAssetStatus(status string) string {
	switch strings.ToUpper(status) {
	case "ACTIVE":
		return "online"
	case "BUILD", "REBOOT", "RESIZE", "MIGRATING", "PAUSED", "SUSPENDED":
		return "warning"
	case "ERROR":
		return "error"
	default:
		return "offline"
	}
}
func syncOpenStackInventory(ctx context.Context, cmdbService *cmdb.Service, client *openStackClient, operator string) (cloudSyncResult, error) {
	instances, err := client.Instances(ctx)
	if err != nil {
		return cloudSyncResult{}, err
	}
	result := cloudSyncResult{Provider: "openstack", RegionID: client.region}
	for _, vm := range instances {
		ip := vm.PublicIP
		if ip == "" {
			ip = vm.PrivateIP
		}
		input := cmdb.CreateAssetInput{ID: "openstack-server-" + safeAssetID(vm.InstanceID), Name: vm.Name, Type: "cloud-host", Status: openStackAssetStatus(vm.Status), IP: ip, Environment: "云平台", ProjectGroup: "云平台组", Owner: "OpenStack", Location: "OpenStack / " + client.region + " / " + vm.ZoneID, Source: "OpenStack", Tags: []string{"OpenStack", "Nova", "云主机"}, Attributes: []cmdb.Attribute{{Name: "cloud_provider", Label: "云厂商", Value: "OpenStack"}, {Name: "cloud_region", Label: "地域", Value: client.region}, {Name: "cloud_project", Label: "项目", Value: client.projectName}, {Name: "cloud_instance_id", Label: "实例 ID", Value: vm.InstanceID}, {Name: "cloud_instance_type", Label: "实例规格", Value: vm.InstanceType}, {Name: "cloud_os", Label: "操作系统", Value: vm.OSName}, {Name: "power_state", Label: "电源状态", Value: vm.Status}}}
		if _, err = cmdbService.UpsertExternalAsset(input, operator); err != nil {
			return result, err
		}
		result.Instances++
	}
	return result, nil
}
