package view

import (
	"strings"
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/config"
	"kammon.bytecoder.nl/internal/kam"
)

var t0 = time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func TestPagesHideAdminItemsFromViewers(t *testing.T) {
	adminOnly := []string{"rooms", "accounts", "automations", "modem"}
	forall := []string{"overview", "traffic", "channels"}
	has := func(role config.Role, key string) bool {
		for _, p := range Pages(role) {
			if p.Key == key {
				return true
			}
		}
		return false
	}
	for _, key := range adminOnly {
		if has(config.RoleViewer, key) {
			t.Errorf("%s must not be offered to a viewer (MON-AX-12)", key)
		}
		if !has(config.RoleAdmin, key) {
			t.Errorf("the administrator still needs %s", key)
		}
	}
	for _, key := range forall {
		if !has(config.RoleViewer, key) {
			t.Errorf("%s is for every role", key)
		}
	}
}

func TestCanReadChannelText(t *testing.T) {
	allow := []string{"#public"}
	if !CanReadChannelText(config.RoleViewer, allow, "#Public") || !CanReadChannelText(config.RoleViewer, allow, "public") {
		t.Error("allow-listed channel must be readable, whatever its spelling")
	}
	if CanReadChannelText(config.RoleViewer, allow, "#emmen") {
		t.Error("other channels are hidden from viewers")
	}
	if !CanReadChannelText(config.RoleAdmin, nil, "#emmen") {
		t.Error("admins read every channel")
	}
}

// Scripts are read for everyone but edited only by admins (MON-AU-4, §6).
func TestCanEditScripts(t *testing.T) {
	if CanEditScripts(config.RoleViewer) {
		t.Error("a viewer may not edit scripts")
	}
	if !CanEditScripts(config.RoleAdmin) {
		t.Error("an admin may edit scripts")
	}
}

func TestPacketLabelHidesSendersFromViewers(t *testing.T) {
	allow := []string{"#public"}
	hidden := kam.Event{Kind: kam.KindRX, PayloadType: "GRP_TXT", Channel: "#emmen", Name: "PA3M"}
	if got := PacketLabel(config.RoleViewer, allow, hidden, nil); strings.Contains(got, "PA3M") || !strings.Contains(got, "#emmen") {
		t.Errorf("viewer label = %q", got)
	}
	if got := PacketLabel(config.RoleAdmin, allow, hidden, nil); got != "PA3M → #emmen" {
		t.Errorf("admin label = %q", got)
	}
	open := kam.Event{Kind: kam.KindRX, PayloadType: "GRP_TXT", Channel: "#public", Name: "Scout"}
	if got := PacketLabel(config.RoleViewer, allow, open, nil); got != "Scout → #public" {
		t.Errorf("allow-listed label = %q", got)
	}
	advert := kam.Event{Kind: kam.KindRX, PayloadType: "ADVERT", Name: "Canal relay"}
	if got := PacketLabel(config.RoleViewer, allow, advert, nil); got != "Canal relay" {
		t.Errorf("advert names are public: %q", got)
	}
}

func TestPacketLabelOtherKinds(t *testing.T) {
	cases := map[string]kam.Event{
		"Unknown channel":    {Kind: kam.KindRX, PayloadType: "GRP_TXT"},
		"→ lobby (probable)": {Kind: kam.KindRX, PayloadType: "REQ", Room: "lobby"},
		"REQ → 8e":           {Kind: kam.KindRX, PayloadType: "REQ", DestHash: "8e"},
		"TRACE":              {Kind: kam.KindRX, PayloadType: "TRACE"},
		"KAM → lobby":        {Kind: kam.KindTX, Origin: "room", Room: "lobby"},
		"Unnamed advert":     {Kind: kam.KindRX, PayloadType: "ADVERT"},
		"KAM":                {Kind: kam.KindTX, Origin: "automation"},
	}
	for want, ev := range cases {
		if got := PacketLabel(config.RoleAdmin, nil, ev, nil); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

// Indicator boundaries from Rooms.md §3.1 (MON-RM-4, MON-OV-9).
func TestActivityIndicatorBoundaries(t *testing.T) {
	now := t0
	cases := []struct {
		name  string
		at    *time.Time
		class string
	}{
		{"under 5 minutes", ptr(now.Add(-2 * time.Second)), "fresh"},
		{"exactly 299 s", ptr(now.Add(-299 * time.Second)), "fresh"},
		{"exactly 300 s", ptr(now.Add(-300 * time.Second)), "recent"},
		{"exactly 3599 s", ptr(now.Add(-3599 * time.Second)), "recent"},
		{"exactly 3600 s", ptr(now.Add(-3600 * time.Second)), "older"},
		{"exactly 86400 s", ptr(now.Add(-86400 * time.Second)), "older"},
		{"over 24 hours", ptr(now.Add(-86401 * time.Second)), "stale"},
	}
	for _, c := range cases {
		ind := Activity(now, c.at)
		if ind.Class != c.class {
			t.Errorf("%s: got %q, want %q", c.name, ind.Class, c.class)
		}
	}
	if ind := Activity(now, nil); ind.Class != "unknown" || ind.Text != "Unknown" {
		t.Errorf("a client without a time is Unknown, got %v", ind)
	}
	if ind := Activity(now, ptr(now.Add(-100*time.Hour))); ind.Text != "Stale Node" {
		t.Errorf("over 24 hours says Stale Node, got %v", ind)
	}
}

// A viewer gets no client rows and never a key (MON-OV-9, MON-RM-12).
func TestRecentClientsVisibility(t *testing.T) {
	clients := []kam.RoomClient{
		{PublicKey: "key-old", Label: ptr("Old"), LastActivityAt: ptr(t0.Add(-48 * time.Hour))},
		{PublicKey: "key-new", Label: ptr("New"), LastActivityAt: ptr(t0.Add(-time.Minute))},
		{PublicKey: "key-none"},
		{PublicKey: "key-mid", LastActivityAt: ptr(t0.Add(-30 * time.Minute))},
	}
	if got := RecentClients(config.RoleViewer, clients, t0); len(got) != 0 {
		t.Fatalf("a viewer gets no client rows, got %v", got)
	}
	got := RecentClients(config.RoleAdmin, clients, t0)
	if len(got) != 2 {
		t.Fatalf("two newest clients, got %d: %v", len(got), got)
	}
	if got[0].Label != "New" || got[1].Label != "key-mid" {
		t.Errorf("second row falls back to the key when the label is absent, got %v", got)
	}
	if got[0].Ind.Class != "fresh" {
		t.Errorf("first indicator fresh, got %v", got[0].Ind)
	}
}

// Events carry room ids; the label resolves them against the room snapshot (Traffic.md §3.1).
func TestPacketLabelResolvesRoomNames(t *testing.T) {
	resolver := func(id string) string {
		if id == "lobby" {
			return "Emmen local"
		}
		return "" // an unknown id keeps its id, a zero room keeps the dash
	}
	rx := kam.Event{Kind: kam.KindRX, PayloadType: "REQ", Room: "lobby"}
	if got := PacketLabel(config.RoleViewer, nil, rx, resolver); got != "→ Emmen local (probable)" {
		t.Errorf("rx room event = %q", got)
	}
	tx := kam.Event{Kind: kam.KindTX, Origin: "room", Room: "lobby"}
	if got := PacketLabel(config.RoleAdmin, nil, tx, resolver); got != "KAM → Emmen local" {
		t.Errorf("tx room event = %q", got)
	}
	if got := PacketLabel(config.RoleAdmin, nil, rx, nil); got != "→ lobby (probable)" {
		t.Errorf("without a resolver the id stands in: %q", got)
	}
	unknown := kam.Event{Kind: kam.KindRX, PayloadType: "REQ", Room: "ghost"}
	if got := PacketLabel(config.RoleAdmin, nil, unknown, resolver); got != "→ ghost (probable)" {
		t.Errorf("an unknown room keeps its id: %q", got)
	}
}
