package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHuaweiEndpointAndURLParsing(t *testing.T) {
	if got := normalizeHuaweiEndpoint("", "cn-north-4"); got != "ecs.cn-north-4.myhuaweicloud.com" {
		t.Fatalf("unexpected default endpoint %q", got)
	}
	endpoint, region, projectID := parseHuaweiURL("ecs.cn-north-4.myhuaweicloud.com#project=project-1&region=cn-north-4")
	if endpoint != "ecs.cn-north-4.myhuaweicloud.com" || region != "cn-north-4" || projectID != "project-1" {
		t.Fatalf("unexpected parsed config: %q %q %q", endpoint, region, projectID)
	}
}

func TestHuaweiClientInventoryPaginatesAndParses(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/v2.1/project-1/servers/detail" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Sdk-Date") == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "SDK-HMAC-SHA256 Access=AKID, SignedHeaders=host;x-sdk-date, Signature=") {
			t.Fatalf("unexpected signature headers: %#v", r.Header)
		}
		offset := r.URL.Query().Get("offset")
		limit := r.URL.Query().Get("limit")
		if limit != "100" {
			t.Fatalf("unexpected limit %q", limit)
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			if offset != "0" {
				t.Fatalf("unexpected first offset %q", offset)
			}
			fmt.Fprint(w, `{"servers":[{"id":"server-1","name":"web-1","status":"ACTIVE","flavor":{"id":"flavor-1","name":"s6.large.2"},"addresses":{"network":[{"addr":"10.0.0.10","OS-EXT-IPS:type":"fixed","version":4},{"addr":"203.0.113.10","OS-EXT-IPS:type":"floating","version":4}]},"metadata":{"os_type":"Linux"},"OS-EXT-AZ:availability_zone":"cn-north-4a"}]}`)
		case 2:
			if offset != "100" {
				t.Fatalf("unexpected second offset %q", offset)
			}
			fmt.Fprint(w, `{"servers":[]}`)
		default:
			t.Fatalf("unexpected call %d", calls)
		}
	}))
	defer server.Close()

	client, err := buildHuaweiClient(server.URL, "cn-north-4", "project-1", "AKID", "SECRET", true)
	if err != nil {
		t.Fatal(err)
	}
	instances, err := client.Instances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(instances) != 1 {
		t.Fatalf("unexpected inventory calls=%d instances=%#v", calls, instances)
	}
	if instances[0].InstanceID != "server-1" || instances[0].Name != "web-1" || instances[0].PublicIP != "203.0.113.10" || instances[0].PrivateIP != "10.0.0.10" || instances[0].OSName != "Linux" {
		t.Fatalf("unexpected instance: %#v", instances[0])
	}
}
