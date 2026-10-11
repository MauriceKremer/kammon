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

// chartSVG draws the stacked airtime bars on the server. The markup is built from numbers only, so no
// text from KAM can reach it; this is the one place template.HTML is allowed (a test enforces that).
// It returns the markup and the top of the axis as a percent of one slot.
func chartSVG(w store.Window) (chartMarkup, float64) {
	const width, height = 600.0, 160.0
	n := len(w.Slots)
	slotMS := float64(w.SlotWidth / time.Millisecond)
	axisMS := slotMS * 0.05 // an empty chart still shows a calm 5 % axis
	for _, slot := range w.Slots {
		var total int64
		for _, v := range slot.AirtimeMS {
			total += v
		}
		axisMS = max(axisMS, float64(total))
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="chart" viewBox="0 0 %.0f %.0f" aria-hidden="true">`, width, height)
	for i := 1; i <= 4; i++ {
		y := height - height*float64(i)/4
		fmt.Fprintf(&b, `<line class="grid" x1="0" x2="%.0f" y1="%.1f" y2="%.1f"/>`, width, y, y)
	}
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
				class.Key(), float64(i)*barWidth+0.5, y, max(barWidth-1, 1), h)
		}
	}
	// Gap and restart marks sit on the axis (MON-OV-5). A restart is a filled triangle, a gap an open
	// one, so the meaning survives without colour; both are also said in words below the chart.
	for _, m := range w.Marks {
		class := "mark-restart"
		if m.Kind == "gap" {
			class = "mark-gap"
		}
		x := (float64(m.Slot) + 0.5) * width / float64(n)
		fmt.Fprintf(&b, `<path class="%s" d="M%.1f 3 L%.1f 13 L%.1f 13 Z"/>`, class, x, x-5, x+5)
	}
	b.WriteString(`</svg>`)
	return chartMarkup(b.String()), axisMS / slotMS * 100
}
