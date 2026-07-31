package monitor

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type AlertRule struct {
	Group       string  `json:"group"`
	Name        string  `json:"name"`
	Query       string  `json:"query"`
	Duration    float64 `json:"duration"`
	Health      string  `json:"health"`
	LastError   string  `json:"lastError"`
	State       string  `json:"state"`
	Severity    string  `json:"severity"`
	FiringCount int     `json:"firingCount"`
}

type prometheusRulesResponse struct {
	Status string `json:"status"`
	Data   struct {
		Groups []struct {
			Name  string `json:"name"`
			Rules []struct {
				Type      string            `json:"type"`
				Name      string            `json:"name"`
				Query     string            `json:"query"`
				Duration  float64           `json:"duration"`
				Health    string            `json:"health"`
				LastError string            `json:"lastError"`
				State     string            `json:"state"`
				Labels    map[string]string `json:"labels"`
				Alerts    []json.RawMessage `json:"alerts"`
			} `json:"rules"`
		} `json:"groups"`
	} `json:"data"`
}

func (s *Service) Rules() ([]AlertRule, error) {
	if s.prometheusURL == "" {
		return []AlertRule{}, nil
	}
	request, err := http.NewRequest(http.MethodGet, s.prometheusURL+"/api/v1/rules?type=alert", nil)
	if err != nil {
		return nil, err
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus returned %s", response.Status)
	}
	var payload prometheusRulesResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Status != "success" {
		return nil, fmt.Errorf("prometheus rules query failed")
	}
	rules := make([]AlertRule, 0)
	for _, group := range payload.Data.Groups {
		for _, rule := range group.Rules {
			if rule.Type != "alerting" {
				continue
			}
			rules = append(rules, AlertRule{Group: group.Name, Name: rule.Name, Query: rule.Query, Duration: rule.Duration, Health: rule.Health, LastError: rule.LastError, State: rule.State, Severity: rule.Labels["severity"], FiringCount: len(rule.Alerts)})
		}
	}
	return rules, nil
}
