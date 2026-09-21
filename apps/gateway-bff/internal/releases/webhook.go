package releases

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func ParseProviderEvent(source string, raw []byte) (EventInput, error) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "harbor":
		return parseHarborEvent(raw)
	case "argocd", "argo":
		return parseArgoCDEvent(raw)
	case "gitlab":
		return parseGitLabEvent(raw)
	default:
		return EventInput{}, errors.New("unsupported provider event")
	}
}

func parseHarborEvent(raw []byte) (EventInput, error) {
	var payload struct {
		Type      string `json:"type"`
		Operator  string `json:"operator"`
		EventData struct {
			Resources []struct {
				Digest      string `json:"digest"`
				Tag         string `json:"tag"`
				ResourceURL string `json:"resource_url"`
			} `json:"resources"`
			Repository struct {
				Name         string `json:"name"`
				Namespace    string `json:"namespace"`
				RepoFullName string `json:"repo_full_name"`
			} `json:"repository"`
		} `json:"event_data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return EventInput{}, err
	}
	if len(payload.EventData.Resources) == 0 {
		return EventInput{}, errors.New("harbor event has no resources")
	}
	resource := payload.EventData.Resources[0]
	application := firstReleaseString(payload.EventData.Repository.RepoFullName, payload.EventData.Repository.Namespace+"/"+payload.EventData.Repository.Name)
	version := firstReleaseString(resource.Tag, resource.Digest)
	if application == "" || version == "" {
		return EventInput{}, errors.New("harbor event missing application or version")
	}
	status := StatusSuccess
	message := "Harbor 制品已推送"
	if strings.Contains(strings.ToLower(payload.Type), "delete") {
		status = StatusFailed
		message = "Harbor 制品已删除"
	}
	return EventInput{Source: "harbor", ExternalID: "harbor:" + application + ":" + version, Type: "artifact", Application: application, Version: version, Environment: "unknown", Status: status, Stage: "artifact", Progress: 100, Message: message, URL: resource.ResourceURL, Operator: payload.Operator}, nil
}

func parseArgoCDEvent(raw []byte) (EventInput, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return EventInput{}, err
	}
	application := releaseMapString(payload, "application", "app", "name")
	if appMap := releaseMap(payload, "app"); appMap != nil {
		application = firstReleaseString(application, releaseMapString(appMap, "name"))
	}
	syncStatus := releaseMapString(payload, "syncStatus", "sync_status")
	if syncMap := releaseMap(payload, "sync"); syncMap != nil {
		syncStatus = firstReleaseString(syncStatus, releaseMapString(syncMap, "status"))
	}
	healthStatus := releaseMapString(payload, "healthStatus", "health_status")
	if healthMap := releaseMap(payload, "health"); healthMap != nil {
		healthStatus = firstReleaseString(healthStatus, releaseMapString(healthMap, "status"))
	}
	revision := releaseMapString(payload, "revision", "rev", "commit")
	if syncMap := releaseMap(payload, "sync"); syncMap != nil {
		revision = firstReleaseString(revision, releaseMapString(syncMap, "revision"))
	}
	environment := releaseMapString(payload, "environment", "env")
	if destination := releaseMap(payload, "destination"); destination != nil {
		environment = firstReleaseString(environment, releaseMapString(destination, "namespace"))
	}
	if application == "" {
		return EventInput{}, errors.New("argocd event missing application")
	}
	if revision == "" {
		revision = "unknown"
	}
	if environment == "" {
		environment = "unknown"
	}
	status := argoEventStatus(syncStatus, healthStatus, releaseMapString(payload, "phase", "status"))
	stage := "deploy"
	progress := 70
	message := firstReleaseString(releaseMapString(payload, "message"), "Argo CD 状态："+firstReleaseString(healthStatus, syncStatus, "unknown"))
	if strings.EqualFold(status, StatusSuccess) {
		stage, progress, message = "verify", 100, firstReleaseString(releaseMapString(payload, "message"), "Argo CD 应用已同步且健康")
	} else if strings.EqualFold(status, StatusFailed) {
		progress = 60
	}
	return EventInput{Source: "argocd", ExternalID: "argocd:" + application + ":" + revision, Type: "deployment", Application: application, Version: revision, Environment: environment, Status: status, Stage: stage, Progress: progress, Message: message, Commit: revision, URL: releaseMapString(payload, "url", "externalURL"), Operator: releaseMapString(payload, "operator", "user")}, nil
}

func parseGitLabEvent(raw []byte) (EventInput, error) {
	var payload struct {
		ObjectKind       string `json:"object_kind"`
		ObjectAttributes struct {
			ID          int64  `json:"id"`
			Ref         string `json:"ref"`
			SHA         string `json:"sha"`
			Status      string `json:"status"`
			URL         string `json:"url"`
			Environment string `json:"environment"`
			User        struct {
				Name     string `json:"name"`
				Username string `json:"username"`
			} `json:"user"`
		} `json:"object_attributes"`
		Project struct {
			Name              string `json:"name"`
			PathWithNamespace string `json:"path_with_namespace"`
			WebURL            string `json:"web_url"`
		} `json:"project"`
		Commit struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		} `json:"commit"`
		User struct {
			Name     string `json:"name"`
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return EventInput{}, err
	}
	if payload.ObjectKind != "" && payload.ObjectKind != "pipeline" {
		return EventInput{}, errors.New("gitlab event is not a pipeline")
	}
	application := firstReleaseString(payload.Project.PathWithNamespace, payload.Project.Name)
	version := firstReleaseString(payload.ObjectAttributes.Ref, payload.ObjectAttributes.SHA, payload.Commit.ID)
	if application == "" || version == "" {
		return EventInput{}, errors.New("gitlab event missing project or pipeline ref")
	}
	environment := firstReleaseString(payload.ObjectAttributes.Environment, "unknown")
	status := gitLabEventStatus(payload.ObjectAttributes.Status)
	progress := 30
	message := "GitLab Pipeline 已创建"
	if status == StatusRunning {
		progress, message = 55, "GitLab Pipeline 执行中"
	} else if status == StatusSuccess {
		progress, message = 75, "GitLab Pipeline 执行成功"
	} else if status == StatusFailed {
		progress, message = 60, "GitLab Pipeline 执行失败"
	}
	externalID := fmt.Sprintf("gitlab:%d", payload.ObjectAttributes.ID)
	if payload.ObjectAttributes.ID == 0 {
		externalID = "gitlab:" + application + ":" + firstReleaseString(payload.ObjectAttributes.SHA, payload.ObjectAttributes.Ref)
	}
	return EventInput{Source: "gitlab", ExternalID: externalID, Type: "pipeline", Application: application, Version: version, Environment: environment, Status: status, Stage: "build", Progress: progress, Message: message, Commit: firstReleaseString(payload.Commit.ID, payload.ObjectAttributes.SHA), URL: payload.ObjectAttributes.URL, Operator: firstReleaseString(payload.ObjectAttributes.User.Username, payload.ObjectAttributes.User.Name, payload.User.Username, payload.User.Name)}, nil
}

func argoEventStatus(syncStatus, healthStatus, phase string) string {
	combined := strings.ToLower(strings.TrimSpace(syncStatus + " " + healthStatus + " " + phase))
	switch {
	case strings.Contains(combined, "degraded"), strings.Contains(combined, "failed"), strings.Contains(combined, "error"):
		return StatusFailed
	case strings.Contains(combined, "synced"), strings.Contains(combined, "healthy"), strings.Contains(combined, "succeeded"):
		return StatusSuccess
	default:
		return StatusRunning
	}
}

func gitLabEventStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success":
		return StatusSuccess
	case "failed", "canceled", "cancelled":
		return StatusFailed
	default:
		return StatusRunning
	}
}

func releaseMap(payload map[string]any, key string) map[string]any {
	value, _ := payload[key].(map[string]any)
	return value
}

func releaseMapString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func firstReleaseString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
