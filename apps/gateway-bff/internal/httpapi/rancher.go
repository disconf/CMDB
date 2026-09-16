package httpapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/credentials"
)

type rancherCluster struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	State             string `json:"state"`
	KubernetesVersion string `json:"kubernetesVersion"`
	NodeCount         int    `json:"nodeCount"`
	DashboardURL      string `json:"dashboardUrl"`
}

type rancherClusterResponse struct {
	Data []struct {
		ID                string `json:"id"`
		Name              string `json:"name"`
		State             string `json:"state"`
		KubernetesVersion string `json:"kubernetesVersion"`
		NodeCount         int    `json:"nodeCount"`
	} `json:"data"`
}

type rancherSyncResult struct {
	Clusters   int `json:"clusters"`
	Nodes      int `json:"nodes"`
	Namespaces int `json:"namespaces"`
	Workloads  int `json:"workloads"`
}

type rancherClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func registerRancherRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, cmdbService *cmdb.Service, credentialsService *credentials.Service) {
	mux.HandleFunc("GET /api/v1/cmdb/rancher/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		config, ok := cmdbService.IntegrationConfig("rancher")
		if !ok {
			baseURL, _, _, insecure := rancherSettings()
			config = cmdb.IntegrationConfig{Provider: "rancher", URL: baseURL, Insecure: insecure, Enabled: baseURL != "", AuthType: "token"}
		}
		writeJSON(w, http.StatusOK, map[string]any{"configured": config.URL != "" && config.Enabled, "provider": config.Provider, "url": config.URL, "username": config.Username, "authType": config.AuthType, "insecure": config.Insecure, "enabled": config.Enabled, "credentialId": config.CredentialID, "updatedAt": config.UpdatedAt})
	})
	mux.HandleFunc("PUT /api/v1/cmdb/rancher/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			URL      string `json:"url"`
			Username string `json:"username"`
			AuthType string `json:"authType"`
			Secret   string `json:"secret"`
			Insecure bool   `json:"insecure"`
			Enabled  bool   `json:"enabled"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		existing, _ := cmdbService.IntegrationConfig("rancher")
		credentialID := existing.CredentialID
		if strings.TrimSpace(input.Secret) != "" {
			credential, err := credentialsService.Create(credentials.CreateInput{Name: "Rancher 集成凭据", Kind: "rancher", Username: input.Username, Secret: input.Secret, Group: "integrations", Description: "Rancher API 登录凭据"})
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "VAULT_UNAVAILABLE", "message": err.Error()})
				return
			}
			credentialID = credential.ID
		}
		config, err := cmdbService.SaveIntegrationConfig(cmdb.IntegrationConfigInput{Provider: "rancher", URL: input.URL, Username: input.Username, CredentialID: credentialID, AuthType: input.AuthType, Insecure: input.Insecure, Enabled: input.Enabled}, "api")
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "Rancher 地址不能为空"})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		auditService.Record(user.Username, "cmdb.rancher_config.update", "rancher", config.URL)
		writeJSON(w, http.StatusOK, map[string]any{"configured": config.Enabled && config.URL != "", "url": config.URL, "username": config.Username, "authType": config.AuthType, "insecure": config.Insecure, "enabled": config.Enabled, "credentialId": config.CredentialID, "updatedAt": config.UpdatedAt})
	})
	mux.HandleFunc("POST /api/v1/cmdb/rancher/config/test", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		var input struct {
			URL      string `json:"url"`
			Username string `json:"username"`
			AuthType string `json:"authType"`
			Secret   string `json:"secret"`
			Insecure bool   `json:"insecure"`
		}
		if decodeJSON(r, &input) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST"})
			return
		}
		client, err := buildRancherClient(r.Context(), input.URL, input.Username, input.Secret, input.AuthType, input.Insecure)
		if err == nil {
			_, err = client.Clusters(r.Context())
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "RANCHER_TEST_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/cmdb/rancher/clusters", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		client, err := newConfiguredRancherClient(r.Context(), cmdbService, credentialsService)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "RANCHER_UNAVAILABLE", "message": err.Error()})
			return
		}
		items, err := client.Clusters(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "RANCHER_UNAVAILABLE", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, items)
	})
	mux.HandleFunc("POST /api/v1/cmdb/rancher/sync", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:manage") {
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		result, err := syncRancherInventoryConfigured(r.Context(), cmdbService, credentialsService, user.Username)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "RANCHER_SYNC_FAILED", "message": err.Error()})
			return
		}
		auditService.Record(user.Username, "cmdb.rancher_sync", "rancher", fmt.Sprintf("clusters=%d nodes=%d namespaces=%d workloads=%d", result.Clusters, result.Nodes, result.Namespaces, result.Workloads))
		writeJSON(w, http.StatusOK, result)
	})
}

func rancherSettings() (string, string, string, bool) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RANCHER_URL")), "/")
	token := strings.TrimSpace(os.Getenv("RANCHER_TOKEN"))
	username := strings.TrimSpace(os.Getenv("RANCHER_USERNAME"))
	password := os.Getenv("RANCHER_PASSWORD")
	insecure, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("RANCHER_INSECURE_SKIP_VERIFY")))
	return baseURL, token, username + "\x00" + password, insecure
}

func newRancherClient(ctx context.Context) (*rancherClient, error) {
	baseURL, token, credentials, insecure := rancherSettings()
	if baseURL == "" {
		return nil, errors.New("Rancher 未配置")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	if insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	rancher := &rancherClient{baseURL: baseURL, token: token, client: client}
	if rancher.token == "" {
		parts := strings.SplitN(credentials, "\x00", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, errors.New("Rancher Token 或用户名密码未配置")
		}
		loginToken, err := rancher.login(ctx, parts[0], parts[1])
		if err != nil {
			return nil, err
		}
		rancher.token = loginToken
	}
	return rancher, nil
}

func newConfiguredRancherClient(ctx context.Context, cmdbService *cmdb.Service, credentialsService *credentials.Service) (*rancherClient, error) {
	config, ok := cmdbService.IntegrationConfig("rancher")
	if !ok || !config.Enabled || strings.TrimSpace(config.URL) == "" {
		return newRancherClient(ctx)
	}
	username := config.Username
	secret := ""
	if config.CredentialID != "" {
		material, err := credentialsService.Resolve(config.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("读取 Rancher 凭据失败: %w", err)
		}
		if username == "" {
			username = material.Username
		}
		secret = material.Secret
	}
	return buildRancherClient(ctx, config.URL, username, secret, config.AuthType, config.Insecure)
}

func buildRancherClient(ctx context.Context, baseURL, username, secret, authType string, insecure bool) (*rancherClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("Rancher 地址不能为空")
	}
	if strings.TrimSpace(secret) == "" {
		envURL, envToken, envCredentials, envInsecure := rancherSettings()
		if envURL != "" && strings.EqualFold(envURL, baseURL) {
			secret = envToken
			if authType == "password" {
				parts := strings.SplitN(envCredentials, "\x00", 2)
				if len(parts) == 2 {
					username, secret = parts[0], parts[1]
				}
			}
			insecure = insecure || envInsecure
		}
	}
	client := &http.Client{Timeout: 30 * time.Second}
	if insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	rancher := &rancherClient{baseURL: baseURL, client: client}
	if strings.EqualFold(authType, "password") {
		if username == "" || secret == "" {
			return nil, errors.New("Rancher 用户名和密码未配置")
		}
		token, err := rancher.login(ctx, username, secret)
		if err != nil {
			return nil, err
		}
		rancher.token = token
		return rancher, nil
	}
	if secret == "" {
		return nil, errors.New("Rancher API Token 未配置")
	}
	rancher.token = secret
	return rancher, nil
}
func (c *rancherClient) login(ctx context.Context, username, password string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"username": username, "password": password, "responseType": "token"})
	endpoint := c.baseURL + "/v3-public/localProviders/local?action=login"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("Rancher 登录返回 %s", response.Status)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return "", err
	}
	if strings.TrimSpace(body.Token) == "" {
		return "", errors.New("Rancher 登录未返回 Token")
	}
	return body.Token, nil
}
func (c *rancherClient) getJSON(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Rancher API %s returned %s", path, response.Status)
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func (c *rancherClient) Clusters(ctx context.Context) ([]rancherCluster, error) {
	var payload rancherClusterResponse
	if err := c.getJSON(ctx, "/v3/clusters?limit=500", &payload); err != nil {
		return nil, err
	}
	result := make([]rancherCluster, 0, len(payload.Data))
	for _, item := range payload.Data {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		result = append(result, rancherCluster{ID: item.ID, Name: item.Name, State: item.State, KubernetesVersion: item.KubernetesVersion, NodeCount: item.NodeCount, DashboardURL: rancherDashboardURL(c.baseURL, item.ID)})
	}
	return result, nil
}

type rancherNode struct {
	ID             string
	Name           string
	IP             string
	State          string
	KubeletVersion string
	OSImage        string
	Architecture   string
	CPUCapacity    string
	MemoryCapacity string
	ProviderID     string
}

type rancherNamespace struct {
	ID     string
	Name   string
	Status string
}

type rancherWorkload struct {
	Kind      string
	ID        string
	Name      string
	Namespace string
	Desired   int
	Ready     int
	Images    string
}

type k8sMetadata struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	UID       string            `json:"uid"`
	Labels    map[string]string `json:"labels"`
}

type k8sNodeList struct {
	Items []struct {
		Metadata k8sMetadata `json:"metadata"`
		Spec     struct {
			ProviderID string `json:"providerID"`
		} `json:"spec"`
		Status struct {
			Addresses []struct {
				Type    string `json:"type"`
				Address string `json:"address"`
			} `json:"addresses"`
			NodeInfo struct {
				KubeletVersion string `json:"kubeletVersion"`
				OSImage        string `json:"osImage"`
				Architecture   string `json:"architecture"`
			} `json:"nodeInfo"`
			Capacity   map[string]string `json:"capacity"`
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"conditions"`
		} `json:"status"`
	} `json:"items"`
}

func (c *rancherClient) Nodes(ctx context.Context, clusterID string) ([]rancherNode, error) {
	var payload k8sNodeList
	path := "/k8s/clusters/" + url.PathEscape(clusterID) + "/api/v1/nodes?limit=500"
	if err := c.getJSON(ctx, path, &payload); err != nil {
		return nil, err
	}
	result := make([]rancherNode, 0, len(payload.Items))
	for _, item := range payload.Items {
		node := rancherNode{ID: item.Metadata.UID, Name: item.Metadata.Name, State: "NotReady", KubeletVersion: item.Status.NodeInfo.KubeletVersion, OSImage: item.Status.NodeInfo.OSImage, Architecture: item.Status.NodeInfo.Architecture, CPUCapacity: item.Status.Capacity["cpu"], MemoryCapacity: item.Status.Capacity["memory"], ProviderID: item.Spec.ProviderID}
		if node.ID == "" {
			node.ID = node.Name
		}
		for _, address := range item.Status.Addresses {
			if address.Type == "InternalIP" {
				node.IP = address.Address
				break
			}
			if node.IP == "" && address.Type == "Hostname" {
				node.IP = address.Address
			}
		}
		for _, condition := range item.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				node.State = "Ready"
				break
			}
		}
		result = append(result, node)
	}
	return result, nil
}

type k8sNamespaceList struct {
	Items []struct {
		Metadata k8sMetadata `json:"metadata"`
		Status   struct {
			Phase string `json:"phase"`
		} `json:"status"`
	} `json:"items"`
}

func (c *rancherClient) Namespaces(ctx context.Context, clusterID string) ([]rancherNamespace, error) {
	var payload k8sNamespaceList
	path := "/k8s/clusters/" + url.PathEscape(clusterID) + "/api/v1/namespaces?limit=500"
	if err := c.getJSON(ctx, path, &payload); err != nil {
		return nil, err
	}
	result := make([]rancherNamespace, 0, len(payload.Items))
	for _, item := range payload.Items {
		id := item.Metadata.UID
		if id == "" {
			id = item.Metadata.Name
		}
		result = append(result, rancherNamespace{ID: id, Name: item.Metadata.Name, Status: item.Status.Phase})
	}
	return result, nil
}

type k8sWorkloadList struct {
	Items []struct {
		Metadata k8sMetadata `json:"metadata"`
		Spec     struct {
			Replicas *int `json:"replicas"`
			Template struct {
				Spec struct {
					Containers []struct {
						Image string `json:"image"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
		Status struct {
			ReadyReplicas int `json:"readyReplicas"`
		} `json:"status"`
	} `json:"items"`
}

func (c *rancherClient) Workloads(ctx context.Context, clusterID, kind, path string) ([]rancherWorkload, error) {
	var payload k8sWorkloadList
	endpoint := "/k8s/clusters/" + url.PathEscape(clusterID) + path + "?limit=500"
	if err := c.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}
	result := make([]rancherWorkload, 0, len(payload.Items))
	for _, item := range payload.Items {
		desired := 0
		if item.Spec.Replicas != nil {
			desired = *item.Spec.Replicas
		}
		images := make([]string, 0, len(item.Spec.Template.Spec.Containers))
		for _, container := range item.Spec.Template.Spec.Containers {
			if container.Image != "" {
				images = append(images, container.Image)
			}
		}
		id := item.Metadata.UID
		if id == "" {
			id = item.Metadata.Namespace + "/" + item.Metadata.Name
		}
		result = append(result, rancherWorkload{Kind: kind, ID: id, Name: item.Metadata.Name, Namespace: item.Metadata.Namespace, Desired: desired, Ready: item.Status.ReadyReplicas, Images: strings.Join(images, ",")})
	}
	return result, nil
}
func syncRancherInventory(ctx context.Context, cmdbService *cmdb.Service, operator string) (rancherSyncResult, error) {
	client, err := newRancherClient(ctx)
	if err != nil {
		return rancherSyncResult{}, err
	}
	return syncRancherInventoryWithClient(ctx, cmdbService, client, operator)
}

func syncRancherInventoryConfigured(ctx context.Context, cmdbService *cmdb.Service, credentialsService *credentials.Service, operator string) (rancherSyncResult, error) {
	client, err := newConfiguredRancherClient(ctx, cmdbService, credentialsService)
	if err != nil {
		return rancherSyncResult{}, err
	}
	return syncRancherInventoryWithClient(ctx, cmdbService, client, operator)
}

func syncRancherInventoryWithClient(ctx context.Context, cmdbService *cmdb.Service, client *rancherClient, operator string) (rancherSyncResult, error) {
	clusters, err := client.Clusters(ctx)
	if err != nil {
		return rancherSyncResult{}, err
	}
	result := rancherSyncResult{}
	for _, cluster := range clusters {
		nodes, nodesErr := client.Nodes(ctx, cluster.ID)
		namespaces, namespacesErr := client.Namespaces(ctx, cluster.ID)
		workloads := make([]rancherWorkload, 0)
		for _, spec := range []struct{ kind, path string }{
			{"Deployment", "/apis/apps/v1/deployments"},
			{"StatefulSet", "/apis/apps/v1/statefulsets"},
			{"DaemonSet", "/apis/apps/v1/daemonsets"},
		} {
			items, workloadErr := client.Workloads(ctx, cluster.ID, spec.kind, spec.path)
			if workloadErr == nil {
				workloads = append(workloads, items...)
			}
		}
		cluster.NodeCount = len(nodes)
		clusterAsset := rancherClusterAsset(cluster, rancherExistingClusterID(cmdbService, cluster.Name))
		if _, err = cmdbService.UpsertExternalAsset(clusterAsset, operator); err != nil {
			return result, err
		}
		result.Clusters++
		clusterAssetID := clusterAsset.ID
		if nodesErr == nil {
			for _, node := range nodes {
				if _, err = cmdbService.UpsertExternalAsset(rancherNodeAsset(cluster, node, clusterAssetID), operator); err != nil {
					return result, err
				}
				result.Nodes++
			}
		}
		namespaceIDs := map[string]string{}
		if namespacesErr == nil {
			for _, namespace := range namespaces {
				namespaceAsset := rancherNamespaceAsset(cluster, namespace, clusterAssetID)
				namespaceIDs[namespace.Name] = namespaceAsset.ID
				if _, err = cmdbService.UpsertExternalAsset(namespaceAsset, operator); err != nil {
					return result, err
				}
				result.Namespaces++
			}
		}
		for _, workload := range workloads {
			if _, err = cmdbService.UpsertExternalAsset(rancherWorkloadAsset(cluster, workload, clusterAssetID, namespaceIDs[workload.Namespace]), operator); err != nil {
				return result, err
			}
			result.Workloads++
		}
	}
	return result, nil
}

func rancherDashboardURL(baseURL, clusterID string) string {
	prefix := strings.TrimRight(strings.TrimSpace(os.Getenv("RANCHER_DASHBOARD_URL")), "/")
	if prefix == "" {
		prefix = baseURL
	}
	return prefix + "/dashboard/c/" + clusterID + "/explore"
}

func rancherClusterAsset(cluster rancherCluster, existingID string) cmdb.CreateAssetInput {
	assetID := strings.TrimSpace(existingID)
	if assetID == "" {
		assetID = "rancher-cluster-" + safeAssetID(cluster.ID)
	}
	return cmdb.CreateAssetInput{
		ID: assetID, Name: cluster.Name, Type: "k8s-cluster",
		Status: statusFromRancherState(cluster.State), Environment: "容器平台", ProjectGroup: "容器平台组",
		Owner: "Rancher", Location: "Rancher", Source: "Rancher", Tags: []string{"Kubernetes", "Rancher"},
		Attributes: []cmdb.Attribute{
			{Name: "cluster_name", Label: "集群名称", Value: cluster.Name},
			{Name: "k8s_version", Label: "Kubernetes 版本", Value: cluster.KubernetesVersion},
			{Name: "rancher_cluster_id", Label: "Rancher 集群 ID", Value: cluster.ID},
			{Name: "node_count", Label: "节点数量", Value: strconv.Itoa(cluster.NodeCount)},
		},
	}
}

func rancherExistingClusterID(cmdbService *cmdb.Service, name string) string {
	for _, asset := range cmdbService.InventoryAssets() {
		if asset.Type == "k8s-cluster" && strings.EqualFold(asset.Name, name) && asset.Source != "Rancher" {
			return asset.ID
		}
	}
	return ""
}
func rancherNodeAsset(cluster rancherCluster, node rancherNode, clusterAssetID string) cmdb.CreateAssetInput {
	return cmdb.CreateAssetInput{
		ID: "rancher-node-" + safeAssetID(cluster.ID) + "-" + safeAssetID(node.ID), Name: node.Name, Type: "k8s-node",
		Status: statusFromRancherState(node.State), IP: node.IP, Environment: "容器平台", ProjectGroup: "容器平台组",
		Owner: "Rancher", Location: "Rancher", Source: "Rancher", Tags: []string{"Kubernetes", "Node"},
		Attributes: []cmdb.Attribute{
			{Name: "cluster_name", Label: "集群名称", Value: cluster.Name},
			{Name: "k8s_uid", Label: "K8s UID", Value: node.ID},
			{Name: "kubelet_version", Label: "Kubelet 版本", Value: node.KubeletVersion},
			{Name: "os_image", Label: "节点操作系统", Value: node.OSImage},
			{Name: "architecture", Label: "架构", Value: node.Architecture},
			{Name: "cpu_capacity", Label: "CPU 容量", Value: node.CPUCapacity},
			{Name: "memory_capacity", Label: "内存容量", Value: node.MemoryCapacity},
			{Name: "provider_id", Label: "Provider ID", Value: node.ProviderID},
		},
		Relations: []cmdb.Relation{{Type: "belongs-to", TargetID: clusterAssetID, TargetName: cluster.Name}},
	}
}

func rancherNamespaceAsset(cluster rancherCluster, namespace rancherNamespace, clusterAssetID string) cmdb.CreateAssetInput {
	return cmdb.CreateAssetInput{
		ID: "rancher-ns-" + safeAssetID(cluster.ID) + "-" + safeAssetID(namespace.Name), Name: namespace.Name, Type: "k8s-namespace",
		Status: statusFromRancherState(namespace.Status), Environment: "容器平台", ProjectGroup: "容器平台组",
		Owner: "Rancher", Location: "Rancher", Source: "Rancher", Tags: []string{"Kubernetes", "Namespace"},
		Attributes: []cmdb.Attribute{
			{Name: "cluster_name", Label: "集群名称", Value: cluster.Name},
			{Name: "k8s_uid", Label: "K8s UID", Value: namespace.ID},
			{Name: "namespace_status", Label: "命名空间状态", Value: namespace.Status},
		},
		Relations: []cmdb.Relation{{Type: "belongs-to", TargetID: clusterAssetID, TargetName: cluster.Name}},
	}
}

func rancherWorkloadAsset(cluster rancherCluster, workload rancherWorkload, clusterAssetID, namespaceAssetID string) cmdb.CreateAssetInput {
	status := "warning"
	if workload.Desired == 0 || workload.Ready >= workload.Desired {
		status = "online"
	}
	relations := []cmdb.Relation{{Type: "belongs-to", TargetID: clusterAssetID, TargetName: cluster.Name}}
	if namespaceAssetID != "" {
		relations = append(relations, cmdb.Relation{Type: "belongs-to", TargetID: namespaceAssetID, TargetName: workload.Namespace})
	}
	return cmdb.CreateAssetInput{
		ID:   "rancher-workload-" + safeAssetID(cluster.ID) + "-" + safeAssetID(workload.Namespace) + "-" + strings.ToLower(workload.Kind) + "-" + safeAssetID(workload.Name),
		Name: workload.Name, Type: "k8s-workload", Status: status, Environment: "容器平台", ProjectGroup: "容器平台组",
		Owner: "Rancher", Location: "Rancher", Source: "Rancher", Tags: []string{"Kubernetes", workload.Kind},
		Attributes: []cmdb.Attribute{
			{Name: "cluster_name", Label: "集群名称", Value: cluster.Name},
			{Name: "namespace", Label: "命名空间", Value: workload.Namespace},
			{Name: "workload_kind", Label: "工作负载类型", Value: workload.Kind},
			{Name: "replicas", Label: "期望副本", Value: strconv.Itoa(workload.Desired)},
			{Name: "ready_replicas", Label: "就绪副本", Value: strconv.Itoa(workload.Ready)},
			{Name: "images", Label: "镜像", Value: workload.Images},
		},
		Relations: relations,
	}
}

func statusFromRancherState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "active", "ready":
		return "online"
	case "error", "failed", "unhealthy":
		return "offline"
	default:
		return "warning"
	}
}

func safeAssetID(value string) string {
	replacer := strings.NewReplacer("/", "-", ":", "-", " ", "-", "_", "-")
	return strings.ToLower(replacer.Replace(value))
}
