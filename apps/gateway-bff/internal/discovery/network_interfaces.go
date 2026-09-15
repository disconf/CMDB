package discovery

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

const sshNetworkMarker = "__CMDB_NETWORK_STATE__"
const sshNeighborMarker = "__CMDB_NEIGHBOR_STATE__"

var (
	ipNeighborPattern = regexp.MustCompile(`^([0-9.]+)\s+dev\s+(\S+)\s+lladdr\s+([0-9A-Fa-f:]+)\s+(\S+)`)
	ipLinkLinePattern = regexp.MustCompile(`^\d+:\s+([^:@]+)(?:@[^:]+)?:\s+<([^>]*)>.*\bstate\s+(\S+).*\blink/(\S+)\s+([0-9A-Fa-f:]+)`)
	ipv4LinePattern   = regexp.MustCompile(`^\d+:\s+(\S+)\s+inet\s+([0-9.]+/\d+)`)
	mtuPattern        = regexp.MustCompile(`\bmtu\s+(\d+)`)
)

func parseSSHNetworkInterfaces(ip, output string) []cmdb.NetworkInterface {
	_, networkOutput, ok := strings.Cut(output, sshNetworkMarker)
	if !ok {
		return []cmdb.NetworkInterface{}
	}
	byName := map[string]*cmdb.NetworkInterface{}
	for _, line := range strings.Split(networkOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if match := ipLinkLinePattern.FindStringSubmatch(line); len(match) == 6 {
			name := strings.TrimSpace(match[1])
			if name == "" || name == "lo" {
				continue
			}
			status := strings.ToLower(strings.TrimSpace(match[3]))
			item := byName[name]
			if item == nil {
				item = &cmdb.NetworkInterface{Name: name}
				byName[name] = item
			}
			item.MACAddress = strings.ToUpper(match[5])
			item.AdminStatus = status
			item.OperStatus = status
			mtu := mtuPattern.FindStringSubmatch(line)
			if len(mtu) == 2 {
				item.MTU = mtu[1]
			}
			flags := strings.ToLower(match[2])
			switch {
			case strings.Contains(flags, "loopback"):
				item.Type = "loopback"
			case strings.HasPrefix(name, "bond"):
				item.Type = "bond"
			case strings.HasPrefix(name, "br"):
				item.Type = "bridge"
			case strings.HasPrefix(name, "vlan"):
				item.Type = "vlan"
			default:
				item.Type = "ethernet"
			}
			continue
		}
		if match := ipNeighborPattern.FindStringSubmatch(line); len(match) == 5 {
			item := byName[strings.TrimSpace(match[2])]
			if item != nil {
				item.Neighbors = append(item.Neighbors, cmdb.NetworkARPEntry{IP: match[1], MACAddress: strings.ToUpper(match[3]), State: match[4], LastSeenAt: time.Now().Format("2006-01-02 15:04:05")})
			}
			continue
		}
		if match := ipv4LinePattern.FindStringSubmatch(line); len(match) == 3 {
			name := strings.TrimSpace(match[1])
			if name == "" || name == "lo" {
				continue
			}
			item := byName[name]
			if item == nil {
				item = &cmdb.NetworkInterface{Name: name, AdminStatus: "unknown", OperStatus: "unknown", Type: "ethernet"}
				byName[name] = item
			}
			if item.IPv4 == "" {
				item.IPv4 = match[2]
			} else {
				item.IPv4 += "," + match[2]
			}
		}
	}
	result := make([]cmdb.NetworkInterface, 0, len(byName))
	now := time.Now().Format("2006-01-02 15:04:05")
	for _, item := range byName {
		if item.AdminStatus == "" {
			item.AdminStatus = "unknown"
		}
		if item.OperStatus == "" {
			item.OperStatus = "unknown"
		}
		item.LastSeenAt = now
		item.ID = "ssh-" + strings.ReplaceAll(ip, ".", "-") + ":if:" + item.Name
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
