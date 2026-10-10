package httpapi

import (
	"context"
	"strings"

	"cmdb/gateway-bff/internal/cmdb"
)

func ensureCloudRegionRelation(ctx context.Context, cmdbService *cmdb.Service, provider, region, operator string) (cmdb.Relation, error) {
	_ = ctx
	provider = strings.ToLower(strings.TrimSpace(provider))
	region = strings.TrimSpace(region)
	if region == "" {
		region = "default"
	}
	label := cloudProviderLabel(provider)
	providerID := "cloud-provider-" + safeAssetID(provider)
	regionID := "cloud-region-" + safeAssetID(provider+"-"+region)
	regionName := label + " / " + region

	existingProvider, providerErr := cmdbService.GetAsset(providerID)
	providerRelations := []cmdb.Relation{}
	if providerErr == nil {
		providerRelations = existingProvider.Relations
	}
	providerRelations = mergeRelation(providerRelations, cmdb.Relation{Type: "contains", TargetID: regionID, TargetName: regionName})
	if _, err := cmdbService.UpsertExternalAsset(cmdb.CreateAssetInput{
		ID: providerID, Name: label, Type: "cloud-provider", Status: "online", Environment: "云平台", ProjectGroup: "云平台组", Owner: label,
		Location: label, Source: label, Tags: []string{label, "云平台"},
		Attributes: []cmdb.Attribute{{Name: "cloud_provider", Label: "云厂商", Value: provider}},
		Relations:  providerRelations,
	}, operator); err != nil {
		return cmdb.Relation{}, err
	}
	if _, err := cmdbService.UpsertExternalAsset(cmdb.CreateAssetInput{
		ID: regionID, Name: regionName, Type: "cloud-region", Status: "online", Environment: "云平台", ProjectGroup: "云平台组", Owner: label,
		Location: regionName, Source: label, Tags: []string{label, "地域", region},
		Attributes: []cmdb.Attribute{{Name: "cloud_provider", Label: "云厂商", Value: provider}, {Name: "cloud_region", Label: "地域", Value: region}},
	}, operator); err != nil {
		return cmdb.Relation{}, err
	}
	return cmdb.Relation{Type: "located-in", TargetID: regionID, TargetName: regionName}, nil
}

func mergeRelation(relations []cmdb.Relation, relation cmdb.Relation) []cmdb.Relation {
	for _, item := range relations {
		if item.Type == relation.Type && item.TargetID == relation.TargetID {
			return relations
		}
	}
	return append(relations, relation)
}

func cloudProviderLabel(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "aliyun":
		return "阿里云"
	case "tencent":
		return "腾讯云"
	case "aws":
		return "AWS"
	case "huawei":
		return "华为云"
	case "openstack":
		return "OpenStack"
	default:
		return provider
	}
}
