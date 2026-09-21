package cicd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

func (s *Service) SyncArgoApplication(ctx context.Context, namespace, name string, prune, dryRun bool) error {
	name = strings.TrimSpace(name)
	namespace = strings.TrimSpace(namespace)
	item := s.integrationState(IntegrationArgoCD)
	if name == "" || !item.Enabled {
		return ErrNotConfigured
	}
	token, err := s.argoToken(ctx, item)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]bool{"prune": prune, "dryRun": dryRun})
	endpoint := joinURL(item.Endpoint, "/api/v1/applications/"+url.PathEscape(name)+"/sync")
	if namespace != "" {
		endpoint += "?appNamespace=" + url.QueryEscape(namespace)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	var response map[string]any
	return s.doJSON(request, &response)
}
