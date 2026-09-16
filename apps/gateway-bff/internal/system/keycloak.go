package system

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type SyncReport struct {
	Users   int      `json:"users"`
	Created int      `json:"created"`
	Updated int      `json:"updated"`
	Groups  int      `json:"groups"`
	Errors  []string `json:"errors,omitempty"`
}

type keycloakUser struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Enabled   bool   `json:"enabled"`
}

type keycloakGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type keycloakRole struct {
	Name string `json:"name"`
}

func (s *Service) SyncKeycloak(ctx context.Context) (SyncReport, error) {
	baseURL := strings.TrimRight(firstSystemValue(os.Getenv("KEYCLOAK_ADMIN_URL"), "http://keycloak:8080"), "/")
	realm := firstSystemValue(os.Getenv("KEYCLOAK_REALM"), "cmdb")
	adminUsername := firstSystemValue(os.Getenv("KEYCLOAK_ADMIN_USERNAME"), "admin")
	adminPassword := strings.TrimSpace(os.Getenv("KEYCLOAK_ADMIN_PASSWORD"))
	if adminPassword == "" {
		return SyncReport{}, fmt.Errorf("KEYCLOAK_ADMIN_PASSWORD is not configured")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	token, err := keycloakAdminToken(ctx, client, baseURL, adminUsername, adminPassword)
	if err != nil {
		return SyncReport{}, err
	}
	headers := map[string]string{"Authorization": "Bearer " + token}
	var kcUsers []keycloakUser
	if err = keycloakRequest(ctx, client, http.MethodGet, baseURL+"/admin/realms/"+url.PathEscape(realm)+"/users?max=1000", headers, nil, &kcUsers); err != nil {
		return SyncReport{}, err
	}
	var kcGroups []keycloakGroup
	if err = keycloakRequest(ctx, client, http.MethodGet, baseURL+"/admin/realms/"+url.PathEscape(realm)+"/groups?max=1000", headers, nil, &kcGroups); err != nil {
		return SyncReport{}, err
	}
	existing := map[string]User{}
	for _, user := range s.Users() {
		existing[user.Username] = user
	}
	report := SyncReport{Users: len(kcUsers), Groups: len(kcGroups)}
	for _, kcUser := range kcUsers {
		var groups []keycloakGroup
		if err = keycloakRequest(ctx, client, http.MethodGet, baseURL+"/admin/realms/"+url.PathEscape(realm)+"/users/"+url.PathEscape(kcUser.ID)+"/groups", headers, nil, &groups); err != nil {
			report.Errors = append(report.Errors, kcUser.Username+": "+err.Error())
			continue
		}
		var roles []keycloakRole
		if err = keycloakRequest(ctx, client, http.MethodGet, baseURL+"/admin/realms/"+url.PathEscape(realm)+"/users/"+url.PathEscape(kcUser.ID)+"/role-mappings/realm", headers, nil, &roles); err != nil {
			report.Errors = append(report.Errors, kcUser.Username+": "+err.Error())
			continue
		}
		item, found := existing[kcUser.Username]
		if !found {
			item = User{ID: "kc-" + kcUser.ID, Username: kcUser.Username, DataScope: "groups"}
			report.Created++
		} else {
			report.Updated++
		}
		item.KeycloakID = kcUser.ID
		item.Email = kcUser.Email
		item.DisplayName = strings.TrimSpace(kcUser.FirstName + " " + kcUser.LastName)
		if item.DisplayName == "" {
			item.DisplayName = kcUser.Username
		}
		item.Status = "disabled"
		if kcUser.Enabled {
			item.Status = "active"
		}
		item.KeycloakGroups = item.KeycloakGroups[:0]
		for _, group := range groups {
			item.KeycloakGroups = mergeValues(item.KeycloakGroups, []string{firstSystemValue(group.Name, group.Path)})
		}
		for _, role := range roles {
			if role.Name == "" || role.Name == "default-roles-cmdb" || role.Name == "offline_access" || role.Name == "uma_authorization" {
				continue
			}
			item.Roles = mergeValues(item.Roles, []string{role.Name})
			if role.Name == "platform-admin" {
				item.DataScope = "all"
			}
		}
		if item.DataScope == "" {
			if len(item.ProjectGroups) > 0 || len(item.KeycloakGroups) > 0 {
				item.DataScope = "groups"
			} else {
				item.DataScope = "none"
			}
		}
		item.SyncedAt = time.Now().Format("2006-01-02 15:04:05")
		if err = upsertSystemUser(ctx, s.db, item); err != nil {
			report.Errors = append(report.Errors, kcUser.Username+": "+err.Error())
			continue
		}
	}
	if err = s.reload(ctx); err != nil {
		return report, err
	}
	return report, nil
}

func keycloakAdminToken(ctx context.Context, client *http.Client, baseURL, username, password string) (string, error) {
	values := url.Values{"grant_type": {"password"}, "client_id": {"admin-cli"}, "username": {username}, "password": {password}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/realms/master/protocol/openid-connect/token", strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("keycloak admin token: %s %s", response.Status, string(body))
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", err
	}
	return payload.AccessToken, nil
}

func keycloakRequest(ctx context.Context, client *http.Client, method, endpoint string, headers map[string]string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("keycloak %s: %s %s", method, response.Status, string(detail))
	}
	if target != nil {
		return json.NewDecoder(response.Body).Decode(target)
	}
	return nil
}

func firstSystemValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
