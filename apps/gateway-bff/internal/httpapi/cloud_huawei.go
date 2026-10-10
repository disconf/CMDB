package httpapi

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/credentials"
)

type huaweiCloudClient struct {
	endpoint, region, projectID, accessKeyID, secretAccessKey string
	client                                                    *http.Client
}
type huaweiServer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Flavor struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"flavor"`
	Addresses map[string][]huaweiAddress `json:"addresses"`
	Metadata  map[string]string          `json:"metadata"`
	AZ        string                     `json:"OS-EXT-AZ:availability_zone"`
	CreatedAt string                     `json:"created"`
	Image     struct {
		ID string `json:"id"`
	} `json:"image"`
}
type huaweiAddress struct {
	Address string `json:"addr"`
	Type    string `json:"OS-EXT-IPS:type"`
	Version int    `json:"version"`
}

func registerHuaweiCloudRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, cmdbService *cmdb.Service, credentialsService *credentials.Service) {
	mux.HandleFunc("GET /api/v1/cmdb/cloud-huawei/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		config, ok := cmdbService.IntegrationConfig("huawei")
		if !ok {
			config = cmdb.IntegrationConfig{Provider: "huawei", AuthType: "access-key"}
		}
		writeJSON(w, http.StatusOK, huaweiConfigResponse(config))
	})
	mux.HandleFunc("PUT /api/v1/cmdb/cloud-huawei/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			Endpoint        string `json:"endpoint"`
			RegionID        string `json:"regionId"`
			ProjectID       string `json:"projectId"`
			AccessKeyID     string `json:"accessKeyId"`
			AccessKeySecret string `json:"accessKeySecret"`
			Insecure        bool   `json:"insecure"`
			Enabled         bool   `json:"enabled"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		existing, _ := cmdbService.IntegrationConfig("huawei")
		credentialID := existing.CredentialID
		if strings.TrimSpace(input.AccessKeySecret) != "" {
			credential, err := credentialsService.Create(credentials.CreateInput{Name: "华为云 ECS 集成凭据", Kind: "huawei-cloud", Username: input.AccessKeyID, Secret: input.AccessKeySecret, Group: "integrations", Description: "华为云 AccessKey 凭据"})
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "VAULT_UNAVAILABLE", "message": err.Error()})
				return
			}
			credentialID = credential.ID
		}
		endpoint := normalizeHuaweiEndpoint(input.Endpoint, input.RegionID)
		fragment := url.Values{"region": {strings.TrimSpace(input.RegionID)}, "project": {strings.TrimSpace(input.ProjectID)}}.Encode()
		config, err := cmdbService.SaveIntegrationConfig(cmdb.IntegrationConfigInput{Provider: "huawei", URL: endpoint + "#" + fragment, Username: strings.TrimSpace(input.AccessKeyID), CredentialID: credentialID, AuthType: "access-key", Insecure: input.Insecure, Enabled: input.Enabled}, "api")
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "华为云地域、项目和 AccessKey 不能为空"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "cmdb.huawei_config.update", "huawei", config.URL)
		response := huaweiConfigResponse(config)
		response["regionId"] = strings.TrimSpace(input.RegionID)
		response["projectId"] = strings.TrimSpace(input.ProjectID)
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("POST /api/v1/cmdb/cloud-huawei/config/test", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			Endpoint        string `json:"endpoint"`
			RegionID        string `json:"regionId"`
			ProjectID       string `json:"projectId"`
			AccessKeyID     string `json:"accessKeyId"`
			AccessKeySecret string `json:"accessKeySecret"`
			Insecure        bool   `json:"insecure"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		client, err := buildHuaweiClient(input.Endpoint, input.RegionID, input.ProjectID, input.AccessKeyID, input.AccessKeySecret, input.Insecure)
		if err == nil {
			_, err = client.Instances(r.Context())
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "HUAWEI_TEST_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "华为云 ECS API 连接成功"})
	})
	mux.HandleFunc("GET /api/v1/cmdb/cloud-huawei/inventory", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		client, err := newConfiguredHuaweiClient(cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "HUAWEI_NOT_READY", "message": err.Error()})
			return
		}
		instances, err := client.Instances(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "HUAWEI_INVENTORY_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cloudInventory{Configured: true, Provider: "huawei", RegionID: client.region, Instances: instances, FetchedAt: time.Now().Format("2006-01-02 15:04:05")})
	})
	mux.HandleFunc("POST /api/v1/cmdb/cloud-huawei/sync", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		client, err := newConfiguredHuaweiClient(cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "HUAWEI_NOT_READY", "message": err.Error()})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		result, err := syncHuaweiInventory(r.Context(), cmdbService, client, user.Username)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "HUAWEI_SYNC_FAILED", "message": err.Error()})
			return
		}
		auditService.Record(user.Username, "cmdb.huawei_sync", "huawei", fmt.Sprintf("region=%s project=%s instances=%d", result.RegionID, client.projectID, result.Instances))
		writeJSON(w, http.StatusOK, result)
	})
}

func huaweiConfigResponse(config cmdb.IntegrationConfig) map[string]any {
	endpoint, region, projectID := parseHuaweiURL(config.URL)
	return map[string]any{"configured": config.URL != "" && config.Enabled, "provider": "huawei", "endpoint": endpoint, "regionId": region, "projectId": projectID, "accessKeyId": config.Username, "insecure": config.Insecure, "enabled": config.Enabled, "credentialId": config.CredentialID, "updatedAt": config.UpdatedAt}
}
func parseHuaweiURL(value string) (string, string, string) {
	endpoint := value
	region, projectID := "", ""
	if index := strings.Index(value, "#"); index >= 0 {
		endpoint = value[:index]
		values, _ := url.ParseQuery(value[index+1:])
		region = values.Get("region")
		projectID = values.Get("project")
	}
	return endpoint, region, projectID
}
func normalizeHuaweiEndpoint(endpoint, region string) string {
	endpoint = strings.TrimSpace(endpoint)
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint != "" {
		return endpoint
	}
	return "ecs." + strings.TrimSpace(region) + ".myhuaweicloud.com"
}
func newConfiguredHuaweiClient(cmdbService *cmdb.Service, credentialsService *credentials.Service) (*huaweiCloudClient, error) {
	config, ok := cmdbService.IntegrationConfig("huawei")
	if !ok || !config.Enabled {
		return nil, errors.New("华为云集成未配置")
	}
	endpoint, region, projectID := parseHuaweiURL(config.URL)
	accessKey, secretKey := config.Username, ""
	if config.CredentialID != "" {
		material, err := credentialsService.Resolve(config.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("读取华为云凭据失败: %w", err)
		}
		if accessKey == "" {
			accessKey = material.Username
		}
		secretKey = material.Secret
	}
	return buildHuaweiClient(endpoint, region, projectID, accessKey, secretKey, config.Insecure)
}
func buildHuaweiClient(endpoint, region, projectID, accessKeyID, secretAccessKey string, insecure bool) (*huaweiCloudClient, error) {
	endpoint, region, projectID = strings.TrimSpace(endpoint), strings.TrimSpace(region), strings.TrimSpace(projectID)
	accessKeyID, secretAccessKey = strings.TrimSpace(accessKeyID), strings.TrimSpace(secretAccessKey)
	endpoint = normalizeHuaweiEndpoint(endpoint, region)
	if endpoint == "ecs..myhuaweicloud.com" || region == "" || projectID == "" || accessKeyID == "" || secretAccessKey == "" {
		return nil, errors.New("华为云地域、项目、AccessKey ID 和 SecretAccessKey 不能为空")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure}}
	return &huaweiCloudClient{endpoint: endpoint, region: region, projectID: projectID, accessKeyID: accessKeyID, secretAccessKey: secretAccessKey, client: &http.Client{Timeout: 45 * time.Second, Transport: transport}}, nil
}

func (c *huaweiCloudClient) Instances(ctx context.Context) ([]cloudInstance, error) {
	const pageSize = 100
	result := []cloudInstance{}
	for offset := 0; ; offset += pageSize {
		servers, err := c.servers(ctx, offset, pageSize)
		if err != nil {
			return nil, err
		}
		for _, server := range servers {
			publicIP, privateIP := huaweiServerIPs(server.Addresses)
			instanceType := server.Flavor.Name
			if instanceType == "" {
				instanceType = server.Flavor.ID
			}
			osName := server.Metadata["os_type"]
			if osName == "" {
				osName = server.Metadata["image_name"]
			}
			result = append(result, cloudInstance{InstanceID: server.ID, Name: server.Name, Status: server.Status, PublicIP: publicIP, PrivateIP: privateIP, ZoneID: server.AZ, InstanceType: instanceType, OSName: osName, RegionID: c.region})
		}
		if len(servers) == 0 {
			break
		}
	}
	return result, nil
}
func (c *huaweiCloudClient) servers(ctx context.Context, offset, limit int) ([]huaweiServer, error) {
	path := "/v2.1/" + url.PathEscape(c.projectID) + "/servers/detail"
	query := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	canonicalQuery := huaweiCanonicalQuery(query)
	date := time.Now().UTC().Format("20060102T150405Z")
	canonicalHeaders := "host:" + c.endpoint + "\nx-sdk-date:" + date + "\n"
	signedHeaders := "host;x-sdk-date"
	canonicalRequest := "GET\n" + path + "\n" + canonicalQuery + "\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + sha256Hex("")
	stringToSign := "SDK-HMAC-SHA256\n" + date + "\n" + sha256Hex(canonicalRequest)
	signature := hex.EncodeToString(hmacSHA256([]byte(c.secretAccessKey), stringToSign))
	authorization := "SDK-HMAC-SHA256 Access=" + c.accessKeyID + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature
	requestURL := "https://" + c.endpoint + path
	if canonicalQuery != "" {
		requestURL += "?" + canonicalQuery
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Sdk-Date", date)
	request.Header.Set("Host", c.endpoint)
	request.Header.Set("Authorization", authorization)
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("华为云 API 返回 %s: %s", response.Status, string(raw))
	}
	var payload struct {
		Servers []huaweiServer `json:"servers"`
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return payload.Servers, nil
}
func huaweiCanonicalQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0)
	for _, key := range keys {
		items := append([]string(nil), values[key]...)
		sort.Strings(items)
		for _, value := range items {
			parts = append(parts, huaweiEscape(key)+"="+huaweiEscape(value))
		}
	}
	return strings.Join(parts, "&")
}
func huaweiEscape(value string) string {
	escaped := url.QueryEscape(value)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "*", "%2A")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}
func huaweiServerIPs(addresses map[string][]huaweiAddress) (string, string) {
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
func huaweiAssetStatus(status string) string {
	switch strings.ToUpper(status) {
	case "ACTIVE":
		return "online"
	case "BUILD", "REBOOT", "REBUILD", "RESIZE", "MIGRATING":
		return "warning"
	case "ERROR":
		return "error"
	default:
		return "offline"
	}
}
func syncHuaweiInventory(ctx context.Context, cmdbService *cmdb.Service, client *huaweiCloudClient, operator string) (cloudSyncResult, error) {
	instances, err := client.Instances(ctx)
	if err != nil {
		return cloudSyncResult{}, err
	}
	result := cloudSyncResult{Provider: "huawei", RegionID: client.region}
	for _, vm := range instances {
		ip := vm.PublicIP
		if ip == "" {
			ip = vm.PrivateIP
		}
		input := cmdb.CreateAssetInput{ID: "huawei-ecs-" + safeAssetID(vm.InstanceID), Name: vm.Name, Type: "cloud-host", Status: huaweiAssetStatus(vm.Status), IP: ip, Environment: "云平台", ProjectGroup: "云平台组", Owner: "HuaweiCloud", Location: "华为云 / " + client.region + " / " + vm.ZoneID, Source: "HuaweiCloud", Tags: []string{"华为云", "ECS", "云主机"}, Attributes: []cmdb.Attribute{{Name: "cloud_provider", Label: "云厂商", Value: "HuaweiCloud"}, {Name: "cloud_region", Label: "地域", Value: client.region}, {Name: "cloud_project_id", Label: "项目 ID", Value: client.projectID}, {Name: "cloud_instance_id", Label: "实例 ID", Value: vm.InstanceID}, {Name: "cloud_instance_type", Label: "实例规格", Value: vm.InstanceType}, {Name: "cloud_os", Label: "操作系统", Value: vm.OSName}, {Name: "power_state", Label: "电源状态", Value: vm.Status}}}
		if _, err = cmdbService.UpsertExternalAsset(input, operator); err != nil {
			return result, err
		}
		result.Instances++
	}
	return result, nil
}
