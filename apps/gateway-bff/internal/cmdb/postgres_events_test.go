package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

type capturedExec struct {
	query string
	args  []any
}

type captureExecer struct {
	calls []capturedExec
}

func (c *captureExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	c.calls = append(c.calls, capturedExec{query: query, args: append([]any(nil), args...)})
	return fakeResult(1), nil
}

type fakeResult int64

func (r fakeResult) LastInsertId() (int64, error) { return int64(r), nil }
func (r fakeResult) RowsAffected() (int64, error) { return int64(r), nil }

func TestInsertAssetEventsPublishesStatusTransition(t *testing.T) {
	exec := &captureExecer{}
	history := HistoryEntry{
		ID: "hist-status-1", AssetID: "host-1", Action: "updated", Operator: "tester",
		Changes: []Change{{Field: "status", Before: "online", After: "warning"}},
	}
	asset := Asset{ID: "host-1", Name: "host-1", Type: "virtual-machine", Status: "warning", IP: "10.1.1.1", Source: "manual"}
	if err := insertAssetEvents(context.Background(), exec, asset, history); err != nil {
		t.Fatalf("insertAssetEvents: %v", err)
	}
	if len(exec.calls) != 3 {
		t.Fatalf("expected change, audit and status events, got %d", len(exec.calls))
	}
	topics := map[string][]byte{}
	for _, call := range exec.calls {
		topic, ok := call.args[1].(string)
		if !ok {
			t.Fatalf("unexpected topic argument: %#v", call.args)
		}
		payload, ok := call.args[3].([]byte)
		if !ok {
			t.Fatalf("unexpected payload argument: %#v", call.args)
		}
		topics[topic] = payload
	}
	statusPayload, ok := topics["cmdb.asset.status.v1"]
	if !ok {
		t.Fatal("missing cmdb.asset.status.v1 event")
	}
	var envelope struct {
		EventType string `json:"eventType"`
		Data      struct {
			AssetID        string `json:"assetId"`
			Status         string `json:"status"`
			PreviousStatus string `json:"previousStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(statusPayload, &envelope); err != nil {
		t.Fatalf("decode status payload: %v", err)
	}
	if envelope.EventType != "cmdb.asset.status.v1" || envelope.Data.AssetID != "host-1" || envelope.Data.Status != "warning" || envelope.Data.PreviousStatus != "online" {
		t.Fatalf("unexpected status event: %+v", envelope)
	}
}

func TestInsertAssetEventsSkipsUnchangedStatus(t *testing.T) {
	exec := &captureExecer{}
	history := HistoryEntry{
		ID: "hist-owner-1", AssetID: "host-1", Action: "updated", Operator: "tester",
		Changes: []Change{{Field: "owner", Before: "old", After: "new"}},
	}
	asset := Asset{ID: "host-1", Name: "host-1", Type: "virtual-machine", Status: "online", IP: "10.1.1.1"}
	if err := insertAssetEvents(context.Background(), exec, asset, history); err != nil {
		t.Fatalf("insertAssetEvents: %v", err)
	}
	if len(exec.calls) != 2 {
		t.Fatalf("expected only change and audit events, got %d", len(exec.calls))
	}
}

func TestHistoryStatusChangeRecognizesCreatedAsset(t *testing.T) {
	previous, changed := historyStatusChange(HistoryEntry{Action: "created"})
	if !changed || previous != "" {
		t.Fatalf("expected created asset status transition, got previous=%q changed=%v", previous, changed)
	}
}
