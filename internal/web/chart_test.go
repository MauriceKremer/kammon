package web

import (
	"strings"
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/store"
)

// TestChartSvgIsNumbersOnly guards rule 5 of AGENTS.md: template.HTML is allowed only in chart.go,
// and only for markup built from numbers. It also checks the MON-OV-5 axis marks.
func TestChartSvgIsNumbersOnly(t *testing.T) {
	w := store.Window{
		Slots:     make([]store.Slot, 4),
		SlotWidth: time.Minute,
		Marks:     []store.WindowMark{{Slot: 1, Kind: "restart"}, {Slot: 3, Kind: "gap"}},
	}
	svg, _ := chartSVG(w)
	s := string(svg)
	if !strings.HasPrefix(s, `<svg class="chart"`) || !strings.HasSuffix(s, `</svg>`) {
		t.Fatalf("svg has an unexpected shape: %s", s)
	}
	for i, want := range []string{
		`<path class="mark-restart" d="M225.0 3 L220.0 13 L230.0 13 Z"/>`, // slot 1 centred, filled
		`<path class="mark-gap" d="M525.0 3 L520.0 13 L530.0 13 Z"/>`,     // slot 3, drawn open
	} {
		if !strings.Contains(s, want) {
			t.Errorf("mark %d missing or misplaced: %s", i, s)
		}
	}
	// Numbers only: every text node between tags is empty; radio text can never show in the SVG.
	for _, seg := range strings.Split(s, ">")[1:] {
		if text, _, ok := strings.Cut(seg, "<"); ok && text != "" {
			t.Errorf("svg contains text %q: %s", text, s)
		}
	}
}
