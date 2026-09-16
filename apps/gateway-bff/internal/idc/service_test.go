package idc

import "testing"

func TestServiceWithoutDBReportsNoDB(t *testing.T) {
	s := NewService()
	if _, err := s.ListRooms(); err == nil {
		t.Fatal("expected ErrNoDB when database is not configured")
	}
	if _, err := s.CreateRoom(CreateRoom{Name: "x"}); err == nil {
		t.Fatal("expected ErrNoDB")
	}
}

func TestParseUPositionSupportsSingleAndRanges(t *testing.T) {
	start, height := parseUPosition("12")
	if start != 12 || height != 1 {
		t.Fatalf("unexpected single U: %d/%d", start, height)
	}
	start, height = parseUPosition("10-13")
	if start != 10 || height != 4 {
		t.Fatalf("unexpected ranged U: %d/%d", start, height)
	}
}
