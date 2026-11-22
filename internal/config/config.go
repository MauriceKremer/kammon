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

// Poll holds how often the poller's reads run (docs/requirements/DataStore.md §5), each read's
// timeout (MON-DS-2), and how long the poller stops asking a read that KAM answers with 404.
// Durations are Go durations, like 500ms, 2s or 30s.
type Poll struct {
	Events    time.Duration `yaml:"events"`
	Status    time.Duration `yaml:"status"`
	Rooms     time.Duration `yaml:"rooms"`
	Channels  time.Duration `yaml:"channels"`
	Adverts   time.Duration `yaml:"adverts"`
	Scripts   time.Duration `yaml:"scripts"`
	Timeout   time.Duration `yaml:"timeout"`
	NotServed time.Duration `yaml:"not_served"`
}

// Every maps the poller's read names to their intervals.
func (p Poll) Every() map[string]time.Duration {
	return map[string]time.Duration{
		"events":   p.Events,
		"status":   p.Status,
		"rooms":    p.Rooms,
		"channels": p.Channels,
		"adverts":  p.Adverts,
		"scripts":  p.Scripts,
	}
}

// Freshness holds when a shown snapshot stops counting as fresh and when it counts as down
// (MON-DS-13). Between the two limits the pages label the data stale.
type Freshness struct {
	StaleAfter time.Duration `yaml:"stale_after"`
	DownAfter  time.Duration `yaml:"down_after"`
}

// Location is where this KAM node stands: latitude and longitude in degrees. It is optional and
// only places advert nodes that do not advertise a position, by their hop distance. Both keys go
// together.
type Location struct {
	Latitude  *float64 `yaml:"latitude"`
	Longitude *float64 `yaml:"longitude"`
}

type Config struct {
	Listen             string    `yaml:"listen"`
	KamMonitorURL      string    `yaml:"kam_monitor_url"`
	Poll               Poll      `yaml:"poll"`
	Freshness          Freshness `yaml:"freshness"`
	SessionIdleHours   int       `yaml:"session_idle_hours"`
	ViewerTextChannels []string  `yaml:"viewer_text_channels"`
	Users              []User    `yaml:"users"`
	Location           *Location `yaml:"location"`
	TLSCert            string    `yaml:"tls_cert"` // optional: serve HTTPS directly (MON-AX-21)
	TLSKey             string    `yaml:"tls_key"`
}

// SessionIdle is the idle timeout of a login session.
func (c Config) SessionIdle() time.Duration {
	return time.Duration(c.SessionIdleHours) * time.Hour
}

func defaults() Config {
	return Config{
		Listen:           ":8080",
		KamMonitorURL:    "http://127.0.0.1:8766",
		Poll:             Poll{Events: time.Second, Status: 2 * time.Second, Rooms: 10 * time.Second, Channels: 30 * time.Second, Adverts: 60 * time.Second, Scripts: 10 * time.Second, Timeout: 3 * time.Second, NotServed: 10 * time.Minute},
		Freshness:        Freshness{StaleAfter: 10 * time.Second, DownAfter: 2 * time.Minute}, // store's FreshFor/StaleFor (MON-DS-13)
		SessionIdleHours: 8,
	}
}

// Demo returns the configuration used by -demo. It listens on loopback only.
func Demo() Config {
	cfg := defaults()
	cfg.Listen = "127.0.0.1:8080"
	cfg.ViewerTextChannels = []string{"#public"}
	latitude, longitude := 52.7792, 6.9069 // Emmen
	cfg.Location = &Location{Latitude: &latitude, Longitude: &longitude}
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
	for _, raw := range []struct{ name, value string }{{"kam_monitor_url", c.KamMonitorURL}} {
		u, err := url.Parse(raw.value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%s must be an http or https URL, got %q", raw.name, raw.value)
		}
	}
	if c.SessionIdleHours < 1 {
		return errors.New("session_idle_hours must be at least 1")
	}
	for _, p := range []struct {
		name string
		d    time.Duration
	}{
		{"poll.events", c.Poll.Events},
		{"poll.status", c.Poll.Status},
		{"poll.rooms", c.Poll.Rooms},
		{"poll.channels", c.Poll.Channels},
		{"poll.adverts", c.Poll.Adverts},
		{"poll.scripts", c.Poll.Scripts},
		{"poll.timeout", c.Poll.Timeout},
		{"poll.not_served", c.Poll.NotServed},
		{"freshness.stale_after", c.Freshness.StaleAfter},
		{"freshness.down_after", c.Freshness.DownAfter},
	} {
		if p.d <= 0 {
			return fmt.Errorf("%s must be a positive duration, like 1s, got %v", p.name, p.d)
		}
	}
	if c.Freshness.DownAfter <= c.Freshness.StaleAfter {
		return errors.New("freshness.down_after must be longer than freshness.stale_after")
	}
	if c.Location != nil {
		if c.Location.Latitude == nil || c.Location.Longitude == nil {
			return errors.New("location needs both latitude and longitude")
		}
		if *c.Location.Latitude < -90 || *c.Location.Latitude > 90 {
			return fmt.Errorf("location latitude must be between -90 and 90, got %v", *c.Location.Latitude)
		}
		if *c.Location.Longitude < -180 || *c.Location.Longitude > 180 {
			return fmt.Errorf("location longitude must be between -180 and 180, got %v", *c.Location.Longitude)
		}
	}
	switch {
	case c.TLSCert == "" && c.TLSKey == "":
		// plain HTTP; a reverse proxy is the other way to HTTPS (MON-AX-20, 21)
	case c.TLSCert != "" && c.TLSKey != "":
		if _, err := os.Stat(c.TLSCert); err != nil {
			return fmt.Errorf("tls_cert: %w", err)
		}
		if _, err := os.Stat(c.TLSKey); err != nil {
			return fmt.Errorf("tls_key: %w", err)
		}
	default:
		return errors.New("tls_cert and tls_key go together, or neither")
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
