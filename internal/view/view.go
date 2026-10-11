// Package view holds the visibility rules: what each role may see (docs/requirements/Access.md §3).
// Templates never decide this; they receive values that are already filtered (MON-AX-14).
package view

import (
	"fmt"
	"slices"
	"time"

	"kammon.bytecoder.nl/internal/config"
	"kammon.bytecoder.nl/internal/kam"
)

// Indicator is the activity indicator of a room client (Rooms.md §3.1). Colour comes from the CSS
// class, the shape belongs to the class too, and Title and Text carry the words (MON-RM-7).
type Indicator struct{ Class, Title, Text string }

// Activity picks the indicator state for a last-heard time, computed from KAM's clock (MON-RM-4, 5).
func Activity(now time.Time, at *time.Time) Indicator {
	if at == nil {
		return Indicator{Class: "unknown", Title: "Unknown: no time is recorded.", Text: "Unknown"}
	}
	switch age := now.Sub(*at); {
	case age < 5*time.Minute:
		return Indicator{Class: "fresh", Title: "Heard under 5 minutes ago."}
	case age < time.Hour:
		return Indicator{Class: "recent", Title: "Heard 5 to 60 minutes ago."}
	case age <= 24*time.Hour:
		return Indicator{Class: "older", Title: "Heard 1 to 24 hours ago."}
	default:
		return Indicator{Class: "stale", Title: "Stale Node: silent for over 24 hours.", Text: "Stale Node"}
	}
}

// ClientRow is what a role may see of a room client's activity (MON-OV-9, MON-RM-8).
type ClientRow struct {
	Label string
	Ind   Indicator
}

// RecentClients are the two most recently active clients, newest first, unknown times last. A viewer
// gets no rows, because client names and keys are admin-only (MON-RM-12).
func RecentClients(role config.Role, clients []kam.RoomClient, nowKAM time.Time) []ClientRow {
	if role != config.RoleAdmin {
		return nil
	}
	sorted := append([]kam.RoomClient(nil), clients...)
	slices.SortFunc(sorted, func(a, b kam.RoomClient) int {
		switch {
		case a.LastActivityAt == nil && b.LastActivityAt == nil:
			return 0
		case a.LastActivityAt == nil:
			return 1
		case b.LastActivityAt == nil:
			return -1
		default:
			return b.LastActivityAt.Compare(*a.LastActivityAt)
		}
	})
	if len(sorted) > 2 {
		sorted = sorted[:2]
	}
	rows := make([]ClientRow, 0, len(sorted))
	for _, c := range sorted {
		label := c.PublicKey
		if c.Label != nil && *c.Label != "" {
			label = *c.Label
		}
		rows = append(rows, ClientRow{Label: label, Ind: Activity(nowKAM, c.LastActivityAt)})
	}
	return rows
}

// Page is one entry of the sidebar.
type Page struct {
	Key, Title, Path string
	Admin            bool
}

var pages = []Page{
	{Key: "overview", Title: "Overview", Path: "/"},
	{Key: "traffic", Title: "Traffic", Path: "/traffic"},
	{Key: "channels", Title: "Channels", Path: "/channels"},
	{Key: "rooms", Title: "Rooms", Path: "/rooms"},
	{Key: "accounts", Title: "Accounts", Path: "/accounts", Admin: true},
	{Key: "automations", Title: "Automations", Path: "/automations"},
	{Key: "modem", Title: "KISS modem", Path: "/modem"},
}

// Pages lists the sidebar entries a role may use (MON-AX-12).
func Pages(role config.Role) []Page {
	out := make([]Page, 0, len(pages))
	for _, p := range pages {
		if !p.Admin || role == config.RoleAdmin {
			out = append(out, p)
		}
	}
	return out
}

// Find returns a page by key.
func Find(key string) (Page, bool) {
	for _, p := range pages {
		if p.Key == key {
			return p, true
		}
	}
	return Page{}, false
}

// CanReadChannelText: admins read every channel; viewers only those on the allow list (MON-AX-13).
func CanReadChannelText(role config.Role, allow []string, channel string) bool {
	return role == config.RoleAdmin || slices.Contains(allow, config.NormalizeChannel(channel))
}

// PacketLabel is the source or destination text of a packet event (docs/requirements/Traffic.md §3.1).
// Sender names on channels a viewer may not read are replaced, and never appear in the page.
func PacketLabel(role config.Role, allow []string, ev kam.Event) string {
	switch {
	case ev.Kind == kam.KindTX:
		switch ev.Origin {
		case "room":
			return "KAM → room " + orDash(ev.Room)
		case "advert":
			return "KAM advert"
		default:
			if ev.Channel != "" {
				return "KAM → " + ev.Channel
			}
			return "KAM"
		}
	case ev.PayloadType == "ADVERT":
		if ev.Name == "" {
			return "Unnamed advert"
		}
		return ev.Name
	case ev.PayloadType == "GRP_TXT" && ev.Channel == "":
		return "Unknown channel"
	case ev.PayloadType == "GRP_TXT":
		if CanReadChannelText(role, allow, ev.Channel) && ev.Name != "" {
			return fmt.Sprintf("%s → %s", ev.Name, ev.Channel)
		}
		return "Sender hidden → " + ev.Channel
	case ev.Room != "":
		return "→ " + ev.Room + " (probable)"
	case ev.DestHash != "":
		return ev.PayloadType + " → " + ev.DestHash
	default:
		return ev.PayloadType
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
