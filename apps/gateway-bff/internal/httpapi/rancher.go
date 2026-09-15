package httpapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
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

func registerRancherRoutes(mux *http.ServeMux, authService *auth.Service, auditService *audit.Service, cmdbService *cmdb.Service) {
	mux.HandleFunc("GET /api/v1/cmdb/rancher/config", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		baseURL, _, _ := rancherSettings()
		writeJSON(w, http.StatusOK, map[string]any{"configured": baseURL != "", "url": baseURL})
	})
	mux.HandleFunc("GET /api/v1/cmdb/rancher/clusters", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "cmdb:view") {
			return
		}
		items, err := fetchRancherClusters(r.Context())
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
		items, err := fetchRancherClusters(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "RANCHER_UNAVAILABLE", "message": err.Error()})
			return
		}
		user, _ := authService.CurrentUser(bearerToken(r))
		for _, cluster := range items {
			if _, err = cmdbService.UpsertExternalAsset(rancherClusterAsset(cluster), user.Username); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "RANCHER_SYNC_FAILED", "message": err.Error()})
				return
			}
		}
		auditService.Record(user.Username, "cmdb.rancher_sync", "rancher", strconv.Itoa(len(items))+" clusters")
		writeJSON(w, http.StatusOK, map[string]int{"synced": len(items)})
	})
}

func rancherSettings() (string, string, bool) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RANCHER_URL")), "/")
	token := strings.TrimSpace(os.Getenv("RANCHER_TOKEN"))
	insecure, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("RANCHER_INSECURE_SKIP_VERIFY")))
	return baseURL, token, insecure
}

func fetchRancherClusters(ctx context.Context) ([]rancherCluster, error) {
	baseURL, token, insecure := rancherSettings()
	if baseURL == "" || token == "" {
		return nil, errors.New("Rancher 未配置")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	if insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v3/clusters", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Rancher API returned %s", response.Status)
	}
	var payload rancherClusterResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	result := make([]rancherCluster, 0, len(payload.Data))
	for _, item := range payload.Data {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		result = append(result, rancherCluster{ID: item.ID, Name: item.Name, State: item.State, KubernetesVersion: item.KubernetesVersion, NodeCount: item.NodeCount, DashboardURL: rancherDashboardURL(baseURL, item.ID)})
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

func rancherClusterAsset(cluster rancherCluster) cmdb.CreateAssetInput {
	return cmdb.CreateAssetInput{
		ID: "rancher-cluster-" + safeAssetID(cluster.ID), Name: cluster.Name, Type: "k8s-cluster",
		Status:      map[bool]string{true: "online", false: "warning"}[strings.EqualFold(cluster.State, "active")],
		Environment: "容器平台", ProjectGroup: "容器平台组", Owner: "Rancher", Location: "Rancher",
		Source: "Rancher", Tags: []string{"Kubernetes", "Rancher"},
		Attributes: []cmdb.Attribute{
			{Name: "cluster_name", Label: "集群名称", Value: cluster.Name},
			{Name: "k8s_version", Label: "Kubernetes 版本", Value: cluster.KubernetesVersion},
			{Name: "rancher_cluster_id", Label: "Rancher 集群 ID", Value: cluster.ID},
			{Name: "node_count", Label: "节点数量", Value: strconv.Itoa(cluster.NodeCount)},
		},
	}
}

func safeAssetID(value string) string {
	replacer := strings.NewReplacer("/", "-", ":", "-", " ", "-")
	return replacer.Replace(value)
}
