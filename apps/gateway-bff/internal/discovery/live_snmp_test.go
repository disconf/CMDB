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
	arpCount := 0
	for _, port := range ports {
		macCount += len(port.MACAddresses)
		arpCount += len(port.ARPEntries)
	}
	t.Logf("host=%s os=%q vendor=%s attributes=%d ports=%d learnedMacs=%d arpEntries=%d", host.Name, host.OS, snapshotAttribute(host.Attributes, "vendor"), len(host.Attributes), len(ports), macCount, arpCount)
	if len(ports) == 0 {
		t.Fatalf("SNMP target responded but network_ports attribute was empty")
	}
	g := &gosnmp.GoSNMP{Target: target, Port: 161, Community: community, Version: gosnmp.Version2c, Timeout: 8 * time.Second, Retries: 0}
	if err := g.Connect(); err == nil {
		defer g.Conn.Close()
		for _, oid := range []string{".1.3.6.1.2.1.17.1.4.1.2", ".1.3.6.1.2.1.17.4.3.1.2", ".1.3.6.1.2.1.17.7.1.2.2.1.2", ".1.3.6.1.2.1.4.22.1.2", ".1.3.6.1.2.1.4.35.1.4"} {
			count := 0
			_ = g.BulkWalk(oid, func(pdu gosnmp.SnmpPDU) error {
				count++
				if strings.Contains(oid, ".4.22.") && count <= 3 {
					t.Logf("arp-media name=%s value=%q", pdu.Name, stringify(pdu))
				}
				if strings.Contains(oid, ".4.35.") && count <= 3 {
					t.Logf("arp-physical name=%s value=%q", pdu.Name, stringify(pdu))
				}
				return nil
			})
			t.Logf("oid=%s rows=%d", oid, count)
		}
	}
}

func snapshotAttribute(attributes []cmdb.Attribute, name string) string {
	for _, attribute := range attributes {
		if attribute.Name == name {
			return attribute.Value
		}
	}
	return ""
}
