package web

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/config"
	"kammon.bytecoder.nl/internal/kam"
)

// baseAdverts sits in the Emmen frame: one located node, two unlocated ones on hop bands.
func baseAdverts() []kam.Advert {
	return []kam.Advert{
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
	}
}

func seedAdverts(t *testing.T, h *harness) {
	t.Helper()
	h.st.SetAdverts(baseAdverts(), false, t0)
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

// TestAdvertsMapFramesTheCountry pins the country-level default frame (D43, D44): about 260 km
// around the configured node, never wider than the country; the fresh dot carries its last-heard
// colour class; the basemap is referenced from the versioned asset, never drawn inline; and the
// zoom controls step ±×2 from there.
func TestAdvertsMapFramesTheCountry(t *testing.T) {
	h := newHarness(t)
	seedAdverts(t, h)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/adverts")
	for _, want := range []string{
		"Framed about 260 km around your node, from location in kammon.yml",
		"never wider than Netherlands",
		`class="map-node fresh"`,
		`<use href="/static/basemap.svg?v=` + AssetVersion + `#basemap">`,
		"Grid lines every",
		`data-n="1"`, // the dot identifies itself by row number, not by a printed label
		`aria-label="Advert 1"`,
		`data-node="1"`, // and the hover/click card exists beside the map
		"hover or click a dot",
		"Dot colour follows Last heard",
		"Major city (Natural Earth)",
		"Groningen", // a NL city inside the frame, drawn from the vendored list
		"Utrecht",
		"Amsterdam",
		`class="map-city-name"`,
		`hx-get="/ui/adverts?mapzoom=2"`,
		`hx-get="/ui/adverts?mapzoom=0.5"`,
		`aria-label="Zoom in"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page misses %q", want)
		}
	}
	// The basemap no longer rides inside the page.
	if strings.Contains(body, `d="M`) {
		t.Errorf("the basemap path text reached the page")
	}
}

// TestAdvertsMapZoomSteps checks the zoomed frames: the words name the factor and ground width,
// the next step's URL rides the buttons, the poller's live URL carries the zoom, and the bounds
// disable their edge (MON-AD-15, D44).
func TestAdvertsMapZoomSteps(t *testing.T) {
	h := newHarness(t)
	seedAdverts(t, h)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/adverts?mapzoom=4")
	for _, want := range []string{
		"Zoomed 4× in: about",
		`hx-get="/ui/adverts?mapzoom=8"`,
		`hx-get="/ui/adverts?mapzoom=2"`,
		`hx-get="/ui/adverts?mapzoom=4"`, // the live region keeps its zoom on the poll
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page misses %q", want)
		}
	}
	_, body, _ = get(t, c, h.ts.URL+"/adverts?mapzoom=16")
	if !strings.Contains(body, `aria-label="Zoom in" disabled`) {
		t.Errorf("a 16× frame still offers zooming in")
	}
	_, body, _ = get(t, c, h.ts.URL+"/adverts?mapzoom=0.25")
	if !strings.Contains(body, "Zoomed 4× out") || !strings.Contains(body, `aria-label="Zoom out" disabled`) {
		t.Errorf("the ¼ frame misses its words or its zoom-out bound")
	}
}

// TestAdvertsMapFramesTheNodesWithoutLocation checks the fallback: no location configured means
// the old node fit, said in words (MON-AD-6, D43).
func TestAdvertsMapFramesTheNodesWithoutLocation(t *testing.T) {
	h := newHarness(t)
	seedAdverts(t, h)
	cfg := config.Demo()
	cfg.Location = nil
	srv, err := New(Deps{Config: cfg, Store: h.st, Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	c := loginOn(t, ts)
	_, body, _ := get(t, c, ts.URL+"/adverts")
	if !strings.Contains(body, "The frame fits the stored nodes; add location") {
		t.Errorf("the frame note misses the node fit and the location hint")
	}
	if !strings.Contains(body, `class="map-node`) {
		t.Errorf("the nodes are not drawn")
	}
}

// TestAdvertsMapDropsNoFixPositions checks the ±0.01° 0°N 0°E rule (D43): the table keeps the
// advertised row, the map draws no dot for it and says so.
func TestAdvertsMapDropsNoFixPositions(t *testing.T) {
	h := newHarness(t)
	seed := []kam.Advert{{
		PublicKey: "aa", NodeHash: "aa", Nickname: ptr("No-fix rig"),
		HasLocation: true, Latitude: ptr(0.0), Longitude: ptr(0.0), HopCount: 0,
		LastReceivedAt: t0.Add(-time.Second), FirstReceivedAt: ptr(t0.Add(-time.Hour)),
	}}
	h.st.SetAdverts(seed, false, t0)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/adverts")
	if !strings.Contains(body, "1 advert sits within ±0.01° of 0°N 0°E") {
		t.Errorf("the no-fix note is missing")
	}
	if strings.Contains(body, `class="map-node fresh"`) {
		t.Errorf("the no-fix position drew a dot")
	}
	// The table stays honest about what was advertised.
	if !strings.Contains(body, `0.00000, 0.00000`) {
		t.Errorf("the table lost the advertised position")
	}
}

// TestAdvertsMapCountsNodesOutsideTheFrame checks that a node outside the country frame is
// counted, not drawn, and never widens the frame (MON-AD-6, D43).
func TestAdvertsMapCountsNodesOutsideTheFrame(t *testing.T) {
	h := newHarness(t)
	seedAdverts(t, h)
	far := []kam.Advert{{
		PublicKey: "bb", NodeHash: "bb", Nickname: ptr("Perth rover"),
		HasLocation: true, Latitude: ptr(-31.95), Longitude: ptr(115.86), HopCount: 0,
		LastReceivedAt: t0.Add(-time.Second), FirstReceivedAt: ptr(t0.Add(-time.Hour)),
	}}
	h.st.SetAdverts(append(baseAdverts(), far...), false, t0)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/adverts")
	if !strings.Contains(body, "1 node falls outside the frame and is not drawn") {
		t.Errorf("the out-of-frame note is missing")
	}
	if strings.Count(body, `class="map-node fresh"`) != 1 {
		t.Errorf("fresh dots = %d, want only the in-frame node", strings.Count(body, `class="map-node fresh"`))
	}
}

// loginOn signs a viewer in on an ad-hoc server, like harness.login on its own.
func loginOn(t *testing.T, ts *httptest.Server) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.PostForm(ts.URL+"/login", url.Values{"name": {"viewer"}, "password": {"viewer"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login: status %d", resp.StatusCode)
	}
	return c
}

// TestBasemapAsset serves the vendored basemap once, revalidated by the asset-version ETag (D42).
func TestBasemapAsset(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	status, body, header := get(t, c, h.ts.URL+"/static/basemap.svg")
	if status != http.StatusOK || !strings.Contains(body, `id="basemap"`) {
		t.Fatalf("basemap: %d", status)
	}
	if header.Get("Content-Type") != "image/svg+xml" || header.Get("ETag") != `"`+AssetVersion+`"` {
		t.Errorf("headers: %q %q", header.Get("Content-Type"), header.Get("ETag"))
	}
	status, _, _ = get(t, c, h.ts.URL+"/static/basemap.svg", "If-None-Match", `"`+AssetVersion+`"`)
	if status != http.StatusNotModified {
		t.Errorf("revalidation: %d, want 304", status)
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
