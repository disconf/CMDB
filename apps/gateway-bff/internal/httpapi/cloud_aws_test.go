package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAWSEndpointDefaultsByRegion(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		region   string
		want     string
	}{
		{name: "default region", region: "us-east-1", want: "ec2.us-east-1.amazonaws.com"},
		{name: "global endpoint for another region", endpoint: "ec2.amazonaws.com", region: "ap-southeast-1", want: "ec2.ap-southeast-1.amazonaws.com"},
		{name: "china region", endpoint: "https://ec2.cn-north-1.amazonaws.com.cn/", region: "cn-north-1", want: "ec2.cn-north-1.amazonaws.com.cn"},
		{name: "explicit endpoint", endpoint: "ec2.local.example", region: "us-east-1", want: "ec2.local.example"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeAWSEndpoint(test.endpoint, test.region); got != test.want {
				t.Fatalf("normalizeAWSEndpoint(%q, %q) = %q, want %q", test.endpoint, test.region, got, test.want)
			}
		})
	}
}

func TestAWSClientInventoryPaginatesAndParses(t *testing.T) {
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if authorization := r.Header.Get("Authorization"); !strings.HasPrefix(authorization, "AWS4-HMAC-SHA256 Credential=AKID/") {
			t.Fatalf("unexpected authorization header: %q", authorization)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		if values.Get("Action") != "DescribeInstances" || values.Get("Version") != "2016-11-15" {
			t.Fatalf("unexpected payload: %s", body)
		}
		w.Header().Set("Content-Type", "text/xml")
		switch calls {
		case 1:
			if values.Get("NextToken") != "" {
				t.Fatalf("first page must not include NextToken: %s", body)
			}
			fmt.Fprint(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>i-001</instanceId><instanceType>m7i.large</instanceType><instanceState><name>running</name></instanceState><privateIpAddress>10.0.0.10</privateIpAddress><ipAddress>203.0.113.10</ipAddress><placement><availabilityZone>us-east-1a</availabilityZone></placement><tagSet><item><key>Name</key><value>web-1</value></item></tagSet></item></instancesSet></item></reservationSet><nextToken>page-2</nextToken></DescribeInstancesResponse>`)
		case 2:
			if values.Get("NextToken") != "page-2" {
				t.Fatalf("second page token = %q, payload: %s", values.Get("NextToken"), body)
			}
			fmt.Fprint(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet><item><instanceId>i-002</instanceId><instanceType>t3.small</instanceType><instanceState><name>stopped</name></instanceState><privateIpAddress>10.0.0.20</privateIpAddress><placement><availabilityZone>us-east-1b</availabilityZone></placement><platform>windows</platform><tagSet><item><key>Name</key><value>db-1</value></item></tagSet></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`)
		default:
			t.Fatalf("unexpected request %d", calls)
		}
	}))
	defer server.Close()

	client, err := buildAWSClient(server.URL, "us-east-1", "AKID", "SECRET", true)
	if err != nil {
		t.Fatal(err)
	}
	instances, err := client.Instances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 API calls, got %d", calls)
	}
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %#v", instances)
	}
	if instances[0].InstanceID != "i-001" || instances[0].Name != "web-1" || instances[0].Status != "running" || instances[0].RegionID != "us-east-1" {
		t.Fatalf("unexpected first instance: %#v", instances[0])
	}
	if instances[1].OSName != "Windows" || instances[1].PublicIP != "" || instances[1].PrivateIP != "10.0.0.20" {
		t.Fatalf("unexpected second instance: %#v", instances[1])
	}
}
