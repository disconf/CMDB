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

func TestVendorFDBProfiles(t *testing.T) {
	huawei := vendorFDBProfiles("Huawei")
	if len(huawei) != 2 || huawei[0].OID != oidHuaweiDynFdbPort || huawei[0].Style != fdbStyleMACFirst {
		t.Fatalf("unexpected Huawei profiles: %#v", huawei)
	}
	h3c := vendorFDBProfiles("H3C")
	if len(h3c) != 1 || h3c[0].OID != oidH3CFdbSearchPort {
		t.Fatalf("unexpected H3C profiles: %#v", h3c)
	}
	if profiles := vendorFDBProfiles("Cisco"); len(profiles) != 0 {
		t.Fatalf("Cisco should use standard bridge MIB: %#v", profiles)
	}
}

func TestParseFDBIndexMACFirst(t *testing.T) {
	mac, vlan, ok := parseFDBIndexWithStyle("0.17.34.51.68.85.100.7.86.83.73.110.97.109.101", fdbStyleMACFirst)
	if !ok || mac != "00:11:22:33:44:55" || vlan != "100" {
		t.Fatalf("unexpected mac-first index: %q %q %t", mac, vlan, ok)
	}
}

func TestParseARPIndex(t *testing.T) {
	ifIndex, ip, ok := parseARPIndex("7.172.28.68.10")
	if !ok || ifIndex != "7" || ip != "172.28.68.10" {
		t.Fatalf("unexpected ARP index: %q %q %t", ifIndex, ip, ok)
	}
	if _, _, ok := parseARPIndex("7.0.0.0.0"); ok {
		t.Fatal("zero IP must be ignored")
	}
}
