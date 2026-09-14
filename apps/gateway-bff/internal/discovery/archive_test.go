package discovery

import "testing"

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
