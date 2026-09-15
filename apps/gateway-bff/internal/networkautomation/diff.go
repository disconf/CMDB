package networkautomation

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

var ErrSnapshotNotFound = errors.New("network snapshot not found")

type NetworkDiffSummary struct {
	Added         int `json:"added"`
	Removed       int `json:"removed"`
	Changed       int `json:"changed"`
	UnknownAccess int `json:"unknownAccess"`
}

type NetworkDiffChange struct {
	Kind   string `json:"kind"`
	Action string `json:"action"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

type NetworkDiff struct {
	From        string              `json:"from"`
	To          string              `json:"to"`
	GeneratedAt string              `json:"generatedAt"`
	Summary     NetworkDiffSummary  `json:"summary"`
	Changes     []NetworkDiffChange `json:"changes"`
}

func (s *Service) NetworkDiff(fromID, toID string) (NetworkDiff, error) {
	var fromSnapshot, toSnapshot cmdb.TopologySnapshot
	if fromID == "" || toID == "" {
		items := s.cmdb.TopologySnapshots(networkSnapshotKind, "", 2, true)
		if len(items) < 2 {
			return NetworkDiff{GeneratedAt: time.Now().Format("2006-01-02 15:04:05"), Changes: []NetworkDiffChange{}}, nil
		}
		toSnapshot = items[0]
		fromSnapshot = items[1]
	} else {
		var err error
		fromSnapshot, err = s.cmdb.TopologySnapshot(fromID)
		if err != nil {
			return NetworkDiff{}, ErrSnapshotNotFound
		}
		toSnapshot, err = s.cmdb.TopologySnapshot(toID)
		if err != nil {
			return NetworkDiff{}, ErrSnapshotNotFound
		}
	}
	var before, after NetworkSnapshotPayload
	if len(fromSnapshot.Payload) == 0 || len(toSnapshot.Payload) == 0 || json.Unmarshal(fromSnapshot.Payload, &before) != nil || json.Unmarshal(toSnapshot.Payload, &after) != nil {
		return NetworkDiff{}, errors.New("invalid network snapshot payload")
	}
	return diffNetworkSnapshots(fromSnapshot, before, toSnapshot, after), nil
}

func diffNetworkSnapshots(fromSnapshot cmdb.TopologySnapshot, before NetworkSnapshotPayload, toSnapshot cmdb.TopologySnapshot, after NetworkSnapshotPayload) NetworkDiff {
	result := NetworkDiff{From: fromSnapshot.ID, To: toSnapshot.ID, GeneratedAt: time.Now().Format("2006-01-02 15:04:05"), Changes: []NetworkDiffChange{}}
	beforeDevices := map[string]cmdb.NetworkDeviceInventory{}
	afterDevices := map[string]cmdb.NetworkDeviceInventory{}
	for _, item := range before.Devices {
		beforeDevices[item.Asset.ID] = item
	}
	for _, item := range after.Devices {
		afterDevices[item.Asset.ID] = item
	}
	for id, item := range afterDevices {
		old, ok := beforeDevices[id]
		if !ok {
			addDiffChange(&result, "device", "added", id, item.Asset.Name, fmt.Sprintf("新增网络设备 %s", item.Asset.IP), "", "")
			continue
		}
		if old.Asset.Status != item.Asset.Status || old.TotalPorts != item.TotalPorts || old.UpPorts != item.UpPorts {
			addDiffChange(&result, "device", "changed", id, item.Asset.Name, "设备状态或端口数量发生变化", fmt.Sprintf("%s/%d/%d", old.Asset.Status, old.TotalPorts, old.UpPorts), fmt.Sprintf("%s/%d/%d", item.Asset.Status, item.TotalPorts, item.UpPorts))
		}
		diffPorts(&result, id, item.Asset.Name, old.Ports, item.Ports)
	}
	for id, item := range beforeDevices {
		if _, ok := afterDevices[id]; !ok {
			addDiffChange(&result, "device", "removed", id, item.Asset.Name, fmt.Sprintf("网络设备已从快照消失 %s", item.Asset.IP), "", "")
		}
	}
	beforeVLANs := map[string]cmdb.VLANInventory{}
	afterVLANs := map[string]cmdb.VLANInventory{}
	for _, item := range before.VLANs {
		beforeVLANs[item.VLAN] = item
	}
	for _, item := range after.VLANs {
		afterVLANs[item.VLAN] = item
	}
	for vlan, item := range afterVLANs {
		old, ok := beforeVLANs[vlan]
		if !ok {
			addDiffChange(&result, "vlan", "added", "vlan:"+vlan, "VLAN "+vlan, "新增 VLAN", "", "")
		} else if old.MemberCount != item.MemberCount || old.UpMembers != item.UpMembers || old.ARPEntryCount != item.ARPEntryCount {
			addDiffChange(&result, "vlan", "changed", "vlan:"+vlan, "VLAN "+vlan, "VLAN 成员或 ARP 数量发生变化", fmt.Sprintf("%d/%d/%d", old.MemberCount, old.UpMembers, old.ARPEntryCount), fmt.Sprintf("%d/%d/%d", item.MemberCount, item.UpMembers, item.ARPEntryCount))
		}
	}
	for vlan := range beforeVLANs {
		if _, ok := afterVLANs[vlan]; !ok {
			addDiffChange(&result, "vlan", "removed", "vlan:"+vlan, "VLAN "+vlan, "VLAN 已从快照消失", "", "")
		}
	}
	beforeLinks := map[string]cmdb.NetworkLink{}
	afterLinks := map[string]cmdb.NetworkLink{}
	for _, item := range before.Links {
		beforeLinks[networkDiffLinkKey(item)] = item
	}
	for _, item := range after.Links {
		afterLinks[networkDiffLinkKey(item)] = item
	}
	for key, item := range afterLinks {
		if _, ok := beforeLinks[key]; !ok {
			addDiffChange(&result, "link", "added", key, item.SourceAssetName, fmt.Sprintf("新增链路 %s/%s -> %s/%s", item.SourceAssetName, item.SourceInterface, item.TargetAssetName, item.TargetPort), "", item.Verification)
		}
	}
	for key, item := range beforeLinks {
		if _, ok := afterLinks[key]; !ok {
			addDiffChange(&result, "link", "removed", key, item.SourceAssetName, fmt.Sprintf("链路已消失 %s/%s -> %s/%s", item.SourceAssetName, item.SourceInterface, item.TargetAssetName, item.TargetPort), item.Verification, "")
		}
	}
	beforeAnomalies := map[string]cmdb.NetworkAnomaly{}
	afterAnomalies := map[string]cmdb.NetworkAnomaly{}
	for _, item := range before.Anomalies {
		beforeAnomalies[item.ID] = item
	}
	for _, item := range after.Anomalies {
		afterAnomalies[item.ID] = item
	}
	for id, item := range afterAnomalies {
		if _, ok := beforeAnomalies[id]; !ok {
			addDiffChange(&result, "anomaly", "added", id, item.Title, item.Detail, "", item.Severity)
			if item.Type == "unmanaged-access" {
				result.Summary.UnknownAccess++
			}
		}
	}
	for id, item := range beforeAnomalies {
		if _, ok := afterAnomalies[id]; !ok {
			addDiffChange(&result, "anomaly", "removed", id, item.Title, "异常已恢复或消失", item.Severity, "")
		}
	}
	sort.Slice(result.Changes, func(i, j int) bool {
		if result.Changes[i].Action != result.Changes[j].Action {
			return result.Changes[i].Action < result.Changes[j].Action
		}
		return result.Changes[i].Name < result.Changes[j].Name
	})
	return result
}

func diffPorts(result *NetworkDiff, deviceID, deviceName string, before, after []cmdb.NetworkPort) {
	beforePorts := map[string]cmdb.NetworkPort{}
	afterPorts := map[string]cmdb.NetworkPort{}
	for _, item := range before {
		beforePorts[item.ID] = item
	}
	for _, item := range after {
		afterPorts[item.ID] = item
	}
	for id, item := range afterPorts {
		old, ok := beforePorts[id]
		if !ok {
			addDiffChange(result, "port", "added", id, deviceName+"/"+item.Name, "新增端口", "", item.OperStatus)
			continue
		}
		macBefore := strings.Join(item.MACAddresses, ",")
		macAfter := strings.Join(old.MACAddresses, ",")
		arpBefore := fmt.Sprint(len(item.ARPEntries))
		arpAfter := fmt.Sprint(len(old.ARPEntries))
		if old.OperStatus != item.OperStatus || old.VLAN != item.VLAN || macBefore != macAfter || arpBefore != arpAfter {
			addDiffChange(result, "port", "changed", id, deviceName+"/"+item.Name, "端口状态、VLAN、MAC 或 ARP 发生变化", fmt.Sprintf("%s/%s/%s/%s", old.OperStatus, old.VLAN, macBefore, arpBefore), fmt.Sprintf("%s/%s/%s/%s", item.OperStatus, item.VLAN, macAfter, arpAfter))
		}
	}
	for id, item := range beforePorts {
		if _, ok := afterPorts[id]; !ok {
			addDiffChange(result, "port", "removed", id, deviceName+"/"+item.Name, "端口已从快照消失", item.OperStatus, "")
		}
	}
}

func addDiffChange(result *NetworkDiff, kind, action, id, name, detail, before, after string) {
	result.Changes = append(result.Changes, NetworkDiffChange{Kind: kind, Action: action, ID: id, Name: name, Detail: detail, Before: before, After: after})
	switch action {
	case "added":
		result.Summary.Added++
	case "removed":
		result.Summary.Removed++
	default:
		result.Summary.Changed++
	}
}

func networkDiffLinkKey(link cmdb.NetworkLink) string {
	left := link.SourceAssetID + "|" + link.SourceInterface
	right := link.TargetAssetID + "|" + link.TargetPort
	if right < left {
		left, right = right, left
	}
	return left + "->" + right
}
