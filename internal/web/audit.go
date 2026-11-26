package web

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"

	"kammon.bytecoder.nl/internal/audit"
	"kammon.bytecoder.nl/internal/auth"
)

// auditChange records an administrator change against the signed-in admin (MON-AT-1..7). It
// returns a warning to show the user when the record could not be written (MON-AT-19), or "".
func (s *Server) auditChange(r *http.Request, action string, target *audit.Target, changes map[string]audit.Change, outcome, detail string) string {
	p := principalOf(r)
	return s.auditAppend(audit.Record{
		At: s.now(), Actor: p.User.Name, Role: string(p.Role),
		Action: action, Target: target, Changes: changes,
		Outcome: outcome, Detail: detail, Remote: auth.RemoteHost(r),
	})
}

// auditSignIn records a sign-in or sign-out (MON-AT-21..25). A failed sign-in has no session,
// so the caller passes the actor and role it knows.
func (s *Server) auditSignIn(r *http.Request, action, actor, role, outcome, detail string) {
	s.auditAppend(audit.Record{
		At: s.now(), Actor: actor, Role: role, Action: action,
		Outcome: outcome, Detail: detail, Remote: auth.RemoteHost(r),
	})
}

// auditAppend writes one record and words a failure for the user (MON-AT-19).
func (s *Server) auditAppend(rec audit.Record) string {
	if err := s.auditLog.Append(rec); err != nil {
		slog.Error("audit trail: could not record event", "action", rec.Action, "actor", rec.Actor, "error", err)
		return "The change happened, but the audit trail could not be written."
	}
	return ""
}

// scriptErrorText words a script-call error for a failed audit record.
func scriptErrorText(err error) string {
	text, _ := kamErrorWords(err)
	return text
}

// sourceFingerprint is the shortened SHA-256 of a script's source. The trail stores this, never
// the source itself (MON-AT-6).
func sourceFingerprint(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:8])
}

// consoleWithWarn appends a warning line to a console when the trail could not be written.
func consoleWithWarn(c scriptConsole, warn string) scriptConsole {
	if warn != "" {
		c.Lines = append(c.Lines, consoleLine{Text: warn, Class: "warn"})
	}
	return c
}
