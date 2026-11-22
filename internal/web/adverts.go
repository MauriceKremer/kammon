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
	kmPerDegreeLat = 111.32 // for the hop bands; the earth is a sphere here, which is plenty
	hopBandKm      = 15.0   // the assumed maximum range of one LoRa hop
	maxHopBand     = 4      // hop 4 and beyond share the outer band
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
	Ind        view.Indicator
}

type advertsData struct {
	Rows                                                    []advertRow
	Metrics                                                 []channelMetric
	Map                                                     chartMarkup
	HasMap                                                  bool
	MapNote, HomeNote, BandsNote, TruncationNote, FreshNote string
	Plan                                                    string
	EmptyMessage                                            string
	LiveURL                                                 string
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
	for i, advert := range list {
		row := advertRow{
			Number:    i + 1,
			Name:      advertName(advert),
			Roles:     advertRoles(advert),
			FullKey:   advert.PublicKey,
			ShortKey:  shortKey(advert.PublicKey),
			HopCount:  advert.HopCount,
			Hops:      hopsText(advert.HopCount),
			Drift:     driftText(advert.TimeDriftSeconds),
			Ind:       view.Activity(nowKAM, &advert.LastReceivedAt),
			LastHeard: ago(max(nowKAM.Sub(advert.LastReceivedAt), 0)),
		}
		if advert.FirstReceivedAt != nil {
			row.FirstHeard = advert.FirstReceivedAt.Local().Format("2006-01-02")
		} else {
			row.FirstHeard = "unknown"
		}
		if advert.HasLocation && advert.Latitude != nil && advert.Longitude != nil {
			located++
			row.Located = true
			row.Position = fmt.Sprintf("%.5f, %.5f", *advert.Latitude, *advert.Longitude)
			row.OSM = osmURL(*advert.Latitude, *advert.Longitude)
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
	}

	data.Metrics = []channelMetric{
		{Label: "Nodes stored", Value: thousands(len(list)), Caption: "One row per public key; KAM keeps the newest advert"},
		{Label: "Advertised a position", Value: thousands(located), Caption: "The rest are shown by hop distance only"},
		{Label: "Repeaters", Value: thousands(repeaters), Caption: pluralCount(roomServers, "room server")},
		{Label: "Silent over 24 h", Value: thousands(silent), Caption: "Stale Node: no advert heard in a day"},
	}

	data.Map, data.HasMap, data.MapNote, data.HomeNote, data.BandsNote = s.advertMap(list, nowKAM)

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

// advertMap projects the located nodes onto an equirectangular map. Nodes that advertised no
// position are drawn on hop-distance bands around the configured location when kammon.yml has
// one; otherwise they are left to the table and the note says so.
func (s *Server) advertMap(list []kam.Advert, nowKAM time.Time) (chartMarkup, bool, string, string, string) {
	type point struct{ lat, lon float64 }
	var located []point
	home := (*point)(nil)
	if s.cfg.Location != nil && s.cfg.Location.Latitude != nil && s.cfg.Location.Longitude != nil {
		home = &point{lat: *s.cfg.Location.Latitude, lon: *s.cfg.Location.Longitude}
	}
	unlocated := 0
	maxBand := 0
	for _, a := range list {
		if a.HasLocation && a.Latitude != nil && a.Longitude != nil {
			located = append(located, point{lat: *a.Latitude, lon: *a.Longitude})
			continue
		}
		unlocated++
		maxBand = max(maxBand, min(a.HopCount, maxHopBand))
	}
	if len(located) == 0 && (home == nil || unlocated == 0) {
		return "", false, "No node has advertised a position, so there is nothing to place on a map. The table and plan still list every node.", "", ""
	}
	if home != nil && unlocated > 0 && maxBand == 0 {
		maxBand = 1
	}

	latMin, latMax, lonMin, lonMax := 90.0, -90.0, 180.0, -180.0
	include := func(p point) {
		latMin, latMax = min(latMin, p.lat), max(latMax, p.lat)
		lonMin, lonMax = min(lonMin, p.lon), max(lonMax, p.lon)
	}
	for _, p := range located {
		include(p)
	}
	if home != nil {
		include(*home)
	}
	centerLat := (latMin + latMax) / 2
	cosLat := max(math.Cos(centerLat*math.Pi/180), 0.2)
	if home != nil && maxBand > 0 {
		outerKm := float64(maxBand+1) * hopBandKm
		dLat := outerKm / kmPerDegreeLat
		dLon := outerKm / (kmPerDegreeLat * cosLat)
		latMin, latMax = min(latMin, home.lat-dLat), max(latMax, home.lat+dLat)
		lonMin, lonMax = min(lonMin, home.lon-dLon), max(lonMax, home.lon+dLon)
	}
	spanLat, spanLon := max(latMax-latMin, 0.02), max(lonMax-lonMin, 0.02)
	// Centre the view and add a little air around the outermost points.
	centerLat, centerLon := (latMin+latMax)/2, (lonMin+lonMax)/2
	spanLat, spanLon = spanLat*1.24, spanLon*1.24
	latMin, latMax = centerLat-spanLat/2, centerLat+spanLat/2
	lonMin, lonMax = centerLon-spanLon/2, centerLon+spanLon/2
	cosLat = max(math.Cos(centerLat*math.Pi/180), 0.2)
	aspect := spanLon * cosLat / spanLat
	height := 380.0
	width := height * aspect
	switch {
	case width < 300:
		width = 300
	case width > 960:
		width = 960
	}
	height = width / aspect
	pxPerLon := width / spanLon
	pxPerLat := height / spanLat
	project := func(p point) (float64, float64) {
		return (p.lon - lonMin) * pxPerLon, (latMax - p.lat) * pxPerLat
	}

	plan := mapPlan{Width: width, Height: height, LonMin: lonMin, LatMin: latMin, PxPerLon: pxPerLon, PxPerLat: pxPerLat}
	if home != nil {
		plan.HomeX, plan.HomeY = project(*home)
		plan.HasHome = true
		for band := 1; band <= maxBand; band++ {
			plan.Bands = append(plan.Bands, float64(band)*hopBandKm/kmPerDegreeLat*pxPerLat)
		}
	}
	for i, a := range list {
		number := i + 1
		if a.HasLocation && a.Latitude != nil && a.Longitude != nil {
			x, y := project(point{lat: *a.Latitude, lon: *a.Longitude})
			plan.Nodes = append(plan.Nodes, mapNode{X: x, Y: y, Number: number})
			continue
		}
		if home == nil {
			continue
		}
		band := min(a.HopCount, maxHopBand)
		radius := (float64(band) + 0.5) * hopBandKm / kmPerDegreeLat * pxPerLat
		angle := float64(number) * 2.399963229728653 // golden angle, so the dots spread out
		plan.Nodes = append(plan.Nodes, mapNode{
			X: plan.HomeX + radius*math.Cos(angle), Y: plan.HomeY + radius*math.Sin(angle),
			Number: number, Probable: true,
		})
	}

	mapNote := "Solid dots are advertised positions. Hollow dots are nodes that advertised no position: they sit at a probable distance by hop count, not at a measured place."
	homeNote, bandsNote := "", ""
	if home != nil {
		homeNote = fmt.Sprintf("Your node: %.5f, %.5f (from kammon.yml).", home.lat, home.lon)
		bandsNote = fmt.Sprintf("Hop bands are drawn at %d km intervals, assuming at most %.0f km per hop.", int(hopBandKm), hopBandKm)
	} else {
		mapNote = "Only nodes that advertised a position are plotted. Add `location` (latitude and longitude) to kammon.yml to place the other nodes by hop distance."
	}
	return advertMapSVG(plan), true, mapNote, homeNote, bandsNote
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
