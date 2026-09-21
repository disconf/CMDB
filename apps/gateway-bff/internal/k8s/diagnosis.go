package k8s

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type PodDiagnosisContainer struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	Image        string `json:"image"`
}

type PodDiagnosisEvent struct {
	Type    string `json:"type"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Count   int32  `json:"count"`
}

type PodDiagnosis struct {
	Namespace    string                  `json:"namespace"`
	Name         string                  `json:"name"`
	Phase        string                  `json:"phase"`
	PodIP        string                  `json:"podIp"`
	NodeName     string                  `json:"nodeName"`
	RestartCount int32                   `json:"restartCount"`
	Containers   []PodDiagnosisContainer `json:"containers"`
	Events       []PodDiagnosisEvent     `json:"events"`
	Logs         []string                `json:"logs"`
}

func (s *Service) DiagnosePod(ctx context.Context, namespace, name, container string) (PodDiagnosis, error) {
	if s.client == nil {
		return PodDiagnosis{}, fmt.Errorf("Kubernetes collector is not configured")
	}
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	container = strings.TrimSpace(container)
	if namespace == "" || name == "" {
		return PodDiagnosis{}, fmt.Errorf("namespace and pod name are required")
	}
	var pod Pod
	path := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(name)
	if err := s.client.get(ctx, path, &pod); err != nil {
		return PodDiagnosis{}, err
	}
	result := PodDiagnosis{Namespace: namespace, Name: name, Phase: pod.Status.Phase, PodIP: pod.Status.PodIP, NodeName: pod.Spec.NodeName}
	for _, item := range pod.Status.ContainerStatuses {
		result.RestartCount += item.RestartCount
		result.Containers = append(result.Containers, PodDiagnosisContainer{Name: item.Name, Ready: item.Ready, RestartCount: item.RestartCount, Image: item.Image})
	}
	var events struct {
		Items []struct {
			Type    string `json:"type"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
			Count   int32  `json:"count"`
		} `json:"items"`
	}
	eventPath := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/events?fieldSelector=" + url.QueryEscape("involvedObject.name="+name)
	if err := s.client.get(ctx, eventPath, &events); err == nil {
		for _, item := range events.Items {
			if len(result.Events) >= 30 {
				break
			}
			result.Events = append(result.Events, PodDiagnosisEvent{Type: item.Type, Reason: item.Reason, Message: item.Message, Count: item.Count})
		}
	}
	if container == "" && len(result.Containers) > 0 {
		container = result.Containers[0].Name
	}
	logPath := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(name) + "/log?tailLines=100&timestamps=false"
	if container != "" {
		logPath += "&container=" + url.QueryEscape(container)
	}
	if logs, err := s.client.getText(ctx, logPath); err == nil {
		result.Logs = splitLogLines(logs, 100)
	}
	return result, nil
}

func (c *Client) getText(ctx context.Context, path string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "text/plain")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("Kubernetes API %s: %s %s", path, response.Status, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	return string(body), err
}

func splitLogLines(value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	raw := strings.Split(value, "\n")
	if len(raw) > limit {
		raw = raw[len(raw)-limit:]
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
