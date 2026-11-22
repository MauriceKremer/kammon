package web

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/kam"
)

// baseAdverts sits in the Emmen frame: one located node and two unlocated ones that must stay
// off the map.
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
		`id="adverts-live"`, `class="advert-map"`, "data-geojson=", "data-map-anchor=",
		"Copy plan", `id="advert-plan"`, "KAMmon advert plan",
		"Nodes, newest heard first", "Hop count is the path length",
		"https://www.openstreetmap.org/?mlat=52.77920",
		"kammon.yml", "2 nodes advertised no position, so they are not plotted",
		"Nearby dots group into a numbered cluster", "Advertised (", "Silent (", "Base Node",
		"Click a dot for details", "Copy Public Key", "Draw Line to Base", "Frequency", "Hops from base",
		"Advertised a position", "Silent over 24 h",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page misses %q", want)
		}
	}
	// Only the located node is drawn: no guessed hop-band placement reaches the map.
	if got := len(mapGeoJSON(t, body).Features); got != 1 {
		t.Errorf("map features = %d, want only the located node", got)
	}
	if strings.Contains(body, "map-band") {
		t.Errorf("a non-GPS placement reached the map")
	}
}

// The unpositioned panel groups exactly the nodes the map leaves off — those that advertised no
// usable position — so an operator reads them without hunting the table (MON-AD-7, D55).
func TestAdvertsUnpositionedPanelListsUnplacedNodes(t *testing.T) {
	h := newHarness(t)
	seedAdverts(t, h)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/adverts")
	start := strings.Index(body, `<details class="unplaced">`)
	if start < 0 {
		t.Fatal("no unpositioned panel")
	}
	rest := body[start:]
	end := strings.Index(rest, "</details>")
	if end < 0 {
		t.Fatal("unclosed unpositioned panel")
	}
	panel := rest[:end]
	for _, want := range []string{"Unpositioned nodes (2)", "Dalen relay", "PA3M", "not on the map"} {
		if !strings.Contains(panel, want) {
			t.Errorf("panel misses %q", want)
		}
	}
	if strings.Contains(panel, "Canal relay") {
		t.Errorf("the positioned node leaked into the unpositioned panel")
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
	if got := len(mapGeoJSON(t, body).Features); got != 0 {
		t.Errorf("the no-fix position drew %d dots", got)
	}
	// The table stays honest about what was advertised.
	if !strings.Contains(body, `0.00000, 0.00000`) {
		t.Errorf("the table lost the advertised position")
	}
}

// TestAdvertsMapPlotsEveryLocatedNode checks that a valid advertised position is plotted even when
// it is far from the base: the Positron map has no fixed frame to fall outside of (MON-AD-6, D57).
func TestAdvertsMapPlotsEveryLocatedNode(t *testing.T) {
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
	if got := len(mapGeoJSON(t, body).Features); got != 2 {
		t.Errorf("map features = %d, want the Emmen and Perth nodes", got)
	}
	if strings.Contains(body, "outside the frame") {
		t.Errorf("the page still speaks of a frame")
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

// TestMapCSPAllowsOnlyTheBasemapHost checks the one online exception (D57): the adverts page may
// reach OpenFreeMap's tiles, glyphs and sprites and start MapLibre's blob workers, while every other
// page keeps the strict same-origin policy.
func TestMapCSPAllowsOnlyTheBasemapHost(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	_, _, header := get(t, c, h.ts.URL+"/adverts")
	csp := header.Get("Content-Security-Policy")
	for _, want := range []string{
		"connect-src 'self' https://tiles.openfreemap.org",
		"img-src 'self' data: blob: https://tiles.openfreemap.org",
		"worker-src 'self' blob:",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("adverts CSP misses %q, got %q", want, csp)
		}
	}
	_, _, header = get(t, c, h.ts.URL+"/")
	if csp := header.Get("Content-Security-Policy"); strings.Contains(csp, "openfreemap") {
		t.Errorf("the overview page carries the map's relaxed CSP: %q", csp)
	}
}

// TestMapLibreAssets checks the prebuilt library is vendored and served same-origin (D57).
func TestMapLibreAssets(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	for _, asset := range []string{"/static/maplibre-gl.js", "/static/maplibre-gl.css"} {
		status, body, _ := get(t, c, h.ts.URL+asset)
		if status != http.StatusOK || len(body) < 1000 {
			t.Errorf("%s: status %d, %d bytes", asset, status, len(body))
		}
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
