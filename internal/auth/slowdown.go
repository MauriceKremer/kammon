// slowdown.go keeps failed logins from one address from hammering the login page (MON-AX-6).
package auth

import (
	"sync"
	"time"
)

// Slowdown remembers failed logins per remote address. After 5 failures within one minute,
// every further attempt is answered after a 2 second pause. The tracked addresses are a bounded
// set, like every other buffer in KAMmon: expired entries go first, and a hard cap drops the
// address whose last failure is oldest.
type Slowdown struct {
	mu        sync.Mutex
	m         map[string][]time.Time
	maxAddrs  int
	window    time.Duration
	delay     time.Duration
	threshold int
}

// NewSlowdown returns the slowdown with the values from Access.md MON-AX-6.
func NewSlowdown() *Slowdown {
	return &Slowdown{
		m:         map[string][]time.Time{},
		maxAddrs:  4096,
		window:    time.Minute,
		delay:     2 * time.Second,
		threshold: 5,
	}
}

// Delay reports how long an attempt from addr must wait before answering, without recording
// the attempt: an unanswered guess is not evidence of anything.
func (s *Slowdown) Delay(addr string, now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.housekeep(addr, now)
	if len(s.m[addr]) >= s.threshold {
		return s.delay
	}
	return 0
}

// Fail records one failed login.
func (s *Slowdown) Fail(addr string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.housekeep(addr, now)
	cur := s.m[addr]
	if len(cur) == 2*s.threshold { // keep the memory per address bounded too
		cur = cur[len(cur)-s.threshold+1:]
	}
	s.m[addr] = append(cur, now)
	if len(s.m) > s.maxAddrs {
		s.dropOldest()
	}
}

// housekeep drops the timed-out entries of one address and dead addresses in the window.
func (s *Slowdown) housekeep(addr string, now time.Time) {
	cur := s.m[addr][:0]
	for _, at := range s.m[addr] {
		if now.Sub(at) < s.window {
			cur = append(cur, at)
		}
	}
	if len(cur) == 0 {
		delete(s.m, addr)
	} else {
		s.m[addr] = cur
	}
}

func (s *Slowdown) dropOldest() {
	oldest, oldestAt := "", time.Time{}
	for addr, times := range s.m {
		last := times[len(times)-1]
		if oldestAt.IsZero() || last.Before(oldestAt) {
			oldest, oldestAt = addr, last
		}
	}
	if oldest != "" {
		delete(s.m, oldest)
	}
}

// TrackedAddresses is how many addresses the slowdown can remember; a test helper and honesty
// about the bound.
func (s *Slowdown) TrackedAddresses() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}
