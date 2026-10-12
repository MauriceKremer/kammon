package auth

import (
	"fmt"
	"testing"
	"time"
)

// MON-AX-6: after 5 failures in a minute, every further attempt answers after 2 seconds.
func TestSlowdownAfterThreshold(t *testing.T) {
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	s := NewSlowdown()
	for i := 0; i < 4; i++ {
		s.Fail("192.0.2.9:40000", now)
	}
	if d := s.Delay("192.0.2.9:40000", now); d != 0 {
		t.Fatalf("4 failures: delay %v, want none", d)
	}
	s.Fail("192.0.2.9:40000", now)
	if d := s.Delay("192.0.2.9:40000", now); d != 2*time.Second {
		t.Fatalf("5 failures: delay %v, want 2 s", d)
	}
	// A different address is unaffected.
	if d := s.Delay("192.0.2.9:40001", now); d != 0 {
		t.Fatalf("another address: delay %v, want none", d)
	}
	// The window slides: a minute after the failures the address is clean again.
	later := now.Add(61 * time.Second)
	if d := s.Delay("192.0.2.9:40000", later); d != 0 {
		t.Fatalf("after the window: delay %v, want none", d)
	}
	// An unanswered guess is not recorded by Delay itself.
	s2 := NewSlowdown()
	for i := 0; i < 10; i++ {
		_ = s2.Delay("192.0.2.9:40002", now) // polling must not count as failures
	}
	if s2.TrackedAddresses() != 0 {
		t.Fatalf("Delay recorded addresses: %d", s2.TrackedAddresses())
	}
}

// The address set is bounded, like every store buffer (MON-DS-18).
func TestSlowdownBounded(t *testing.T) {
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	s := NewSlowdown()
	for i := 0; i < s.maxAddrs+2000; i++ {
		s.Fail(addrFor(i), now)
	}
	if got := s.TrackedAddresses(); got > s.maxAddrs {
		t.Fatalf("tracked = %d, beyond the cap %d", got, s.maxAddrs)
	}
	// A newer failure keeps its address; the one dropped is the oldest, not a fresh attacker.
	recent := addrFor(s.maxAddrs)
	for i := 0; i < 5; i++ {
		s.Fail(recent, now.Add(time.Second))
	}
	if d := s.Delay(recent, now.Add(time.Second)); d != 2*time.Second {
		t.Fatal("the freshly slowed attack address survived the cap")
	}
}

func addrFor(i int) string {
	return fmt.Sprintf("192.0.2.%d:%d", i/65536, i%65536)
}
