package networkautomation

import (
	"encoding/json"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

const networkSnapshotKind = "network-inventory"

type NetworkSnapshotPayload struct {
	CapturedAt string                        `json:"capturedAt"`
	Devices    []cmdb.NetworkDeviceInventory `json:"devices"`
	VLANs      []cmdb.VLANInventory          `json:"vlans"`
	Links      []cmdb.NetworkLink            `json:"links"`
	Anomalies  []cmdb.NetworkAnomaly         `json:"anomalies"`
}

func (s *Service) CaptureNetworkSnapshot(operator string) (cmdb.TopologySnapshot, error) {
	payload := NetworkSnapshotPayload{CapturedAt: time.Now().Format("2006-01-02 15:04:05"), Devices: s.cmdb.NetworkDevices(), VLANs: s.cmdb.VLANInventory(), Links: s.cmdb.NetworkLinks(), Anomalies: s.cmdb.NetworkAnomalies()}
	body, err := json.Marshal(payload)
	if err != nil {
		return cmdb.TopologySnapshot{}, err
	}
	return s.cmdb.SaveTopologySnapshot(cmdb.TopologySnapshotInput{Kind: networkSnapshotKind, NodeCount: len(payload.Devices) + len(payload.VLANs), EdgeCount: len(payload.Links), Payload: body, Operator: operator})
}

func (s *Service) NetworkSnapshots(limit int) []cmdb.TopologySnapshot {
	return s.cmdb.TopologySnapshots(networkSnapshotKind, "", limit, false)
}

func (s *Service) maybeCaptureNetworkSnapshot(operator string) bool {
	items := s.cmdb.TopologySnapshots(networkSnapshotKind, "", 1, false)
	if len(items) == 0 {
		_, err := s.CaptureNetworkSnapshot(operator)
		return err == nil
	}
	created, err := time.ParseInLocation("2006-01-02 15:04:05", items[0].CreatedAt, time.Local)
	if err != nil || time.Since(created) >= 15*time.Minute {
		_, captureErr := s.CaptureNetworkSnapshot(operator)
		return captureErr == nil
	}
	return false
}
