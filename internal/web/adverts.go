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
	kmPerDegreeLat = 111.32 // for the hop bands; the earth is a sphere here, which is plenty
	hopBandKm      = 15.0   // the assumed maximum range of one LoRa hop
	maxHopBand     = 4      // hop 4 and beyond share the outer band
	// Coordinates within ±0.01° of 0°N 0°E are a missing GPS fix, not a place (MON-AD-6, D43):
	// the table keeps the advertised row, the map drops the dot and never frames for it.
	noFixDegrees = 0.01
)

// scaleBarKm are the lengths the scale bar may take, so it always reads a round figure.
var scaleBarKm = []int{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000}

// defaultFrameKm is the map frame's nominal width: the plan shows about 260 km around the
// configured node, never wider than the country's own outline (D43, D44) — wide enough to hold the
// major cities of a Dutch-sized mesh.
const defaultFrameKm = 260.0

// mapZoomLevels are the zoom steps the controls step through: a plain span divisor, 1 the default.
var mapZoomLevels = []float64{0.25, 0.5, 1, 2, 4, 8, 16}

// mapZoom snaps the ?mapzoom= value onto the levels; anything unreadable is the default.
func mapZoom(requested string) float64 {
	raw, err := strconv.ParseFloat(requested, 64)
	if err != nil {
		return 1
	}
	pick, best := 1.0, math.Inf(1)
	for _, level := range mapZoomLevels {
		if d := math.Abs(math.Log2(raw / level)); d < best {
			pick, best = level, d
		}
	}
	return pick
}

// zoomToken is the canonical ?mapzoom= value of a level.
func zoomToken(zoom float64) string { return strconv.FormatFloat(zoom, 'f', -1, 64) }

// zoomNeighbours name the next level up and down, "" at a bound.
func zoomNeighbours(zoom float64) (up, down string) {
	for i, level := range mapZoomLevels {
		if level != zoom {
			continue
		}
		if i+1 < len(mapZoomLevels) {
			up = zoomToken(mapZoomLevels[i+1])
		}
		if i > 0 {
			down = zoomToken(mapZoomLevels[i-1])
		}
	}
	return up, down
}

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
	Rows                                                                            []advertRow
	Metrics                                                                         []channelMetric
	Map                                                                             chartMarkup
	HasMap                                                                          bool
	MapNote, FrameNote, HomeNote, BandsNote, TruncationNote, FreshNote, MapSkipNote string
	ZoomInURL, ZoomOutURL                                                           string
	Plan                                                                            string
	EmptyMessage                                                                    string
	LiveURL                                                                         string
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
	zoom := mapZoom(r.URL.Query().Get("mapzoom"))
	data := advertsData{LiveURL: "/ui/adverts"}
	if zoom != 1 {
		data.LiveURL += "?mapzoom=" + zoomToken(zoom) // the 60s poll keeps the zoomed frame
	}
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

	mapHTML, hasMap, notes := s.advertMap(list, nowKAM, zoom)
	data.Map, data.HasMap = mapHTML, hasMap
	data.MapNote, data.FrameNote, data.HomeNote, data.BandsNote, data.MapSkipNote =
		notes.Map, notes.Frame, notes.Home, notes.Bands, notes.Skip
	if hasMap {
		up, down := zoomNeighbours(zoom)
		if up != "" {
			data.ZoomInURL = "/ui/adverts?mapzoom=" + up
		}
		if down != "" {
			data.ZoomOutURL = "/ui/adverts?mapzoom=" + down
		}
	}

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

// advertMapNotes is what the map says below itself, in words: what the dots are, how the frame is
// chosen, and what the map left out.
type advertMapNotes struct {
	Map, Frame, Home, Bands, Skip string
}

// advertMap frames the planned nodes: about 200 km around the configured location, never wider
// than the country's own ring, with the zoom dividing the frame around its centre; the stored-node
// fit remains the fallback when location is missing or unmatched (D43, D44). The frame never
// widens for an advert, so a bad advertised coordinate cannot stretch the view. Nodes that
// advertised no position draw on hop-distance bands around location; nodes outside the frame are
// counted, not drawn.
func (s *Server) advertMap(list []kam.Advert, nowKAM time.Time, zoom float64) (chartMarkup, bool, advertMapNotes) {
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
	type probableNode struct{ number, hops int }
	var located []locatedNode
	var probables []probableNode
	noFix := 0
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
		probables = append(probables, probableNode{number: number, hops: advert.HopCount})
	}
	maxBand := 0
	for _, probable := range probables {
		maxBand = max(maxBand, min(probable.hops, maxHopBand))
	}
	if home != nil && len(probables) > 0 && maxBand == 0 {
		maxBand = 1
	}
	if len(located) == 0 && (home == nil || len(probables) == 0) {
		notes := advertMapNotes{
			Map: "No node has advertised a position, so there is nothing to place on a map. The table and plan still list every node.",
		}
		notes.Skip = mapSkipNote(noFix, 0)
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
			if maxBand > 0 {
				cosLat := max(math.Cos(home.lat*math.Pi/180), 0.2)
				d := float64(maxBand+1) * hopBandKm / (kmPerDegreeLat * cosLat)
				dLat := d * cosLat
				xMin, xMax = min(xMin, home.lon-d), max(xMax, home.lon+d)
				yMin, yMax = min(yMin, mercatorY(home.lat)-d), max(yMax, mercatorY(home.lat)+d)
				latMin, latMax = min(latMin, home.lat-dLat), max(latMax, home.lat+dLat)
			}
		}
		spanX = max(xMax-xMin, 0.02) * pad
		spanY = max(yMax-yMin, 0.02) * pad
		cx, cy = (xMin+xMax)/2, (yMin+yMax)/2
		latMid = (latMin + latMax) / 2
	}
	spanX = max(spanX/zoom, 0.02)
	spanY = max(spanY/zoom, 0.02)
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
		if kept >= 24 {
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
		for _, placed := range plan.Cities {
			if math.Hypot(placed.X-x, placed.Y-y) < 24 {
				closeOne = true
				break
			}
		}
		if closeOne {
			continue
		}
		plan.Cities = append(plan.Cities, mapCity{Name: city.name, X: x, Y: y})
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
	place := func(x, y float64, number int, probable bool, fresh string) {
		if x < -4 || y < -4 || x > plan.Width+4 || y > plan.Height+4 {
			outside++
			return
		}
		key := [2]int{int(math.Round(x)), int(math.Round(y))}
		if n := seen[key]; n > 0 {
			angle := float64(n) * 2.399963229728653 // golden angle, so the dots spread out
			step := 9.0 + 4.0*float64(n)
			x += step * math.Cos(angle)
			y += step * math.Sin(angle)
		}
		seen[key]++
		plan.Nodes = append(plan.Nodes, mapNode{X: x, Y: y, Number: number, Probable: probable, Fresh: fresh})
	}
	for _, node := range located {
		x := (node.lon - xMin) * plan.PxPerLon
		y := height - (mercatorY(node.lat)-yMin)*plan.PxPerMerc
		place(x, y, node.number, false, node.fresh)
	}
	notes := advertMapNotes{
		Map: "Solid dots are advertised positions. Hollow dots are nodes that advertised no position: they sit at a probable distance by hop count, not at a measured place.",
	}
	if home != nil {
		notes.Home = fmt.Sprintf("Your node: %.5f, %.5f (from kammon.yml).", home.lat, home.lon)
		plan.HomeX = (home.lon - xMin) * plan.PxPerLon
		plan.HomeY = height - (mercatorY(home.lat)-yMin)*plan.PxPerMerc
		plan.HasHome = true
		pxPerKmHome := plan.PxPerMerc / (kmPerDegreeLat * max(math.Cos(home.lat*math.Pi/180), 0.2))
		for band := 1; band <= maxBand; band++ {
			plan.Bands = append(plan.Bands, float64(band)*hopBandKm*pxPerKmHome)
		}
		for _, probable := range probables {
			band := min(probable.hops, maxHopBand)
			radius := (float64(band) + 0.5) * hopBandKm * pxPerKmHome
			angle := float64(probable.number) * 2.399963229728653
			place(plan.HomeX+radius*math.Cos(angle), plan.HomeY+radius*math.Sin(angle), probable.number, true, "")
		}
		notes.Bands = fmt.Sprintf("Hop bands are drawn at %d km intervals, assuming at most %.0f km per hop.", int(hopBandKm), hopBandKm)
		if len(plan.Bands) > 0 {
			notes.Bands += " A band past the frame is clipped, and the frame never widens for it."
		}
	}
	if home == nil {
		notes.Map = "Only nodes that advertised a position are plotted. Add `location` (latitude and longitude) to kammon.yml to place the other nodes by hop distance."
	}
	notes.Frame = mapFrameNote(frame, countryName, home != nil, plan.Meridians, plan.Parallels, scaleKm, scaleLat, zoom, spanKm(spanX, spanY, scaleLat))
	notes.Skip = mapSkipNote(noFix, outside)
	return advertMapSVG(plan), true, notes
}

// spanKm is the larger ground-km width of the frame, said in the frame words.
func spanKm(spanX, spanY, scaleLat float64) float64 {
	kmPerUnit := kmPerDegreeLat * math.Cos(scaleLat*math.Pi/180)
	return max(spanX, spanY) * kmPerUnit
}

// mapFrameNote says how the frame was chosen, how far it reaches, what the grid means.
func mapFrameNote(frame, countryName string, hasHome bool, meridians, parallels []float64, scaleKm int, scaleLat float64, zoom, acrossKm float64) string {
	var words string
	span := fmt.Sprintf("about %s km across", thousandsSpan(acrossKm))
	switch {
	case frame == "country" && zoom == 1:
		words = fmt.Sprintf("Framed about %s km around your node, from location in kammon.yml — never wider than %s, and the frame never widens for a node.", zoomToken(defaultFrameKm), countryName)
	case frame == "country":
		words = fmt.Sprintf("Zoomed %s× %s: %s around your node; the %s km default comes from location in kammon.yml.", zoomFactor(zoom), zoomDir(zoom), span, zoomToken(defaultFrameKm))
	case hasHome:
		words = fmt.Sprintf("The configured location did not match a country outline, so the frame fits the stored nodes, %s.", span)
		if zoom != 1 {
			words = fmt.Sprintf("Zoomed %s× %s into the fit of the stored nodes, %s; the %s km default needs location in kammon.yml.", zoomFactor(zoom), zoomDir(zoom), span, zoomToken(defaultFrameKm))
		}
	default:
		words = "The frame fits the stored nodes; add location (latitude, longitude) to kammon.yml to frame your country and its 200 km default."
		if zoom != 1 {
			words = "Zoomed " + zoomFactor(zoom) + "× " + zoomDir(zoom) + " into the fit of the stored nodes; the 200 km default needs location in kammon.yml."
		}
	}
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

// zoomFactor and zoomDir say a zoom level in words: "2 in", "4 out".
func zoomFactor(zoom float64) string {
	if zoom >= 1 {
		return zoomToken(zoom)
	}
	return zoomToken(1 / zoom)
}

func zoomDir(zoom float64) string {
	if zoom >= 1 {
		return "in"
	}
	return "out"
}

// thousandsSpan rounds a km width to a whole number for the words.
func thousandsSpan(km float64) string {
	return thousands(int(math.Round(km)))
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
func mapSkipNote(noFix, outside int) string {
	var parts []string
	switch {
	case noFix == 1:
		parts = append(parts, "1 advert sits within ±0.01° of 0°N 0°E: an advert without a GPS fix, so not plotted.")
	case noFix > 1:
		parts = append(parts, fmt.Sprintf("%s adverts sit within ±0.01° of 0°N 0°E: adverts without a GPS fix, so not plotted.", thousands(noFix)))
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
