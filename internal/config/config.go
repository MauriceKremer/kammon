package config

// Package config loads and validates kammon.yml.

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Role string

const (
	RoleViewer Role = "viewer"
	RoleAdmin  Role = "admin"
)

// User is a KAMmon login. Passwords are plain text in kammon.yml, like channel keys in KAM.
type User struct {
	Name     string `yaml:"name"`
	Password string `yaml:"password"`
	Role     Role   `yaml:"role"`
}

// LogValue keeps the password out of logs (MON-AX-16).
func (u User) LogValue() slog.Value {
	return slog.GroupValue(slog.String("name", u.Name), slog.String("role", string(u.Role)))
}

type Config struct {
	Listen             string   `yaml:"listen"`
	KamMonitorURL      string   `yaml:"kam_monitor_url"`
	KamScriptsURL      string   `yaml:"kam_scripts_url"`
	SessionIdleHours   int      `yaml:"session_idle_hours"`
	ViewerTextChannels []string `yaml:"viewer_text_channels"`
	Users              []User   `yaml:"users"`
}

// SessionIdle is the idle timeout of a login session.
func (c Config) SessionIdle() time.Duration {
	return time.Duration(c.SessionIdleHours) * time.Hour
}

func defaults() Config {
	return Config{
		Listen:           ":8080",
		KamMonitorURL:    "http://127.0.0.1:8766",
		KamScriptsURL:    "http://127.0.0.1:8765",
		SessionIdleHours: 8,
	}
}

// Demo returns the configuration used by -demo. It listens on loopback only.
func Demo() Config {
	cfg := defaults()
	cfg.Listen = "127.0.0.1:8080"
	cfg.ViewerTextChannels = []string{"#public"}
	cfg.Users = []User{
		{Name: "admin", Password: "admin", Role: RoleAdmin},
		{Name: "viewer", Password: "viewer", Role: RoleViewer},
	}
	return cfg
}

// Load reads path, applies defaults and validates the result.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := defaults()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.normalize(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) normalize() error {
	for _, raw := range []struct{ name, value string }{{"kam_monitor_url", c.KamMonitorURL}, {"kam_scripts_url", c.KamScriptsURL}} {
		u, err := url.Parse(raw.value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%s must be an http or https URL, got %q", raw.name, raw.value)
		}
	}
	if c.SessionIdleHours < 1 {
		return errors.New("session_idle_hours must be at least 1")
	}
	if len(c.Users) == 0 {
		return errors.New("at least one user is required")
	}
	seen := map[string]bool{}
	for i, u := range c.Users {
		switch {
		case u.Name == "":
			return fmt.Errorf("users[%d]: name is required", i)
		case u.Password == "":
			return fmt.Errorf("users[%d] (%s): password is required", i, u.Name)
		case u.Role != RoleViewer && u.Role != RoleAdmin:
			return fmt.Errorf("users[%d] (%s): role must be viewer or admin, got %q", i, u.Name, u.Role)
		case seen[u.Name]:
			return fmt.Errorf("users[%d]: duplicate name %q", i, u.Name)
		}
		seen[u.Name] = true
	}
	for i, name := range c.ViewerTextChannels {
		c.ViewerTextChannels[i] = NormalizeChannel(name)
	}
	return nil
}

// NormalizeChannel lower-cases a channel name and gives it a leading '#', as KAM does.
func NormalizeChannel(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name != "" && !strings.HasPrefix(name, "#") {
		name = "#" + name
	}
	return name
}
