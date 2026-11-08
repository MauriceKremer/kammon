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

// TestChartSpansTheHealthZones pins the channel-health bands (D36): the axis ends at 30 % or a whole
// 10 % band above, the coloured washes sit behind the bars, and the hold-back line runs at 25 %.
func TestChartSpansTheHealthZones(t *testing.T) {
	empty := store.Window{Slots: make([]store.Slot, 4), SlotWidth: time.Minute}
	svg, axisTop := chartSVG(empty)
	s := string(svg)
	if axisTop != 30 {
		t.Fatalf("an empty chart keeps a %v %% axis, want 30", axisTop)
	}
	for _, want := range []string{ // the axis spans 0 to 30 % of a slot, so 10 % sits a third of the way up
		`<rect class="air-calm" x="0" y="106.67" width="600" height="53.33"/>`,
		`<rect class="air-busy" x="0" y="53.33" width="600" height="53.33"/>`,
		`<rect class="air-congested" x="0" y="0.00" width="600" height="53.33"/>`,
		`<line class="air-hold" x1="0" x2="600" y1="26.67" y2="26.67"/>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the health zones miss %q: %s", want, s)
		}
	}
	// A busy chart grows the axis a whole band, and congested reaches to the top of it.
	busy := store.Window{Slots: make([]store.Slot, 4), SlotWidth: time.Minute}
	busy.Slots[2].AirtimeMS[store.ClassGroupText] = 25200 // 42 % of the 60 000 ms slot
	svg, axisTop = chartSVG(busy)
	s = string(svg)
	if axisTop != 50 {
		t.Fatalf("a 42 %% busy chart keeps a %v %% axis, want 50 (a whole 10 %% band)", axisTop)
	}
	if !strings.Contains(s, `<rect class="air-congested" x="0" y="0.00" width="600" height="96.00"/>`) {
		t.Errorf("congested must reach from 20 pct to the grown axis: %s", s)
	}
	if !strings.Contains(s, `<line class="air-hold" x1="0" x2="600" y1="80.00" y2="80.00"/>`) { // 25 % of the slot
		t.Errorf("the hold-back line must sit at 25 pct of a slot whatever the axis: %s", s)
	}
	if !strings.Contains(s, `<rect class="c-group" x="300.50" y="25.60" width="149.00" height="134.40"/>`) {
		t.Errorf("the busy bar must fill 42 pct of the grown axis: %s", s)
	}
}

// TestPostsSparkSvgIsNumbersOnly applies the same rule-5 guard to the room posts mini chart and
// pins its shape: one bar per hour that has posts, the peak hour at full height, silence otherwise.
func TestPostsSparkSvgIsNumbersOnly(t *testing.T) {
	hours := [24]int{}
	hours[0], hours[22], hours[23] = 1, 3, 7
	s := string(sparkSVG("c-posts", hours[:]))
	if !strings.HasPrefix(s, `<svg class="chart chart-spark"`) || !strings.HasSuffix(s, `</svg>`) {
		t.Fatalf("svg has an unexpected shape: %s", s)
	}
	if !strings.Contains(s, `preserveAspectRatio="none"`) {
		t.Errorf("posts svg must stretch to its box: %s", s)
	}
	for _, want := range []string{
		`<rect class="c-posts" x="0.50" y="20.57" width="24.00" height="3.43"/>`,   // hour 0, 1/7 of the peak
		`<rect class="c-posts" x="575.50" y="0.00" width="24.00" height="24.00"/>`, // hour 23, the peak
	} {
		if !strings.Contains(s, want) {
			t.Errorf("bar missing or misplaced: %s", s)
		}
	}
	if got := strings.Count(s, `<rect class="c-posts"`); got != 3 {
		t.Errorf("bars = %d, want one per hour with posts: %s", got, s)
	}
	var empty [24]int
	if s := string(sparkSVG("c-posts", empty[:])); strings.Contains(s, `<rect`) {
		t.Errorf("hours without posts must not draw bars: %s", s)
	}
	// Numbers only: every text node between tags is empty; radio text can never show in the SVG.
	for _, seg := range strings.Split(s, ">")[1:] {
		if text, _, ok := strings.Cut(seg, "<"); ok && text != "" {
			t.Errorf("svg contains text %q: %s", text, s)
		}
	}
}

// TestGaugeSvgIsNumbersOnly applies the rule-5 guard to the server block's used/free bar and
// pins its shape: a red segment of the used percent, a free one for the rest, clamped to 100.
func TestGaugeSvgIsNumbersOnly(t *testing.T) {
	s := string(gaugeSVG(31.4))
	if !strings.HasPrefix(s, `<svg class="chart-gauge"`) || !strings.HasSuffix(s, `</svg>`) {
		t.Fatalf("svg has an unexpected shape: %s", s)
	}
	if !strings.Contains(s, `preserveAspectRatio="none"`) || !strings.Contains(s, `aria-label="31.4% used"`) {
		t.Errorf("the bar stretches to its box and says its percent for readers: %s", s)
	}
	for _, want := range []string{
		`<rect class="gauge-used" x="0" y="0" width="31.40" height="8"/>`,
		`<rect class="gauge-free" x="31.40" y="0" width="68.60" height="8"/>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("segment missing or misplaced: %s", s)
		}
	}
	// A percent beyond either end is clamped, and the empty segment is not drawn at all.
	if s := string(gaugeSVG(150)); !strings.Contains(s, `width="100.00"`) || strings.Contains(s, `gauge-free`) {
		t.Errorf("over-full percent clamps to a full red bar: %s", s)
	}
	if s := string(gaugeSVG(-5)); strings.Contains(s, `gauge-used`) || !strings.Contains(s, `width="100.00"`) {
		t.Errorf("a negative percent leaves only the free bar: %s", s)
	}
	// Numbers only: every text node between tags is empty; radio text can never show in the SVG.
	for _, seg := range strings.Split(s, ">")[1:] {
		if text, _, ok := strings.Cut(seg, "<"); ok && text != "" {
			t.Errorf("svg contains text %q: %s", text, s)
		}
	}
}

// TestMessagesSparkSvgIsNumbersOnly applies the same rule-5 guard to the channel messages mini
// chart and pins its shape: one square, height equal to width, per minute that decoded messages,
// resting on the base of the box, however many messages the minute held.
func TestMessagesSparkSvgIsNumbersOnly(t *testing.T) {
	minutes := [60]int{}
	minutes[0], minutes[58], minutes[59] = 1, 2, 4
	s := string(squareSparkSVG("c-msgs", minutes[:]))
	if !strings.HasPrefix(s, `<svg class="chart chart-spark chart-square"`) || !strings.HasSuffix(s, `</svg>`) {
		t.Fatalf("svg has an unexpected shape: %s", s)
	}
	if strings.Contains(s, `preserveAspectRatio="none"`) {
		t.Errorf("the square spark must keep the viewBox aspect, so squares stay square: %s", s)
	}
	for _, want := range []string{
		`<rect class="c-msgs" x="0.50" y="15.00" width="9.00" height="9.00"/>`,   // minute 0, one square
		`<rect class="c-msgs" x="580.50" y="15.00" width="9.00" height="9.00"/>`, // minute 58, two messages, same square
		`<rect class="c-msgs" x="590.50" y="15.00" width="9.00" height="9.00"/>`, // minute 59, four messages, same square
	} {
		if !strings.Contains(s, want) {
			t.Errorf("square missing or misplaced: %s", s)
		}
	}
	if got := strings.Count(s, `<rect class="c-msgs"`); got != 3 {
		t.Errorf("squares = %d, want one per minute with messages: %s", got, s)
	}
	var empty [60]int
	if s := string(squareSparkSVG("c-msgs", empty[:])); strings.Contains(s, `<rect`) {
		t.Errorf("minutes without messages must not draw squares: %s", s)
	}
	// Numbers only: every text node between tags is empty; radio text can never show in the SVG.
	for _, seg := range strings.Split(s, ">")[1:] {
		if text, _, ok := strings.Cut(seg, "<"); ok && text != "" {
			t.Errorf("svg contains text %q: %s", text, s)
		}
	}
}
