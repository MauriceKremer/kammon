package web

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/config"
	"kammon.bytecoder.nl/internal/kam"
)

// mapGeoJSON reads the FeatureCollection out of the map container's data attribute and decodes it
// the way the browser does: attribute entities first, then JSON.
func mapGeoJSON(t *testing.T, body string) mapFeatureCollection {
	t.Helper()
	const prefix = `data-geojson="`
	start := strings.Index(body, prefix)
	if start < 0 {
		t.Fatal("the map carries no GeoJSON")
	}
	rest := body[start+len(prefix):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatal("the GeoJSON attribute is not closed")
	}
	var collection mapFeatureCollection
	if err := json.Unmarshal([]byte(html.UnescapeString(rest[:end])), &collection); err != nil {
		t.Fatalf("the GeoJSON does not decode: %v", err)
	}
	return collection
}

// attrOf returns an attribute's value from the first element that carries it.
func attrOf(t *testing.T, body, name string) string {
	t.Helper()
	needle := ` ` + name + `="`
	start := strings.Index(body, needle)
	if start < 0 {
		t.Fatalf("the page misses the %s attribute", name)
	}
	rest := body[start+len(needle):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("the %s attribute is not closed", name)
	}
	return html.UnescapeString(rest[:end])
}

// TestAdvertsMapShowsPositronSurface pins the OpenFreeMap surface (D57): a MapLibre container with
// the located adverts as GeoJSON, the required attribution, the flat controls and the honest notes.
func TestAdvertsMapShowsPositronSurface(t *testing.T) {
	h := newHarness(t)
	seedAdverts(t, h)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/adverts")
	for _, want := range []string{
		`class="advert-map"`,
		`data-map-anchor="home|52.77920|6.90690|100"`,
		`data-has-center="1"`,
		`data-has-home="1"`,
		`data-radius-km="100"`,
		"OpenFreeMap",
		"OpenStreetMap contributors",
		`<link rel="stylesheet" href="/static/maplibre-gl.css?v=` + AssetVersion + `">`,
		`src="/static/maplibre-gl.js?v=` + AssetVersion + `"`,
		`data-node="1"`,
		"Advert 1: Canal relay",
		"Click a dot for details",
		`data-map-toggle="advertised"`,
		`data-map-toggle="silent"`,
		`data-map-toggle="base"`,
		"Copy Public Key",
		"Draw Line to Base",
		"MHz",
		`data-map-zoom="in"`,
		`data-map-zoom="out"`,
		`data-map-reset`,
		`aria-label="Reset to the 100 km view around your node"`,
		"The map opens on a 100 km radius around your node, from location in kammon.yml",
		"Drag to pan, scroll or pinch to zoom",
		"The scale bar is exact at 52.8°N.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page misses %q", want)
		}
	}
	collection := mapGeoJSON(t, body)
	if len(collection.Features) != 1 {
		t.Fatalf("features = %d, want the one located node", len(collection.Features))
	}
	feature := collection.Features[0]
	if feature.Geometry.Type != "Point" || feature.Geometry.Coordinates[0] != 6.9069 || feature.Geometry.Coordinates[1] != 52.7792 {
		t.Errorf("feature geometry = %+v, want Emmen at [lon, lat]", feature.Geometry)
	}
	if feature.Properties.Number != 1 || feature.Properties.Activity != "fresh" || feature.Properties.Name != "Canal relay" {
		t.Errorf("feature properties = %+v", feature.Properties)
	}
	// The basemap is tiles now, never inline SVG text or the retired asset.
	for _, unwanted := range []string{`d="M`, "/static/basemap.svg", "map-city-name"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("the page still carries %q", unwanted)
		}
	}
}

// TestAdvertsMapOpensOnHomeCentre checks the opening view: the configured node (Emmen in the demo)
// is the anchor and the client fits a 100 km radius around it; the camera rides no URL (MON-AD-15).
func TestAdvertsMapOpensOnHomeCentre(t *testing.T) {
	h := newHarness(t)
	seedAdverts(t, h)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/adverts")
	if got := attrOf(t, body, "data-center-lat"); got != "52.779200" {
		t.Errorf("center latitude = %q, want Emmen", got)
	}
	if got := attrOf(t, body, "data-center-lon"); got != "6.906900" {
		t.Errorf("center longitude = %q, want Emmen", got)
	}
	if got := attrOf(t, body, "data-radius-km"); got != "100" {
		t.Errorf("radius = %q km, want 100", got)
	}
	if !strings.HasPrefix(attrOf(t, body, "data-map-anchor"), "home|") {
		t.Errorf("the reload anchor is not the configured node")
	}
	if strings.Contains(body, "mapzoom") || strings.Contains(body, `hx-get="/ui/adverts?`) {
		t.Errorf("the map's camera still rides the URL")
	}
	if !strings.Contains(body, `src="/static/adverts-map.js?v=`+AssetVersion+`"`) {
		t.Errorf("the map script is not on the page")
	}
}

// TestAdvertsMapFramesTheNodesWithoutLocation checks the fallback: without location the view centres
// on the mean of the located nodes and says so (MON-AD-15, D57).
func TestAdvertsMapFramesTheNodesWithoutLocation(t *testing.T) {
	h := newHarness(t)
	seedAdverts(t, h)
	cfg := config.Demo()
	cfg.Location = nil
	ts, c := advertsServer(t, h, cfg)
	_, body, _ := get(t, c, ts.URL+"/adverts")
	if !strings.HasPrefix(attrOf(t, body, "data-map-anchor"), "nodes|") {
		t.Errorf("the anchor does not follow the located nodes")
	}
	if attrOf(t, body, "data-has-home") != "0" || attrOf(t, body, "data-has-center") != "1" {
		t.Errorf("the fallback view is not centred on the nodes")
	}
	if !strings.Contains(body, "The map opens on a 100 km radius around the centre of the 1 node that advertised a position.") {
		t.Errorf("the frame note misses the node centre")
	}
	if got := len(mapGeoJSON(t, body).Features); got != 1 {
		t.Errorf("features = %d, want the located node", got)
	}
}

// TestAdvertsMapWholeWorldWithoutLocationOrPosition checks the last fallback: no location and no
// advertised position opens a whole-world view with no guessed dots (MON-AD-6/7, MON-AD-15).
func TestAdvertsMapWholeWorldWithoutLocationOrPosition(t *testing.T) {
	h := newHarness(t)
	h.st.SetAdverts([]kam.Advert{{
		PublicKey: "aa", NodeHash: "aa", Nickname: ptr("No position"),
		HopCount: 0, LastReceivedAt: t0, FirstReceivedAt: ptr(t0.Add(-time.Hour)),
	}}, false, t0)
	cfg := config.Demo()
	cfg.Location = nil
	ts, c := advertsServer(t, h, cfg)
	_, body, _ := get(t, c, ts.URL+"/adverts")
	if !strings.HasPrefix(attrOf(t, body, "data-map-anchor"), "world|") || attrOf(t, body, "data-has-center") != "0" {
		t.Errorf("the map does not open on the whole world")
	}
	if !strings.Contains(body, "The map opens on a whole-world view; add location") {
		t.Errorf("the whole-world note is missing")
	}
	if got := len(mapGeoJSON(t, body).Features); got != 0 {
		t.Errorf("features = %d, want none without a position", got)
	}
}

// advertsServer builds a server with a substituted config and signs a viewer in.
func advertsServer(t *testing.T, h *harness, cfg config.Config) (*httptest.Server, *http.Client) {
	t.Helper()
	srv, err := New(Deps{Config: cfg, Store: h.st, Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, loginOn(t, ts)
}
