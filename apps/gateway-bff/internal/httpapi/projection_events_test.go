package httpapi

import "testing"

func TestAssetEventTargetReadsChangedAsset(t *testing.T) {
	payload := []byte(`{"eventType":"cmdb.asset.changed.v1","data":{"action":"updated","asset":{"id":"k8s-pod/ops-qa/gateway-abc","name":"gateway"}}}`)
	identifier, ok := assetEventTarget("cmdb.asset.changed.v1", payload)
	if !ok || identifier != "k8s-pod/ops-qa/gateway-abc" {
		t.Fatalf("unexpected target %q ok=%v", identifier, ok)
	}
}

func TestAssetEventTargetReadsStatusAsset(t *testing.T) {
	payload := []byte(`{"eventType":"cmdb.asset.status.v1","data":{"assetId":"host-node01.jzq.com","status":"offline"}}`)
	identifier, ok := assetEventTarget("cmdb.asset.status.v1", payload)
	if !ok || identifier != "host-node01.jzq.com" {
		t.Fatalf("unexpected target %q ok=%v", identifier, ok)
	}
}

func TestAssetEventTargetIgnoresUnrelatedTopicsAndPayloads(t *testing.T) {
	if _, ok := assetEventTarget("audit.operation.created.v1", []byte(`{"data":{"assetId":"host-1"}}`)); ok {
		t.Fatal("audit events must not trigger projection")
	}
	if _, ok := assetEventTarget("cmdb.asset.changed.v1", []byte(`not-json`)); ok {
		t.Fatal("unparsable payloads must be skipped")
	}
	if _, ok := assetEventTarget("cmdb.asset.changed.v1", []byte(`{"data":{"asset":{"id":"   "}}}`)); ok {
		t.Fatal("blank identifiers must be skipped")
	}
}
