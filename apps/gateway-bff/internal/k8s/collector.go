package k8s

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"cmdb/gateway-bff/internal/cmdb"
)

type Snapshot struct {
	Version      Version
	Nodes        []Node
	Namespaces   []Namespace
	Deployments  []Workload
	StatefulSets []Workload
	DaemonSets   []Workload
	Pods         []Pod
	Services     []ServiceResource
	Ingresses    []Ingress
}

type RelationRef struct {
	Type      string
	TargetKey string
}
type Resource struct {
	Key       string
	Asset     cmdb.CreateAssetInput
	Relations []RelationRef
}

func (c *Client) Collect(ctx context.Context) (Snapshot, error) {
	var snapshot Snapshot
	if err := c.get(ctx, "/version", &snapshot.Version); err != nil {
		return snapshot, err
	}
	var nodes NodeList
	if err := c.get(ctx, "/api/v1/nodes", &nodes); err != nil {
		return snapshot, err
	}
	snapshot.Nodes = nodes.Items
	var namespaces NamespaceList
	if err := c.get(ctx, "/api/v1/namespaces", &namespaces); err != nil {
		return snapshot, err
	}
	snapshot.Namespaces = namespaces.Items
	var deployments WorkloadList
	if err := c.get(ctx, "/apis/apps/v1/deployments", &deployments); err != nil {
		return snapshot, err
	}
	snapshot.Deployments = deployments.Items
	var statefulsets WorkloadList
	if err := c.get(ctx, "/apis/apps/v1/statefulsets", &statefulsets); err != nil {
		return snapshot, err
	}
	snapshot.StatefulSets = statefulsets.Items
	var daemonsets WorkloadList
	if err := c.get(ctx, "/apis/apps/v1/daemonsets", &daemonsets); err != nil {
		return snapshot, err
	}
	snapshot.DaemonSets = daemonsets.Items
	var pods PodList
	if err := c.get(ctx, "/api/v1/pods", &pods); err != nil {
		return snapshot, err
	}
	snapshot.Pods = pods.Items
	var services ServiceList
	if err := c.get(ctx, "/api/v1/services", &services); err != nil {
		return snapshot, err
	}
	snapshot.Services = services.Items
	var ingresses IngressList
	if err := c.get(ctx, "/apis/networking.k8s.io/v1/ingresses", &ingresses); err != nil {
		return snapshot, err
	}
	snapshot.Ingresses = ingresses.Items
	return snapshot, nil
}

func BuildResources(clusterName, apiServer string, snapshot Snapshot) []Resource {
	resources := []Resource{}
	clusterKey := "cluster/" + clusterName
	resources = append(resources, Resource{Key: clusterKey, Asset: cmdb.CreateAssetInput{ID: "k8s-cluster/" + clusterName, Name: clusterName, Type: "k8s-cluster", Status: "online", Environment: "生产", ProjectGroup: "Kubernetes", Owner: "待分配", Location: "Kubernetes 集群", Source: "kubernetes", Tags: []string{"K8s", "Cluster"}, Attributes: attrs(map[string]string{"cluster_name": clusterName, "api_server": apiServer, "k8s_version": snapshot.Version.GitVersion})}})
	for _, node := range snapshot.Nodes {
		name, ip, status := node.Metadata.Name, nodeAddress(node), nodeStatus(node)
		resources = append(resources, Resource{Key: "node/" + name, Asset: cmdb.CreateAssetInput{ID: "k8s-node/" + name, Name: name, Type: "k8s-node", Status: status, IP: ip, Environment: "生产", ProjectGroup: "Kubernetes", Owner: "待分配", Location: clusterName + " / Node", Source: "kubernetes", Tags: []string{"K8s", "Node"}, Attributes: attrs(map[string]string{"cluster_name": clusterName, "k8s_uid": node.Metadata.UID, "kubelet_version": node.Status.NodeInfo.KubeletVersion, "os_image": node.Status.NodeInfo.OSImage, "architecture": node.Status.NodeInfo.Architecture, "cpu_capacity": node.Status.Capacity["cpu"], "memory_capacity": node.Status.Capacity["memory"], "provider_id": node.Spec.ProviderID})}, Relations: []RelationRef{{Type: "belongs-to", TargetKey: clusterKey}}})
	}
	for _, namespace := range snapshot.Namespaces {
		name, status := namespace.Metadata.Name, strings.ToLower(namespace.Status.Phase)
		if status == "active" {
			status = "online"
		}
		resources = append(resources, Resource{Key: "namespace/" + name, Asset: cmdb.CreateAssetInput{ID: "k8s-namespace/" + name, Name: name, Type: "k8s-namespace", Status: status, Environment: "生产", ProjectGroup: "Kubernetes", Owner: "待分配", Location: clusterName + " / Namespace", Source: "kubernetes", Tags: []string{"K8s", "Namespace"}, Attributes: attrs(map[string]string{"cluster_name": clusterName, "k8s_uid": namespace.Metadata.UID, "namespace_status": namespace.Status.Phase})}, Relations: []RelationRef{{Type: "belongs-to", TargetKey: clusterKey}}})
	}
	for _, workload := range snapshot.Deployments {
		resources = append(resources, workloadResource("Deployment", workload, clusterName))
	}
	for _, workload := range snapshot.StatefulSets {
		resources = append(resources, workloadResource("StatefulSet", workload, clusterName))
	}
	for _, workload := range snapshot.DaemonSets {
		resources = append(resources, workloadResource("DaemonSet", workload, clusterName))
	}
	for _, pod := range snapshot.Pods {
		namespace, name, status := pod.Metadata.Namespace, pod.Metadata.Name, podStatus(pod)
		relations := []RelationRef{{Type: "belongs-to", TargetKey: "namespace/" + namespace}}
		if pod.Spec.NodeName != "" {
			relations = append(relations, RelationRef{Type: "runs-on", TargetKey: "node/" + pod.Spec.NodeName})
		}
		resources = append(resources, Resource{Key: "pod/" + namespace + "/" + name, Asset: cmdb.CreateAssetInput{ID: "k8s-pod/" + namespace + "/" + name, Name: name, Type: "k8s-pod", Status: status, IP: pod.Status.PodIP, Environment: "生产", ProjectGroup: "Kubernetes", Owner: "待分配", Location: clusterName + " / " + namespace + " / " + pod.Spec.NodeName, Source: "kubernetes", Tags: []string{"K8s", "Pod", namespace}, Attributes: attrs(map[string]string{"cluster_name": clusterName, "namespace": namespace, "node_name": pod.Spec.NodeName, "pod_phase": pod.Status.Phase, "restart_count": strconv.Itoa(int(podRestarts(pod))), "images": podImages(pod)})}, Relations: relations})
	}
	for _, service := range snapshot.Services {
		namespace, name := service.Metadata.Namespace, service.Metadata.Name
		resources = append(resources, Resource{Key: "service/" + namespace + "/" + name, Asset: cmdb.CreateAssetInput{ID: "k8s-service/" + namespace + "/" + name, Name: name, Type: "k8s-service", Status: "online", IP: service.Spec.ClusterIP, Environment: "生产", ProjectGroup: "Kubernetes", Owner: "待分配", Location: clusterName + " / " + namespace, Source: "kubernetes", Tags: []string{"K8s", "Service", namespace}, Attributes: attrs(map[string]string{"cluster_name": clusterName, "namespace": namespace, "service_type": service.Spec.Type, "cluster_ip": service.Spec.ClusterIP, "ports": servicePorts(service), "external_name": service.Spec.ExternalName})}, Relations: []RelationRef{{Type: "belongs-to", TargetKey: "namespace/" + namespace}}})
	}
	for _, ingress := range snapshot.Ingresses {
		namespace, name, status := ingress.Metadata.Namespace, ingress.Metadata.Name, "online"
		if ingressLB(ingress) == "" {
			status = "warning"
		}
		resources = append(resources, Resource{Key: "ingress/" + namespace + "/" + name, Asset: cmdb.CreateAssetInput{ID: "k8s-ingress/" + namespace + "/" + name, Name: name, Type: "k8s-ingress", Status: status, Environment: "生产", ProjectGroup: "Kubernetes", Owner: "待分配", Location: clusterName + " / " + namespace, Source: "kubernetes", Tags: []string{"K8s", "Ingress", namespace}, Attributes: attrs(map[string]string{"cluster_name": clusterName, "namespace": namespace, "hosts": ingressHosts(ingress), "load_balancer": ingressLB(ingress)})}, Relations: []RelationRef{{Type: "belongs-to", TargetKey: "namespace/" + namespace}}})
	}
	return resources
}

func workloadResource(kind string, workload Workload, clusterName string) Resource {
	namespace, name, status := workload.Metadata.Namespace, workload.Metadata.Name, workloadStatus(workload)
	return Resource{Key: workloadKey(kind, namespace, name), Asset: cmdb.CreateAssetInput{ID: "k8s-workload/" + namespace + "/" + strings.ToLower(kind) + "/" + name, Name: name, Type: "k8s-workload", Status: status, Environment: "生产", ProjectGroup: "Kubernetes", Owner: "待分配", Location: clusterName + " / " + namespace, Source: "kubernetes", Tags: []string{"K8s", kind, namespace}, Attributes: attrs(map[string]string{"cluster_name": clusterName, "namespace": namespace, "workload_kind": kind, "replicas": strconv.Itoa(int(workloadDesired(workload))), "ready_replicas": strconv.Itoa(int(workloadReady(workload))), "images": workloadImage(workload)})}, Relations: []RelationRef{{Type: "belongs-to", TargetKey: "namespace/" + namespace}}}
}

func attrs(values map[string]string) []cmdb.Attribute {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]cmdb.Attribute, 0, len(keys))
	for _, key := range keys {
		if values[key] != "" {
			out = append(out, cmdb.Attribute{Name: key, Value: values[key]})
		}
	}
	return out
}
func nodeAddress(node Node) string {
	for _, address := range node.Status.Addresses {
		if address.Type == "InternalIP" {
			return address.Address
		}
	}
	return ""
}
func nodeStatus(node Node) string {
	for _, condition := range node.Status.Conditions {
		if condition.Type == "Ready" {
			if condition.Status == "True" {
				return "online"
			}
			return "warning"
		}
	}
	return "warning"
}
func workloadImage(workload Workload) string {
	names := []string{}
	for _, item := range workload.Spec.Template.Spec.Containers {
		names = append(names, item.Image)
	}
	return strings.Join(names, " | ")
}
func workloadDesired(workload Workload) int32 {
	if workload.Spec.Replicas != nil {
		return *workload.Spec.Replicas
	}
	if workload.Status.DesiredNumberScheduled > 0 {
		return workload.Status.DesiredNumberScheduled
	}
	return 0
}
func workloadReady(workload Workload) int32 {
	if workload.Status.ReadyReplicas > 0 {
		return workload.Status.ReadyReplicas
	}
	return workload.Status.NumberReady
}
func workloadStatus(workload Workload) string {
	desired, ready := workloadDesired(workload), workloadReady(workload)
	if desired > 0 && ready < desired {
		return "warning"
	}
	return "online"
}
func podStatus(pod Pod) string {
	switch strings.ToLower(pod.Status.Phase) {
	case "running", "succeeded":
		return "online"
	case "failed":
		return "warning"
	default:
		return "warning"
	}
}
func podImages(pod Pod) string {
	values := []string{}
	for _, item := range pod.Status.ContainerStatuses {
		if item.Image != "" {
			values = append(values, item.Image)
		}
	}
	if len(values) == 0 {
		for _, item := range pod.Spec.Containers {
			values = append(values, item.Image)
		}
	}
	return strings.Join(values, " | ")
}
func podRestarts(pod Pod) int32 {
	var total int32
	for _, item := range pod.Status.ContainerStatuses {
		total += item.RestartCount
	}
	return total
}
func servicePorts(service ServiceResource) string {
	values := []string{}
	for _, port := range service.Spec.Ports {
		value := strconv.Itoa(int(port.Port))
		if port.Name != "" {
			value = port.Name + ":" + value
		}
		if port.Protocol != "" {
			value += "/" + port.Protocol
		}
		values = append(values, value)
	}
	return strings.Join(values, ", ")
}
func ingressHosts(ingress Ingress) string {
	values := []string{}
	for _, rule := range ingress.Spec.Rules {
		if rule.Host != "" {
			values = append(values, rule.Host)
		}
	}
	return strings.Join(values, ", ")
}
func ingressLB(ingress Ingress) string {
	values := []string{}
	for _, item := range ingress.Status.LoadBalancer.Ingress {
		value := item.IP
		if value == "" {
			value = item.Hostname
		}
		if value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, ", ")
}
func workloadKey(kind, namespace, name string) string {
	return fmt.Sprintf("workload/%s/%s/%s", namespace, strings.ToLower(kind), name)
}
