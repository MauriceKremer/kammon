package auth

// Package auth holds KAMmon's users, sessions and the local-request check. KAM has no part in any of it.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
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

// IsLocal reports whether a request comes from this computer: a loopback connection address and no
// proxy header at all. Any forwarding header — including one with an empty value — means a proxy is
// in the path, and a present-but-empty header must not be mistaken for “no header” (MON-AX-9).
func IsLocal(r *http.Request) bool {
	for _, h := range []string{"X-Forwarded-For", "X-Real-IP", "Forwarded"} {
		if _, present := r.Header[http.CanonicalHeaderKey(h)]; present {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsLoopback()
}

// EffectiveRole is checked on every request, not once at login (MON-AX-8): an admin away from this computer is a viewer.
func EffectiveRole(u config.User, r *http.Request) config.Role {
	if u.Role == config.RoleAdmin && IsLocal(r) {
		return config.RoleAdmin
	}
	return config.RoleViewer
}
