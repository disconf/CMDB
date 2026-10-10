package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
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

type cloudInstance struct {
	InstanceID   string
	Name         string
	Status       string
	PublicIP     string
	PrivateIP    string
	ZoneID       string
	InstanceType string
	OSName       string
	CPU          int
	MemoryMiB    int
	RegionID     string
}
type cloudInventory struct {
	Configured bool            `json:"configured"`
	Provider   string          `json:"provider"`
	RegionID   string          `json:"regionId"`
	Instances  []cloudInstance `json:"instances"`
	FetchedAt  string          `json:"fetchedAt"`
}
type cloudSyncResult struct {
	Provider  string `json:"provider"`
	RegionID  string `json:"regionId"`
	Instances int    `json:"instances"`
}
type aliyunClient struct {
	endpoint, accessKeyID, accessKeySecret, regionID string
	client                                           *http.Client
}

func registerCloudRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, cmdbService *cmdb.Service, credentialsService *credentials.Service) {
	mux.HandleFunc("GET /api/v1/cmdb/cloud/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		config, ok := cmdbService.IntegrationConfig("aliyun")
		if !ok {
			config = cmdb.IntegrationConfig{Provider: "aliyun", AuthType: "access-key"}
		}
		writeJSON(w, http.StatusOK, cloudConfigResponse(config))
	})
	mux.HandleFunc("PUT /api/v1/cmdb/cloud/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			Endpoint        string `json:"endpoint"`
			RegionID        string `json:"regionId"`
			AccessKeyID     string `json:"accessKeyId"`
			AccessKeySecret string `json:"accessKeySecret"`
			Insecure        bool   `json:"insecure"`
			Enabled         bool   `json:"enabled"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		existing, _ := cmdbService.IntegrationConfig("aliyun")
		credentialID := existing.CredentialID
		if strings.TrimSpace(input.AccessKeySecret) != "" {
			credential, err := credentialsService.Create(credentials.CreateInput{Name: "阿里云 ECS 集成凭据", Kind: "aliyun", Username: input.AccessKeyID, Secret: input.AccessKeySecret, Group: "integrations", Description: "阿里云 AccessKey 凭据"})
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "VAULT_UNAVAILABLE", "message": err.Error()})
				return
			}
			credentialID = credential.ID
		}
		endpoint := strings.TrimSpace(input.Endpoint)
		if endpoint == "" {
			endpoint = "ecs.aliyuncs.com"
		}
		endpoint = endpoint + "#region=" + url.QueryEscape(strings.TrimSpace(input.RegionID))
		config, err := cmdbService.SaveIntegrationConfig(cmdb.IntegrationConfigInput{Provider: "aliyun", URL: endpoint, Username: input.AccessKeyID, CredentialID: credentialID, AuthType: "access-key", Insecure: input.Insecure, Enabled: input.Enabled}, "api")
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "阿里云地域和 AccessKey 不能为空"})
			return
		}

		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "cmdb.cloud_config.update", "aliyun", config.URL)
		response := cloudConfigResponse(config)
		response["regionId"] = strings.TrimSpace(input.RegionID)
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("POST /api/v1/cmdb/cloud/config/test", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			Endpoint        string `json:"endpoint"`
			RegionID        string `json:"regionId"`
			AccessKeyID     string `json:"accessKeyId"`
			AccessKeySecret string `json:"accessKeySecret"`
			Insecure        bool   `json:"insecure"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		client, err := buildAliyunClient(input.Endpoint, input.RegionID, input.AccessKeyID, input.AccessKeySecret, input.Insecure)
		if err == nil {
			_, err = client.Instances(r.Context())
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "CLOUD_TEST_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "阿里云 ECS API 连接成功"})
	})
	mux.HandleFunc("GET /api/v1/cmdb/cloud/inventory", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		client, err := newConfiguredAliyunClient(cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "CLOUD_NOT_READY", "message": err.Error()})
			return
		}
		instances, err := client.Instances(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "CLOUD_INVENTORY_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cloudInventory{Configured: true, Provider: "aliyun", RegionID: client.regionID, Instances: instances, FetchedAt: time.Now().Format("2006-01-02 15:04:05")})
	})
	mux.HandleFunc("POST /api/v1/cmdb/cloud/sync", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		client, err := newConfiguredAliyunClient(cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "CLOUD_NOT_READY", "message": err.Error()})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		result, err := syncAliyunInventory(r.Context(), cmdbService, client, user.Username)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "CLOUD_SYNC_FAILED", "message": err.Error()})
			return
		}
		auditService.Record(user.Username, "cmdb.cloud_sync", "aliyun", fmt.Sprintf("region=%s instances=%d", result.RegionID, result.Instances))
		writeJSON(w, http.StatusOK, result)
	})
}

func cloudConfigResponse(config cmdb.IntegrationConfig) map[string]any {
	endpoint, region := parseCloudURL(config.URL)
	return map[string]any{"configured": config.URL != "" && config.Enabled, "provider": "aliyun", "endpoint": endpoint, "regionId": region, "accessKeyId": config.Username, "insecure": config.Insecure, "enabled": config.Enabled, "credentialId": config.CredentialID, "updatedAt": config.UpdatedAt}
}
func parseCloudURL(value string) (string, string) {
	endpoint, region := value, ""
	if i := strings.Index(value, "#region="); i >= 0 {
		endpoint = value[:i]
		if decoded, err := url.QueryUnescape(value[i+8:]); err == nil {
			region = decoded
		}
	}
	return endpoint, region
}
func newConfiguredAliyunClient(cmdbService *cmdb.Service, credentialsService *credentials.Service) (*aliyunClient, error) {
	config, ok := cmdbService.IntegrationConfig("aliyun")
	if !ok || !config.Enabled {
		return nil, errors.New("阿里云集成未配置")
	}
	endpoint, region := parseCloudURL(config.URL)
	username, secret := config.Username, ""
	if config.CredentialID != "" {
		material, err := credentialsService.Resolve(config.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("读取阿里云凭据失败: %w", err)
		}
		if username == "" {
			username = material.Username
		}
		secret = material.Secret
	}
	return buildAliyunClient(endpoint, region, username, secret, config.Insecure)
}
func buildAliyunClient(endpoint, region, accessKeyID, accessKeySecret string, insecure bool) (*aliyunClient, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = "ecs.aliyuncs.com"
	}
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	endpoint = strings.TrimRight(endpoint, "/")
	region, accessKeyID, accessKeySecret = strings.TrimSpace(region), strings.TrimSpace(accessKeyID), strings.TrimSpace(accessKeySecret)
	if region == "" || accessKeyID == "" || accessKeySecret == "" {
		return nil, errors.New("阿里云地域、AccessKey ID 和 AccessKey Secret 不能为空")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure}}
	return &aliyunClient{endpoint: endpoint, regionID: region, accessKeyID: accessKeyID, accessKeySecret: accessKeySecret, client: &http.Client{Timeout: 45 * time.Second, Transport: transport}}, nil
}
func (c *aliyunClient) request(ctx context.Context, params map[string]string) ([]byte, error) {
	values := url.Values{}
	values.Set("Action", "DescribeInstances")
	values.Set("Version", "2014-05-26")
	values.Set("Format", "JSON")
	values.Set("SignatureMethod", "HMAC-SHA1")
	values.Set("SignatureVersion", "1.0")
	values.Set("SignatureNonce", strconv.FormatInt(time.Now().UnixNano(), 10))
	values.Set("Timestamp", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	values.Set("AccessKeyId", c.accessKeyID)
	values.Set("RegionId", c.regionID)
	values.Set("PageSize", "100")
	for k, v := range params {
		values.Set(k, v)
	}
	canonical := aliyunEscape(c.percentEncode(values))
	stringToSign := "GET&%2F&" + canonical
	mac := hmac.New(sha1.New, []byte(c.accessKeySecret+"&"))
	mac.Write([]byte(stringToSign))
	values.Set("Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	requestURL := "https://" + c.endpoint + "/?" + values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	body := string(raw)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("阿里云 API 返回 %s: %s", response.Status, body)
	}
	if strings.Contains(body, `"Code"`) {
		return nil, fmt.Errorf("阿里云 API 错误: %s", body)
	}
	return raw, nil
}

func (c *aliyunClient) percentEncode(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		for _, value := range values[key] {
			parts = append(parts, aliyunEscape(key)+"="+aliyunEscape(value))
		}
	}
	return strings.Join(parts, "&")
}
func aliyunEscape(value string) string {
	encoded := url.QueryEscape(value)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}

type aliyunDescribeInstancesResponse struct {
	Instances struct {
		Instance []struct {
			InstanceID   string `json:"InstanceId"`
			InstanceName string `json:"InstanceName"`
			Status       string `json:"Status"`
			ZoneID       string `json:"ZoneId"`
			InstanceType string `json:"InstanceType"`
			OSName       string `json:"OSName"`
			CPU          int    `json:"Cpu"`
			Memory       int    `json:"Memory"`
			PublicIP     struct {
				IP []string `json:"IpAddress"`
			} `json:"PublicIpAddress"`
			PrivateIP struct {
				IP []string `json:"IpAddress"`
			} `json:"PrivateIpAddress"`
		} `json:"Instance"`
	} `json:"Instances"`
}

func (c *aliyunClient) Instances(ctx context.Context) ([]cloudInstance, error) {
	body, err := c.request(ctx, nil)
	if err != nil {
		return nil, err
	}
	var payload aliyunDescribeInstancesResponse
	if err = json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	result := []cloudInstance{}
	for _, item := range payload.Instances.Instance {
		publicIP, privateIP := "", ""
		if len(item.PublicIP.IP) > 0 {
			publicIP = item.PublicIP.IP[0]
		}
		if len(item.PrivateIP.IP) > 0 {
			privateIP = item.PrivateIP.IP[0]
		}
		result = append(result, cloudInstance{InstanceID: item.InstanceID, Name: item.InstanceName, Status: item.Status, PublicIP: publicIP, PrivateIP: privateIP, ZoneID: item.ZoneID, InstanceType: item.InstanceType, OSName: item.OSName, CPU: item.CPU, MemoryMiB: item.Memory, RegionID: c.regionID})
	}
	return result, nil
}
func syncAliyunInventory(ctx context.Context, cmdbService *cmdb.Service, client *aliyunClient, operator string) (cloudSyncResult, error) {
	instances, err := client.Instances(ctx)
	if err != nil {
		return cloudSyncResult{}, err
	}
	result := cloudSyncResult{Provider: "aliyun", RegionID: client.regionID}
	regionRelation, err := ensureCloudRegionRelation(ctx, cmdbService, "aliyun", client.regionID, operator)
	if err != nil {
		return result, err
	}
	for _, vm := range instances {
		ip := vm.PublicIP
		if ip == "" {
			ip = vm.PrivateIP
		}
		input := cmdb.CreateAssetInput{ID: "aliyun-ecs-" + safeAssetID(vm.InstanceID), Name: vm.Name, Type: "cloud-host", Status: strings.ToLower(vm.Status), IP: ip, Environment: "云平台", ProjectGroup: "云平台组", Owner: "AlibabaCloud", Location: "阿里云 / " + client.regionID + " / " + vm.ZoneID, Source: "AlibabaCloud", Tags: []string{"阿里云", "ECS", "云主机"}, Attributes: []cmdb.Attribute{{Name: "cloud_provider", Label: "云厂商", Value: "AlibabaCloud"}, {Name: "cloud_region", Label: "地域", Value: client.regionID}, {Name: "cloud_instance_id", Label: "实例 ID", Value: vm.InstanceID}, {Name: "cloud_instance_type", Label: "实例规格", Value: vm.InstanceType}, {Name: "cloud_os", Label: "操作系统", Value: vm.OSName}, {Name: "cpu_count", Label: "vCPU", Value: strconv.Itoa(vm.CPU)}, {Name: "memory_mib", Label: "内存(MiB)", Value: strconv.Itoa(vm.MemoryMiB)}, {Name: "power_state", Label: "电源状态", Value: vm.Status}}}
		input.Relations = []cmdb.Relation{regionRelation}
		if _, err = cmdbService.UpsertExternalAsset(input, operator); err != nil {
			return result, err
		}
		result.Instances++
	}
	return result, nil
}
