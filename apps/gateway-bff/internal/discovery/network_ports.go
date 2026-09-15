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
	oidIfDescr       = ".1.3.6.1.2.1.2.2.1.2"
	oidIfPhysAddress = ".1.3.6.1.2.1.2.2.1.6"
	oidIfAdminStatus = ".1.3.6.1.2.1.2.2.1.7"
	oidIfOperStatus  = ".1.3.6.1.2.1.2.2.1.8"
	oidIfSpeed       = ".1.3.6.1.2.1.2.2.1.5"
	oidIfHighSpeed   = ".1.3.6.1.2.1.31.1.1.1.15"
	oidIfName        = ".1.3.6.1.2.1.31.1.1.1.1"
	oidIfAlias       = ".1.3.6.1.2.1.31.1.1.1.18"
	oidDot1qPvid     = ".1.3.6.1.2.1.17.7.1.4.5.1.1"
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
