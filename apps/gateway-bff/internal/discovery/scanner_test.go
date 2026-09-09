package discovery

import "testing"

const sampleNodeExporter = `# HELP node_uname_info Node uname information
node_uname_info{domainname="(none)",machine="x86_64",nodename="test-host-01",release="5.15.0-86-generic",sysname="Linux"} 1
node_os_info{id="ubuntu",name="Ubuntu",pretty_name="Ubuntu 22.04.3 LTS",version_id="22.04"} 1
node_memory_MemTotal_bytes 16769171456
node_cpu_seconds_total{cpu="0",mode="idle"} 12345
node_cpu_seconds_total{cpu="1",mode="idle"} 12345
node_boot_time_seconds 1722768762
node_filesystem_size_bytes{device="/dev/sda2",fstype="ext4",mountpoint="/"} 102503628800
`

func TestParseNodeExporter(t *testing.T) {
	h := parseNodeExporter("10.0.0.5", sampleNodeExporter)
	if h.Name != "test-host-01" {
		t.Fatalf("name=%q", h.Name)
	}
	if h.OS != "Ubuntu 22.04.3 LTS" {
		t.Fatalf("os=%q", h.OS)
	}
	if h.CPUCount != 2 {
		t.Fatalf("cpu=%d", h.CPUCount)
	}
	if h.MemoryBytes != 16769171456 {
		t.Fatalf("mem=%d", h.MemoryBytes)
	}
	if h.DiskBytes != 102503628800 {
		t.Fatalf("disk=%d", h.DiskBytes)
	}
	if len(h.Attributes) < 6 {
		t.Fatalf("attributes too few")
	}
}
