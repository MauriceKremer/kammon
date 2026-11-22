package web

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"kammon.bytecoder.nl/internal/kam"
	"kammon.bytecoder.nl/internal/view"
)

// advertMapNotes is what the map says below itself, in words: what the dots are, how the opening
// view is chosen, and what the map left out. Reset is the label the client's reset button shows.
// The camera values ride the map container's data attributes; none of them is radio text.
type advertMapNotes struct {
	Map, Frame, Home, Skip, Reset string
	Active, Silent                int // located dots, by activity
	Anchor                        string
	CenterLat, CenterLon          float64
	HasCenter                     bool
	HomeLat, HomeLon              float64
	HasHome                       bool
}

// mapPoint is a latitude and longitude, as adverts and kammon.yml give them.
type mapPoint struct{ lat, lon float64 }

// locatedNode is an advert that may be drawn: its advertised position and the table's last-heard
// class. Only these nodes become GeoJSON features; a node without a position or with a no-fix
// coordinate is counted, never guessed onto the map (MON-AD-6/7, D43, D48).
type locatedNode struct {
	lat, lon float64
	number   int
	fresh    string
	name     string
}

// The GeoJSON written to the page. Names ride as properties, never as markup: the client writes
// them to the DOM as text, so radio text still never reaches the chart exception (rule 5, D57).
type mapFeatureCollection struct {
	Type     string       `json:"type"`
	Features []mapFeature `json:"features"`
}

type mapFeature struct {
	Type       string        `json:"type"`
	ID         int           `json:"id"`
	Geometry   mapGeometry   `json:"geometry"`
	Properties mapProperties `json:"properties"`
}

type mapGeometry struct {
	Type        string     `json:"type"`
	Coordinates [2]float64 `json:"coordinates"` // longitude, latitude
}

type mapProperties struct {
	Number   int    `json:"number"`
	Activity string `json:"activity"`
	Name     string `json:"name"`
}

// advertMap turns the located adverts into the page's GeoJSON and the words around it. The map
// opens on a 100 km radius around the configured node (the located nodes' centre without one, a
// whole-world view without either); MapLibre draws the Positron basemap and the client script keeps
// the camera (MON-AD-6, MON-AD-15, D57).
func (s *Server) advertMap(list []kam.Advert, nowKAM time.Time) (string, bool, advertMapNotes) {
	home := (*mapPoint)(nil)
	if s.cfg.Location != nil && s.cfg.Location.Latitude != nil && s.cfg.Location.Longitude != nil {
		home = &mapPoint{lat: *s.cfg.Location.Latitude, lon: *s.cfg.Location.Longitude}
	}

	var located []locatedNode
	noFix, unlocated := 0, 0
	for i, advert := range list {
		if advert.HasLocation && advert.Latitude != nil && advert.Longitude != nil {
			if math.Abs(*advert.Latitude) <= noFixDegrees && math.Abs(*advert.Longitude) <= noFixDegrees {
				noFix++ // an advert without a fix, not a place (MON-AD-6, D43)
				continue
			}
			located = append(located, locatedNode{
				lat: *advert.Latitude, lon: *advert.Longitude, number: i + 1,
				fresh: view.Activity(nowKAM, &advert.LastReceivedAt).Class,
				name:  advertName(advert),
			})
			continue
		}
		unlocated++ // no advertised position: kept in the table, never guessed onto the map
	}

	notes := advertMapNotes{
		Map: "Solid dots are advertised positions. Nodes that advertised no position are not plotted, so every dot is a coordinate the node itself chose. Nearby dots group into a numbered cluster: click it to zoom in, and overlapping dots fan out at the closest zoom.",
	}
	notes.Active, notes.Silent = 0, 0
	for _, node := range located {
		if node.fresh == "stale" {
			notes.Silent++
		} else {
			notes.Active++
		}
	}

	// The opening view's centre: the configured node, else the located nodes' mean position, else
	// none, which the client draws as a whole-world view (MON-AD-15, D50).
	if home != nil {
		notes.HomeLat, notes.HomeLon, notes.HasHome = home.lat, home.lon, true
		notes.Home = fmt.Sprintf("Your node: %.5f, %.5f (from kammon.yml).", home.lat, home.lon)
		notes.CenterLat, notes.CenterLon, notes.HasCenter = home.lat, home.lon, true
		notes.Anchor = fmt.Sprintf("home|%.5f|%.5f|%.0f", home.lat, home.lon, viewRadiusKm)
	} else if len(located) > 0 {
		var latSum, lonSum float64
		for _, node := range located {
			latSum += node.lat
			lonSum += node.lon
		}
		notes.CenterLat, notes.CenterLon = latSum/float64(len(located)), lonSum/float64(len(located))
		notes.HasCenter = true
		notes.Anchor = fmt.Sprintf("nodes|%.5f|%.5f|%.0f", notes.CenterLat, notes.CenterLon, viewRadiusKm)
	} else {
		notes.Anchor = fmt.Sprintf("world|0|0|%.0f", viewRadiusKm)
	}
	if len(located) == 0 {
		notes.Map = "No node has advertised a position, so there is nothing to place on the map. The table and plan still list every node."
	}
	notes.Frame = mapFrameNote(notes.HasHome, notes.HomeLat, len(located))
	notes.Reset = "Reset view"
	if notes.HasHome {
		notes.Reset = fmt.Sprintf("Reset to the %s km view around your node", thousands(int(viewRadiusKm)))
	} else if len(located) > 0 {
		notes.Reset = fmt.Sprintf("Reset to the %s km view", thousands(int(viewRadiusKm)))
	}
	notes.Skip = mapSkipNote(noFix, unlocated)
	return advertGeoJSON(located), len(list) > 0, notes
}

// advertGeoJSON marshals the located adverts as a FeatureCollection. encoding/json escapes HTML
// characters, and the template escapes the attribute again, so a nickname can never break out of
// the data attribute; the client parses it and writes names as text, never as inner HTML.
func advertGeoJSON(located []locatedNode) string {
	list := mapFeatureCollection{Type: "FeatureCollection", Features: make([]mapFeature, 0, len(located))}
	for _, node := range located {
		list.Features = append(list.Features, mapFeature{
			Type:     "Feature",
			ID:       node.number,
			Geometry: mapGeometry{Type: "Point", Coordinates: [2]float64{node.lon, node.lat}},
			Properties: mapProperties{
				Number:   node.number,
				Activity: node.fresh,
				Name:     node.name,
			},
		})
	}
	raw, err := json.Marshal(list)
	if err != nil { // impossible for these plain values, but never hand a half-written map to the page
		return `{"type":"FeatureCollection","features":[]}`
	}
	return string(raw)
}

// mapFrameNote says how the opening view was chosen and what the scale bar means.
func mapFrameNote(hasHome bool, homeLat float64, located int) string {
	var words string
	switch {
	case hasHome:
		words = fmt.Sprintf("The map opens on a %s km radius around your node, from location in kammon.yml.", thousands(int(viewRadiusKm)))
	case located > 0:
		words = fmt.Sprintf("The map opens on a %s km radius around the centre of the %s that advertised a position.", thousands(int(viewRadiusKm)), pluralCount(located, "node"))
	default:
		words = "The map opens on a whole-world view; add location (latitude, longitude) to kammon.yml to open on your node."
	}
	words += " Drag to pan, scroll or pinch to zoom, and the buttons on the map step the zoom and reset the view."
	if hasHome {
		dir := "N"
		if homeLat < 0 {
			dir = "S"
		}
		words += fmt.Sprintf(" The scale bar is exact at %.1f°%s.", math.Abs(homeLat), dir)
	}
	return words
}

// mapSkipNote counts what the map left out, so the drawing cannot quietly drop a node.
func mapSkipNote(noFix, unlocated int) string {
	var parts []string
	switch {
	case noFix == 1:
		parts = append(parts, "1 advert sits within ±0.01° of 0°N 0°E: an advert without a GPS fix, so not plotted.")
	case noFix > 1:
		parts = append(parts, fmt.Sprintf("%s adverts sit within ±0.01° of 0°N 0°E: adverts without a GPS fix, so not plotted.", thousands(noFix)))
	}
	switch {
	case unlocated == 1:
		parts = append(parts, "1 node advertised no position, so it is not plotted.")
	case unlocated > 1:
		parts = append(parts, fmt.Sprintf("%s nodes advertised no position, so they are not plotted.", thousands(unlocated)))
	}
	return strings.Join(parts, " ")
}
