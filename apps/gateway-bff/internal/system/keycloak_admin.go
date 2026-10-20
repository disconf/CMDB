package system

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type keycloakAdmin struct {
	client  *http.Client
	baseURL string
	realm   string
	headers map[string]string
}

func newKeycloakAdmin(ctx context.Context) (*keycloakAdmin, error) {
	baseURL := strings.TrimRight(firstSystemValue(os.Getenv("KEYCLOAK_ADMIN_URL"), "http://keycloak:8080"), "/")
	realm := firstSystemValue(os.Getenv("KEYCLOAK_REALM"), "cmdb")
	username := firstSystemValue(os.Getenv("KEYCLOAK_ADMIN_USERNAME"), "admin")
	password := strings.TrimSpace(os.Getenv("KEYCLOAK_ADMIN_PASSWORD"))
	if password == "" {
		return nil, ErrKeycloakNotConfigured
	}
	client := &http.Client{Timeout: 20 * time.Second}
	token, err := keycloakAdminToken(ctx, client, baseURL, username, password)
	if err != nil {
		return nil, err
	}
	return &keycloakAdmin{
		client: client, baseURL: baseURL, realm: realm,
		headers: map[string]string{"Authorization": "Bearer " + token},
	}, nil
}

func (a *keycloakAdmin) usersPath() string {
	return a.baseURL + "/admin/realms/" + url.PathEscape(a.realm) + "/users"
}

func (a *keycloakAdmin) userPath(id string) string {
	return a.usersPath() + "/" + url.PathEscape(id)
}

func (a *keycloakAdmin) CreateUser(ctx context.Context, input UserCreateInput, password string) (string, error) {
	payload := map[string]any{
		"username":      input.Username,
		"email":         input.Email,
		"firstName":     input.DisplayName,
		"lastName":      "",
		"enabled":       true,
		"emailVerified": false,
		"credentials": []map[string]any{{
			"type": "password", "value": password, "temporary": true,
		}},
		"requiredActions": []string{"UPDATE_PASSWORD"},
	}
	if err := keycloakRequest(ctx, a.client, http.MethodPost, a.usersPath(), a.headers, payload, nil); err != nil {
		return "", err
	}
	var users []keycloakUser
	endpoint := a.usersPath() + "?username=" + url.QueryEscape(input.Username) + "&exact=true"
	if err := keycloakRequest(ctx, a.client, http.MethodGet, endpoint, a.headers, nil, &users); err != nil {
		return "", err
	}
	if len(users) == 0 || strings.TrimSpace(users[0].ID) == "" {
		return "", fmt.Errorf("keycloak created user %s but did not return the user id", input.Username)
	}
	return users[0].ID, nil
}

func (a *keycloakAdmin) SetEnabled(ctx context.Context, id string, enabled bool) error {
	return keycloakRequest(ctx, a.client, http.MethodPut, a.userPath(id), a.headers, map[string]any{"enabled": enabled}, nil)
}

func (a *keycloakAdmin) UpdateProfile(ctx context.Context, id, displayName, email string) error {
	return keycloakRequest(ctx, a.client, http.MethodPut, a.userPath(id), a.headers, map[string]any{
		"firstName": displayName,
		"lastName":  "",
		"email":     email,
	}, nil)
}

func (a *keycloakAdmin) ResetPassword(ctx context.Context, id, password string) error {
	return keycloakRequest(ctx, a.client, http.MethodPut, a.userPath(id)+"/reset-password", a.headers, map[string]any{
		"type": "password", "value": password, "temporary": true,
	}, nil)
}

func (a *keycloakAdmin) Logout(ctx context.Context, id string) error {
	return keycloakRequest(ctx, a.client, http.MethodPost, a.userPath(id)+"/logout", a.headers, nil, nil)
}
