package k8s

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"

type Client struct {
	baseURL     string
	token       string
	clusterName string
	httpClient  *http.Client
}

func NewClientFromEnv() (*Client, error) {
	baseURL := strings.TrimRight(os.Getenv("KUBERNETES_API_URL"), "/")
	if baseURL == "" {
		baseURL = "https://kubernetes.default.svc"
	}
	token := strings.TrimSpace(os.Getenv("KUBERNETES_TOKEN"))
	if token == "" {
		raw, err := os.ReadFile(serviceAccountDir + "/token")
		if err != nil {
			return nil, fmt.Errorf("read Kubernetes service account token: %w", err)
		}
		token = strings.TrimSpace(string(raw))
	}
	if token == "" {
		return nil, errors.New("Kubernetes service account token is empty")
	}
	caFile := strings.TrimSpace(os.Getenv("KUBERNETES_CA_FILE"))
	if caFile == "" {
		caFile = serviceAccountDir + "/ca.crt"
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if raw, err := os.ReadFile(caFile); err == nil && len(raw) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(raw) {
			return nil, errors.New("Kubernetes CA file is invalid")
		}
		tlsConfig.RootCAs = pool
	}
	clusterName := strings.TrimSpace(os.Getenv("CMDB_K8S_CLUSTER_NAME"))
	if clusterName == "" {
		clusterName = "jzq-cluster"
	}
	return &Client{baseURL: baseURL, token: token, clusterName: clusterName, httpClient: &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig}}}, nil
}

func (c *Client) ClusterName() string { return c.clusterName }
func (c *Client) BaseURL() string     { return c.baseURL }

func (c *Client) get(ctx context.Context, path string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var status struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(response.Body).Decode(&status)
		if status.Message == "" {
			status.Message = response.Status
		}
		return fmt.Errorf("Kubernetes API %s: %s", path, status.Message)
	}
	return json.NewDecoder(response.Body).Decode(out)
}

type Version struct {
	Major      string `json:"major"`
	Minor      string `json:"minor"`
	GitVersion string `json:"gitVersion"`
}
type ObjectMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	Labels            map[string]string `json:"labels"`
	CreationTimestamp string            `json:"creationTimestamp"`
	DeletionTimestamp *string           `json:"deletionTimestamp"`
}
type ListMeta struct {
	Continue string `json:"continue"`
}
type NodeAddress struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}
type NodeCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}
type Node struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		ProviderID string `json:"providerID"`
	} `json:"spec"`
	Status struct {
		Addresses  []NodeAddress     `json:"addresses"`
		Conditions []NodeCondition   `json:"conditions"`
		Capacity   map[string]string `json:"capacity"`
		NodeInfo   struct {
			Architecture            string `json:"architecture"`
			KubeletVersion          string `json:"kubeletVersion"`
			OSImage                 string `json:"osImage"`
			ContainerRuntimeVersion string `json:"containerRuntimeVersion"`
		} `json:"nodeInfo"`
	} `json:"status"`
}
type Namespace struct {
	Metadata ObjectMeta `json:"metadata"`
	Status   struct {
		Phase string `json:"phase"`
	} `json:"status"`
}
type Workload struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Replicas *int32 `json:"replicas"`
		Template struct {
			Spec struct {
				Containers []struct {
					Name  string `json:"name"`
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		Replicas               int32 `json:"replicas"`
		ReadyReplicas          int32 `json:"readyReplicas"`
		AvailableReplicas      int32 `json:"availableReplicas"`
		NumberReady            int32 `json:"numberReady"`
		DesiredNumberScheduled int32 `json:"desiredNumberScheduled"`
	} `json:"status"`
}
type Pod struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		NodeName   string `json:"nodeName"`
		Containers []struct {
			Name  string `json:"name"`
			Image string `json:"image"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase      string `json:"phase"`
		PodIP      string `json:"podIP"`
		HostIP     string `json:"hostIP"`
		StartTime  string `json:"startTime"`
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
		ContainerStatuses []struct {
			Name         string `json:"name"`
			Ready        bool   `json:"ready"`
			RestartCount int32  `json:"restartCount"`
			Image        string `json:"image"`
		} `json:"containerStatuses"`
	} `json:"status"`
}
type ServiceResource struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Type         string   `json:"type"`
		ClusterIP    string   `json:"clusterIP"`
		ExternalName string   `json:"externalName"`
		ExternalIPs  []string `json:"externalIPs"`
		Ports        []struct {
			Name       string `json:"name"`
			Port       int32  `json:"port"`
			TargetPort any    `json:"targetPort"`
			Protocol   string `json:"protocol"`
		} `json:"ports"`
	} `json:"spec"`
}
type PersistentVolumeClaim struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		VolumeName       string   `json:"volumeName"`
		StorageClassName string   `json:"storageClassName"`
		AccessModes      []string `json:"accessModes"`
		Resources        struct {
			Requests map[string]string `json:"requests"`
		} `json:"resources"`
	} `json:"spec"`
	Status struct {
		Phase    string            `json:"phase"`
		Capacity map[string]string `json:"capacity"`
	} `json:"status"`
}
type PersistentVolume struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		StorageClassName string            `json:"storageClassName"`
		AccessModes      []string          `json:"accessModes"`
		Capacity         map[string]string `json:"capacity"`
		ClaimRef         *struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"claimRef"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}
type StorageClass struct {
	Metadata             ObjectMeta `json:"metadata"`
	Provisioner          string     `json:"provisioner"`
	ReclaimPolicy        string     `json:"reclaimPolicy"`
	VolumeBindingMode    string     `json:"volumeBindingMode"`
	AllowVolumeExpansion *bool      `json:"allowVolumeExpansion"`
}
type PVCList struct {
	Metadata ListMeta                `json:"metadata"`
	Items    []PersistentVolumeClaim `json:"items"`
}
type PVList struct {
	Metadata ListMeta           `json:"metadata"`
	Items    []PersistentVolume `json:"items"`
}
type StorageClassList struct {
	Metadata ListMeta       `json:"metadata"`
	Items    []StorageClass `json:"items"`
}
type Ingress struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Rules []struct {
			Host string `json:"host"`
		} `json:"rules"`
	} `json:"spec"`
	Status struct {
		LoadBalancer struct {
			Ingress []struct {
				IP       string `json:"ip"`
				Hostname string `json:"hostname"`
			} `json:"ingress"`
		} `json:"loadBalancer"`
	} `json:"status"`
}
type NodeList struct {
	Metadata ListMeta `json:"metadata"`
	Items    []Node   `json:"items"`
}
type NamespaceList struct {
	Metadata ListMeta    `json:"metadata"`
	Items    []Namespace `json:"items"`
}
type WorkloadList struct {
	Metadata ListMeta   `json:"metadata"`
	Items    []Workload `json:"items"`
}
type PodList struct {
	Metadata ListMeta `json:"metadata"`
	Items    []Pod    `json:"items"`
}
type ServiceList struct {
	Metadata ListMeta          `json:"metadata"`
	Items    []ServiceResource `json:"items"`
}
type IngressList struct {
	Metadata ListMeta  `json:"metadata"`
	Items    []Ingress `json:"items"`
}
