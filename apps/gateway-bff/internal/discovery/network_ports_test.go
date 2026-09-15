package discovery

import "testing"

func TestParseFDBIndex(t *testing.T) {
	mac, vlan, ok := parseFDBIndex("0.17.34.51.68.85")
	if !ok || mac != "00:11:22:33:44:55" || vlan != "" {
		t.Fatalf("unexpected bridge index: %q %q %t", mac, vlan, ok)
	}
	mac, vlan, ok = parseFDBIndex("100.0.17.34.51.68.85")
	if !ok || mac != "00:11:22:33:44:55" || vlan != "100" {
		t.Fatalf("unexpected q-bridge index: %q %q %t", mac, vlan, ok)
	}
	if _, _, ok := parseFDBIndex("0.1.0.0.0.0.0"); ok {
		t.Fatal("multicast MAC must be ignored")
	}
	if _, _, ok := parseFDBIndex("0.255.255.255.255.255.255"); ok {
		t.Fatal("broadcast MAC must be ignored")
	}
}
