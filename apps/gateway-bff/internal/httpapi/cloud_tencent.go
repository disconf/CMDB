package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
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

type tencentClient struct {
	endpoint, secretID, secretKey, region string
	client                                *http.Client
}
type tencentInstance struct {
	InstanceID, Name, State, InstanceType, OSName, Zone string
	CPU, MemoryMiB                                      int
	PublicIP, PrivateIP                                 string
}

func registerTencentCloudRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, cmdbService *cmdb.Service, credentialsService *credentials.Service) {
	mux.HandleFunc("GET /api/v1/cmdb/cloud-tencent/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		config, ok := cmdbService.IntegrationConfig("tencent")
		if !ok {
			config = cmdb.IntegrationConfig{Provider: "tencent", AuthType: "secret-key"}
		}
		writeJSON(w, http.StatusOK, tencentConfigResponse(config))
	})
	mux.HandleFunc("PUT /api/v1/cmdb/cloud-tencent/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			Endpoint  string `json:"endpoint"`
			RegionID  string `json:"regionId"`
			SecretID  string `json:"secretId"`
			SecretKey string `json:"secretKey"`
			Insecure  bool   `json:"insecure"`
			Enabled   bool   `json:"enabled"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		existing, _ := cmdbService.IntegrationConfig("tencent")
		credentialID := existing.CredentialID
		if strings.TrimSpace(input.SecretKey) != "" {
			credential, err := credentialsService.Create(credentials.CreateInput{Name: "腾讯云 CVM 集成凭据", Kind: "tencent", Username: input.SecretID, Secret: input.SecretKey, Group: "integrations", Description: "腾讯云 API SecretKey 凭据"})
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "VAULT_UNAVAILABLE", "message": err.Error()})
				return
			}
			credentialID = credential.ID
		}
		endpoint := strings.TrimSpace(input.Endpoint)
		if endpoint == "" {
			endpoint = "cvm.tencentcloudapi.com"
		}
		endpoint += "#region=" + url.QueryEscape(strings.TrimSpace(input.RegionID))
		config, err := cmdbService.SaveIntegrationConfig(cmdb.IntegrationConfigInput{Provider: "tencent", URL: endpoint, Username: input.SecretID, CredentialID: credentialID, AuthType: "secret-key", Insecure: input.Insecure, Enabled: input.Enabled}, "api")
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "腾讯云地域和 SecretId 不能为空"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "cmdb.tencent_config.update", "tencent", config.URL)
		response := tencentConfigResponse(config)
		response["regionId"] = strings.TrimSpace(input.RegionID)
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("POST /api/v1/cmdb/cloud-tencent/config/test", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			Endpoint  string `json:"endpoint"`
			RegionID  string `json:"regionId"`
			SecretID  string `json:"secretId"`
			SecretKey string `json:"secretKey"`
			Insecure  bool   `json:"insecure"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		client, err := buildTencentClient(input.Endpoint, input.RegionID, input.SecretID, input.SecretKey, input.Insecure)
		if err == nil {
			_, err = client.Instances(r.Context())
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "TENCENT_TEST_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "腾讯云 CVM API 连接成功"})
	})
	mux.HandleFunc("GET /api/v1/cmdb/cloud-tencent/inventory", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		client, err := newConfiguredTencentClient(cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "TENCENT_NOT_READY", "message": err.Error()})
			return
		}
		instances, err := client.Instances(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "TENCENT_INVENTORY_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cloudInventory{Configured: true, Provider: "tencent", RegionID: client.region, Instances: instances, FetchedAt: time.Now().Format("2006-01-02 15:04:05")})
	})
	mux.HandleFunc("POST /api/v1/cmdb/cloud-tencent/sync", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		client, err := newConfiguredTencentClient(cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "TENCENT_NOT_READY", "message": err.Error()})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		result, err := syncTencentInventory(r.Context(), cmdbService, client, user.Username)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "TENCENT_SYNC_FAILED", "message": err.Error()})
			return
		}
		auditService.Record(user.Username, "cmdb.tencent_sync", "tencent", fmt.Sprintf("region=%s instances=%d", result.RegionID, result.Instances))
		writeJSON(w, http.StatusOK, result)
	})
}

func tencentConfigResponse(config cmdb.IntegrationConfig) map[string]any {
	endpoint, region := parseCloudURL(config.URL)
	return map[string]any{"configured": config.URL != "" && config.Enabled, "provider": "tencent", "endpoint": endpoint, "regionId": region, "secretId": config.Username, "insecure": config.Insecure, "enabled": config.Enabled, "credentialId": config.CredentialID, "updatedAt": config.UpdatedAt}
}
func newConfiguredTencentClient(cmdbService *cmdb.Service, credentialsService *credentials.Service) (*tencentClient, error) {
	config, ok := cmdbService.IntegrationConfig("tencent")
	if !ok || !config.Enabled {
		return nil, errors.New("腾讯云集成未配置")
	}
	endpoint, region := parseCloudURL(config.URL)
	secretID, secretKey := config.Username, ""
	if config.CredentialID != "" {
		material, err := credentialsService.Resolve(config.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("读取腾讯云凭据失败: %w", err)
		}
		if secretID == "" {
			secretID = material.Username
		}
		secretKey = material.Secret
	}
	return buildTencentClient(endpoint, region, secretID, secretKey, config.Insecure)
}
func buildTencentClient(endpoint, region, secretID, secretKey string, insecure bool) (*tencentClient, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = "cvm.tencentcloudapi.com"
	}
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	endpoint = strings.TrimRight(endpoint, "/")
	region, secretID, secretKey = strings.TrimSpace(region), strings.TrimSpace(secretID), strings.TrimSpace(secretKey)
	if region == "" || secretID == "" || secretKey == "" {
		return nil, errors.New("腾讯云地域、SecretId 和 SecretKey 不能为空")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure}}
	return &tencentClient{endpoint: endpoint, region: region, secretID: secretID, secretKey: secretKey, client: &http.Client{Timeout: 45 * time.Second, Transport: transport}}, nil
}
func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}
func (c *tencentClient) Instances(ctx context.Context) ([]cloudInstance, error) {
	payload := []byte(`{"Limit":100,"Offset":0}`)
	now := time.Now().UTC()
	timestamp := strconv.FormatInt(now.Unix(), 10)
	date := now.Format("2006-01-02")
	canonicalHeaders := "content-type:application/json; charset=utf-8\nhost:" + c.endpoint + "\nx-tc-action:describeinstances\n"
	signedHeaders := "content-type;host;x-tc-action"
	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + sha256Hex(string(payload))
	scope := date + "/cvm/tc3_request"
	stringToSign := "TC3-HMAC-SHA256\n" + timestamp + "\n" + scope + "\n" + sha256Hex(canonicalRequest)
	secretDate := hmacSHA256([]byte("TC3"+c.secretKey), date)
	secretService := hmacSHA256(secretDate, "cvm")
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))
	authorization := "TC3-HMAC-SHA256 Credential=" + c.secretID + "/" + scope + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+c.endpoint+"/", strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Host", c.endpoint)
	request.Header.Set("X-TC-Action", "DescribeInstances")
	request.Header.Set("X-TC-Version", "2017-03-12")
	request.Header.Set("X-TC-Region", c.region)
	request.Header.Set("X-TC-Timestamp", timestamp)
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
		return nil, fmt.Errorf("腾讯云 API 返回 %s: %s", response.Status, string(raw))
	}
	var doc struct {
		Response struct {
			TotalCount  int `json:"TotalCount"`
			InstanceSet []struct {
				InstanceID    string   `json:"InstanceId"`
				InstanceName  string   `json:"InstanceName"`
				InstanceState string   `json:"InstanceState"`
				InstanceType  string   `json:"InstanceType"`
				OSName        string   `json:"OsName"`
				CPU           int      `json:"CPU"`
				Memory        int      `json:"Memory"`
				PublicIP      []string `json:"PublicIpAddresses"`
				PrivateIP     []string `json:"PrivateIpAddresses"`
				Placement     struct {
					Zone string `json:"Zone"`
				} `json:"Placement"`
			} `json:"InstanceSet"`
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Response.Error != nil {
		return nil, fmt.Errorf("腾讯云 API 错误 %s: %s", doc.Response.Error.Code, doc.Response.Error.Message)
	}
	result := []cloudInstance{}
	for _, item := range doc.Response.InstanceSet {
		publicIP, privateIP := "", ""
		if len(item.PublicIP) > 0 {
			publicIP = item.PublicIP[0]
		}
		if len(item.PrivateIP) > 0 {
			privateIP = item.PrivateIP[0]
		}
		result = append(result, cloudInstance{InstanceID: item.InstanceID, Name: item.InstanceName, Status: item.InstanceState, PublicIP: publicIP, PrivateIP: privateIP, ZoneID: item.Placement.Zone, InstanceType: item.InstanceType, OSName: item.OSName, CPU: item.CPU, MemoryMiB: item.Memory, RegionID: c.region})
	}
	return result, nil
}
func syncTencentInventory(ctx context.Context, cmdbService *cmdb.Service, client *tencentClient, operator string) (cloudSyncResult, error) {
	instances, err := client.Instances(ctx)
	if err != nil {
		return cloudSyncResult{}, err
	}
	result := cloudSyncResult{Provider: "tencent", RegionID: client.region}
	regionRelation, err := ensureCloudRegionRelation(ctx, cmdbService, "tencent", client.region, operator)
	if err != nil {
		return result, err
	}
	for _, vm := range instances {
		ip := vm.PublicIP
		if ip == "" {
			ip = vm.PrivateIP
		}
		input := cmdb.CreateAssetInput{ID: "tencent-cvm-" + safeAssetID(vm.InstanceID), Name: vm.Name, Type: "cloud-host", Status: strings.ToLower(vm.Status), IP: ip, Environment: "云平台", ProjectGroup: "云平台组", Owner: "TencentCloud", Location: "腾讯云 / " + client.region + " / " + vm.ZoneID, Source: "TencentCloud", Tags: []string{"腾讯云", "CVM", "云主机"}, Attributes: []cmdb.Attribute{{Name: "cloud_provider", Label: "云厂商", Value: "TencentCloud"}, {Name: "cloud_region", Label: "地域", Value: client.region}, {Name: "cloud_instance_id", Label: "实例 ID", Value: vm.InstanceID}, {Name: "cloud_instance_type", Label: "实例规格", Value: vm.InstanceType}, {Name: "cloud_os", Label: "操作系统", Value: vm.OSName}, {Name: "cpu_count", Label: "vCPU", Value: strconv.Itoa(vm.CPU)}, {Name: "memory_mib", Label: "内存(MiB)", Value: strconv.Itoa(vm.MemoryMiB)}, {Name: "power_state", Label: "电源状态", Value: vm.Status}}}
		input.Relations = []cmdb.Relation{regionRelation}
		if _, err = cmdbService.UpsertExternalAsset(input, operator); err != nil {
			return result, err
		}
		result.Instances++
	}
	return result, nil
}
