package web

import (
	"fmt"
	"html/template"
	"strings"
	"time"

	"kammon.bytecoder.nl/internal/store"
)

// chartMarkup is markup built by chartSVG from numbers only. It is the one type allowed to skip escaping.
type chartMarkup = template.HTML

// Bottom to top, so group text sits on top of the stack.
var stackOrder = []store.Class{store.ClassOther, store.ClassAdvert, store.ClassRoom, store.ClassGroupText}

// The axis speaks the channel's health bands (D37): busy and congested always fit the chart, so the
// axis never ends under 30 % of one slot, and a busier chart grows its axis a whole 10 % band.
const axisFloorPercent = 30.0

// chartSVG draws the stacked airtime bars on the server. The markup is built from numbers only, so no
// text from KAM can reach it; this is the one place template.HTML is allowed (a test enforces that).
// It returns the markup and the top of the axis as a percent of one slot.
func chartSVG(w store.Window) (chartMarkup, float64) {
	const width, gutter, height = 600.0, 40.0, 160.0 // the gutter keeps the y-axis percents clear of the bars
	n := len(w.Slots)
	slotMS := float64(w.SlotWidth / time.Millisecond)
	axisMS := slotMS * axisFloorPercent / 100
	for _, slot := range w.Slots {
		var total int64
		for _, v := range slot.AirtimeMS {
			total += v
		}
		axisMS = max(axisMS, float64(total))
	}
	// Grow a whole 10 % band past the floor, so the zone lines keep their places in the words, and
	// then draw everything on the rounded axis: the label must match the scale the bars use.
	axisTop := axisMS / slotMS * 100
	steps := int(axisTop / 10)
	if axisTop > float64(steps)*10 {
		steps++
	}
	axisTop = float64(steps * 10)
	axisMS = slotMS * axisTop / 100
	var toY = func(percent float64) float64 { return height - height*percent/axisTop }

	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="chart" viewBox="0 0 %.0f %.0f" aria-hidden="true">`, width+gutter, height)
	// The channel's health zones as backgrounds (D37): healthy to 10 %, busy to 20 %, congested
	// above. The words live below the chart, like the gap and restart marks' words (MON-OV-5).
	var band = func(fromPercent, toPercent float64, class string) {
		toPercent = min(toPercent, axisTop)
		if fromPercent >= toPercent {
			return
		}
		y := toY(toPercent)
		fmt.Fprintf(&b, `<rect class="%s" x="%.0f" y="%.2f" width="%.0f" height="%.2f"/>`,
			class, gutter, y, width, toY(fromPercent)-y)
	}
	band(0, 10, "air-calm")
	band(10, 20, "air-busy")
	band(20, axisTop, "air-congested")
	// A 10 % ladder answers the zone words: a grid line on every step, so the bands read as numbers.
	// The percents themselves are laid over the gutter by the template in plain HTML, so they keep
	// the page's font at every window width; the svg itself scales. The hold line falls between steps.
	for v := 10.0; v <= axisTop; v += 10 {
		y := toY(v)
		fmt.Fprintf(&b, `<line class="grid" x1="%.0f" x2="%.0f" y1="%.1f" y2="%.1f"/>`, gutter, width+gutter, y, y)
	}
	// The hold-back line at 25 % (D37): past it a LoRa channel's backoffs grow quickly, so the busy
	// bars run into a warning border. Its words sit below the chart with the zones'.
	y := toY(25)
	fmt.Fprintf(&b, `<line class="air-hold" x1="%.0f" x2="%.0f" y1="%.2f" y2="%.2f"/>`, gutter, width+gutter, y, y)
	barWidth := width / float64(n)
	for i, slot := range w.Slots {
		y := height
		for _, class := range stackOrder {
			h := float64(slot.AirtimeMS[class]) / axisMS * height
			if h <= 0 {
				continue
			}
			y -= h
			fmt.Fprintf(&b, `<rect class="c-%s" x="%.2f" y="%.2f" width="%.2f" height="%.2f"/>`,
				class.Key(), gutter+float64(i)*barWidth+0.5, y, max(barWidth-1, 1), h)
		}
	}
	// Gap and restart marks sit on the axis (MON-OV-5). A restart is a filled triangle, a gap an open
	// one, so the meaning survives without colour; both are also said in words below the chart.
	for _, m := range w.Marks {
		class := "mark-restart"
		if m.Kind == "gap" {
			class = "mark-gap"
		}
		x := gutter + (float64(m.Slot)+0.5)*width/float64(n)
		fmt.Fprintf(&b, `<path class="%s" d="M%.1f 3 L%.1f 13 L%.1f 13 Z"/>`, class, x, x-5, x+5)
	}
	b.WriteString(`</svg>`)
	return chartMarkup(b.String()), axisTop
}

// sparkSVG draws the small bar trend in a detail strip: room posts per hour, channel messages per
// minute. Like chartSVG the markup is built from numbers only, so no text from KAM can reach it;
// this stays inside the template.HTML exception (a test enforces that). It stretches to its box
// (preserveAspectRatio="none"), so one fixed CSS height fits every strip width.
func sparkSVG(class string, values []int) chartMarkup {
	const width, height = 600.0, 24.0
	var peak int
	for _, n := range values {
		peak = max(peak, n)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="chart chart-spark" viewBox="0 0 %.0f %.0f" preserveAspectRatio="none" aria-hidden="true">`, width, height)
	barWidth := width / float64(len(values))
	for i, n := range values {
		if n <= 0 {
			continue
		}
		h := float64(n) / float64(peak) * height
		fmt.Fprintf(&b, `<rect class="%s" x="%.2f" y="%.2f" width="%.2f" height="%.2f"/>`,
			class, float64(i)*barWidth+0.5, height-h, max(barWidth-1, 1), h)
	}
	b.WriteString(`</svg>`)
	return chartMarkup(b.String())
}

// gaugeSVG draws the used/free bar of the server block: the used share as a red segment, the rest
// in the signal colour. Like the other charts the markup is built from numbers only, so this stays
// inside the template.HTML exception (a test enforces that); the aria-label is the text
// alternative. preserveAspectRatio="none" lets one drawn bar fit the block's every width.
func gaugeSVG(usedPercent float64) chartMarkup {
	pct := min(max(usedPercent, 0), 100)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="chart-gauge" viewBox="0 0 100 8" preserveAspectRatio="none" role="img" aria-label="%.1f%% used">`, pct)
	if pct > 0 {
		fmt.Fprintf(&b, `<rect class="gauge-used" x="0" y="0" width="%.2f" height="8"/>`, pct)
	}
	if pct < 100 {
		fmt.Fprintf(&b, `<rect class="gauge-free" x="%.2f" y="0" width="%.2f" height="8"/>`, pct, 100-pct)
	}
	b.WriteString(`</svg>`)
	return chartMarkup(b.String())
}

// squareSparkSVG draws the presence trend under the messages metric: one square per minute that
// decoded at least one message, resting on the base of the box, height equal to width (MON-CH-2).
// A quiet minute looks like a quiet minute; how many the minute held lives in the metric's number
// and its text alternative. It keeps the viewBox aspect instead of stretching, so the squares stay
// squares at any strip width; the CSS gives .chart-square the matching aspect. Like sparkSVG the
// markup is built from numbers only, so no text from KAM can reach it (a test enforces that).
func squareSparkSVG(class string, values []int) chartMarkup {
	const width, height = 600.0, 24.0
	slot := width / float64(len(values))
	side := max(slot-1, 1)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="chart chart-spark chart-square" viewBox="0 0 %.0f %.0f" aria-hidden="true">`, width, height)
	for i, n := range values {
		if n <= 0 {
			continue
		}
		fmt.Fprintf(&b, `<rect class="%s" x="%.2f" y="%.2f" width="%.2f" height="%.2f"/>`,
			class, float64(i)*slot+0.5, height-side, side, side)
	}
	b.WriteString(`</svg>`)
	return chartMarkup(b.String())
}

func gridStep(span float64) float64 {
	target := span / 6
	for _, step := range []float64{0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 15, 30} {
		if step >= target {
			return step
		}
	}
	return 30
}

// mapNode is one advert node already projected into the map's pixel space. Only nodes with a
// valid advertised position become map nodes. Fresh is the table's last-heard indicator class, so
// a dot reads like its table row (MON-AD-6).
type mapNode struct {
	X, Y   float64
	Number int
	Fresh  string
}

// mapCity is one major city placed in the map's pixel space, with its vendored Natural Earth name
// and scalerank (0 the most major) (never radio text: the list is generated data, D45). The rank
// lets the client hide the lower-priority names at the wide opening view.
type mapCity struct {
	Name string
	X, Y float64
	Rank int
}

// mapPlan is everything advertMapSVG draws, in pixels. The basemap was projected once at generate
// time into Web Mercator units (x = longitude, y = mercatorY(latitude)), so the frame is a plain
// affine slice of it: shapes stay true and the fit decides only the aspect (D42, D43).
type mapPlan struct {
	Width, Height       float64
	LonMin, MercMin     float64 // the frame's left and bottom edge, in projected units
	PxPerLon, PxPerMerc float64
	// ViewX, ViewY, ViewW and ViewH are the view the map opens on, in frame pixels: a 100 km
	// radius around the centre of the located nodes, or the whole frame when none advertise a
	// position. The client script pans and zooms inside the frame, so this is only its start.
	ViewX, ViewY, ViewW, ViewH float64
	// Meridians and Parallels are the grid lines' longitudes and latitudes in degrees; the
	// parallels project through mercatorY because Mercator spreads latitudes unevenly.
	Meridians, Parallels []float64
	Cities               []mapCity
	Nodes                []mapNode
	HomeX, HomeY         float64
	HasHome              bool
	ScaleEnd             float64 // the scale bar's right end in px (left = 14); 0 leaves it out
}

// advertMapSVG draws the advert map: the vendored Natural Earth basemap through one <use>
// reference, a mark for the configured own position and one fine dot per node with a valid
// advertised position. Each dot announces its row number — the table is the text alternative — and
// the hover and click cards are server-escaped HTML beside it. Like the other charts the markup is
// built from numbers only, so no text from the radio can reach it (a test enforces that).
func advertMapSVG(p mapPlan) chartMarkup {
	viewX, viewY, viewW, viewH := p.ViewX, p.ViewY, p.ViewW, p.ViewH
	if viewW == 0 || viewH == 0 { // the whole frame is the view when no located node set it
		viewX, viewY, viewW, viewH = 0, 0, p.Width, p.Height
	}
	// data-frame-width/height tell the client script the frame it may pan and zoom within.
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="chart advert-map" viewBox="%.2f %.2f %.2f %.2f" data-frame-width="%.0f" data-frame-height="%.0f" preserveAspectRatio="xMidYMid meet" role="img" aria-label="Advert map: node positions, each dot announcing its row number">`,
		viewX, viewY, viewW, viewH, p.Width, p.Height)
	fmt.Fprintf(&b, `<g transform="translate(0 %.2f) scale(%.8f %.8f) translate(%.8f %.8f)">`, p.Height, p.PxPerLon, -p.PxPerMerc, -p.LonMin, -p.MercMin)
	fmt.Fprintf(&b, `<use href="/static/basemap.svg?v=%s#basemap"></use>`, AssetVersion)
	b.WriteString(`</g>`)
	// A faint graticule gives the picture a scale; the steps are said in words below the map, and
	// the parallel lines project through mercatorY, so they sit at their true Mercator height.
	for _, lon := range p.Meridians {
		x := (lon - p.LonMin) * p.PxPerLon
		fmt.Fprintf(&b, `<line class="map-grid" x1="%.2f" y1="0" x2="%.2f" y2="%.2f"/>`, x, x, p.Height)
	}
	for _, lat := range p.Parallels {
		y := p.Height - (mercatorY(lat)-p.MercMin)*p.PxPerMerc
		fmt.Fprintf(&b, `<line class="map-grid" x1="0" y1="%.2f" x2="%.2f" y2="%.2f"/>`, y, p.Width, y)
	}
	// The scale bar: one kilometre line with end ticks; its km figure is said in words below.
	if p.ScaleEnd > 14 {
		y := p.Height - 12
		fmt.Fprintf(&b, `<line class="map-scale" x1="14" y1="%.2f" x2="%.2f" y2="%.2f"/>`, y, p.ScaleEnd, y)
		fmt.Fprintf(&b, `<line class="map-scale" x1="14" y1="%.2f" x2="14" y2="%.2f"/>`, y-4, y)
		fmt.Fprintf(&b, `<line class="map-scale" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f"/>`, p.ScaleEnd, y-4, p.ScaleEnd, y)
	}
	// Major cities under the plan: small dots with their vendored names, filtered to the frame by
	// the caller, so the operator orientates without any radio text reaching the SVG (D45).
	for _, city := range p.Cities {
		fmt.Fprintf(&b, `<g class="map-city-group" data-rank="%d">`, city.Rank)
		fmt.Fprintf(&b, `<circle class="map-city" cx="%.2f" cy="%.2f" r="1.8"/>`, city.X, city.Y)
		fmt.Fprintf(&b, `<text class="map-city-name" x="%.2f" y="%.2f">%s</text>`, city.X+4, city.Y+3, city.Name)
		b.WriteString(`</g>`)
	}
	if p.HasHome {
		// The base node: a bold neutral star with a static ring, kept a constant screen size by the
		// camera script. It is an anchor, not a state, so it carries no status colour and no pulse (D55).
		fmt.Fprintf(&b, `<g class="map-home" data-x="%.2f" data-y="%.2f" transform="translate(%.2f %.2f)">`, p.HomeX, p.HomeY, p.HomeX, p.HomeY)
		b.WriteString(`<title>Base node</title>`)
		b.WriteString(`<circle class="map-home-ring" r="10"></circle>`)
		b.WriteString(`<polygon class="map-home-star" points="0,-7.5 1.88,-2.59 7.13,-2.32 3.04,0.99 4.41,6.07 0,3.2 -4.41,6.07 -3.04,0.99 -7.13,-2.32 -1.88,-2.59"></polygon>`)
		b.WriteString(`</g>`)
	}
	for _, node := range p.Nodes {
		class := "map-node"
		if node.Fresh != "" {
			class += " " + node.Fresh
		}
		// The SVG carries numbers only (MON-AD-12): the dot announces its row number for screen
		// readers, and hover and click cards are server-escaped HTML beside the map (D46).
		fmt.Fprintf(&b, `<circle class="%s" data-n="%d" tabindex="0" aria-label="Advert %d" cx="%.2f" cy="%.2f" r="3.2"></circle>`, class, node.Number, node.Number, node.X, node.Y)
	}
	b.WriteString(`</svg>`)
	return chartMarkup(b.String())
}
