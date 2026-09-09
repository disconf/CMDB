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
