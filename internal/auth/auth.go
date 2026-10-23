package auth

// Package auth holds KAMmon's users, sessions and the login slowdown. KAM has no part in any of it.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"

	"kammon.bytecoder.nl/internal/config"
)

const maxSessions = 1000

// Check compares a name and password with every configured user in constant time (MON-AX-5).
// A wrong name and a wrong password are indistinguishable to the caller.
func Check(users []config.User, name, password string) (config.User, bool) {
	var found config.User
	ok := 0
	for _, u := range users {
		nameOK := subtle.ConstantTimeCompare([]byte(u.Name), []byte(name))
		passOK := subtle.ConstantTimeCompare([]byte(u.Password), []byte(password))
		if nameOK&passOK == 1 {
			found = u
		}
		ok |= nameOK & passOK
	}
	return found, ok == 1
}

type session struct {
	user    config.User
	expires time.Time
}

// Sessions lives in memory; a restart ends every session (MON-AX-4).
type Sessions struct {
	mu   sync.Mutex
	idle time.Duration
	now  func() time.Time
	m    map[string]session
}

func NewSessions(idle time.Duration, now func() time.Time) *Sessions {
	return &Sessions{idle: idle, now: now, m: map[string]session{}}
}

// Create starts a session and returns its random 128-bit id (MON-AX-3).
func (s *Sessions) Create(u config.User) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	id := hex.EncodeToString(raw[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, v := range s.m {
		if !now.Before(v.expires) {
			delete(s.m, k)
		}
	}
	if len(s.m) >= maxSessions {
		var oldest string
		for k, v := range s.m {
			if oldest == "" || v.expires.Before(s.m[oldest].expires) {
				oldest = k
			}
		}
		delete(s.m, oldest)
	}
	s.m[id] = session{user: u, expires: now.Add(s.idle)}
	return id
}

// Get returns the user of a live session and restarts its idle timer.
func (s *Sessions) Get(id string) (config.User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[id]
	if !ok || !s.now().Before(v.expires) {
		delete(s.m, id)
		return config.User{}, false
	}
	v.expires = s.now().Add(s.idle)
	s.m[id] = v
	return v.user, true
}

func (s *Sessions) Delete(id string) {
	s.mu.Lock()
	delete(s.m, id)
	s.mu.Unlock()
}

// RemoteHost is the host part of the request's connection address — the key for per-address
// settings such as the login slowdown (MON-AX-6). A client's changing port is the same address.
func RemoteHost(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
