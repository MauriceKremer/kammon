package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/kam"
)

func seedAdverts(t *testing.T, h *harness) {
	t.Helper()
	h.st.SetAdverts([]kam.Advert{
		{
			PublicKey: "ed8342bc8aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			NodeHash:  "8e", Nickname: ptr("Canal relay"), IsRepeater: true,
			HasLocation: true, Latitude: ptr(52.7792), Longitude: ptr(6.9069),
			SignatureValid: true, HopCount: 2, HopStations: []string{"a1", "b2"},
			TimeDriftSeconds: 12, AdvertTimestamp: t0.Add(-time.Minute),
			FirstReceivedAt: ptr(t0.Add(-20 * 24 * time.Hour)), LastReceivedAt: t0.Add(-30 * time.Second),
		},
		{
			PublicKey: "72f188ca8aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			NodeHash:  "72", Nickname: ptr("Dalen relay"), IsRepeater: true,
			SignatureValid: true, HopCount: 1, HopStations: []string{},
			LastReceivedAt: t0.Add(-2 * time.Hour), FirstReceivedAt: ptr(t0.Add(-10 * 24 * time.Hour)),
		},
		{
			PublicKey: "b590f2e88aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			NodeHash:  "b5", Nickname: ptr("PA3M"), IsChatNode: true,
			SignatureValid: true, HopCount: 0,
			LastReceivedAt: t0.Add(-30 * time.Hour), FirstReceivedAt: ptr(t0.Add(-26 * 24 * time.Hour)),
		},
	}, false, t0)
}

func TestAdvertsPageShowsInventoryPlanAndCopyableText(t *testing.T) {
	h := newHarness(t)
	seedAdverts(t, h)
	c := h.login(t, "viewer", "viewer")
	status, body, _ := get(t, c, h.ts.URL+"/adverts")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	for _, want := range []string{
		"Canal relay", "Dalen relay", "PA3M",
		`id="adverts-live"`, "advert-map", `class="map-node`, `class="map-node probable"`,
		`class="map-band"`, `class="map-home"`,
		"Copy plan", `id="advert-plan"`, "KAMmon advert plan",
		"Nodes, newest heard first", "Hop count is the path length",
		"https://www.openstreetmap.org/?mlat=52.77920",
		"kammon.yml", "assuming at most 15 km per hop",
		"Advertised a position", "Silent over 24 h",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page misses %q", want)
		}
	}
	// The unlocated node is placed on a hop band; the located one is a solid dot.
	if strings.Count(body, `class="map-node probable"`) != 2 {
		t.Errorf("probable dots = %d", strings.Count(body, `class="map-node probable"`))
	}
}

func TestAdvertsPageEscapesNamesAndHidesNoFacts(t *testing.T) {
	h := newHarness(t)
	h.st.SetAdverts([]kam.Advert{{
		PublicKey: "aa", NodeHash: "aa", Nickname: ptr(`Bad <img src=x onerror=alert(1)>`),
		HopCount: 0, LastReceivedAt: t0,
	}}, false, t0)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/adverts")
	if strings.Contains(body, "<img src=x") {
		t.Fatal("radio text reached the page unescaped")
	}
	if !strings.Contains(body, "&lt;img src=x") {
		t.Fatal("the nickname is missing from the table")
	}
}

func TestAdvertsPartialAndRedirect(t *testing.T) {
	h := newHarness(t)
	seedAdverts(t, h)
	c := h.login(t, "viewer", "viewer")

	status, body, _ := get(t, c, h.ts.URL+"/ui/adverts", "HX-Request", "true")
	if status != http.StatusOK || !strings.Contains(body, `id="adverts-live"`) {
		t.Fatalf("partial: %d", status)
	}
	if strings.Contains(body, "<html") {
		t.Fatal("the partial sent a whole page")
	}

	status, _, header := get(t, c, h.ts.URL+"/ui/adverts")
	if status != http.StatusSeeOther || header.Get("Location") != "/adverts" {
		t.Fatalf("shared link: %d %q", status, header.Get("Location"))
	}
}

func TestAdvertsEmptyState(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	status, body, _ := get(t, c, h.ts.URL+"/adverts")
	if status != http.StatusOK || !strings.Contains(body, "Waiting for KAM") {
		t.Fatalf("empty page: %d", status)
	}
}
