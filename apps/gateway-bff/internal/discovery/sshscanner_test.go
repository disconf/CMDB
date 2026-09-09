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
