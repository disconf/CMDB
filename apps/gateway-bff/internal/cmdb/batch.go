package cmdb

import (
	"strings"
)

func (s *Service) BatchUpdateAssets(input BatchUpdateAssetInput, operator string) (BatchUpdateAssetResult, error) {
	ids := uniqueBatchIDs(input.AssetIDs)
	if len(ids) == 0 || len(ids) > 200 {
		return BatchUpdateAssetResult{}, ErrValidation
	}
	result := BatchUpdateAssetResult{Total: len(ids), Items: make([]BatchUpdateAssetItem, 0, len(ids))}
	host, hostErr := s.GetAsset(strings.TrimSpace(input.HostID))
	if input.HostID != "" && (hostErr != nil || (host.Type != "physical-server" && host.Type != "virtualization-cluster")) {
		return BatchUpdateAssetResult{}, ErrValidation
	}
	for _, assetID := range ids {
		item := BatchUpdateAssetItem{AssetID: assetID}
		asset, err := s.GetAsset(assetID)
		if err == nil {
			item.AssetName = asset.Name
		}
		if err != nil {
			item.Error = err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		if !batchServerType(asset.Type) {
			item.Error = "只有物理机、虚拟机和 K8s 节点支持批量配置"
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		next := UpdateAssetInput{Name: asset.Name, Type: asset.Type, Status: asset.Status, IP: asset.IP, Environment: asset.Environment, ProjectGroup: asset.ProjectGroup, Owner: asset.Owner, Location: asset.Location, Tags: append([]string(nil), asset.Tags...), Attributes: append([]Attribute(nil), asset.Attributes...), Relations: append([]Relation(nil), asset.Relations...)}
		if input.Status != "" {
			next.Status = input.Status
		}
		if input.Environment != "" {
			next.Environment = input.Environment
		}
		if input.ProjectGroup != "" {
			next.ProjectGroup = input.ProjectGroup
		}
		if input.Owner != "" {
			next.Owner = input.Owner
		}
		if input.Location != "" {
			next.Location = input.Location
		}
		if len(input.Tags) > 0 {
			if strings.EqualFold(input.TagsMode, "append") {
				next.Tags = mergeBatchTags(next.Tags, input.Tags)
			} else {
				next.Tags = append([]string(nil), input.Tags...)
			}
		}
		if input.HostID != "" && asset.Type == "virtual-machine" {
			relations := make([]Relation, 0, len(next.Relations)+1)
			for _, relation := range next.Relations {
				if relation.Type != "runs-on" {
					relations = append(relations, relation)
				}
			}
			relations = append(relations, Relation{Type: "runs-on", TargetID: host.ID, TargetName: host.Name})
			next.Relations = relations
			if input.Location == "" && strings.TrimSpace(next.Location) == "" {
				next.Location = host.Location
			}
		}
		if _, err = s.UpdateAsset(assetID, next, operator); err != nil {
			item.Error = err.Error()
			result.Failed++
		} else {
			item.Success = true
			result.Succeeded++
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func batchServerType(assetType string) bool {
	return assetType == "physical-server" || assetType == "virtual-machine" || assetType == "k8s-node"
}

func uniqueBatchIDs(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func mergeBatchTags(base, extra []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(base)+len(extra))
	for _, value := range append(append([]string(nil), base...), extra...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
