package releases

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type DispatchInfo struct {
	Mode     string `json:"mode"`
	Ready    bool   `json:"ready"`
	Endpoint string `json:"endpoint,omitempty"`
}

type DispatchRequest struct {
	Action             string `json:"action"`
	ReleaseID          string `json:"releaseId"`
	Application        string `json:"application"`
	Version            string `json:"version"`
	PreviousVersion    string `json:"previousVersion"`
	Environment        string `json:"environment"`
	TicketID           string `json:"ticketId,omitempty"`
	Operator           string `json:"operator"`
	AuthorizationToken string `json:"authorizationToken"`
}

type Dispatcher interface {
	Dispatch(context.Context, DispatchRequest) error
	Info() DispatchInfo
}

type manualDispatcher struct{}

func (manualDispatcher) Info() DispatchInfo                              { return DispatchInfo{Mode: "manual", Ready: true} }
func (manualDispatcher) Dispatch(context.Context, DispatchRequest) error { return nil }

type httpDispatcher struct {
	endpoint string
	token    string
	client   *http.Client
}

func (d *httpDispatcher) Info() DispatchInfo {
	return DispatchInfo{Mode: "http", Ready: d.endpoint != "", Endpoint: d.endpoint}
}

func (d *httpDispatcher) Dispatch(ctx context.Context, input DispatchRequest) error {
	payload, _ := json.Marshal(input)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if d.token != "" {
		request.Header.Set("Authorization", "Bearer "+d.token)
	}
	response, err := d.client.Do(request)
	if err != nil {
		return fmt.Errorf("release executor request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("release executor returned %s", response.Status)
	}
	return nil
}

func newDispatcherFromEnv() Dispatcher {
	raw := strings.TrimSpace(os.Getenv("RELEASE_EXECUTOR_URL"))
	if raw == "" {
		return manualDispatcher{}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		panic(fmt.Sprintf("invalid RELEASE_EXECUTOR_URL: %q", raw))
	}
	return &httpDispatcher{
		endpoint: strings.TrimRight(raw, "/"),
		token:    strings.TrimSpace(os.Getenv("RELEASE_EXECUTOR_TOKEN")),
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *Service) ExecutorInfo() DispatchInfo {
	if s.dispatcher == nil {
		return manualDispatcher{}.Info()
	}
	return s.dispatcher.Info()
}

func (s *Service) Dispatch(ctx context.Context, item Release, action, authorizationToken string) error {
	if s.dispatcher == nil {
		return nil
	}
	action = strings.TrimSpace(action)
	if action != "deploy" && action != "rollback" {
		return errors.New("invalid release action")
	}
	return s.dispatcher.Dispatch(ctx, DispatchRequest{
		Action: action, ReleaseID: item.ID, Application: item.Application,
		Version: item.Version, PreviousVersion: item.PreviousVersion,
		Environment: item.Environment, TicketID: item.TicketID,
		Operator: item.Operator, AuthorizationToken: authorizationToken,
	})
}
