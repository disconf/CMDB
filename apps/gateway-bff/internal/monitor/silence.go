package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type SilenceMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual bool   `json:"isEqual"`
}
type SilenceStatus struct {
	State string `json:"state"`
}
type Silence struct {
	ID        string           `json:"id"`
	Matchers  []SilenceMatcher `json:"matchers"`
	StartsAt  time.Time        `json:"startsAt"`
	EndsAt    time.Time        `json:"endsAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
	CreatedBy string           `json:"createdBy"`
	Comment   string           `json:"comment"`
	Status    SilenceStatus    `json:"status"`
}
type CreateSilenceInput struct {
	Matchers  []SilenceMatcher `json:"matchers"`
	StartsAt  time.Time        `json:"startsAt"`
	EndsAt    time.Time        `json:"endsAt"`
	CreatedBy string           `json:"createdBy"`
	Comment   string           `json:"comment"`
}

func (s *Service) alertmanagerRequest(method, path string, body any, target any) error {
	if s.alertmanagerURL == "" {
		return fmt.Errorf("alertmanager is not configured")
	}
	var payload *bytes.Reader
	if body == nil {
		payload = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, s.alertmanagerURL+path, payload)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("alertmanager returned %s", response.Status)
	}
	if target != nil {
		return json.NewDecoder(response.Body).Decode(target)
	}
	return nil
}

func (s *Service) Silences() ([]Silence, error) {
	var result []Silence
	if err := s.alertmanagerRequest(http.MethodGet, "/api/v2/silences", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}
func (s *Service) CreateSilence(input CreateSilenceInput) (string, error) {
	if len(input.Matchers) == 0 || input.EndsAt.IsZero() || !input.EndsAt.After(time.Now()) {
		return "", fmt.Errorf("invalid silence")
	}
	if input.StartsAt.IsZero() {
		input.StartsAt = time.Now()
	}
	for i := range input.Matchers {
		if input.Matchers[i].Name == "" || input.Matchers[i].Value == "" {
			return "", fmt.Errorf("invalid matcher")
		}
		input.Matchers[i].IsEqual = true
	}
	var result struct {
		SilenceID string `json:"silenceID"`
	}
	if err := s.alertmanagerRequest(http.MethodPost, "/api/v2/silences", input, &result); err != nil {
		return "", err
	}
	return result.SilenceID, nil
}
func (s *Service) ExpireSilence(id string) error {
	if id == "" {
		return fmt.Errorf("silence id is required")
	}
	return s.alertmanagerRequest(http.MethodDelete, "/api/v2/silence/"+url.PathEscape(id), nil, nil)
}
