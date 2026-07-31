package monitor

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
)

type RoutingRule struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MatcherName  string `json:"matcherName"`
	MatcherValue string `json:"matcherValue"`
	Team         string `json:"team"`
	Owner        string `json:"owner"`
	ChannelID    string `json:"channelId"`
	Enabled      bool   `json:"enabled"`
}
type CreateRoutingRuleInput struct {
	Name         string `json:"name"`
	MatcherName  string `json:"matcherName"`
	MatcherValue string `json:"matcherValue"`
	Team         string `json:"team"`
	Owner        string `json:"owner"`
	ChannelID    string `json:"channelId"`
}

func routeID() string {
	data := make([]byte, 8)
	_, _ = rand.Read(data)
	return "route-" + hex.EncodeToString(data)
}
func validateRoute(input CreateRoutingRuleInput) error {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.MatcherName) == "" || strings.TrimSpace(input.MatcherValue) == "" || strings.TrimSpace(input.Team) == "" || strings.TrimSpace(input.Owner) == "" {
		return fmt.Errorf("all routing fields are required")
	}
	return nil
}
func (s *Service) RoutingRules() []RoutingRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]RoutingRule(nil), s.routes...)
}
func (s *Service) routeTargetLocked(labels map[string]string) (string, string) {
	for _, route := range s.routes {
		if route.Enabled && labels[route.MatcherName] == route.MatcherValue {
			return route.Owner, route.ChannelID
		}
	}
	return "", ""
}
func (s *Service) CreateRoutingRule(input CreateRoutingRuleInput) (RoutingRule, error) {
	if err := validateRoute(input); err != nil {
		return RoutingRule{}, err
	}
	rule := RoutingRule{ID: routeID(), Name: strings.TrimSpace(input.Name), MatcherName: strings.TrimSpace(input.MatcherName), MatcherValue: strings.TrimSpace(input.MatcherValue), Team: strings.TrimSpace(input.Team), Owner: strings.TrimSpace(input.Owner), ChannelID: strings.TrimSpace(input.ChannelID), Enabled: true}
	s.mu.Lock()
	defer s.mu.Unlock()
	if rule.ChannelID != "" {
		found := false
		for _, channel := range s.channels {
			if channel.ID == rule.ChannelID {
				found = true
				break
			}
		}
		if !found {
			return RoutingRule{}, fmt.Errorf("notification channel not found")
		}
	}
	if s.db != nil {
		if _, err := s.db.Exec(`INSERT INTO monitor_routing_rules(id,name,matcher_name,matcher_value,team,owner,channel_id,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, rule.ID, rule.Name, rule.MatcherName, rule.MatcherValue, rule.Team, rule.Owner, rule.ChannelID, rule.Enabled); err != nil {
			return RoutingRule{}, err
		}
	}
	s.routes = append(s.routes, rule)
	return rule, nil
}
func (s *Service) ToggleRoutingRule(id string) (RoutingRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.routes {
		if s.routes[i].ID == id {
			s.routes[i].Enabled = !s.routes[i].Enabled
			if s.db != nil {
				if _, err := s.db.Exec(`UPDATE monitor_routing_rules SET enabled=$2,updated_at=now() WHERE id=$1`, id, s.routes[i].Enabled); err != nil {
					return RoutingRule{}, err
				}
			}
			return s.routes[i], nil
		}
	}
	return RoutingRule{}, ErrNotFound
}
func (s *Service) DeleteRoutingRule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.routes {
		if s.routes[i].ID == id {
			if s.db != nil {
				if _, err := s.db.Exec(`DELETE FROM monitor_routing_rules WHERE id=$1`, id); err != nil {
					return err
				}
			}
			s.routes = append(s.routes[:i], s.routes[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
func loadRoutingRules(ctx context.Context, db *sql.DB) ([]RoutingRule, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,matcher_name,matcher_value,team,owner,channel_id,enabled FROM monitor_routing_rules ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RoutingRule{}
	for rows.Next() {
		var rule RoutingRule
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.MatcherName, &rule.MatcherValue, &rule.Team, &rule.Owner, &rule.ChannelID, &rule.Enabled); err != nil {
			return nil, err
		}
		result = append(result, rule)
	}
	return result, rows.Err()
}
