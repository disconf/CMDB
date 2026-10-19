package monitor

import (
	"math"
	"strconv"
)

type KubernetesCapacity struct {
	Available bool             `json:"available"`
	Cluster   string           `json:"cluster"`
	Source    string           `json:"source"`
	Message   string           `json:"message,omitempty"`
	Pods      CapacityPods     `json:"pods"`
	CPU       CapacityResource `json:"cpu"`
	Memory    CapacityResource `json:"memory"`
}

type CapacityPods struct {
	Available    bool    `json:"available"`
	Used         int     `json:"used"`
	Capacity     int     `json:"capacity"`
	UsagePercent float64 `json:"usagePercent"`
}

type CapacityResource struct {
	Available    bool    `json:"available"`
	Requested    float64 `json:"requested"`
	Capacity     float64 `json:"capacity"`
	UsagePercent float64 `json:"usagePercent"`
	Unit         string  `json:"unit"`
}

// KubernetesCapacity reads the KSM metrics produced for a single downstream
// cluster. Missing metrics are reported as unavailable instead of zero so the
// UI does not mistake a broken scrape for an empty cluster.
func (s *Service) KubernetesCapacity(cluster string) KubernetesCapacity {
	result := KubernetesCapacity{Cluster: cluster, Source: "prometheus/kube-state-metrics"}
	if s.prometheusURL == "" {
		result.Message = "Prometheus 未配置"
		return result
	}
	label := strconv.Quote(cluster)
	podUsed, podUsedErr := s.instant(`count(kube_pod_info{cluster=` + label + `})`)
	podCapacity, podCapacityErr := s.instant(`sum(kube_node_status_allocatable{resource="pods",cluster=` + label + `})`)
	cpuRequested, cpuRequestedErr := s.instant(`sum(kube_pod_container_resource_requests{resource="cpu",cluster=` + label + `})`)
	cpuCapacity, cpuCapacityErr := s.instant(`sum(kube_node_status_allocatable{resource="cpu",cluster=` + label + `})`)
	memoryRequested, memoryRequestedErr := s.instant(`sum(kube_pod_container_resource_requests{resource="memory",cluster=` + label + `})`)
	memoryCapacity, memoryCapacityErr := s.instant(`sum(kube_node_status_allocatable{resource="memory",cluster=` + label + `})`)

	result.Pods = CapacityPods{
		Available: podUsedErr == nil && podCapacityErr == nil,
		Used:      int(math.Round(podUsed)),
		Capacity:  int(math.Round(podCapacity)),
	}
	if result.Pods.Available {
		result.Pods.UsagePercent = percent(float64(result.Pods.Used), float64(result.Pods.Capacity))
	}

	result.CPU = CapacityResource{
		Available: cpuRequestedErr == nil && cpuCapacityErr == nil,
		Requested: roundTwo(cpuRequested),
		Capacity:  roundTwo(cpuCapacity),
		Unit:      "核",
	}
	if result.CPU.Available {
		result.CPU.UsagePercent = percent(cpuRequested, cpuCapacity)
	}

	result.Memory = CapacityResource{
		Available: memoryRequestedErr == nil && memoryCapacityErr == nil,
		Requested: roundTwo(memoryRequested / (1024 * 1024 * 1024)),
		Capacity:  roundTwo(memoryCapacity / (1024 * 1024 * 1024)),
		Unit:      "GiB",
	}
	if result.Memory.Available {
		result.Memory.UsagePercent = percent(memoryRequested, memoryCapacity)
	}

	result.Available = result.Pods.Available || result.CPU.Available || result.Memory.Available
	if !result.Available {
		result.Message = "当前集群没有可用的 kube-state-metrics 指标"
	}
	return result
}

func percent(used, capacity float64) float64 {
	if capacity <= 0 {
		return 0
	}
	return roundTwo(used / capacity * 100)
}

func roundTwo(value float64) float64 {
	return math.Round(value*100) / 100
}
