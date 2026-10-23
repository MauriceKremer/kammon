package auth

import (
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/config"
)

var users = []config.User{
	{Name: "maurice", Password: "pw1", Role: config.RoleAdmin},
	{Name: "observer", Password: "pw2", Role: config.RoleViewer},
}

func TestCheck(t *testing.T) {
	if u, ok := Check(users, "maurice", "pw1"); !ok || u.Role != config.RoleAdmin {
		t.Fatalf("good login: %+v %v", u, ok)
	}
	for _, c := range [][2]string{{"maurice", "pw2"}, {"nobody", "pw1"}, {"", ""}, {"observer", "pw1"}} {
		if _, ok := Check(users, c[0], c[1]); ok {
			t.Errorf("%v must not log in", c)
		}
	}
}

func TestSessionsLifecycle(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	s := NewSessions(time.Hour, func() time.Time { return now })
	id := s.Create(users[0])
	if len(id) != 32 {
		t.Fatalf("id = %q", id)
	}
	if u, ok := s.Get(id); !ok || u.Name != "maurice" {
		t.Fatal("fresh session must resolve")
	}
	now = now.Add(50 * time.Minute)
	if _, ok := s.Get(id); !ok {
		t.Fatal("use must extend the idle timer")
	}
	now = now.Add(50 * time.Minute)
	if _, ok := s.Get(id); !ok {
		t.Fatal("extended session must still be live")
	}
	now = now.Add(61 * time.Minute)
	if _, ok := s.Get(id); ok {
		t.Fatal("idle session must expire")
	}
	id2 := s.Create(users[1])
	s.Delete(id2)
	if _, ok := s.Get(id2); ok {
		t.Fatal("deleted session must be gone")
	}
}

func TestSessionsAreBounded(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	s := NewSessions(time.Hour, func() time.Time { now = now.Add(time.Second); return now })
	for i := 0; i < maxSessions+50; i++ {
		s.Create(users[1])
	}
	if len(s.m) > maxSessions {
		t.Fatalf("sessions = %d", len(s.m))
	}
}
