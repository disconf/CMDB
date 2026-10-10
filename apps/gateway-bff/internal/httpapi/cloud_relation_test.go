package httpapi

import (
	"context"
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
)

func TestEnsureCloudRegionRelationCreatesParentAssets(t *testing.T) {
	service := cmdb.NewService()
	relation, err := ensureCloudRegionRelation(context.Background(), service, "cloud-relation-test", "region-a", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if relation.Type != "located-in" || relation.TargetID != "cloud-region-cloud-relation-test-region-a" {
		t.Fatalf("unexpected relation: %#v", relation)
	}
	region, err := service.GetAsset(relation.TargetID)
	if err != nil || region.Type != "cloud-region" {
		t.Fatalf("unexpected region asset: %#v %v", region, err)
	}
	provider, err := service.GetAsset("cloud-provider-cloud-relation-test")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range provider.Relations {
		if item.Type == "contains" && item.TargetID == relation.TargetID {
			found = true
		}
	}
	if !found {
		t.Fatalf("provider relation missing: %#v", provider.Relations)
	}
}

func TestMergeRelationAvoidsDuplicates(t *testing.T) {
	relation := cmdb.Relation{Type: "contains", TargetID: "region-1"}
	items := mergeRelation([]cmdb.Relation{relation}, relation)
	if len(items) != 1 {
		t.Fatalf("expected duplicate relation to be ignored: %#v", items)
	}
}
