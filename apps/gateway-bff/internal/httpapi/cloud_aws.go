package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/credentials"
)

type awsClient struct {
	endpoint, region, accessKeyID, secretAccessKey string
	client                                         *http.Client
}

func registerAWSCloudRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, cmdbService *cmdb.Service, credentialsService *credentials.Service) {
	mux.HandleFunc("GET /api/v1/cmdb/cloud-aws/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		config, ok := cmdbService.IntegrationConfig("aws")
		if !ok {
			config = cmdb.IntegrationConfig{Provider: "aws", AuthType: "access-key"}
		}
		writeJSON(w, http.StatusOK, awsConfigResponse(config))
	})
	mux.HandleFunc("PUT /api/v1/cmdb/cloud-aws/config", func(w http.ResponseWriter, r *http.Request) {
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
		existing, _ := cmdbService.IntegrationConfig("aws")
		credentialID := existing.CredentialID
		if strings.TrimSpace(input.AccessKeySecret) != "" {
			credential, err := credentialsService.Create(credentials.CreateInput{Name: "AWS EC2 集成凭据", Kind: "aws", Username: input.AccessKeyID, Secret: input.AccessKeySecret, Group: "integrations", Description: "AWS AccessKey 凭据"})
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "VAULT_UNAVAILABLE", "message": err.Error()})
				return
			}
			credentialID = credential.ID
		}
		endpoint := normalizeAWSEndpoint(input.Endpoint, input.RegionID)
		endpoint += "#region=" + url.QueryEscape(strings.TrimSpace(input.RegionID))
		config, err := cmdbService.SaveIntegrationConfig(cmdb.IntegrationConfigInput{Provider: "aws", URL: endpoint, Username: input.AccessKeyID, CredentialID: credentialID, AuthType: "access-key", Insecure: input.Insecure, Enabled: input.Enabled}, "api")
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "AWS 地域和 AccessKey 不能为空"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "cmdb.aws_config.update", "aws", config.URL)
		response := awsConfigResponse(config)
		response["regionId"] = strings.TrimSpace(input.RegionID)
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("POST /api/v1/cmdb/cloud-aws/config/test", func(w http.ResponseWriter, r *http.Request) {
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
		client, err := buildAWSClient(input.Endpoint, input.RegionID, input.AccessKeyID, input.AccessKeySecret, input.Insecure)
		if err == nil {
			_, err = client.Instances(r.Context())
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "AWS_TEST_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "AWS EC2 API 连接成功"})
	})
	mux.HandleFunc("GET /api/v1/cmdb/cloud-aws/inventory", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		client, err := newConfiguredAWSClient(cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "AWS_NOT_READY", "message": err.Error()})
			return
		}
		instances, err := client.Instances(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "AWS_INVENTORY_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cloudInventory{Configured: true, Provider: "aws", RegionID: client.region, Instances: instances, FetchedAt: time.Now().Format("2006-01-02 15:04:05")})
	})
	mux.HandleFunc("POST /api/v1/cmdb/cloud-aws/sync", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		client, err := newConfiguredAWSClient(cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]string{"code": "AWS_NOT_READY", "message": err.Error()})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		result, err := syncAWSInventory(r.Context(), cmdbService, client, user.Username)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "AWS_SYNC_FAILED", "message": err.Error()})
			return
		}
		auditService.Record(user.Username, "cmdb.aws_sync", "aws", fmt.Sprintf("region=%s instances=%d", result.RegionID, result.Instances))
		writeJSON(w, http.StatusOK, result)
	})
}

func awsConfigResponse(config cmdb.IntegrationConfig) map[string]any {
	endpoint, region := parseCloudURL(config.URL)
	return map[string]any{"configured": config.URL != "" && config.Enabled, "provider": "aws", "endpoint": endpoint, "regionId": region, "accessKeyId": config.Username, "insecure": config.Insecure, "enabled": config.Enabled, "credentialId": config.CredentialID, "updatedAt": config.UpdatedAt}
}
func newConfiguredAWSClient(cmdbService *cmdb.Service, credentialsService *credentials.Service) (*awsClient, error) {
	config, ok := cmdbService.IntegrationConfig("aws")
	if !ok || !config.Enabled {
		return nil, errors.New("AWS 集成未配置")
	}
	endpoint, region := parseCloudURL(config.URL)
	accessKey, secretKey := config.Username, ""
	if config.CredentialID != "" {
		material, err := credentialsService.Resolve(config.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("读取 AWS 凭据失败: %w", err)
		}
		if accessKey == "" {
			accessKey = material.Username
		}
		secretKey = material.Secret
	}
	return buildAWSClient(endpoint, region, accessKey, secretKey, config.Insecure)
}
func normalizeAWSEndpoint(endpoint, region string) string {
	endpoint = strings.TrimSpace(endpoint)
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint != "" && endpoint != "ec2.amazonaws.com" {
		return endpoint
	}
	region = strings.TrimSpace(region)
	if region == "" {
		return endpoint
	}
	suffix := "amazonaws.com"
	if strings.HasPrefix(region, "cn-") {
		suffix = "amazonaws.com.cn"
	}
	return "ec2." + region + "." + suffix
}
func buildAWSClient(endpoint, region, accessKeyID, secretAccessKey string, insecure bool) (*awsClient, error) {
	endpoint, region, accessKeyID, secretAccessKey = strings.TrimSpace(endpoint), strings.TrimSpace(region), strings.TrimSpace(accessKeyID), strings.TrimSpace(secretAccessKey)
	endpoint = normalizeAWSEndpoint(endpoint, region)
	if region == "" || accessKeyID == "" || secretAccessKey == "" {
		return nil, errors.New("AWS 地域、AccessKey ID 和 SecretAccessKey 不能为空")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure}}
	return &awsClient{endpoint: endpoint, region: region, accessKeyID: accessKeyID, secretAccessKey: secretAccessKey, client: &http.Client{Timeout: 45 * time.Second, Transport: transport}}, nil
}
func awsSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func awsHMAC(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

type awsDescribeInstancesResponse struct {
	NextToken    string `xml:"nextToken"`
	Reservations []struct {
		Instances []struct {
			InstanceID   string `xml:"instanceId"`
			InstanceType string `xml:"instanceType"`
			State        struct {
				Name string `xml:"name"`
			} `xml:"instanceState"`
			PrivateIP string `xml:"privateIpAddress"`
			PublicIP  string `xml:"ipAddress"`
			Region    string `xml:"placement>availabilityZone"`
			Platform  string `xml:"platform"`
			Tags      []struct {
				Key   string `xml:"key"`
				Value string `xml:"value"`
			} `xml:"tagSet>item"`
		} `xml:"instancesSet>item"`
	} `xml:"reservationSet>item"`
}

func (c *awsClient) Instances(ctx context.Context) ([]cloudInstance, error) {
	result := []cloudInstance{}
	nextToken := ""
	for {
		doc, err := c.describeInstances(ctx, nextToken)
		if err != nil {
			return nil, err
		}
		for _, reservation := range doc.Reservations {
			for _, item := range reservation.Instances {
				name := item.InstanceID
				for _, tag := range item.Tags {
					if strings.EqualFold(tag.Key, "Name") && tag.Value != "" {
						name = tag.Value
					}
				}
				osName := "Linux/Unix"
				if strings.EqualFold(item.Platform, "windows") {
					osName = "Windows"
				}
				result = append(result, cloudInstance{InstanceID: item.InstanceID, Name: name, Status: item.State.Name, PublicIP: item.PublicIP, PrivateIP: item.PrivateIP, ZoneID: item.Region, InstanceType: item.InstanceType, OSName: osName, RegionID: c.region})
			}
		}
		if doc.NextToken == "" {
			break
		}
		nextToken = doc.NextToken
	}
	return result, nil
}

func (c *awsClient) describeInstances(ctx context.Context, nextToken string) (awsDescribeInstancesResponse, error) {
	values := url.Values{"Action": {"DescribeInstances"}, "Version": {"2016-11-15"}, "MaxResults": {"100"}}
	if nextToken != "" {
		values.Set("NextToken", nextToken)
	}
	payload := values.Encode()
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	host := c.endpoint
	canonicalHeaders := "content-type:application/x-www-form-urlencoded; charset=utf-8\nhost:" + host + "\nx-amz-date:" + amzDate + "\n"
	signedHeaders := "content-type;host;x-amz-date"
	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + awsSHA256(payload)
	scope := date + "/" + c.region + "/ec2/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + awsSHA256(canonicalRequest)
	kDate := awsHMAC([]byte("AWS4"+c.secretAccessKey), date)
	kRegion := awsHMAC(kDate, c.region)
	kService := awsHMAC(kRegion, "ec2")
	kSigning := awsHMAC(kService, "aws4_request")
	signature := hex.EncodeToString(awsHMAC(kSigning, stringToSign))
	authorization := "AWS4-HMAC-SHA256 Credential=" + c.accessKeyID + "/" + scope + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+host+"/", strings.NewReader(payload))
	if err != nil {
		return awsDescribeInstancesResponse{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	request.Header.Set("Host", host)
	request.Header.Set("X-Amz-Date", amzDate)
	request.Header.Set("Authorization", authorization)
	response, err := c.client.Do(request)
	if err != nil {
		return awsDescribeInstancesResponse{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return awsDescribeInstancesResponse{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return awsDescribeInstancesResponse{}, fmt.Errorf("AWS API 返回 %s: %s", response.Status, string(raw))
	}
	var doc awsDescribeInstancesResponse
	if err = xml.Unmarshal(raw, &doc); err != nil {
		return awsDescribeInstancesResponse{}, err
	}
	return doc, nil
}
func syncAWSInventory(ctx context.Context, cmdbService *cmdb.Service, client *awsClient, operator string) (cloudSyncResult, error) {
	instances, err := client.Instances(ctx)
	if err != nil {
		return cloudSyncResult{}, err
	}
	result := cloudSyncResult{Provider: "aws", RegionID: client.region}
	for _, vm := range instances {
		ip := vm.PublicIP
		if ip == "" {
			ip = vm.PrivateIP
		}
		status := "offline"
		if strings.EqualFold(vm.Status, "running") {
			status = "online"
		} else if strings.EqualFold(vm.Status, "pending") {
			status = "warning"
		}
		input := cmdb.CreateAssetInput{ID: "aws-ec2-" + safeAssetID(vm.InstanceID), Name: vm.Name, Type: "cloud-host", Status: status, IP: ip, Environment: "云平台", ProjectGroup: "云平台组", Owner: "AWS", Location: "AWS / " + client.region + " / " + vm.ZoneID, Source: "AWS", Tags: []string{"AWS", "EC2", "云主机"}, Attributes: []cmdb.Attribute{{Name: "cloud_provider", Label: "云厂商", Value: "AWS"}, {Name: "cloud_region", Label: "地域", Value: client.region}, {Name: "cloud_instance_id", Label: "实例 ID", Value: vm.InstanceID}, {Name: "cloud_instance_type", Label: "实例规格", Value: vm.InstanceType}, {Name: "cloud_os", Label: "操作系统", Value: vm.OSName}, {Name: "power_state", Label: "电源状态", Value: vm.Status}}}
		if _, err = cmdbService.UpsertExternalAsset(input, operator); err != nil {
			return result, err
		}
		result.Instances++
	}
	return result, nil
}
