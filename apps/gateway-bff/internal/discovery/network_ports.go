package discovery

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"

	"cmdb/gateway-bff/internal/cmdb"
)

const (
	oidIfDescr          = ".1.3.6.1.2.1.2.2.1.2"
	oidIfPhysAddress    = ".1.3.6.1.2.1.2.2.1.6"
	oidIfAdminStatus    = ".1.3.6.1.2.1.2.2.1.7"
	oidIfOperStatus     = ".1.3.6.1.2.1.2.2.1.8"
	oidIfSpeed          = ".1.3.6.1.2.1.2.2.1.5"
	oidIfHighSpeed      = ".1.3.6.1.2.1.31.1.1.1.15"
	oidIfName           = ".1.3.6.1.2.1.31.1.1.1.1"
	oidIfAlias          = ".1.3.6.1.2.1.31.1.1.1.18"
	oidDot1qPvid        = ".1.3.6.1.2.1.17.7.1.4.5.1.1"
	oidDot1dPortIfIndex = ".1.3.6.1.2.1.17.1.4.1.2"
	oidDot1dTpFdbPort   = ".1.3.6.1.2.1.17.4.3.1.2"
	oidDot1qTpFdbPort   = ".1.3.6.1.2.1.17.7.1.2.2.1.2"
)

func collectNetworkPorts(g *gosnmp.GoSNMP, neighbors []LLDPNeighbor, limit int) []cmdb.NetworkPort {
	rows := map[string]*cmdb.NetworkPort{}
	get := func(index string) *cmdb.NetworkPort {
		row := rows[index]
		if row == nil {
			row = &cmdb.NetworkPort{IfIndex: index, AdminStatus: "unknown", OperStatus: "unknown"}
			rows[index] = row
		}
		return row
	}
	walkNetworkColumn(g, oidIfDescr, limit, func(index, value string) { get(index).Description = value })
	walkNetworkColumn(g, oidIfName, limit, func(index, value string) { get(index).Name = value })
	walkNetworkColumn(g, oidIfAlias, limit, func(index, value string) { get(index).Alias = value })
	walkNetworkColumn(g, oidIfAdminStatus, limit, func(index, value string) { get(index).AdminStatus = adminStatus(value) })
	walkNetworkColumn(g, oidIfOperStatus, limit, func(index, value string) { get(index).OperStatus = operStatus(value) })
	walkNetworkColumn(g, oidIfHighSpeed, limit, func(index, value string) { get(index).SpeedMbps = value })
	walkNetworkColumn(g, oidIfSpeed, limit, func(index, value string) {
		row := get(index)
		if row.SpeedMbps == "" {
			if speed, err := strconv.ParseUint(value, 10, 64); err == nil && speed > 0 {
				row.SpeedMbps = strconv.FormatUint(speed/1000000, 10)
			}
		}
	})
	walkNetworkColumn(g, oidIfPhysAddress, limit, func(index, value string) { get(index).MACAddress = normalizeMAC(value) })
	walkNetworkColumn(g, oidDot1qPvid, limit, func(index, value string) { get(index).VLAN = value })

	for _, neighbor := range neighbors {
		port := rows[neighbor.LocalPort]
		if port == nil {
			for _, candidate := range rows {
				if strings.EqualFold(candidate.Name, neighbor.LocalPort) || strings.EqualFold(candidate.Description, neighbor.LocalPort) {
					port = candidate
					break
				}
			}
		}
		if port == nil {
			port = get(neighbor.LocalPort)
			port.Name = neighbor.LocalPort
		}
		port.NeighborName = neighbor.RemoteSysName
		port.NeighborPort = firstNetworkValue(neighbor.RemotePortDesc, neighbor.RemotePortID)
		port.NeighborChassis = neighbor.RemoteChassis
	}

	ports := make([]cmdb.NetworkPort, 0, len(rows))
	for _, port := range rows {
		if port.Name == "" {
			port.Name = firstNetworkValue(port.Description, "if-"+port.IfIndex)
		}
		port.LastSeenAt = time.Now().Format("2006-01-02 15:04:05")
		ports = append(ports, *port)
	}
	sort.Slice(ports, func(i, j int) bool {
		left, leftErr := strconv.Atoi(ports[i].IfIndex)
		right, rightErr := strconv.Atoi(ports[j].IfIndex)
		if leftErr == nil && rightErr == nil && left != right {
			return left < right
		}
		return ports[i].IfIndex < ports[j].IfIndex
	})
	if limit > 0 && len(ports) > limit {
		ports = ports[:limit]
	}
	return ports
}

func walkNetworkColumn(g *gosnmp.GoSNMP, base string, limit int, apply func(index, value string)) {
	count := 0
	_ = g.BulkWalk(base, func(pdu gosnmp.SnmpPDU) error {
		count++
		if count > limit {
			return fmt.Errorf("network port limit reached")
		}
		index := oidSuffix(pdu.Name, base)
		if index == "" {
			return nil
		}
		apply(index, stringify(pdu))
		return nil
	})
}

func oidSuffix(name, base string) string {
	name = strings.TrimPrefix(name, ".")
	base = strings.TrimPrefix(base, ".")
	if !strings.HasPrefix(name, base) {
		return ""
	}
	return strings.TrimPrefix(strings.TrimPrefix(name, base), ".")
}

func adminStatus(value string) string {
	switch value {
	case "1":
		return "up"
	case "2":
		return "down"
	case "3":
		return "testing"
	default:
		return "unknown"
	}
}

func operStatus(value string) string {
	switch value {
	case "1":
		return "up"
	case "2":
		return "down"
	case "3":
		return "testing"
	case "4":
		return "unknown"
	case "5":
		return "dormant"
	case "6":
		return "not-present"
	case "7":
		return "lower-layer-down"
	default:
		return "unknown"
	}
}

func collectFDBMACs(g *gosnmp.GoSNMP, rows map[string]*cmdb.NetworkPort, limit int) {
	if limit < 128 {
		limit = 128
	}
	portIfIndex := map[string]string{}
	walkNetworkColumn(g, oidDot1dPortIfIndex, limit, func(index, value string) {
		portIfIndex[index] = value
	})
	byInterface := map[string]map[string]bool{}
	add := func(ifIndex, mac, vlan string) {
		if ifIndex == "" || mac == "" {
			return
		}
		port := rows[ifIndex]
		if port == nil {
			return
		}
		if byInterface[ifIndex] == nil {
			byInterface[ifIndex] = map[string]bool{}
		}
		if byInterface[ifIndex][mac] {
			return
		}
		byInterface[ifIndex][mac] = true
		if len(port.MACAddresses) >= 128 {
			return
		}
		port.MACAddresses = append(port.MACAddresses, mac)
		if port.VLAN == "" && vlan != "" {
			port.VLAN = vlan
		}
	}
	walkFDB(g, oidDot1dTpFdbPort, limit, func(suffix, bridgePort string) {
		mac, vlan, ok := parseFDBIndex(suffix)
		if !ok {
			return
		}
		ifIndex := portIfIndex[bridgePort]
		if ifIndex == "" {
			ifIndex = bridgePort
		}
		add(ifIndex, mac, vlan)
	})
	walkFDB(g, oidDot1qTpFdbPort, limit, func(suffix, bridgePort string) {
		mac, vlan, ok := parseFDBIndex(suffix)
		if !ok {
			return
		}
		ifIndex := portIfIndex[bridgePort]
		if ifIndex == "" {
			ifIndex = bridgePort
		}
		add(ifIndex, mac, vlan)
	})
	for _, port := range rows {
		sort.Strings(port.MACAddresses)
	}
}

func walkFDB(g *gosnmp.GoSNMP, base string, limit int, apply func(suffix, value string)) {
	count := 0
	_ = g.BulkWalk(base, func(pdu gosnmp.SnmpPDU) error {
		count++
		if count > limit {
			return fmt.Errorf("fdb limit reached")
		}
		suffix := oidSuffix(pdu.Name, base)
		if suffix == "" {
			return nil
		}
		apply(suffix, stringify(pdu))
		return nil
	})
}

func parseFDBIndex(suffix string) (string, string, bool) {
	parts := strings.Split(strings.Trim(suffix, "."), ".")
	if len(parts) < 6 {
		return "", "", false
	}
	octets := make([]byte, 0, 6)
	for _, part := range parts[len(parts)-6:] {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 255 {
			return "", "", false
		}
		octets = append(octets, byte(value))
	}
	if octets[0]&1 == 1 || isBroadcastMAC(octets) || isZeroMAC(octets) {
		return "", "", false
	}
	mac := fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", octets[0], octets[1], octets[2], octets[3], octets[4], octets[5])
	vlan := ""
	if len(parts) > 6 {
		vlan = parts[len(parts)-7]
	}
	return mac, vlan, true
}

func isBroadcastMAC(value []byte) bool {
	for _, item := range value {
		if item != 0xFF {
			return false
		}
	}
	return true
}

func isZeroMAC(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
func normalizeMAC(value string) string {
	if value == "" {
		return ""
	}
	if strings.Contains(value, ":") {
		return strings.ToUpper(value)
	}
	parts := make([]string, 0, len(value))
	for _, b := range []byte(value) {
		parts = append(parts, fmt.Sprintf("%02X", b))
	}
	return strings.Join(parts, ":")
}

func firstNetworkValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
