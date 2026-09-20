package cicd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ResourceOverview struct {
	GeneratedAt string          `json:"generatedAt"`
	Harbor      HarborResources `json:"harbor"`
	ArgoCD      ArgoCDResources `json:"argoCD"`
	GitLab      GitLabResources `json:"gitlab"`
}

type HarborResources struct {
	Available          bool               `json:"available"`
	Status             string             `json:"status"`
	Message            string             `json:"message"`
	Projects           int                `json:"projects"`
	Repositories       int                `json:"repositories"`
	RecentRepositories []HarborRepository `json:"recentRepositories"`
}

type HarborRepository struct {
	Project       string `json:"project"`
	Name          string `json:"name"`
	ArtifactCount int    `json:"artifactCount"`
	PullCount     int    `json:"pullCount"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
}

type ArgoCDResources struct {
	Available    bool              `json:"available"`
	Status       string            `json:"status"`
	Message      string            `json:"message"`
	Applications int               `json:"applications"`
	Healthy      int               `json:"healthy"`
	Degraded     int               `json:"degraded"`
	Synced       int               `json:"synced"`
	OutOfSync    int               `json:"outOfSync"`
	Items        []ArgoApplication `json:"items"`
}

type ArgoApplication struct {
	Name          string `json:"name"`
	Namespace     string `json:"namespace"`
	Project       string `json:"project"`
	Destination   string `json:"destination"`
	SyncStatus    string `json:"syncStatus"`
	HealthStatus  string `json:"healthStatus"`
	Revision      string `json:"revision,omitempty"`
	LastOperation string `json:"lastOperation,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
}

type GitLabResources struct {
	Available      bool            `json:"available"`
	Status         string          `json:"status"`
	Message        string          `json:"message"`
	Projects       int             `json:"projects"`
	RecentProjects []GitLabProject `json:"recentProjects"`
}

type GitLabProject struct {
	ID             int             `json:"id"`
	Name           string          `json:"name"`
	Path           string          `json:"path"`
	Visibility     string          `json:"visibility,omitempty"`
	DefaultBranch  string          `json:"defaultBranch,omitempty"`
	LastActivityAt string          `json:"lastActivityAt,omitempty"`
	WebURL         string          `json:"webUrl,omitempty"`
	LatestPipeline *GitLabPipeline `json:"latestPipeline,omitempty"`
}

type GitLabPipeline struct {
	Status    string `json:"status"`
	Ref       string `json:"ref,omitempty"`
	SHA       string `json:"sha,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	WebURL    string `json:"webUrl,omitempty"`
}

func (s *Service) Resources(ctx context.Context, force bool) ResourceOverview {
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	if !force && !s.resourceAt.IsZero() && time.Since(s.resourceAt) < 30*time.Second {
		return cloneResourceOverview(s.resourceCache)
	}

	overview := ResourceOverview{GeneratedAt: time.Now().Format(time.RFC3339)}
	overview.Harbor = s.fetchHarborResources(ctx)
	overview.ArgoCD = s.fetchArgoCDResources(ctx)
	overview.GitLab = s.fetchGitLabResources(ctx)
	s.resourceCache = overview
	s.resourceAt = time.Now()
	return cloneResourceOverview(overview)
}

func (s *Service) fetchHarborResources(ctx context.Context) HarborResources {
	integration := s.integrationState(IntegrationHarbor)
	if !integration.Enabled {
		return HarborResources{Status: StatusNotConfigured, Message: integration.Message}
	}

	var projects []struct {
		Name            string `json:"name"`
		RepositoryCount int    `json:"repo_count"`
		UpdateTime      string `json:"update_time"`
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURL(integration.Endpoint, "/api/v2.0/projects?page=1&page_size=100&with_detail=true"), nil)
	if err != nil {
		return harborResourceError(err)
	}
	request.SetBasicAuth(s.config.HarborUsername, s.config.HarborPassword)
	if err := s.doJSON(request, &projects); err != nil {
		return harborResourceError(err)
	}

	var repositories []struct {
		Name          string `json:"name"`
		ArtifactCount int    `json:"artifact_count"`
		PullCount     int    `json:"pull_count"`
		UpdateTime    string `json:"update_time"`
	}
	repositoryRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURL(integration.Endpoint, "/api/v2.0/repositories?page=1&page_size=30&sort=-update_time"), nil)
	if err != nil {
		return harborResourceError(err)
	}
	repositoryRequest.SetBasicAuth(s.config.HarborUsername, s.config.HarborPassword)
	repositoryMessage := "Harbor 项目与仓库信息读取成功"
	if err := s.doJSON(repositoryRequest, &repositories); err != nil {
		repositoryMessage = "项目信息读取成功，仓库明细读取失败"
	}

	recent := make([]HarborRepository, 0, len(repositories))
	for _, repository := range repositories {
		name := strings.TrimSpace(repository.Name)
		project := ""
		if index := strings.Index(name, "/"); index > 0 {
			project = name[:index]
			name = name[index+1:]
		}
		recent = append(recent, HarborRepository{
			Project:       project,
			Name:          name,
			ArtifactCount: repository.ArtifactCount,
			PullCount:     repository.PullCount,
			UpdatedAt:     repository.UpdateTime,
		})
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].UpdatedAt > recent[j].UpdatedAt })
	repositoryCount := 0
	for _, project := range projects {
		repositoryCount += project.RepositoryCount
	}
	if repositoryCount < len(recent) {
		repositoryCount = len(recent)
	}
	return HarborResources{
		Available:          true,
		Status:             StatusConnected,
		Message:            repositoryMessage,
		Projects:           len(projects),
		Repositories:       repositoryCount,
		RecentRepositories: recent,
	}
}

func (s *Service) fetchArgoCDResources(ctx context.Context) ArgoCDResources {
	integration := s.integrationState(IntegrationArgoCD)
	if !integration.Enabled {
		return ArgoCDResources{Status: StatusNotConfigured, Message: integration.Message}
	}
	token, err := s.argoToken(ctx, integration)
	if err != nil {
		return argoResourceError(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURL(integration.Endpoint, "/api/v1/applications"), nil)
	if err != nil {
		return argoResourceError(err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	var payload struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Spec struct {
				Project     string `json:"project"`
				Destination struct {
					Server    string `json:"server"`
					Namespace string `json:"namespace"`
				} `json:"destination"`
			} `json:"spec"`
			Status struct {
				Sync struct {
					Status   string `json:"status"`
					Revision string `json:"revision"`
				} `json:"sync"`
				Health struct {
					Status string `json:"status"`
				} `json:"health"`
				OperationState struct {
					Phase      string `json:"phase"`
					FinishedAt string `json:"finishedAt"`
				} `json:"operationState"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := s.doJSON(request, &payload); err != nil {
		return argoResourceError(err)
	}
	result := ArgoCDResources{Available: true, Status: StatusConnected, Message: "Argo CD 应用状态读取成功", Applications: len(payload.Items), Items: make([]ArgoApplication, 0, len(payload.Items))}
	for _, application := range payload.Items {
		syncStatus := normalizeArgoStatus(application.Status.Sync.Status)
		healthStatus := normalizeArgoStatus(application.Status.Health.Status)
		if syncStatus == "Synced" {
			result.Synced++
		} else {
			result.OutOfSync++
		}
		switch healthStatus {
		case "Healthy":
			result.Healthy++
		case "Degraded":
			result.Degraded++
		}
		destination := application.Spec.Destination.Server
		if application.Spec.Destination.Namespace != "" {
			destination += "/" + application.Spec.Destination.Namespace
		}
		result.Items = append(result.Items, ArgoApplication{
			Name:          application.Metadata.Name,
			Namespace:     application.Metadata.Namespace,
			Project:       application.Spec.Project,
			Destination:   destination,
			SyncStatus:    syncStatus,
			HealthStatus:  healthStatus,
			Revision:      application.Status.Sync.Revision,
			LastOperation: application.Status.OperationState.Phase,
			UpdatedAt:     application.Status.OperationState.FinishedAt,
		})
	}
	sort.SliceStable(result.Items, func(i, j int) bool { return result.Items[i].Name < result.Items[j].Name })
	return result
}

func (s *Service) fetchGitLabResources(ctx context.Context) GitLabResources {
	integration := s.integrationState(IntegrationGitLab)
	if !integration.Enabled {
		return GitLabResources{Status: StatusNotConfigured, Message: integration.Message}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURL(integration.Endpoint, "/api/v4/projects?membership=true&order_by=last_activity_at&sort=desc&per_page=20&simple=true"), nil)
	if err != nil {
		return gitLabResourceError(err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("PRIVATE-TOKEN", s.config.GitLabToken)
	var projects []struct {
		ID             int    `json:"id"`
		Name           string `json:"name"`
		Path           string `json:"path_with_namespace"`
		Visibility     string `json:"visibility"`
		DefaultBranch  string `json:"default_branch"`
		LastActivityAt string `json:"last_activity_at"`
		WebURL         string `json:"web_url"`
	}
	if err := s.doJSON(request, &projects); err != nil {
		return gitLabResourceError(err)
	}
	result := GitLabResources{Available: true, Status: StatusConnected, Message: "GitLab 项目信息读取成功", Projects: len(projects), RecentProjects: make([]GitLabProject, 0, len(projects))}
	for index, project := range projects {
		projectSummary := GitLabProject{ID: project.ID, Name: project.Name, Path: project.Path, Visibility: project.Visibility, DefaultBranch: project.DefaultBranch, LastActivityAt: project.LastActivityAt, WebURL: project.WebURL}
		if index < 8 {
			projectSummary.LatestPipeline = s.fetchGitLabPipeline(ctx, integration.Endpoint, project.ID)
		}
		result.RecentProjects = append(result.RecentProjects, projectSummary)
	}
	return result
}

func (s *Service) fetchGitLabPipeline(ctx context.Context, endpoint string, projectID int) *GitLabPipeline {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURL(endpoint, "/api/v4/projects/"+strconv.Itoa(projectID)+"/pipelines?per_page=1&order_by=id&sort=desc"), nil)
	if err != nil {
		return nil
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("PRIVATE-TOKEN", s.config.GitLabToken)
	var pipelines []struct {
		Status    string `json:"status"`
		Ref       string `json:"ref"`
		SHA       string `json:"sha"`
		UpdatedAt string `json:"updated_at"`
		WebURL    string `json:"web_url"`
	}
	if err := s.doJSON(request, &pipelines); err != nil || len(pipelines) == 0 {
		return nil
	}
	return &GitLabPipeline{Status: pipelines[0].Status, Ref: pipelines[0].Ref, SHA: pipelines[0].SHA, UpdatedAt: pipelines[0].UpdatedAt, WebURL: pipelines[0].WebURL}
}

func (s *Service) argoToken(ctx context.Context, integration Integration) (string, error) {
	if token := strings.TrimSpace(s.config.ArgoCDToken); token != "" {
		return token, nil
	}
	body, _ := json.Marshal(map[string]string{"username": s.config.ArgoCDUsername, "password": s.config.ArgoCDPassword})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURL(integration.Endpoint, "/api/v1/session"), strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	var session struct {
		Token string `json:"token"`
	}
	if err := s.doJSON(request, &session); err != nil {
		return "", err
	}
	if strings.TrimSpace(session.Token) == "" {
		return "", errors.New("Argo CD 登录成功但未返回访问令牌")
	}
	return session.Token, nil
}

func (s *Service) integrationState(id string) Integration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.integrations[id]
}

func harborResourceError(err error) HarborResources {
	return HarborResources{Status: StatusError, Message: safeProbeMessage(err)}
}

func argoResourceError(err error) ArgoCDResources {
	return ArgoCDResources{Status: StatusError, Message: safeProbeMessage(err)}
}

func gitLabResourceError(err error) GitLabResources {
	return GitLabResources{Status: StatusError, Message: safeProbeMessage(err)}
}

func normalizeArgoStatus(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	return value
}

func cloneResourceOverview(value ResourceOverview) ResourceOverview {
	copyValue := value
	copyValue.Harbor.RecentRepositories = append([]HarborRepository(nil), value.Harbor.RecentRepositories...)
	copyValue.ArgoCD.Items = append([]ArgoApplication(nil), value.ArgoCD.Items...)
	copyValue.GitLab.RecentProjects = make([]GitLabProject, len(value.GitLab.RecentProjects))
	copy(copyValue.GitLab.RecentProjects, value.GitLab.RecentProjects)
	return copyValue
}
