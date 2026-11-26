package web

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"kammon.bytecoder.nl/internal/audit"
	"kammon.bytecoder.nl/internal/kamfake"
)

// auditForm calls a change route the way the browser's htmx does (Origin + HX-Request, MON-AX-18).
func auditForm(t *testing.T, h *harness, c *http.Client, method, path string, form url.Values) {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", h.ts.URL)
	req.Header.Set("HX-Request", "true")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func newestRecord(t *testing.T, l *audit.Log) audit.Record {
	t.Helper()
	recs := l.Recent(1)
	if len(recs) == 0 {
		t.Fatal("no audit record was written")
	}
	return recs[0]
}

// TestAuditRecordsWebUserChanges checks MON-AT-1, 4 and 5 for the web-user actions.
func TestAuditRecordsWebUserChanges(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "admin", "admin")

	// Create.
	auditForm(t, h, c, http.MethodPost, "/ui/web-users/save", url.Values{
		"name": {"alice"}, "role": {"viewer"}, "password": {"s3cret-pass"},
	})
	rec := newestRecord(t, h.s.auditLog)
	if rec.Action != "user.create" || rec.Actor != "admin" || rec.Outcome != audit.OK {
		t.Fatalf("create record = %+v", rec)
	}
	if rec.Target == nil || rec.Target.Type != "user" || rec.Target.Name != "alice" {
		t.Fatalf("target = %+v", rec.Target)
	}
	if got := rec.Changes["password"].To; got != "<set>" {
		t.Errorf("password change = %v, want <set>", got)
	}
	if got := rec.Changes["role"].To; got != "viewer" {
		t.Errorf("role change = %v, want viewer", got)
	}

	// Edit: role viewer -> admin, password left blank.
	auditForm(t, h, c, http.MethodPost, "/ui/web-users/save", url.Values{
		"orig_name": {"alice"}, "name": {"alice"}, "role": {"admin"}, "password": {""},
	})
	rec = newestRecord(t, h.s.auditLog)
	if rec.Action != "user.update" {
		t.Fatalf("update action = %q, want user.update", rec.Action)
	}
	if rec.Changes["role"].From != "viewer" || rec.Changes["role"].To != "admin" {
		t.Errorf("role change = %+v, want viewer -> admin", rec.Changes["role"])
	}
	if _, ok := rec.Changes["password"]; ok {
		t.Error("a blank password must not be recorded")
	}
	if _, ok := rec.Changes["name"]; ok {
		t.Error("an unchanged name must not be recorded")
	}

	// Delete.
	auditForm(t, h, c, http.MethodDelete, "/ui/web-users?name=alice", nil)
	rec = newestRecord(t, h.s.auditLog)
	if rec.Action != "user.delete" || rec.Target == nil || rec.Target.Name != "alice" {
		t.Fatalf("delete record = %+v", rec)
	}

	// No password or hash may appear anywhere in the trail (MON-AT-5).
	for _, r := range h.s.auditLog.Recent(0) {
		blob := r.Detail + fmt.Sprint(r.Changes)
		for _, secret := range []string{"s3cret-pass", "$pbkdf2"} {
			if strings.Contains(blob, secret) {
				t.Errorf("record %q leaks a secret %q", r.Action, secret)
			}
		}
	}
}

// TestAuditRecordsAFailedAction checks MON-AT-2: a refused action is still recorded, marked failed.
func TestAuditRecordsAFailedAction(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "admin", "admin")

	// Deleting the last admin is refused (MON-WA-7).
	auditForm(t, h, c, http.MethodDelete, "/ui/web-users?name=admin", nil)
	rec := newestRecord(t, h.s.auditLog)
	if rec.Action != "user.delete" || rec.Outcome != audit.Failed {
		t.Fatalf("failed delete record = %+v", rec)
	}
	if rec.Detail == "" {
		t.Error("a failed record should say why")
	}
}

// TestAuditRecordsSignIns checks MON-AT-21, 22 and 24 for password sign-in, failure and sign-out.
func TestAuditRecordsSignIns(t *testing.T) {
	h := newHarness(t)

	// Successful password sign-in.
	c := h.login(t, "admin", "admin")
	rec := newestRecord(t, h.s.auditLog)
	if rec.Action != "login.success" || rec.Actor != "admin" || rec.Role != "admin" || rec.Outcome != audit.OK {
		t.Fatalf("success record = %+v", rec)
	}

	// Failed sign-in carries the name that was typed, not the password.
	resp, err := http.PostForm(h.ts.URL+"/login", url.Values{"name": {"admin"}, "password": {"guess"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	rec = newestRecord(t, h.s.auditLog)
	if rec.Action != "login.failed" || rec.Actor != "admin" || rec.Role != "" || rec.Outcome != audit.Failed {
		t.Fatalf("failed record = %+v", rec)
	}
	for _, r := range h.s.auditLog.Recent(0) {
		if strings.Contains(fmt.Sprint(r), "guess") {
			t.Error("the failed sign-in record leaked the attempted password")
		}
	}

	// Sign-out.
	auditForm(t, h, c, http.MethodPost, "/logout", nil)
	rec = newestRecord(t, h.s.auditLog)
	if rec.Action != "logout" || rec.Actor != "admin" {
		t.Fatalf("logout record = %+v", rec)
	}
}

// TestAuditRecordsGuestLogin checks MON-AT-23: a guest sign-in is recorded as Guest, a viewer.
func TestAuditRecordsGuestLogin(t *testing.T) {
	h := newGuestHarness(t, true)
	resp, err := http.PostForm(h.ts.URL+"/login/guest", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	rec := newestRecord(t, h.s.auditLog)
	if rec.Action != "login.guest" || rec.Actor != "Guest" || rec.Role != "viewer" || rec.Outcome != audit.OK {
		t.Fatalf("guest record = %+v", rec)
	}
}

// TestAuditPageNeedsAdmin checks MON-AT-14 and 17: the page is admin-only and says it is permanent.
func TestAuditPageNeedsAdmin(t *testing.T) {
	h := newHarness(t)
	v := h.login(t, "viewer", "viewer")
	if status, _, _ := get(t, v, h.ts.URL+"/audit"); status != http.StatusForbidden {
		t.Fatalf("viewer /audit: %d, want 403", status)
	}
	if status, _, _ := get(t, v, h.ts.URL+"/ui/audit"); status != http.StatusForbidden {
		t.Fatalf("viewer /ui/audit: %d, want 403", status)
	}

	a := h.login(t, "admin", "admin")
	status, body, _ := get(t, a, h.ts.URL+"/audit")
	if status != http.StatusOK {
		t.Fatalf("admin /audit: %d, want 200", status)
	}
	if !strings.Contains(body, "permanent record") {
		t.Error("the page must say the trail is a permanent record (MON-AT-17)")
	}
}

// TestAuditPageShowsRecords checks MON-AT-15 and MON-AT-5 on the page and its partial.
func TestAuditPageShowsRecords(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "admin", "admin")
	auditForm(t, h, c, http.MethodPost, "/ui/web-users/save", url.Values{
		"name": {"carol"}, "role": {"viewer"}, "password": {"z9-topsecret"},
	})

	status, body, _ := get(t, c, h.ts.URL+"/audit")
	if status != http.StatusOK {
		t.Fatalf("admin /audit: %d, want 200", status)
	}
	for _, want := range []string{"user.create", "carol", "login.success", "Showing"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page misses %q", want)
		}
	}
	if strings.Contains(body, "z9-topsecret") {
		t.Error("the page leaked a password (MON-AT-5)")
	}

	// The partial answers "Show more" with the same rows.
	status, partial, _ := get(t, c, h.ts.URL+"/ui/audit?limit=1")
	if status != http.StatusOK || !strings.Contains(partial, `id="audit-table"`) || !strings.Contains(partial, "user.create") {
		t.Fatalf("partial: %d %s", status, partial)
	}
}

// TestAuditRecordsScriptActions checks MON-AT-1, 4 and 6 for the script actions against a fake KAM.
func TestAuditRecordsScriptActions(t *testing.T) {
	h := newScriptHarness(t)
	h.login(t, "admin", "admin")

	// Create.
	h.formPost(t, http.MethodPost, "/ui/script-save", url.Values{
		"id": {""}, "revision": {"0"}, "enabled": {"false"}, "name": {"audit.star"},
		"source": {kamfake.Starter}, "timeout": {"250"},
	})
	rec := newestRecord(t, h.srv.auditLog)
	if rec.Action != "script.create" || rec.Outcome != audit.OK {
		t.Fatalf("create record = %+v", rec)
	}
	if rec.Target == nil || rec.Target.ID != "audit.star" {
		t.Fatalf("create target = %+v", rec.Target)
	}
	if got := rec.Changes["source"].To; got != "<set>" {
		t.Errorf("source change = %v, want <set>", got)
	}

	// Update: rename and change the timeout, same source. Only the changed fields are recorded.
	h.formPost(t, http.MethodPost, "/ui/script-save", url.Values{
		"id": {"audit.star"}, "revision": {"1"}, "enabled": {"false"}, "name": {"audit2.star"},
		"source": {kamfake.Starter}, "timeout": {"900"},
		"orig_name": {"audit.star"}, "orig_timeout": {"250"}, "orig_source_hash": {sourceFingerprint(kamfake.Starter)},
	})
	rec = newestRecord(t, h.srv.auditLog)
	if rec.Action != "script.update" {
		t.Fatalf("update action = %q, want script.update", rec.Action)
	}
	if _, ok := rec.Changes["source"]; ok {
		t.Error("an unchanged source must not be recorded")
	}
	if rec.Changes["name"].From != "audit.star" || rec.Changes["name"].To != "audit2.star" {
		t.Errorf("name change = %+v", rec.Changes["name"])
	}
	if rec.Changes["timeout"].From != "250" || rec.Changes["timeout"].To != "900" {
		t.Errorf("timeout change = %+v", rec.Changes["timeout"])
	}

	// Enable.
	h.formPost(t, http.MethodPost, "/ui/script-toggle", url.Values{
		"id": {"echo"}, "revision": {"4"}, "enabled": {"false"}, "timeout": {"500"},
		"enabled-box": {"on"}, "name": {"echo.star"}, "source": {kamfake.Starter},
	})
	rec = newestRecord(t, h.srv.auditLog)
	if rec.Action != "script.enable" {
		t.Fatalf("enable action = %q, want script.enable", rec.Action)
	}
	if rec.Changes["enabled"].From != false || rec.Changes["enabled"].To != true {
		t.Errorf("enabled change = %+v, want false -> true", rec.Changes["enabled"])
	}

	// Delete.
	h.formPost(t, http.MethodDelete, "/ui/script", url.Values{"id": {"welcome"}, "revision": {"2"}})
	rec = newestRecord(t, h.srv.auditLog)
	if rec.Action != "script.delete" || rec.Target == nil || rec.Target.ID != "welcome" {
		t.Fatalf("delete record = %+v", rec)
	}

	// The trail never stores the source body (MON-AT-6).
	for _, r := range h.srv.auditLog.Recent(0) {
		if strings.Contains(fmt.Sprint(r.Changes), "def on_group_text") {
			t.Errorf("record %q stored the script source", r.Action)
		}
	}
}
