package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kammon.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAppliesDefaultsAndNormalizesChannels(t *testing.T) {
	cfg, err := Load(write(t, "viewer_text_channels: [Public, \"#Makers\"]\nusers:\n  - {name: a, password: p, role: admin}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8080" || cfg.KamMonitorURL != "http://127.0.0.1:8766" || cfg.SessionIdle().Hours() != 8 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if got := strings.Join(cfg.ViewerTextChannels, ","); got != "#public,#makers" {
		t.Fatalf("channels = %q", got)
	}
}

func TestLoadRejectsBadConfig(t *testing.T) {
	cases := map[string]string{
		"no users":       "listen: ':1'\n",
		"bad role":       "users:\n  - {name: a, password: p, role: root}\n",
		"empty password": "users:\n  - {name: a, password: '', role: viewer}\n",
		"duplicate name": "users:\n  - {name: a, password: p, role: viewer}\n  - {name: a, password: q, role: admin}\n",
		"bad url":        "kam_monitor_url: not-a-url\nusers:\n  - {name: a, password: p, role: viewer}\n",
		"zero idle":      "session_idle_hours: 0\nusers:\n  - {name: a, password: p, role: viewer}\n",
	}
	for name, body := range cases {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestUserLogValueHidesPassword(t *testing.T) {
	v := User{Name: "a", Password: "secret", Role: RoleAdmin}.LogValue()
	if strings.Contains(v.String(), "secret") {
		t.Fatalf("password leaked: %s", v)
	}
}

func TestExampleConfigLoads(t *testing.T) {
	if _, err := Load("../../kammon.yml"); err != nil {
		t.Fatal(err)
	}
}

// MON-AX-21: optional TLS needs both halves and readable files.
func TestTLSConfigPairs(t *testing.T) {
	write := func(name, content string) string {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cert := write("cert.pem", "fake")
	key := write("key.pem", "fake")
	missing := filepath.Join(t.TempDir(), "missing.pem")

	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"plain http", "users: [{name: a, password: p, role: viewer}]", ""},
		{"cert without key", "tls_cert: " + missing + "\nusers: [{name: a, password: p, role: viewer}]", "go together"},
		{"key without cert", "tls_key: " + missing + "\nusers: [{name: a, password: p, role: viewer}]", "go together"},
		{"missing cert file", "tls_cert: " + missing + "\ntls_key: " + key + "\nusers: [{name: a, password: p, role: viewer}]", "tls_cert:"},
		{"missing key file", "tls_cert: " + cert + "\ntls_key: " + missing + "\nusers: [{name: a, password: p, role: viewer}]", "tls_key:"},
		{"valid pair", "tls_cert: " + cert + "\ntls_key: " + key + "\nusers: [{name: a, password: p, role: viewer}]", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "kammon.yml")
			if err := os.WriteFile(path, []byte(c.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("%s: %v", c.name, err)
			case c.want == "" && cfg.TLSCert != cert && c.name == "valid pair":
				t.Fatalf("%s: cert not kept", c.name)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("%s: err = %v, want it to contain %q", c.name, err, c.want)
			}
		})
	}
}
