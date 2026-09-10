package discovery

import "testing"

func TestParseSSHInventory(t *testing.T) {
	out := `my-host
"Rocky Linux 9.4"
5.14.0-362
x86_64
16
33554432
107374182400
1718726400
`
	h := parseSSHInventory("10.0.0.9", out)
	if h == nil {
		t.Fatal("nil host")
	}
	if h.Name != "my-host" || h.OS != "Rocky Linux 9.4" || h.CPUCount != 16 {
		t.Fatalf("bad parse: %+v", h)
	}
	if h.MemoryBytes != 33554432*1024 {
		t.Fatalf("mem=%d", h.MemoryBytes)
	}
	if h.DiskBytes != 107374182400 {
		t.Fatalf("disk=%d", h.DiskBytes)
	}
}

func TestParseSSHInventoryVirtualization(t *testing.T) {
	out := "vm-host\n\"Ubuntu 22.04\"\n5.15.0\nx86_64\n4\n8388608\n10737418240\n1718726400\nvmware"
	h := parseSSHInventory("10.0.0.10", out)
	if h == nil || !h.Virtual {
		t.Fatalf("expected virtual host: %+v", h)
	}
	found := false
	for _, item := range h.Attributes {
		if item.Name == "virtualization_type" && item.Value == "vmware" {
			found = true
		}
	}
	if !found {
		t.Fatalf("virtualization attribute missing: %+v", h.Attributes)
	}
}
