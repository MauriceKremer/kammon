package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateUsersFile(t *testing.T) {
	origYAML := `# Top comment
listen: ":8080"
kam_monitor_url: "http://127.0.0.1:8766"
viewer_text_channels: ["#public"]

# User section
users:
  - { name: oldadmin, password: "pw", role: admin }
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "kammon.yml")
	if err := os.WriteFile(cfgPath, []byte(origYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	newUsers := []User{
		{Name: "newadmin", Password: "$pbkdf2-sha256$i=1000$abcd$ef01", Role: RoleAdmin},
		{Name: "viewer1", Password: "$pbkdf2-sha256$i=1000$abcd$ef02", Role: RoleViewer},
	}

	if err := UpdateUsersFile(cfgPath, newUsers); err != nil {
		t.Fatalf("UpdateUsersFile failed: %v", err)
	}

	// Verify file can be loaded by Load
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load re-written config failed: %v", err)
	}
	if len(cfg.Users) != 2 {
		t.Fatalf("users count = %d, want 2", len(cfg.Users))
	}
	if cfg.Users[0].Name != "newadmin" || cfg.Users[0].Role != RoleAdmin {
		t.Errorf("unexpected user 0: %+v", cfg.Users[0])
	}
	if cfg.Users[1].Name != "viewer1" || cfg.Users[1].Role != RoleViewer {
		t.Errorf("unexpected user 1: %+v", cfg.Users[1])
	}
	if cfg.Listen != ":8080" || cfg.KamMonitorURL != "http://127.0.0.1:8766" {
		t.Errorf("other settings lost or modified")
	}

	// Read raw text to check comments preservation
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "Top comment") {
		t.Errorf("comments should be preserved where possible")
	}
}

func TestValidateUsersInvariants(t *testing.T) {
	cases := []struct {
		name    string
		users   []User
		wantErr string
	}{
		{"empty list", nil, "at least one user is required"},
		{"empty name", []User{{Name: "", Password: "pw", Role: RoleAdmin}}, "name is required"},
		{"empty password", []User{{Name: "a", Password: "", Role: RoleAdmin}}, "password is required"},
		{"invalid role", []User{{Name: "a", Password: "pw", Role: "guest"}}, "role must be viewer or admin"},
		{"duplicate name", []User{{Name: "a", Password: "pw", Role: RoleAdmin}, {Name: "a", Password: "pw", Role: RoleViewer}}, "duplicate name"},
		{"no admin", []User{{Name: "a", Password: "pw", Role: RoleViewer}}, "at least one administrator is required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUsers(tc.users)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ValidateUsers() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}
