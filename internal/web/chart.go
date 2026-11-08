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
	// A 10 % ladder answers the zone words: a grid line and a percent on every step, so the bands
	// read as numbers. The percents live in the gutter; the 25 % hold line falls between its steps.
	for v := 10.0; v <= axisTop; v += 10 {
		y := toY(v)
		fmt.Fprintf(&b, `<line class="grid" x1="%.0f" x2="%.0f" y1="%.1f" y2="%.1f"/>`, gutter, width+gutter, y, y)
	}
	for v := 0.0; v <= axisTop; v += 10 {
		yy := toY(v)
		if v == 0 {
			yy = height - 3
		} else {
			yy += 3.5
		}
		fmt.Fprintf(&b, `<text class="axis" x="%.0f" y="%.1f" text-anchor="end">%.0f%%</text>`, gutter-4, yy, v)
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
