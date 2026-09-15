//go:build integration

package discovery

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

func TestLiveSNMPInventory(t *testing.T) {
	target := strings.TrimSpace(os.Getenv("CMDB_TEST_SNMP_TARGET"))
	community := strings.TrimSpace(os.Getenv("CMDB_TEST_SNMP_COMMUNITY"))
	if target == "" || community == "" {
		t.Skip("CMDB_TEST_SNMP_TARGET/CMDB_TEST_SNMP_COMMUNITY not configured")
	}
	host := probeSNMP(target, 161, community, 8*time.Second)
	if host == nil {
		t.Fatalf("SNMP target %s did not respond", target)
	}
	var ports []cmdb.NetworkPort
	for _, attribute := range host.Attributes {
		if attribute.Name == "network_ports" && strings.TrimSpace(attribute.Value) != "" {
			_ = json.Unmarshal([]byte(attribute.Value), &ports)
		}
	}
	t.Logf("host=%s attributes=%d ports=%d", host.Name, len(host.Attributes), len(ports))
	if len(ports) == 0 {
		t.Fatalf("SNMP target responded but network_ports attribute was empty")
	}
}
