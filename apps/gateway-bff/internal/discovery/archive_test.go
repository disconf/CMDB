package discovery

import (
	"testing"
	"time"
)

func TestRecordingArchiveRoundTrip(t *testing.T) {
	input := TerminalRecording{Events: []RemoteTerminalEvent{{Sequence: 1, Direction: "output", Data: "aGVsbG8=", CreatedAt: "2026-01-01 00:00:00.000"}}}
	content, err := encodeRecordingArchive(input)
	if err != nil {
		t.Fatal(err)
	}
	output, err := decodeRecordingArchive(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Events) != 1 || output.Events[0].Data != "aGVsbG8=" || output.Events[0].Sequence != 1 {
		t.Fatalf("unexpected archive round trip: %+v", output)
	}
}

func TestArchiveSessionLockSerializesSameSession(t *testing.T) {
	service := NewService()
	firstUnlock := service.lockArchiveSession("session-1")
	started := make(chan struct{})
	acquired := make(chan struct{})
	go func() {
		close(started)
		secondUnlock := service.lockArchiveSession("session-1")
		close(acquired)
		secondUnlock()
	}()
	<-started
	select {
	case <-acquired:
		t.Fatal("second archive lock was acquired before the first was released")
	case <-time.After(50 * time.Millisecond):
	}
	firstUnlock()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second archive lock was not released after the first completed")
	}
}
