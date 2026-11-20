package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kammon.bytecoder.nl/internal/config"
)

func TestWebUsersAdminAccess(t *testing.T) {
	h := newHarness(t)

	// Viewer receives 403 (MON-WA-1)
	vClient := h.login(t, "viewer", "viewer")
	status, _, _ := get(t, vClient, h.ts.URL+"/web-users")
	if status != http.StatusForbidden {
		t.Fatalf("viewer on /web-users: got %d, want 403", status)
	}

	// Admin can access
	aClient := h.login(t, "admin", "admin")
	status, body, _ := get(t, aClient, h.ts.URL+"/web-users")
	if status != http.StatusOK {
		t.Fatalf("admin on /web-users: got %d, want 200", status)
	}
	if !strings.Contains(body, "Web users") || !strings.Contains(body, "admin") {
		t.Errorf("expected Web users table in page")
	}
}

func TestWebUsersCRUD(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "kammon.yml")
	origYAML := `listen: ":8080"
users:
  - { name: admin, password: "adminpassword", role: admin }
`
	if err := os.WriteFile(cfgPath, []byte(origYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	srv, err := New(Deps{Config: cfg, ConfigPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Add user "alice" as viewer
	err = srv.userMgr.save("", "alice", config.RoleViewer, "alicepass")
	if err != nil {
		t.Fatalf("save new user failed: %v", err)
	}

	users := srv.userMgr.list()
	if len(users) != 2 {
		t.Fatalf("user count = %d, want 2", len(users))
	}

	// Check password hash format (MON-WA-4)
	var alice config.User
	for _, u := range users {
		if u.Name == "alice" {
			alice = u
			break
		}
	}
	if !strings.HasPrefix(alice.Password, "$pbkdf2-sha256$") {
		t.Errorf("alice password should be hashed with pbkdf2, got: %s", alice.Password)
	}

	// 2. Reject update removing last admin (MON-WA-7)
	err = srv.userMgr.delete("admin")
	if err == nil || !strings.Contains(err.Error(), "at least one administrator") {
		t.Fatalf("delete last admin should fail, got: %v", err)
	}

	// 3. Edit alice: rename to bob and change role to admin
	err = srv.userMgr.save("alice", "bob", config.RoleAdmin, "")
	if err != nil {
		t.Fatalf("edit alice failed: %v", err)
	}

	var bob config.User
	for _, u := range srv.userMgr.list() {
		if u.Name == "bob" {
			bob = u
			break
		}
	}
	if bob.Role != config.RoleAdmin || bob.Password != alice.Password {
		t.Errorf("bob should have admin role and keep existing password hash, got: %+v", bob)
	}

	// 4. Delete bob
	err = srv.userMgr.delete("bob")
	if err != nil {
		t.Fatalf("delete bob failed: %v", err)
	}
	if len(srv.userMgr.list()) != 1 {
		t.Fatalf("user count after delete = %d, want 1", len(srv.userMgr.list()))
	}
}

func TestWebUsersSaveEndpointValidation(t *testing.T) {
	h := newHarness(t)
	aClient := h.login(t, "admin", "admin")

	// Post without HX-Request / Origin from external client or invalid fields
	req, _ := http.NewRequest(http.MethodPost, h.ts.URL+"/ui/web-users/save", strings.NewReader(url.Values{
		"name":     {""},
		"role":     {"viewer"},
		"password": {"pass"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Origin", h.ts.URL)

	resp, err := aClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 for invalid username", resp.StatusCode)
	}
}

// TestBlankPasswordPreservesHash verifies MON-WA-10: editing a user with blank password leaves existing hash unchanged.
func TestBlankPasswordPreservesHash(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "kammon.yml")
	orig := `listen: ":8080"
users:
  - { name: admin, password: "oldpass", role: admin }
`
	if err := os.WriteFile(cfgPath, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	srv, err := New(Deps{Config: cfg, ConfigPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}

	// Get original password hash
	originalHash := srv.userMgr.list()[0].Password

	// Edit admin: change role to admin (no-op) and leave password blank
	err = srv.userMgr.save("admin", "admin", config.RoleAdmin, "")
	if err != nil {
		t.Fatalf("edit with blank password failed: %v", err)
	}

	// Verify password hash unchanged
	updatedHash := srv.userMgr.list()[0].Password
	if updatedHash != originalHash {
		t.Errorf("password hash changed on blank edit: got %q, want %q", updatedHash, originalHash)
	}
}

// TestAdminSelfEditDoesNotRevokeOwnSession verifies MON-WA-11:
// when an admin edits their own credentials, their session is not revoked.
func TestAdminSelfEditDoesNotRevokeOwnSession(t *testing.T) {
	h := newHarness(t)
	aClient := h.login(t, "admin", "admin")

	// Admin edits their own password
	req, _ := http.NewRequest(http.MethodPost, h.ts.URL+"/ui/web-users/save", strings.NewReader(url.Values{
		"orig_name": {"admin"},
		"name":      {"admin"},
		"role":      {"admin"},
		"password":  {"newadminpass"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Origin", h.ts.URL)

	resp, err := aClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// After password change, admin should still be able to access admin-only pages with same session.
	// Try accessing the web-users page again (requires valid session).
	status, body, _ := get(t, aClient, h.ts.URL+"/web-users")
	if status != http.StatusOK {
		t.Errorf("admin session revoked after self-edit: got %d, want 200", status)
	}
	if !strings.Contains(body, "admin") {
		t.Errorf("expected admin user in list after self-edit")
	}
}
