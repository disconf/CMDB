//go:build integration

package discovery

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

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
	macCount := 0
	for _, port := range ports {
		macCount += len(port.MACAddresses)
	}
	t.Logf("host=%s attributes=%d ports=%d learnedMacs=%d", host.Name, len(host.Attributes), len(ports), macCount)
	if len(ports) == 0 {
		t.Fatalf("SNMP target responded but network_ports attribute was empty")
	}
	g := &gosnmp.GoSNMP{Target: target, Port: 161, Community: community, Version: gosnmp.Version2c, Timeout: 8 * time.Second, Retries: 0}
	if err := g.Connect(); err == nil {
		defer g.Conn.Close()
		for _, oid := range []string{".1.3.6.1.2.1.17.1.4.1.2", ".1.3.6.1.2.1.17.4.3.1.2", ".1.3.6.1.2.1.17.7.1.2.2.1.2"} {
			count := 0
			_ = g.BulkWalk(oid, func(gosnmp.SnmpPDU) error { count++; return nil })
			t.Logf("oid=%s rows=%d", oid, count)
		}
	}
}
