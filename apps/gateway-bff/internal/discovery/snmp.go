package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"

	"cmdb/gateway-bff/internal/cmdb"
)

type SNMPScanInput struct {
	CIDRs           []string `json:"cidrs"`
	Community       string   `json:"community"`
	Port            int      `json:"port"`
	DefaultType     string   `json:"defaultType"` // network-device / physical-server / ""(自动)
	CredentialID    string   `json:"credentialId"`
	RequireApproval bool     `json:"requireApproval"`
}

type SNMPScanResult struct {
	Scanned   int                `json:"scanned"`
	Found     int                `json:"found"`
	Adopted   int                `json:"adopted"`
	Merged    int                `json:"merged"`
	Conflicts int                `json:"conflicts"`
	Hosts     []NodeExporterHost `json:"hosts"`
}

const (
	oidSysDescr     = ".1.3.6.1.2.1.1.1.0"
	oidSysObjectID  = ".1.3.6.1.2.1.1.2.0"
	oidSysName      = ".1.3.6.1.2.1.1.5.0"
	oidIfNumber     = ".1.3.6.1.2.1.2.1.0"
	oidEntityBase   = ".1.3.6.1.2.1.47.1.1.1.1"
	oidEntPhysDescr = ".1.3.6.1.2.1.47.1.1.1.1.2"
	oidEntPhysName  = ".1.3.6.1.2.1.47.1.1.1.1.5"
	oidEntPhysSwRev = ".1.3.6.1.2.1.47.1.1.1.1.8"
	oidEntPhysSn    = ".1.3.6.1.2.1.47.1.1.1.1.11"
	oidEntPhysModel = ".1.3.6.1.2.1.47.1.1.1.1.13"
	oidLldpRemBase  = ".1.0.8802.1.1.2.1.4.1.1"
)

type LLDPNeighbor struct {
	LocalPort      string `json:"localPort"`
	RemoteSysName  string `json:"remoteSysName"`
	RemoteChassis  string `json:"remoteChassisId"`
	RemotePortID   string `json:"remotePortId"`
	RemotePortDesc string `json:"remotePortDesc"`
}

var oobKeywords = []string{"ilo", "idrac", "ibmc", "bmc", "ipmi", "integrated lights-out", "drac", "redfish", "impic"}
var vendorRules = []struct {
	name   string
	match  []string
	oidTag string
}{
	{"Huawei", []string{"vrp", "huawei"}, ".1.3.6.1.4.1.2011"},
	{"H3C", []string{"comware", "h3c"}, ".1.3.6.1.4.1.25506"},
	{"Cisco", []string{"cisco ios", "ios-xe", "nx-os", "catalyst"}, ".1.3.6.1.4.1.9"},
	{"Ruijie", []string{"rgos", "ruijie"}, ".1.3.6.1.4.1.4881"},
	{"HPE", []string{"proliant", "hpe"}, ".1.3.6.1.4.1.232"},
	{"Dell", []string{"idrac", "dell"}, ".1.3.6.1.4.1.674"},
}

func classifySNMP(sysDescr, sysObjectID string) (vendor string, oob bool) {
	low := strings.ToLower(sysDescr + " " + sysObjectID)
	for _, kw := range oobKeywords {
		if strings.Contains(low, kw) {
			oob = true
		}
	}
	for _, r := range vendorRules {
		for _, m := range r.match {
			if strings.Contains(low, m) {
				vendor = r.name
				return vendor, oob
			}
		}
		if r.oidTag != "" && strings.Contains(sysObjectID, strings.TrimPrefix(r.oidTag, ".")) {
			vendor = r.name
			return vendor, oob
		}
	}
	return "", oob
}

func stringify(v gosnmp.SnmpPDU) string {
	switch v.Type {
	case gosnmp.OctetString, gosnmp.IPAddress, gosnmp.Opaque:
		if b, ok := v.Value.([]byte); ok {
			return strings.TrimSpace(string(b))
		}
	case gosnmp.Integer, gosnmp.Counter32, gosnmp.Counter64, gosnmp.Gauge32, gosnmp.TimeTicks, gosnmp.Uinteger32:
		return fmt.Sprint(gosnmp.ToBigInt(v.Value))
	}
	return ""
}

func probeSNMP(ip string, port int, community string, timeout time.Duration) *NodeExporterHost {
	if community == "" {
		return nil
	}
	g := &gosnmp.GoSNMP{Target: ip, Port: uint16(port), Community: community, Version: gosnmp.Version2c, Timeout: timeout, Retries: 0}
	if err := g.Connect(); err != nil {
		return nil
	}
	defer g.Conn.Close()
	resp, err := g.Get([]string{oidSysDescr, oidSysObjectID, oidSysName, oidIfNumber})
	if err != nil || resp == nil {
		return nil
	}
	vals := map[string]string{}
	for _, v := range resp.Variables {
		vals[v.Name] = stringify(v)
	}
	sysDescr := vals[oidSysDescr]
	sysObjectID := vals[oidSysObjectID]
	sysName := vals[oidSysName]
	ifNumber := vals[oidIfNumber]
	if sysDescr == "" && sysName == "" {
		return nil
	}
	host := &NodeExporterHost{IP: ip, Name: sysName, OS: sysDescr}
	if host.Name == "" {
		host.Name = ip
	}
	model, serial, hwDescr := "", "", ""
	vendor, oob := classifySNMP(sysDescr, sysObjectID)
	// entity walk for model/serial (best effort, capped)
	count := 0
	_ = g.BulkWalk(oidEntityBase, func(pdu gosnmp.SnmpPDU) error {
		count++
		if count > 400 {
			return errors.New("limit")
		}
		value := stringify(pdu)
		if value == "" {
			return nil
		}
		switch {
		case strings.HasSuffix(pdu.Name, ".11") && serial == "":
			serial = value
		case strings.HasSuffix(pdu.Name, ".13") && model == "":
			model = value
		case strings.HasSuffix(pdu.Name, ".2") && hwDescr == "":
			hwDescr = value
		}
		return nil
	})
	neighbors := collectLLDP(g, 500)
	if hwDescr != "" && vendor == "" {
		vendor = vendorFromDescr(hwDescr)
	}
	host.Attributes = []cmdb.Attribute{
		{Name: "sys_descr", Label: "系统描述", Value: sysDescr},
		{Name: "sys_name", Label: "设备名", Value: sysName},
		{Name: "sys_object_id", Label: "sysObjectID", Value: sysObjectID},
		{Name: "management_ip", Label: "管理IP", Value: ip},
	}
	if vendor != "" {
		host.Attributes = append(host.Attributes, cmdb.Attribute{Name: "vendor", Label: "厂商", Value: vendor})
	}
	if model != "" {
		host.Attributes = append(host.Attributes, cmdb.Attribute{Name: "model", Label: "型号", Value: model})
	}
	if serial != "" {
		host.Attributes = append(host.Attributes, cmdb.Attribute{Name: "serial", Label: "序列号", Value: serial})
	}
	if hwDescr != "" {
		host.Attributes = append(host.Attributes, cmdb.Attribute{Name: "entity_desc", Label: "实体描述", Value: hwDescr})
	}
	if ifNumber != "" {
		host.Attributes = append(host.Attributes, cmdb.Attribute{Name: "if_number", Label: "接口数", Value: ifNumber})
	}
	if oob {
		host.Attributes = append(host.Attributes, cmdb.Attribute{Name: "bmc_ip", Label: "带外管理IP(BMC)", Value: ip})
		host.Attributes = append(host.Attributes, cmdb.Attribute{Name: "oob", Label: "带外管理口", Value: "true"})
	}
	if len(neighbors) > 0 {
		if payload, err := json.Marshal(neighbors); err == nil {
			host.Attributes = append(host.Attributes, cmdb.Attribute{Name: "lldp_neighbors", Label: "LLDP邻居", Value: string(payload)})
		}
	}
	host.Attributes = append(host.Attributes, cmdb.Attribute{Name: "snmp_discovered", Label: "SNMP发现", Value: "true"})
	host.Virtual = false
	host.OS = sysDescr
	_ = vendorFromDescr // silence unused in odd builds
	return host
}

func collectLLDP(g *gosnmp.GoSNMP, limit int) []LLDPNeighbor {
	byKey := map[string]*LLDPNeighbor{}
	count := 0
	_ = g.BulkWalk(oidLldpRemBase, func(pdu gosnmp.SnmpPDU) error {
		count++
		if count > limit*6 {
			return errors.New("limit")
		}
		suffix := strings.TrimPrefix(pdu.Name, strings.TrimPrefix(oidLldpRemBase, "."))
		suffix = strings.TrimPrefix(suffix, ".")
		parts := strings.Split(suffix, ".")
		if len(parts) < 4 {
			return nil
		}
		column := parts[0]
		key := parts[len(parts)-2] + "." + parts[len(parts)-1]
		neighbor := byKey[key]
		if neighbor == nil {
			neighbor = &LLDPNeighbor{LocalPort: parts[len(parts)-2]}
			byKey[key] = neighbor
		}
		value := stringify(pdu)
		switch column {
		case "5":
			neighbor.RemoteChassis = value
		case "7":
			neighbor.RemotePortID = value
		case "8":
			neighbor.RemotePortDesc = value
		case "9":
			neighbor.RemoteSysName = value
		}
		return nil
	})
	neighbors := make([]LLDPNeighbor, 0, len(byKey))
	for _, neighbor := range byKey {
		if neighbor.RemoteSysName != "" || neighbor.RemoteChassis != "" {
			neighbors = append(neighbors, *neighbor)
		}
	}
	sort.Slice(neighbors, func(i, j int) bool {
		if neighbors[i].LocalPort == neighbors[j].LocalPort {
			return neighbors[i].RemoteSysName < neighbors[j].RemoteSysName
		}
		return neighbors[i].LocalPort < neighbors[j].LocalPort
	})
	if len(neighbors) > limit {
		neighbors = neighbors[:limit]
	}
	return neighbors
}
func vendorFromDescr(descr string) string {
	low := strings.ToLower(descr)
	for _, r := range vendorRules {
		for _, m := range r.match {
			if strings.Contains(low, m) {
				return r.name
			}
		}
	}
	return ""
}

// ScanSNMP scans CIDRs over SNMP v2c and adopts devices/servers into CMDB.
func (s *Service) ScanSNMP(ctx context.Context, in SNMPScanInput) (SNMPScanResult, error) {
	result := SNMPScanResult{}
	if len(in.CIDRs) == 0 {
		return result, errors.New("validation")
	}
	community := in.Community
	if in.CredentialID != "" {
		if _, sec, err := s.resolveCredential(in.CredentialID); err == nil && sec != "" {
			community = sec
		}
	}
	if community == "" {
		community = os.Getenv("CMDB_SNMP_COMMUNITY")
	}
	if community == "" {
		return result, errors.New("SNMP community not configured")
	}
	port := in.Port
	if port == 0 {
		port = 161
	}
	if port < 1 || port > 65535 {
		return result, errors.New("invalid port")
	}
	var targets []string
	for _, cidr := range in.CIDRs {
		ips, err := expandCIDR(cidr)
		if err != nil {
			return result, err
		}
		targets = append(targets, ips...)
	}
	result.Scanned = len(targets)
	var mu sync.Mutex
	sem := make(chan struct{}, 48)
	var wg sync.WaitGroup
	found := []*NodeExporterHost{}
	for _, ip := range targets {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			select {
			case <-ctx.Done():
				return
			default:
			}
			if host := probeSNMP(address, port, community, 1500*time.Millisecond); host != nil {
				mu.Lock()
				found = append(found, host)
				mu.Unlock()
			}
		}(ip)
	}
	wg.Wait()
	result.Found = len(found)
	items := []DiscoveredItem{}
	for _, host := range found {
		_, oob := classifySNMP(host.OS, "")
		assetType := strings.TrimSpace(in.DefaultType)
		if assetType == "" {
			if oob {
				assetType = "physical-server"
			} else {
				assetType = "network-device"
			}
		}
		items = append(items, DiscoveredItem{
			ID:         "snmp-" + strings.ReplaceAll(host.IP, ".", "-"),
			Name:       host.Name,
			IP:         host.IP,
			Type:       assetType,
			Confidence: 88,
			Attributes: host.Attributes,
		})
		result.Hosts = append(result.Hosts, *host)
	}
	if len(items) > 0 {
		ing, err := s.Ingest(IngestInput{Source: "snmp", Scope: strings.Join(in.CIDRs, ","), Items: items, RequireApproval: in.RequireApproval})
		if err != nil {
			return result, err
		}
		result.Adopted = ing.Imported
		result.Merged = ing.Merged
		result.Conflicts = ing.Conflicts
	}
	return result, nil
}
