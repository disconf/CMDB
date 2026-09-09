package discovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"

	"cmdb/gateway-bff/internal/cmdb"
)

type SNMPScanInput struct {
	CIDRs     []string `json:"cidrs"`
	Community string   `json:"community"`
	Port      int      `json:"port"`
}

type SNMPScanResult struct {
	Scanned   int                `json:"scanned"`
	Found     int                `json:"found"`
	Adopted   int                `json:"adopted"`
	Merged    int                `json:"merged"`
	Conflicts int                `json:"conflicts"`
	Hosts     []NodeExporterHost `json:"hosts"`
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
	oids := []string{".1.3.6.1.2.1.1.1.0", ".1.3.6.1.2.1.1.5.0", ".1.3.6.1.2.1.2.1.0"}
	resp, err := g.Get(oids)
	if err != nil || resp == nil {
		return nil
	}
	var sysDescr, sysName string
	var ifNumber string
	for _, v := range resp.Variables {
		if v.Type == gosnmp.OctetString || v.Type == gosnmp.IPAddress {
			value := string(v.Value.([]byte))
			switch v.Name {
			case oids[0]:
				sysDescr = strings.TrimSpace(value)
			case oids[1]:
				sysName = strings.TrimSpace(value)
			}
		} else if v.Type == gosnmp.Integer {
			value := fmt.Sprint(gosnmp.ToBigInt(v.Value).Int64())
			if v.Name == oids[2] {
				ifNumber = value
			}
		}
	}
	if sysDescr == "" && sysName == "" {
		return nil
	}
	host := &NodeExporterHost{IP: ip, Name: sysName, OS: sysDescr, Virtual: false}
	if host.Name == "" {
		host.Name = ip
	}
	host.Attributes = []cmdb.Attribute{
		{Name: "sys_descr", Label: "系统描述", Value: sysDescr},
		{Name: "sys_name", Label: "设备名", Value: sysName},
		{Name: "management_ip", Label: "管理IP", Value: ip},
	}
	if ifNumber != "" {
		host.Attributes = append(host.Attributes, cmdb.Attribute{Name: "if_number", Label: "接口数", Value: ifNumber})
	}
	return host
}

// ScanSNMP scans CIDRs over SNMP v2c and adopts network devices into CMDB.
func (s *Service) ScanSNMP(ctx context.Context, in SNMPScanInput) (SNMPScanResult, error) {
	result := SNMPScanResult{}
	if len(in.CIDRs) == 0 {
		return result, errors.New("validation")
	}
	community := in.Community
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
			if host := probeSNMP(address, port, community, 1200*time.Millisecond); host != nil {
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
		items = append(items, DiscoveredItem{
			ID:         "snmp-" + strings.ReplaceAll(host.IP, ".", "-"),
			Name:       host.Name,
			IP:         host.IP,
			Type:       "network-device",
			Confidence: 88,
			Attributes: host.Attributes,
		})
		result.Hosts = append(result.Hosts, *host)
	}
	if len(items) > 0 {
		ing, err := s.Ingest(IngestInput{Source: "snmp", Scope: strings.Join(in.CIDRs, ","), Items: items})
		if err != nil {
			return result, err
		}
		result.Adopted = ing.Imported
		result.Merged = ing.Merged
		result.Conflicts = ing.Conflicts
	}
	return result, nil
}
