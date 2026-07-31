package monitor

import (
	"encoding/json"
	"testing"
)

func TestNotificationTaskCodec(t *testing.T) {
	task := NotificationTask{EventID: "event-1", EventType: notificationTopic, Alert: Alert{ID: "alert-1", Title: "Host down"}, ChannelID: "channel-1"}
	payload, _ := json.Marshal(task)
	decoded, err := decodeNotificationTask(payload)
	if err != nil || decoded.Alert.ID != "alert-1" || decoded.ChannelID != "channel-1" {
		t.Fatalf("unexpected task: %+v %v", decoded, err)
	}
}

func TestNotificationTaskRejectsWrongEventType(t *testing.T) {
	if _, err := decodeNotificationTask([]byte(`{"eventId":"event-1","eventType":"wrong"}`)); err == nil {
		t.Fatal("expected invalid event type")
	}
}

func TestDeadLetterEnvelopePreservesOriginalTask(t *testing.T) {
	task := NotificationTask{EventID: "original", EventType: notificationTopic, Alert: Alert{ID: "alert-1", Title: "Database down"}, ChannelID: "channel-1"}
	payload, _ := json.Marshal(deadLetterEnvelope{EventID: "dlq-1", EventType: notificationDLQTopic, Original: task, Error: "timeout"})
	var decoded deadLetterEnvelope
	if err := json.Unmarshal(payload, &decoded); err != nil || decoded.Original.EventID != "original" || decoded.Original.ChannelID != "channel-1" {
		t.Fatalf("unexpected envelope: %+v %v", decoded, err)
	}
}
