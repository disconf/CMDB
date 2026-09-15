package cmdb

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type VLANMember struct {
	DeviceID     string   `json:"deviceId"`
	DeviceName   string   `json:"deviceName"`
	PortName     string   `json:"portName"`
	IfIndex      string   `json:"ifIndex"`
	AssetID      string   `json:"assetId,omitempty"`
	AssetName    string   `json:"assetName,omitempty"`
	Status       string   `json:"status"`
	MACAddresses []string `json:"macAddresses,omitempty"`
	ARPCount     int      `json:"arpCount"`
	NeighborName string   `json:"neighborName,omitempty"`
	LastSeenAt   string   `json:"lastSeenAt,omitempty"`
}

type VLANInventory struct {
	VLAN            string       `json:"vlan"`
	MemberCount     int          `json:"memberCount"`
	UpMembers       int          `json:"upMembers"`
	ConflictMembers int          `json:"conflictMembers"`
	LearnedMACCount int          `json:"learnedMacCount"`
	ARPEntryCount   int          `json:"arpEntryCount"`
	Members         []VLANMember `json:"members"`
	UpdatedAt       string       `json:"updatedAt"`
}

type NetworkAnomaly struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	Severity       string   `json:"severity"`
	Status         string   `json:"status"`
	Title          string   `json:"title"`
	Detail         string   `json:"detail"`
	AssetID        string   `json:"assetId,omitempty"`
	AssetName      string   `json:"assetName,omitempty"`
	ProjectGroup   string   `json:"projectGroup,omitempty"`
	Environment    string   `json:"environment,omitempty"`
	Location       string   `json:"location,omitempty"`
	DeviceID       string   `json:"deviceId,omitempty"`
	DeviceName     string   `json:"deviceName,omitempty"`
	PortName       string   `json:"portName,omitempty"`
	VLAN           string   `json:"vlan,omitempty"`
	IP             string   `json:"ip,omitempty"`
	MACAddress     string   `json:"macAddress,omitempty"`
	Evidence       []string `json:"evidence,omitempty"`
	AcknowledgedBy string   `json:"acknowledgedBy,omitempty"`
	AcknowledgedAt string   `json:"acknowledgedAt,omitempty"`
	Note           string   `json:"note,omitempty"`
	DetectedAt     string   `json:"detectedAt"`
}

type NetworkAnomalyState struct {
	AnomalyID string `json:"anomalyId"`
	Status    string `json:"status"`
	Operator  string `json:"operator"`
	Note      string `json:"note"`
	UpdatedAt string `json:"updatedAt"`
}
type arpObservation struct {
	DeviceID   string
	DeviceName string
	PortName   string
	VLAN       string
	IP         string
	MAC        string
	AssetID    string
	AssetName  string
}

func (s *Service) VLANInventory() []VLANInventory {
	assets := s.InventoryAssets()
	byName := make(map[string]Asset, len(assets))
	for _, asset := range assets {
		byName[normalizeNetworkName(asset.Name)] = asset
	}
	groups := map[string][]VLANMember{}
	updated := map[string]string{}
	for _, asset := range assets {
		if asset.Type != "network-device" && asset.Type != "load-balancer" {
			continue
		}
		for _, port := range parseNetworkPorts(asset) {
			target := Asset{}
			if match, ok := byName[normalizeNetworkName(port.NeighborName)]; ok {
				target = match
			}
			if target.ID == "" {
				for _, entry := range port.ARPEntries {
					if entry.AssetID != "" {
						target = Asset{ID: entry.AssetID, Name: entry.AssetName}
						break
					}
				}
			}
			vlans := []string{strings.TrimSpace(port.VLAN)}
			for _, entry := range port.ARPEntries {
				if entry.VLAN != "" && !containsString(vlans, entry.VLAN) {
					vlans = append(vlans, entry.VLAN)
				}
			}
			for _, vlan := range vlans {
				if vlan == "" {
					continue
				}
				member := VLANMember{DeviceID: asset.ID, DeviceName: asset.Name, PortName: port.Name, IfIndex: port.IfIndex, AssetID: target.ID, AssetName: firstNonEmpty(target.Name, port.NeighborName), Status: port.OperStatus, MACAddresses: append([]string(nil), port.MACAddresses...), ARPCount: countARPVLAN(port.ARPEntries, vlan), NeighborName: port.NeighborName, LastSeenAt: port.LastSeenAt}
				groups[vlan] = append(groups[vlan], member)
				if port.LastSeenAt > updated[vlan] {
					updated[vlan] = port.LastSeenAt
				}
			}
		}
	}
	result := make([]VLANInventory, 0, len(groups))
	for vlan, members := range groups {
		item := VLANInventory{VLAN: vlan, Members: members, MemberCount: len(members), UpdatedAt: updated[vlan]}
		for _, member := range members {
			if member.Status == "up" {
				item.UpMembers++
			}
			item.LearnedMACCount += len(member.MACAddresses)
			item.ARPEntryCount += member.ARPCount
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return vlanLess(result[i].VLAN, result[j].VLAN) })
	return result
}

func (s *Service) NetworkAnomalies() []NetworkAnomaly {
	assets := s.InventoryAssets()
	byIP := make(map[string]Asset, len(assets))
	byID := make(map[string]Asset, len(assets))
	for _, asset := range assets {
		byID[asset.ID] = asset
		if asset.IP != "" {
			byIP[asset.IP] = asset
		}
	}
	anomalies := []NetworkAnomaly{}
	arpByIP := map[string][]arpObservation{}
	macTargets := map[string]map[string]arpObservation{}
	knownMACs := knownNetworkMACs(assets)
	for _, asset := range assets {
		if asset.Type != "network-device" && asset.Type != "load-balancer" {
			continue
		}
		deviceMACs := map[string]map[string]arpObservation{}
		for _, port := range parseNetworkPorts(asset) {
			for _, entry := range port.ARPEntries {
				if entry.IP == "" || entry.MACAddress == "" {
					continue
				}
				obs := arpObservation{DeviceID: asset.ID, DeviceName: asset.Name, PortName: port.Name, VLAN: firstNonEmpty(entry.VLAN, port.VLAN), IP: entry.IP, MAC: normalizeMACForCompare(entry.MACAddress), AssetID: entry.AssetID, AssetName: entry.AssetName}
				arpByIP[obs.IP] = append(arpByIP[obs.IP], obs)
				if macTargets[obs.MAC] == nil {
					macTargets[obs.MAC] = map[string]arpObservation{}
				}
				macTargets[obs.MAC][obs.DeviceID+"|"+obs.PortName+"|"+obs.VLAN] = obs
				if entry.AssetID == "" && !knownMACs[obs.MAC] {
					severity := "warning"
					if _, knownIP := byIP[obs.IP]; !knownIP {
						severity = "critical"
					}
					addNetworkAnomaly(&anomalies, "unmanaged-access", severity, "发现未授权或未纳管接入", fmt.Sprintf("%s 端口 %s 上发现未知 IP %s / MAC %s", asset.Name, port.Name, obs.IP, obs.MAC), "", "", asset.ID, asset.Name, port.Name, obs.VLAN, obs.IP, obs.MAC, []string{"ARP 中的 IP/MAC 未匹配 CMDB 资产"})
				}
				if deviceMACs[obs.MAC] == nil {
					deviceMACs[obs.MAC] = map[string]arpObservation{}
				}
				deviceMACs[obs.MAC][obs.PortName] = obs
			}
			for _, mac := range port.MACAddresses {
				normalized := normalizeMACForCompare(mac)
				if normalized == "" {
					continue
				}
				if deviceMACs[normalized] == nil {
					deviceMACs[normalized] = map[string]arpObservation{}
				}
				deviceMACs[normalized][port.Name] = arpObservation{DeviceID: asset.ID, DeviceName: asset.Name, PortName: port.Name, MAC: normalized}
			}
		}
		for mac, ports := range deviceMACs {
			if len(ports) <= 1 {
				continue
			}
			linkNames := make([]string, 0, len(ports))
			for name := range ports {
				linkNames = append(linkNames, name)
			}
			sort.Strings(linkNames)
			addNetworkAnomaly(&anomalies, "mac-port-flapping", "critical", fmt.Sprintf("MAC %s 在多个端口出现", mac), fmt.Sprintf("%s 的同一 MAC 同时出现在 %s", asset.Name, strings.Join(linkNames, "、")), asset.ID, asset.Name, "", "", "", "0", "", mac, []string{"交换机同一 MAC 学习到多个端口"})
		}
	}
	for ip, entries := range arpByIP {
		macs := map[string]bool{}
		for _, entry := range entries {
			macs[entry.MAC] = true
		}
		if len(macs) > 1 {
			addNetworkAnomaly(&anomalies, "arp-ip-conflict", "critical", fmt.Sprintf("IP %s 对应多个 MAC", ip), "ARP 表中同一 IP 被多个 MAC 地址声明", "", "", "", "", entries[0].PortName, entries[0].VLAN, ip, "", []string{"ARP IP/MAC 冲突"})
		}
		if asset, ok := byIP[ip]; ok {
			ifaceMACs := interfaceMACSet(asset)
			if len(ifaceMACs) > 0 {
				matched := false
				for mac := range macs {
					if ifaceMACs[mac] {
						matched = true
						break
					}
				}
				if !matched {
					addNetworkAnomaly(&anomalies, "arp-asset-mismatch", "warning", fmt.Sprintf("资产 %s 的 ARP MAC 不一致", asset.Name), fmt.Sprintf("资产 IP %s 在 ARP 表中未匹配已采集网卡 MAC", ip), asset.ID, asset.Name, entries[0].DeviceID, entries[0].DeviceName, entries[0].PortName, entries[0].VLAN, ip, "", []string{"资产网卡 MAC 与 ARP 表不一致"})
				}
			}
		}
	}
	for mac, targets := range macTargets {
		if len(targets) < 2 {
			continue
		}
		locations := make([]string, 0, len(targets))
		for _, item := range targets {
			locations = append(locations, item.DeviceName+"/"+item.PortName)
		}
		sort.Strings(locations)
		addNetworkAnomaly(&anomalies, "mac-drift", "warning", fmt.Sprintf("MAC %s 发生漂移", mac), "同一 MAC 出现在多个设备或端口："+strings.Join(uniqueStrings(locations), "、"), "", "", "", "", "", "", "", mac, []string{"ARP/MAC 位置漂移"})
	}
	for _, link := range s.NetworkLinks() {
		if link.Verification != "conflict" {
			continue
		}
		addNetworkAnomaly(&anomalies, "link-conflict", "critical", fmt.Sprintf("物理链路冲突：%s", link.SourceAssetName), fmt.Sprintf("%s %s 与 %s %s 的绑定存在 MAC 冲突", link.SourceAssetName, link.SourceInterface, link.TargetAssetName, link.TargetPort), link.SourceAssetID, link.SourceAssetName, link.TargetAssetID, link.TargetAssetName, link.TargetPort, "", "", "", link.Evidence)
	}
	sort.Slice(anomalies, func(i, j int) bool {
		if severityRank(anomalies[i].Severity) != severityRank(anomalies[j].Severity) {
			return severityRank(anomalies[i].Severity) < severityRank(anomalies[j].Severity)
		}
		return anomalies[i].Title < anomalies[j].Title
	})
	for i := range anomalies {
		asset := byID[anomalies[i].AssetID]
		if asset.ID == "" {
			asset = byID[anomalies[i].DeviceID]
		}
		if asset.ID != "" {
			anomalies[i].ProjectGroup = asset.ProjectGroup
			anomalies[i].Environment = asset.Environment
			anomalies[i].Location = asset.Location
		}
	}
	states := s.networkAnomalyStateMap()
	for i := range anomalies {
		if state, ok := states[anomalies[i].ID]; ok {
			anomalies[i].Status = state.Status
			anomalies[i].AcknowledgedBy = state.Operator
			anomalies[i].Note = state.Note
			if state.Status == "acknowledged" || state.Status == "resolved" {
				anomalies[i].AcknowledgedAt = state.UpdatedAt
			}
		}
	}
	return anomalies
}

func (s *Service) networkAnomalyStateMap() map[string]NetworkAnomalyState {
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		states, err := loadNetworkAnomalyStates(ctx, s.db)
		if err == nil {
			return states
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]NetworkAnomalyState, len(s.anomalyStates))
	for key, value := range s.anomalyStates {
		result[key] = value
	}
	return result
}

func (s *Service) AcknowledgeNetworkAnomaly(id, operator, note string) (NetworkAnomaly, error) {
	return s.changeNetworkAnomalyState(id, "acknowledged", operator, note)
}

func (s *Service) ResolveNetworkAnomaly(id, operator, note string) (NetworkAnomaly, error) {
	return s.changeNetworkAnomalyState(id, "resolved", operator, note)
}

func (s *Service) changeNetworkAnomalyState(id, status, operator, note string) (NetworkAnomaly, error) {
	operator = strings.TrimSpace(operator)
	if operator == "" {
		return NetworkAnomaly{}, ErrValidation
	}
	var target NetworkAnomaly
	found := false
	for _, item := range s.NetworkAnomalies() {
		if item.ID == id {
			target = item
			found = true
			break
		}
	}
	if !found {
		return NetworkAnomaly{}, ErrNotFound
	}
	state := NetworkAnomalyState{AnomalyID: id, Status: status, Operator: operator, Note: strings.TrimSpace(note), UpdatedAt: time.Now().Format("2006-01-02 15:04:05")}
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := upsertNetworkAnomalyState(ctx, s.db, state); err != nil {
			return NetworkAnomaly{}, err
		}
	} else {
		s.mu.Lock()
		s.anomalyStates[id] = state
		s.mu.Unlock()
	}
	target.Status = state.Status
	target.AcknowledgedBy = state.Operator
	target.AcknowledgedAt = state.UpdatedAt
	target.Note = state.Note
	return target, nil
}

func addNetworkAnomaly(list *[]NetworkAnomaly, anomalyType, severity, title, detail, assetID, assetName, deviceID, deviceName, portName, vlan, ip, mac string, evidence []string) {
	id := networkAnomalyID(anomalyType, assetID, deviceID, portName, vlan, ip, mac)
	for _, item := range *list {
		if item.ID == id {
			return
		}
	}
	*list = append(*list, NetworkAnomaly{ID: id, Type: anomalyType, Severity: severity, Status: "open", Title: title, Detail: detail, AssetID: assetID, AssetName: assetName, DeviceID: deviceID, DeviceName: deviceName, PortName: portName, VLAN: vlan, IP: ip, MACAddress: mac, Evidence: evidence, DetectedAt: time.Now().Format("2006-01-02 15:04:05")})
}

func networkAnomalyID(parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return "net-anomaly-" + hex.EncodeToString(sum[:8])
}

func knownNetworkMACs(assets []Asset) map[string]bool {
	result := map[string]bool{}
	for _, asset := range assets {
		for _, item := range parseNetworkInterfaces(asset) {
			if mac := normalizeMACForCompare(item.MACAddress); mac != "" {
				result[mac] = true
			}
		}
		for _, port := range parseNetworkPorts(asset) {
			if mac := normalizeMACForCompare(port.MACAddress); mac != "" {
				result[mac] = true
			}
			for _, mac := range port.MACAddresses {
				if normalized := normalizeMACForCompare(mac); normalized != "" {
					result[normalized] = true
				}
			}
		}
	}
	return result
}
func interfaceMACSet(asset Asset) map[string]bool {
	result := map[string]bool{}
	for _, item := range parseNetworkInterfaces(asset) {
		if mac := normalizeMACForCompare(item.MACAddress); mac != "" {
			result[mac] = true
		}
	}
	return result
}

func countARPVLAN(entries []NetworkARPEntry, vlan string) int {
	count := 0
	for _, entry := range entries {
		if entry.VLAN == "" || entry.VLAN == vlan {
			count++
		}
	}
	return count
}

func vlanLess(left, right string) bool {
	a, aErr := strconv.Atoi(left)
	b, bErr := strconv.Atoi(right)
	if aErr == nil && bErr == nil {
		return a < b
	}
	return left < right
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func severityRank(value string) int {
	if value == "critical" {
		return 0
	}
	if value == "warning" {
		return 1
	}
	return 2
}
