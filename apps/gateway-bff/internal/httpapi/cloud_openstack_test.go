package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenStackURLParsing(t *testing.T) {
	authURL, region, project, userDomain, projectDomain := parseOpenStackURL("https://keystone.example/v3#project=ops&project_domain=Default&region=RegionOne&user_domain=Default")
	if authURL != "https://keystone.example/v3" || region != "RegionOne" || project != "ops" || userDomain != "Default" || projectDomain != "Default" {
		t.Fatalf("unexpected parsed OpenStack URL: %q %q %q %q %q", authURL, region, project, userDomain, projectDomain)
	}
	if got := openStackTokenURL("https://keystone.example"); got != "https://keystone.example/v3/auth/tokens" {
		t.Fatalf("unexpected token URL %q", got)
	}
}

func TestOpenStackClientAuthenticatesAndPaginates(t *testing.T) {
	var serverURL string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/auth/tokens":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected auth method %s", r.Method)
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			auth, _ := payload["auth"].(map[string]any)
			if auth == nil {
				t.Fatal("missing auth payload")
			}
			w.Header().Set("X-Subject-Token", "token-1")
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"token":{"catalog":[{"type":"compute","endpoints":[{"interface":"public","region":"RegionOne","url":"%s/compute/v2.1"}]}]}}`, serverURL)
		case "/compute/v2.1/servers/detail":
			if r.Header.Get("X-Auth-Token") != "token-1" {
				t.Fatalf("missing auth token: %q", r.Header.Get("X-Auth-Token"))
			}
			if r.URL.Query().Get("limit") != "100" {
				t.Fatalf("unexpected limit: %q", r.URL.Query().Get("limit"))
			}
			if r.URL.Query().Get("marker") == "" {
				fmt.Fprintf(w, `{"servers":[{"id":"server-1","name":"app-1","status":"ACTIVE","flavor":{"id":"flavor-1"},"image":{"id":"image-1"},"addresses":{"private":[{"addr":"10.0.0.10","OS-EXT-IPS:type":"fixed","version":4}],"public":[{"addr":"203.0.113.10","OS-EXT-IPS:type":"floating","version":4}]},"OS-EXT-AZ:availability_zone":"nova"}],"servers_links":[{"rel":"next","href":"%s/compute/v2.1/servers/detail?limit=100&marker=server-1"}]}`, serverURL)
			} else {
				fmt.Fprint(w, `{"servers":[]}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL
	client, err := buildOpenStackClient(server.URL, "RegionOne", "ops", "Default", "Default", "admin", "secret", true)
	if err != nil {
		t.Fatal(err)
	}
	instances, err := client.Instances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].InstanceID != "server-1" || instances[0].PublicIP != "203.0.113.10" || instances[0].PrivateIP != "10.0.0.10" {
		t.Fatalf("unexpected OpenStack inventory: %#v", instances)
	}
	if !strings.Contains(instances[0].Name, "app-1") {
		t.Fatalf("unexpected server name: %#v", instances[0])
	}
}
