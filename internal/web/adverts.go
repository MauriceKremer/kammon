// adverts.go builds the Adverts page: KAM's stored advert inventory (GET /v1/adverts) plus a
// descriptive plan — an inventory table, a map of the nodes that advertised a position, and a
// plain-text plan that can be copied. It makes no recommendations: every line is a stored fact
// (docs/requirements/Adverts.md).
package web

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"kammon.bytecoder.nl/internal/kam"
	"kammon.bytecoder.nl/internal/store"
	"kammon.bytecoder.nl/internal/view"
)

const (
	// Coordinates within ±0.01° of 0°N 0°E are a missing GPS fix, not a place (MON-AD-6, D43):
	// the table keeps the advertised row, the map drops the dot and never frames for it.
	noFixDegrees = 0.01
	// viewRadiusKm is the radius of the view the map opens on, centred on the configured node (or
	// the located nodes' centre); the client fits its camera around it (MON-AD-15, D50).
	viewRadiusKm = 100.0
)

type advertRow struct {
	Number     int
	Name       string
	Roles      string
	ShortKey   string
	FullKey    string
	Hops       string
	HopCount   int
	LastHeard  string
	FirstHeard string
	Drift      string
	Position   string
	OSM        string
	Located    bool
	Plotted    bool    // located and not a 0°N 0°E no-fix advert: drawn on the map
	Lat, Lon   float64 // the advertised position, when Plotted
	Role       string
	Ind        view.Indicator
}

type advertsData struct {
	Rows     []advertRow
	Unplaced []advertRow // nodes with no usable advertised position
	Metrics  []channelMetric
	// GeoJSON is the located adverts as a FeatureCollection (D57). It rides an HTML data
	// attribute, so the template escapes every name; the client parses it and writes names as text.
	GeoJSON                                                              string
	MapAnchor                                                            string // what the opening view was fitted to; gates reload restore
	HasMap, HasCenter, HasHome                                           bool
	CenterLat, CenterLon                                                 float64
	HomeLat, HomeLon                                                     float64
	RadiusKm                                                             float64
	MapNote, FrameNote, HomeNote, TruncationNote, FreshNote, MapSkipNote string
	ResetLabel                                                           string
	Plan                                                                 string
	EmptyMessage                                                         string
	LiveURL                                                              string
	Frequency                                                            string
	MapActive, MapSilent                                                 int
}

func (s *Server) advertsPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "adverts", s.pageFor(r, "adverts", "Adverts", "Nodes heard advertising, and the plan they describe", s.adverts(r)))
}

// advertsPartial is what htmx refreshes about once a minute: adverts change slowly and the poller
// reads them every 60 seconds, so a faster refresh would redraw the same plan.
func (s *Server) advertsPartial(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/adverts", http.StatusSeeOther)
		return
	}
	s.execute(w, http.StatusOK, s.pages["adverts"], "adverts-live", s.adverts(r))
}

func (s *Server) adverts(r *http.Request) advertsData {
	data := advertsData{LiveURL: "/ui/adverts"}
	list, kamTruncated, dropped := s.store.Adverts()
	now := s.now()
	status, _ := s.store.Status()
	data.Frequency = frequencyText(status.Radio.FrequencyHz)
	nowKAM := status.Now
	if nowKAM.IsZero() {
		nowKAM = now.Add(s.store.ClockOffset())
	}
	if len(list) == 0 {
		overall, _ := s.store.Freshness(now)
		switch {
		case s.store.NotServed("adverts", now):
			data.EmptyMessage = "Needs a newer KAM: this one does not offer the advert inventory."
		case overall == store.Down:
			data.EmptyMessage = "Adverts are unavailable right now."
		default:
			data.EmptyMessage = "Waiting for KAM's advert inventory."
		}
		return data
	}

	var located, repeaters, roomServers, silent int
	var unplaced []advertRow
	for i, advert := range list {
		ind := view.Activity(nowKAM, &advert.LastReceivedAt)
		row := advertRow{
			Number:    i + 1,
			Name:      advertName(advert),
			Roles:     advertRoles(advert),
			Role:      advertRoleSummary(advert),
			FullKey:   advert.PublicKey,
			ShortKey:  shortKey(advert.PublicKey),
			HopCount:  advert.HopCount,
			Hops:      hopsText(advert.HopCount),
			Drift:     driftText(advert.TimeDriftSeconds),
			Ind:       ind,
			LastHeard: ago(max(nowKAM.Sub(advert.LastReceivedAt), 0)),
		}
		if advert.FirstReceivedAt != nil {
			row.FirstHeard = advert.FirstReceivedAt.Local().Format("2006-01-02")
		} else {
			row.FirstHeard = "unknown"
		}
		plottable := false
		if advert.HasLocation && advert.Latitude != nil && advert.Longitude != nil {
			located++
			row.Located = true
			row.Position = fmt.Sprintf("%.5f, %.5f", *advert.Latitude, *advert.Longitude)
			row.OSM = osmURL(*advert.Latitude, *advert.Longitude)
			row.Lat, row.Lon = *advert.Latitude, *advert.Longitude
			// A position within ±noFixDegrees of 0°N 0°E is a missing fix (D43): it stays in the
			// table but is never placed on the map, so it joins the unpositioned panel below it.
			plottable = !(math.Abs(*advert.Latitude) <= noFixDegrees && math.Abs(*advert.Longitude) <= noFixDegrees)
			row.Plotted = plottable
		} else {
			row.Position = "Not advertised"
		}
		if advert.IsRepeater {
			repeaters++
		}
		if advert.IsRoomServer {
			roomServers++
		}
		if nowKAM.Sub(advert.LastReceivedAt) > 24*time.Hour {
			silent++
		}
		data.Rows = append(data.Rows, row)
		if !plottable {
			unplaced = append(unplaced, row)
		}
	}
	data.Unplaced = unplaced

	data.Metrics = []channelMetric{
		{Label: "Nodes stored", Value: thousands(len(list)), Caption: "One row per public key; KAM keeps the newest advert"},
		{Label: "Advertised a position", Value: thousands(located), Caption: "Only these nodes are plotted on the map"},
		{Label: "Repeaters", Value: thousands(repeaters), Caption: pluralCount(roomServers, "room server")},
		{Label: "Silent over 24 h", Value: thousands(silent), Caption: "Stale Node: no advert heard in a day"},
	}

	mapJSON, hasMap, notes := s.advertMap(list, nowKAM)
	data.GeoJSON, data.HasMap = mapJSON, hasMap
	data.CenterLat, data.CenterLon, data.HasCenter = notes.CenterLat, notes.CenterLon, notes.HasCenter
	data.HomeLat, data.HomeLon, data.HasHome = notes.HomeLat, notes.HomeLon, notes.HasHome
	data.RadiusKm, data.MapAnchor = viewRadiusKm, notes.Anchor
	data.MapActive, data.MapSilent = notes.Active, notes.Silent
	data.MapNote, data.FrameNote, data.HomeNote, data.MapSkipNote =
		notes.Map, notes.Frame, notes.Home, notes.Skip
	data.ResetLabel = notes.Reset

	if kamTruncated {
		data.TruncationNote = "KAM returned only the first rows of its advert table."
	}
	if dropped > 0 {
		data.TruncationNote = fmt.Sprintf("KAMmon keeps the first %s nodes; %s more were not stored.", thousands(store.MaxAdvertNodes), thousands(dropped))
	}
	// The poller reads adverts once a minute, so the ordinary 10-second freshness window would
	// call a healthy snapshot stale. Only say the data may be out of date when the snapshot is
	// actually down (two minutes without an answer), or when KAM says it has no such route.
	if freshness, _ := s.store.SnapshotFreshness("adverts", now); freshness == store.Down {
		data.FreshNote = "This inventory may be out of date: KAM has not answered the advert read recently."
	}
	data.Plan = s.advertPlanText(data, list, nowKAM)
	return data
}

func advertName(a kam.Advert) string {
	if a.Nickname != nil && *a.Nickname != "" {
		return *a.Nickname
	}
	return "Unnamed advert"
}

// advertRoles lists every role flag the advert carried. A node can claim more than one; the words
// are the contract's field names, not a single guessed role.
func advertRoles(a kam.Advert) string {
	var roles []string
	for _, role := range []struct {
		on   bool
		name string
	}{{a.IsRepeater, "repeater"}, {a.IsRoomServer, "room server"}, {a.IsChatNode, "chat node"}, {a.IsSensor, "sensor"}} {
		if role.on {
			roles = append(roles, role.name)
		}
	}
	if len(roles) == 0 {
		return "—"
	}
	return strings.Join(roles, " · ")
}

// advertRoleSummary names the advertised role for the inspector card. A node that claims no role
// is a plain client; several claimed roles are all named, highest duty first.
func advertRoleSummary(a kam.Advert) string {
	var roles []string
	for _, role := range []struct {
		on   bool
		name string
	}{{a.IsRepeater, "Repeater"}, {a.IsRoomServer, "Room Server"}, {a.IsChatNode, "Client"}, {a.IsSensor, "Sensor"}} {
		if role.on {
			roles = append(roles, role.name)
		}
	}
	if len(roles) == 0 {
		return "Client"
	}
	return strings.Join(roles, " · ")
}

func shortKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:12] + "…"
}

func hopsText(hops int) string {
	if hops == 0 {
		return "direct"
	}
	if hops == 1 {
		return "1 hop"
	}
	return fmt.Sprintf("%d hops", hops)
}

func pluralCount(n int, singular string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%s %ss", thousands(n), singular)
}

func driftText(seconds int64) string {
	if seconds < -1<<31 {
		seconds = -1 << 31
	}
	if seconds > 1<<31 {
		seconds = 1 << 31
	}
	if seconds == 0 {
		return "0 s"
	}
	sign := "+"
	if seconds < 0 {
		sign, seconds = "−", -seconds
	}
	return fmt.Sprintf("%s%s s", sign, thousands(int(seconds)))
}

func osmURL(lat, lon float64) string {
	return fmt.Sprintf("https://www.openstreetmap.org/?mlat=%.5f&mlon=%.5f#map=15/%.5f/%.5f", lat, lon, lat, lon)
}

// frequencyText is the base radio's frequency, as the inspector cards show it.
func frequencyText(hz uint32) string {
	if hz == 0 {
		return "Unknown"
	}
	return fmt.Sprintf("%.3f MHz", float64(hz)/1e6)
}

// advertPlanText is the copyable plan: a header, the counts, one line per node and the notes that
// keep the claims honest. It offers facts only.
func (s *Server) advertPlanText(data advertsData, list []kam.Advert, nowKAM time.Time) string {
	var b strings.Builder
	node := "KAM node"
	if status, _ := s.store.Status(); status.NodeName != "" {
		node = status.NodeName
	}
	fmt.Fprintf(&b, "KAMmon advert plan — %s\n", node)
	fmt.Fprintf(&b, "Generated %s\n\n", s.now().Local().Format("2006-01-02 15:04"))

	var located, repeaters, roomServers, silent int
	for _, advert := range list {
		if advert.HasLocation && advert.Latitude != nil && advert.Longitude != nil {
			located++
		}
		if advert.IsRepeater {
			repeaters++
		}
		if advert.IsRoomServer {
			roomServers++
		}
		if nowKAM.Sub(advert.LastReceivedAt) > 24*time.Hour {
			silent++
		}
	}
	fmt.Fprintf(&b, "Summary\n")
	fmt.Fprintf(&b, "  Nodes stored: %d (one row per public key; KAM keeps the newest advert)\n", len(list))
	fmt.Fprintf(&b, "  With an advertised position: %d\n", located)
	fmt.Fprintf(&b, "  Repeaters: %d; room servers: %d\n", repeaters, roomServers)
	fmt.Fprintf(&b, "  Silent for over 24 hours: %d\n\n", silent)

	fmt.Fprintf(&b, "Nodes, newest heard first\n")
	fmt.Fprintf(&b, "  %2s  %-28s %-24s %-8s %-18s %-11s %-9s %s\n", "#", "Name", "Roles", "Hops", "Last heard", "First heard", "Clock", "Position")
	for _, row := range data.Rows {
		fmt.Fprintf(&b, "  %2d  %-28s %-24s %-8s %-18s %-11s %-9s %s\n",
			row.Number, clip(row.Name, 28), clip(row.Roles, 24), row.Hops, row.LastHeard, row.FirstHeard, row.Drift, row.Position)
	}
	b.WriteString("\nNotes\n")
	b.WriteString("  Names, roles and positions are what each node advertised; they are not verified identity or proof.\n")
	b.WriteString("  Hop count is the path length of the packet that delivered the newest advert; 0 means heard directly.\n")
	b.WriteString("  Clock is the advert's clock minus KAM's clock at reception, in seconds.\n")
	if data.TruncationNote != "" {
		fmt.Fprintf(&b, "  %s\n", data.TruncationNote)
	}
	return b.String()
}

func clip(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit < 2 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}
