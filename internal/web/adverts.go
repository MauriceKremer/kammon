// adverts.go builds the Adverts page: KAM's stored advert inventory (GET /v1/adverts) plus a
// descriptive plan — an inventory table, a map of the nodes that advertised a position, and a
// plain-text plan that can be copied. It makes no recommendations: every line is a stored fact
// (docs/requirements/Adverts.md).
package web

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"kammon.bytecoder.nl/internal/kam"
	"kammon.bytecoder.nl/internal/store"
	"kammon.bytecoder.nl/internal/view"
)

const (
	kmPerDegreeLat = 111.32 // the earth is a sphere here, which is plenty for framing and scale
	// Coordinates within ±0.01° of 0°N 0°E are a missing GPS fix, not a place (MON-AD-6, D43):
	// the table keeps the advertised row, the map drops the dot and never frames for it.
	noFixDegrees = 0.01
	// viewRadiusKm is the radius of the view the map opens on, centred on the centre of the
	// located nodes; the client script pans and zooms inside the frame around it (MON-AD-15, D47).
	viewRadiusKm = 100.0
)

// scaleBarKm are the lengths the scale bar may take, so it always reads a round figure.
var scaleBarKm = []int{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000}

// defaultFrameKm is the map frame's nominal width: the plan shows about 260 km around the
// configured node, never wider than the country's own outline (D43, D44) — wide enough to hold the
// major cities of a Dutch-sized mesh.
const defaultFrameKm = 260.0

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
	Role       string
	Ind        view.Indicator
}

type advertsData struct {
	Rows                                                                 []advertRow
	Metrics                                                              []channelMetric
	Map                                                                  chartMarkup
	HasMap                                                               bool
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
		{Label: "Advertised a position", Value: thousands(located), Caption: "Only these nodes are plotted on the map"},
		{Label: "Repeaters", Value: thousands(repeaters), Caption: pluralCount(roomServers, "room server")},
		{Label: "Silent over 24 h", Value: thousands(silent), Caption: "Stale Node: no advert heard in a day"},
	}

	mapHTML, hasMap, notes := s.advertMap(list, nowKAM)
	data.Map, data.HasMap = mapHTML, hasMap
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

// advertMapNotes is what the map says below itself, in words: what the dots are, how the frame and
// the opening view are chosen, and what the map left out. Reset is the label the client's reset
// button shows.
type advertMapNotes struct {
	Map, Frame, Home, Skip, Reset string
	Active, Silent                int // the dots actually drawn inside the frame, by activity
}

// advertMap frames the planned nodes: about 200 km around the configured location, never wider
// than the country's own ring, with the node fit as the fallback when location is missing or
// unmatched (D43, D44). The frame never widens for an advert, so a bad advertised coordinate
// cannot stretch the view. Only nodes with a valid advertised position are plotted; nodes without
// one are counted, not drawn, so the map stays geographically honest. The map opens on a 100 km
// view around the centre of the located nodes, and the client script pans and zooms inside the
// frame (MON-AD-15, D47).
func (s *Server) advertMap(list []kam.Advert, nowKAM time.Time) (chartMarkup, bool, advertMapNotes) {
	type point struct{ lat, lon float64 }
	home := (*point)(nil)
	if s.cfg.Location != nil && s.cfg.Location.Latitude != nil && s.cfg.Location.Longitude != nil {
		home = &point{lat: *s.cfg.Location.Latitude, lon: *s.cfg.Location.Longitude}
	}

	type locatedNode struct {
		lat, lon float64
		number   int
		fresh    string
	}
	var located []locatedNode
	noFix, unlocated := 0, 0
	for i, advert := range list {
		number := i + 1
		if advert.HasLocation && advert.Latitude != nil && advert.Longitude != nil {
			if math.Abs(*advert.Latitude) <= noFixDegrees && math.Abs(*advert.Longitude) <= noFixDegrees {
				noFix++ // an advert without a fix, not a place (MON-AD-6, D43)
				continue
			}
			located = append(located, locatedNode{
				lat: *advert.Latitude, lon: *advert.Longitude, number: number,
				fresh: view.Activity(nowKAM, &advert.LastReceivedAt).Class,
			})
			continue
		}
		unlocated++ // no advertised position: kept in the table, never guessed onto the map
	}
	if len(located) == 0 && home == nil {
		notes := advertMapNotes{
			Map: "No node has advertised a position, so there is nothing to place on a map. The table and plan still list every node.",
		}
		notes.Skip = mapSkipNote(noFix, 0, 0)
		return "", false, notes
	}

	// The frame. Its centre and projected width and height: the country frame is the 200 km default
	// around the configured location, clamped to the holding ring's own span; the node fit keeps air
	// around the outermost points. The zoom divides the frame around its centre (D44).
	var countryName string
	frame := "nodes"
	var spanX, spanY, cx, cy, latMid float64
	pad := 1.16
	if home != nil {
		if name, west, east, south, north, ok := countryAt(home.lat, home.lon); ok {
			frame, countryName = "country", name
			cosLat := max(math.Cos(home.lat*math.Pi/180), 0.2)
			nominal := defaultFrameKm / (kmPerDegreeLat * cosLat)
			spanX = min(nominal, east-west) * pad
			spanY = min(nominal, mercatorY(north)-mercatorY(south)) * pad
			cx, cy, latMid = home.lon, mercatorY(home.lat), home.lat
		}
	}
	if frame == "nodes" {
		pad = 1.24
		xMin, xMax := 180.0, -180.0
		yMin, yMax := math.Inf(1), math.Inf(-1)
		latMin, latMax := 90.0, -90.0
		add := func(lat, lon float64) {
			xMin, xMax = min(xMin, lon), max(xMax, lon)
			yMin, yMax = min(yMin, mercatorY(lat)), max(yMax, mercatorY(lat))
			latMin, latMax = min(latMin, lat), max(latMax, lat)
		}
		for _, node := range located {
			add(node.lat, node.lon)
		}
		if home != nil {
			add(home.lat, home.lon)
		}
		spanX = max(xMax-xMin, 0.02) * pad
		spanY = max(yMax-yMin, 0.02) * pad
		cx, cy = (xMin+xMax)/2, (yMin+yMax)/2
		latMid = (latMin + latMax) / 2
	}
	spanX = max(spanX, 0.02)
	spanY = max(spanY, 0.02)
	xMin, xMax := cx-spanX/2, cx+spanX/2
	yMin := cy - spanY/2 // the frame's southern edge in projected units; the top follows from the height
	aspect := spanX / spanY
	height := 380.0
	width := height * aspect
	width = min(max(width, 300), 960)
	height = width / aspect
	if height > 720 { // a tall country keeps its shape but fits the page; width follows
		height, width = 720, 720*aspect
	}
	if width > 960 {
		width, height = 960, 960/aspect
	}
	// The grid's degree steps and the scale latitude come from the final frame: the parallels sit
	// at their true Mercator height, and the scale bar is exact along one parallel — the home
	// latitude when there is one, else the frame's centre. Both are said in words below (MON-AD-6).
	scaleLat := latMid
	if home != nil {
		scaleLat = home.lat
	}
	latMin := latMid - spanY*math.Cos(scaleLat*math.Pi/180)/2
	latMax := latMid + spanY*math.Cos(scaleLat*math.Pi/180)/2
	plan := mapPlan{
		Width: width, Height: height, LonMin: xMin, MercMin: yMin,
		PxPerLon: width / spanX, PxPerMerc: height / spanY,
	}
	// The opening view (MON-AD-15, D50): a viewRadiusKm square around the configured home
	// (`location` in kammon.yml, Emmen in the demo). Without a home it falls back to the centre of
	// the located nodes, and without either advertMapSVG opens on the whole frame. The view is held
	// inside the frame's rectangle. Web Mercator scales longitude and its y alike at one latitude,
	// so one radius number serves both axes; the centre is clamped so the view still shows the
	// frame.
	centreLat, centreLon, hasCentre := 0.0, 0.0, false
	if home != nil {
		centreLat, centreLon, hasCentre = home.lat, home.lon, true
	} else if len(located) > 0 {
		var latSum, lonSum float64
		for _, node := range located {
			latSum += node.lat
			lonSum += node.lon
		}
		centreLat, centreLon = latSum/float64(len(located)), lonSum/float64(len(located))
		hasCentre = true
	}
	if hasCentre {
		cosMid := max(math.Cos(centreLat*math.Pi/180), 0.2)
		radius := viewRadiusKm / (kmPerDegreeLat * cosMid)
		viewW, viewH := 2*radius*plan.PxPerLon, 2*radius*plan.PxPerMerc
		midX := (centreLon - xMin) * plan.PxPerLon
		midY := plan.Height - (mercatorY(centreLat)-yMin)*plan.PxPerMerc
		centreWithin := func(centre, side, bound float64) float64 {
			if side >= bound {
				return bound / 2
			}
			return min(max(centre, side/2), bound-side/2)
		}
		cx := centreWithin(midX, viewW, plan.Width)
		cy := centreWithin(midY, viewH, plan.Height)
		plan.ViewX, plan.ViewY = cx-viewW/2, cy-viewH/2
		plan.ViewW, plan.ViewH = viewW, viewH
	}
	for grid := math.Ceil(xMin/gridStep(xMax-xMin)) * gridStep(xMax-xMin); grid <= xMax; grid += gridStep(xMax - xMin) {
		plan.Meridians = append(plan.Meridians, grid)
	}
	for grid := math.Ceil(latMin/gridStep(latMax-latMin)) * gridStep(latMax-latMin); grid <= latMax; grid += gridStep(latMax - latMin) {
		plan.Parallels = append(plan.Parallels, grid)
	}

	// Major cities inside the frame (D45): most major first from the vendored list, thinned by a
	// minimum label distance so the names stay readable, capped so a dense country stays calm.
	kept := 0
	for _, city := range cities() {
		if kept >= 40 { // room for the national Dutch tier without evicting rank-7 majors (D54)
			break
		}
		x := (city.lon - xMin) * plan.PxPerLon
		if x < 8 || x > plan.Width-8 {
			continue
		}
		y := height - (mercatorY(city.lat)-yMin)*plan.PxPerMerc
		if y < 8 || y > plan.Height-8 {
			continue
		}
		closeOne := false
		if !city.local { // the curated local tier is allowed to sit closer
			for _, placed := range plan.Cities {
				if math.Hypot(placed.X-x, placed.Y-y) < 24 {
					closeOne = true
					break
				}
			}
		}
		if closeOne {
			continue
		}
		plan.Cities = append(plan.Cities, mapCity{Name: city.name, X: x, Y: y, Rank: city.rank})
		kept++
	}

	// The scale bar is exact along one parallel, so it sits at the home latitude when there is one,
	// else at the frame's centre.
	pxPerKm := plan.PxPerMerc / (kmPerDegreeLat * math.Cos(scaleLat*math.Pi/180))
	scaleKm := 0
	for _, km := range scaleBarKm {
		if float64(km)*pxPerKm >= 100 && 28+float64(km)*pxPerKm <= width-20 {
			scaleKm = km
			break
		}
	}
	if scaleKm > 0 {
		plan.ScaleEnd = 14 + float64(scaleKm)*pxPerKm
	}

	// Placing. A node outside the frame is counted, not drawn; two nodes at one spot step aside
	// deterministically, so every render spreads them the same way (D43).
	seen := map[[2]int]int{}
	outside := 0
	place := func(x, y float64, number int, fresh string) bool {
		if x < -4 || y < -4 || x > plan.Width+4 || y > plan.Height+4 {
			outside++
			return false
		}
		key := [2]int{int(math.Round(x)), int(math.Round(y))}
		if n := seen[key]; n > 0 {
			angle := float64(n) * 2.399963229728653 // golden angle, so the dots spread out
			step := 9.0 + 4.0*float64(n)
			x += step * math.Cos(angle)
			y += step * math.Sin(angle)
		}
		seen[key]++
		plan.Nodes = append(plan.Nodes, mapNode{X: x, Y: y, Number: number, Fresh: fresh})
		return true
	}
	active, silentDots := 0, 0
	for _, node := range located {
		x := (node.lon - xMin) * plan.PxPerLon
		y := height - (mercatorY(node.lat)-yMin)*plan.PxPerMerc
		if place(x, y, node.number, node.fresh) {
			if node.fresh == "stale" {
				silentDots++
			} else {
				active++
			}
		}
	}
	notes := advertMapNotes{
		Map: "Solid dots are advertised positions. Nodes that advertised no position are not plotted, so every dot is a coordinate the node itself chose. Nearby dots group into a numbered cluster: click it to zoom in, and overlapping dots fan out at the closest zoom.",
	}
	if home != nil {
		notes.Home = fmt.Sprintf("Your node: %.5f, %.5f (from kammon.yml).", home.lat, home.lon)
		plan.HomeX = (home.lon - xMin) * plan.PxPerLon
		plan.HomeY = height - (mercatorY(home.lat)-yMin)*plan.PxPerMerc
		plan.HasHome = true
	}
	if home == nil {
		notes.Map = "Only nodes that advertised a position are plotted; nodes without one stay in the table. Nearby dots group into a numbered cluster: click it to zoom in, and overlapping dots fan out at the closest zoom."
	}
	notes.Frame = mapFrameNote(frame, countryName, home != nil, len(located), plan.Meridians, plan.Parallels, scaleKm, scaleLat)
	notes.Reset = "Reset view"
	if home != nil {
		notes.Reset = fmt.Sprintf("Reset to the %s km view around your node", thousands(int(viewRadiusKm)))
	} else if len(located) > 0 {
		notes.Reset = fmt.Sprintf("Reset to %s km view", thousands(int(viewRadiusKm)))
	}
	notes.Skip = mapSkipNote(noFix, outside, unlocated)
	notes.Active, notes.Silent = active, silentDots
	return advertMapSVG(plan), true, notes
}

// mapFrameNote says how the frame was chosen, how the view opens on it, and what the grid means.
func mapFrameNote(frame, countryName string, hasHome bool, located int, meridians, parallels []float64, scaleKm int, scaleLat float64) string {
	var words string
	switch {
	case frame == "country":
		words = fmt.Sprintf("Framed about %.0f km around your node, from location in kammon.yml — never wider than %s, and the frame never widens for a node.", defaultFrameKm, countryName)
	case hasHome:
		words = "The configured location did not match a country outline, so the frame fits the stored nodes."
	default:
		words = "The frame fits the stored nodes; add location (latitude, longitude) to kammon.yml to frame your country and its 200 km default."
	}
	switch {
	case hasHome:
		words += fmt.Sprintf(" The view opens on a %s km radius around your node, from location in kammon.yml.", thousands(int(viewRadiusKm)))
	case located > 0:
		words += fmt.Sprintf(" The view opens on a %s km radius around the centre of the %s that advertised a position.", thousands(int(viewRadiusKm)), pluralCount(located, "node"))
	default:
		words += " The view opens on the whole frame."
	}
	words += " Drag to pan, scroll or pinch to zoom, and the buttons on the map step the zoom and reset the view."
	words += " Grid lines every " + gridStepWord(meridians, parallels) + "."
	if scaleKm > 0 {
		words += fmt.Sprintf(" The scale bar is %s km, exact at %.1f°%s.", thousands(scaleKm), math.Abs(scaleLat), func() string {
			if scaleLat < 0 {
				return "S"
			}
			return "N"
		}())
	}
	return words
}

// gridStepWord names the grid spacing in degrees; the two axes can differ.
func gridStepWord(meridians, parallels []float64) string {
	if len(meridians) < 2 || len(parallels) < 2 {
		return "less than a degree"
	}
	lonStep, latStep := meridians[1]-meridians[0], parallels[1]-parallels[0]
	step := func(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) + "°" }
	if lonStep == latStep {
		return step(lonStep)
	}
	return fmt.Sprintf("%s latitude, %s longitude", step(latStep), step(lonStep))
}

// mapSkipNote counts what the map left out, so the drawing cannot quietly drop a node.
func mapSkipNote(noFix, outside, unlocated int) string {
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
	switch {
	case outside == 1:
		parts = append(parts, "1 node falls outside the frame and is not drawn.")
	case outside > 1:
		parts = append(parts, fmt.Sprintf("%s nodes fall outside the frame and are not drawn.", thousands(outside)))
	}
	return strings.Join(parts, " ")
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
