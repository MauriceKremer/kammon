// Package view holds the visibility rules: what each role may see (docs/requirements/Access.md §3).
// Templates never decide this; they receive values that are already filtered (MON-AX-14).
package view

import (
	"fmt"
	"slices"

	"kammon.bytecoder.nl/internal/config"
	"kammon.bytecoder.nl/internal/kam"
)

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
