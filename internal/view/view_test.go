package view

import (
	"strings"
	"testing"

	"kammon.bytecoder.nl/internal/config"
	"kammon.bytecoder.nl/internal/kam"
)

func TestPagesHideAccountsFromViewers(t *testing.T) {
	has := func(role config.Role, key string) bool {
		for _, p := range Pages(role) {
			if p.Key == key {
				return true
			}
		}
		return false
	}
	if has(config.RoleViewer, "accounts") || !has(config.RoleAdmin, "accounts") || !has(config.RoleViewer, "modem") {
		t.Fatal("accounts is admin only; modem snapshot is for everyone")
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

func TestPacketLabelHidesSendersFromViewers(t *testing.T) {
	allow := []string{"#public"}
	hidden := kam.Event{Kind: kam.KindRX, PayloadType: "GRP_TXT", Channel: "#emmen", Name: "PA3M"}
	if got := PacketLabel(config.RoleViewer, allow, hidden); strings.Contains(got, "PA3M") || !strings.Contains(got, "#emmen") {
		t.Errorf("viewer label = %q", got)
	}
	if got := PacketLabel(config.RoleAdmin, allow, hidden); got != "PA3M → #emmen" {
		t.Errorf("admin label = %q", got)
	}
	open := kam.Event{Kind: kam.KindRX, PayloadType: "GRP_TXT", Channel: "#public", Name: "Scout"}
	if got := PacketLabel(config.RoleViewer, allow, open); got != "Scout → #public" {
		t.Errorf("allow-listed label = %q", got)
	}
	advert := kam.Event{Kind: kam.KindRX, PayloadType: "ADVERT", Name: "Canal relay"}
	if got := PacketLabel(config.RoleViewer, allow, advert); got != "Canal relay" {
		t.Errorf("advert names are public: %q", got)
	}
}

func TestPacketLabelOtherKinds(t *testing.T) {
	cases := map[string]kam.Event{
		"Unknown channel":    {Kind: kam.KindRX, PayloadType: "GRP_TXT"},
		"→ lobby (probable)": {Kind: kam.KindRX, PayloadType: "REQ", Room: "lobby"},
		"REQ → 8e":           {Kind: kam.KindRX, PayloadType: "REQ", DestHash: "8e"},
		"TRACE":              {Kind: kam.KindRX, PayloadType: "TRACE"},
		"KAM → room lobby":   {Kind: kam.KindTX, Origin: "room", Room: "lobby"},
		"Unnamed advert":     {Kind: kam.KindRX, PayloadType: "ADVERT"},
		"KAM":                {Kind: kam.KindTX, Origin: "automation"},
	}
	for want, ev := range cases {
		if got := PacketLabel(config.RoleAdmin, nil, ev); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}
