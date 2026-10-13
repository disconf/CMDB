package httpapi

import (
	"context"
	"fmt"
	"strings"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/redfish"
)

func reconcileRedfishAsset(ctx context.Context, cmdbService *cmdb.Service, client redfish.Client, address, username, password, assetID, operator string) ([]cmdb.Asset, error) {
	result, err := client.Test(ctx, address, username, password)
	if err != nil {
		return nil, err
	}
	assets := []cmdb.Asset{}
	for _, system := range result.Systems {
		targetID := strings.TrimSpace(assetID)
		if targetID == "" {
			targetID = "bmc-" + safeAssetID(strings.TrimSpace(system.SerialNumber))
			if strings.TrimSpace(system.SerialNumber) == "" {
				targetID = "bmc-" + safeAssetID(address+"-"+system.ID)
			}
		}
		existing, getErr := cmdbService.GetAsset(targetID)
		input := cmdb.CreateAssetInput{ID: targetID, Name: system.Name, Type: "physical-server", Status: "online", Environment: "待确认", ProjectGroup: "自动发现", Owner: "待分配", Location: "BMC 自动发现", Source: "Redfish", Tags: []string{"Redfish", "BMC", "带外管理"}, Attributes: []cmdb.Attribute{{Name: "bmc_address", Label: "带外管理地址", Value: address}, {Name: "bmc_manufacturer", Label: "BMC 厂商", Value: system.Manufacturer}, {Name: "bmc_model", Label: "BMC 型号", Value: system.Model}, {Name: "bmc_serial", Label: "序列号", Value: system.SerialNumber}, {Name: "bmc_power_state", Label: "电源状态", Value: system.PowerState}, {Name: "bmc_system_id", Label: "Redfish 系统 ID", Value: system.ID}}}
		if getErr == nil {
			input.Name, input.Type, input.Status, input.IP = existing.Name, existing.Type, existing.Status, existing.IP
			input.Environment, input.ProjectGroup, input.Owner, input.Location = existing.Environment, existing.ProjectGroup, existing.Owner, existing.Location
			input.Tags = append(input.Tags, existing.Tags...)
			input.Attributes = append(existing.Attributes, input.Attributes...)
			input.Relations = existing.Relations
		} else if strings.TrimSpace(input.Name) == "" {
			input.Name = fmt.Sprintf("BMC-%s", system.ID)
		}
		asset, err := cmdbService.UpsertExternalAsset(input, operator)
		if err != nil {
			return assets, err
		}
		assets = append(assets, asset)
	}
	return assets, nil
}
