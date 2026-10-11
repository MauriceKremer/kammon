// traffic.go builds the Traffic page from the store's event ring alone (docs/requirements/Traffic.md,
// MON-TR-1). The filter combines free text on the rendered source / destination text, packet type,
// direction and a hide-repeats toggle; the state lives in the URL query string (MON-TR-5, 6).
package web

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"kammon.bytecoder.nl/internal/config"
	"kammon.bytecoder.nl/internal/kam"
	"kammon.bytecoder.nl/internal/store"
	"kammon.bytecoder.nl/internal/view"
)

const rowsPerLoad = 100 // MON-TR-2

type typeCount struct{ Name, Count, Percent string }

type trafficRow struct {
	Time, Dir, Type, TypeClass, Label, Route, SNR, RSSI, Airtime string
	Repeat                                                       bool
}

type trafficData struct {
	Text, Type, Direction string
	HideRepeats           bool

	TypeOptions []string
	Types       []typeCount
	Summary     string

	Rows        []trafficRow
	MoreURL     string
	LiveURL     string
	PushURL     string
	CSVURL      string
	HistoryNote string
}

// trafficView filters the event ring and returns the page state for one request.
func (s *Server) trafficView(role config.Role, q url.Values) trafficData {
	allow := s.cfg.ViewerTextChannels
	f := trafficFilterFrom(q)
	data := trafficData{Text: f.text, Type: f.typ, Direction: f.dir, HideRepeats: f.hide}
	matches := s.trafficMatches(role, allow, f)

	total := len(matches)
	rx, tx := 0, 0
	for _, ev := range matches {
		if ev.Kind == kam.KindRX {
			rx++
		} else {
			tx++
		}
	}
	data.Summary = fmt.Sprintf("%s matching · %s RX · %s TX", thousands(total), thousands(rx), thousands(tx))

	// The type summary covers everything the filter matches, not only the visible rows (MON-TR-7).
	data.Types = typeCounts(countTypes(matches), total)
	data.TypeOptions = typeOptions(s.store.Recent(store.EventRingSize))

	// Newest first, rowsPerLoad rows from skip (MON-TR-2).
	skip := skipFrom(q)
	begin, end := min(skip, total), min(skip+rowsPerLoad, total)
	if begin < end {
		for _, ev := range matches[begin:end] {
			data.Rows = append(data.Rows, s.trafficRowFor(role, allow, ev))
		}
	}

	base := f.query()
	data.PushURL = "/traffic?" + base
	data.CSVURL = "/traffic.csv?" + base
	data.LiveURL = "/ui/traffic?" + f.querySkip(skip)
	if end < total {
		data.MoreURL = "/ui/traffic?" + f.querySkip(end)
	} else {
		// The store holds fewer events than asked for (MON-TR-12).
		if oldest := s.store.OldestEventAt(); total > 0 {
			data.HistoryNote = "History starts at " + oldest.Local().Format("15:04") + "."
		} else {
			data.HistoryNote = "No packets in the store yet."
		}
	}
	return data
}

// trafficMatches is every packet event in the ring that passes the filter, newest first.
func (s *Server) trafficMatches(role config.Role, allow []string, f trafficFilter) []kam.Event {
	all := s.store.Recent(store.EventRingSize)
	matches := make([]kam.Event, 0, 256)
	for _, ev := range all {
		if ev.Kind != kam.KindRX && ev.Kind != kam.KindTX {
			continue // room, script and modem activity are not packets (MON-TR-4)
		}
		if !f.matches(ev, view.PacketLabel(role, allow, ev, s.roomNames())) {
			continue
		}
		matches = append(matches, ev)
	}
	return matches
}

func (s *Server) trafficRowFor(role config.Role, allow []string, ev kam.Event) trafficRow {
	row := trafficRow{
		Time:  ev.At.Local().Format("15:04:05"),
		Dir:   "RX",
		Type:  orDash(ev.PayloadType),
		Label: view.PacketLabel(role, allow, ev, s.roomNames()),
		Route: routeText(ev),
		SNR:   "—", RSSI: "—", Airtime: "—",
	}
	if ev.Kind == kam.KindTX {
		row.Dir = "TX"
	}
	row.TypeClass = typeClass(ev.PayloadType)
	if ev.SNRdB != nil {
		row.SNR = fmt.Sprintf("%+.1f", *ev.SNRdB)
	}
	if ev.RSSIdBm != nil {
		row.RSSI = fmt.Sprintf("%d dBm", *ev.RSSIdBm)
	}
	if ev.AirtimeMS != nil {
		row.Airtime = fmt.Sprintf("%s ms", thousands(*ev.AirtimeMS))
	}
	row.Repeat = ev.Duplicate
	return row
}

// routeText renders the route and hops (Traffic.md §1): "flood · 2 hops", or a dash when the event
// carries none. Zero is never used to mean unknown (MON-TR-8).
func routeText(ev kam.Event) string {
	switch {
	case ev.Route == "" && ev.Hops == 0:
		return "—"
	case ev.Route == "":
		return fmt.Sprintf("%d hops", ev.Hops)
	case ev.Hops > 0:
		return fmt.Sprintf("%s · %d hops", ev.Route, ev.Hops)
	default:
		return ev.Route
	}
}

func typeClass(payload string) string {
	switch payload {
	case "ADVERT":
		return "advert"
	case "ACK":
		return "ack"
	}
	return ""
}

func countTypes(events []kam.Event) map[string]int {
	out := map[string]int{}
	for _, ev := range events {
		out[ev.PayloadType]++
	}
	return out
}

// typeCounts orders per type, count first, name second.
func typeCounts(types map[string]int, total int) []typeCount {
	type pair struct {
		name  string
		count int
	}
	list := make([]pair, 0, len(types))
	for n, c := range types {
		list = append(list, pair{n, c})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].count != list[j].count {
			return list[i].count > list[j].count
		}
		return list[i].name < list[j].name
	})
	out := make([]typeCount, 0, len(list))
	for _, p := range list {
		out = append(out, typeCount{
			Name: p.name, Count: thousands(p.count),
			Percent: fmt.Sprintf("%.0f%%", float64(p.count)/float64(max(total, 1))*100),
		})
	}
	return out
}

// typeOptions is every packet type the store holds — the select stays complete across filters.
func typeOptions(events []kam.Event) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(seen))
	for _, ev := range events {
		if ev.Kind != kam.KindRX && ev.Kind != kam.KindTX {
			continue
		}
		if !seen[ev.PayloadType] {
			seen[ev.PayloadType] = true
			out = append(out, ev.PayloadType)
		}
	}
	sort.Strings(out)
	return out
}

// trafficFilter is the AND of the four filter controls (MON-TR-5). The text matches the rendered
// source / destination text only, so a viewer cannot find a hidden sender by searching (§6).
type trafficFilter struct {
	text, typ, dir string
	hide           bool
}

func trafficFilterFrom(q url.Values) trafficFilter {
	f := trafficFilter{
		text: strings.TrimSpace(q.Get("text")),
		typ:  q.Get("type"),
		dir:  q.Get("dir"),
		hide: q.Get("hide") != "",
	}
	if f.dir != "rx" && f.dir != "tx" {
		f.dir = ""
	}
	return f
}

func (f trafficFilter) matches(ev kam.Event, label string) bool {
	if f.hide && ev.Duplicate {
		return false
	}
	if f.typ != "" && ev.PayloadType != f.typ {
		return false
	}
	switch {
	case f.dir == "rx" && ev.Kind != kam.KindRX:
		return false
	case f.dir == "tx" && ev.Kind != kam.KindTX:
		return false
	}
	if f.text != "" && !strings.Contains(strings.ToLower(label), strings.ToLower(f.text)) {
		return false
	}
	return true
}

// query encodes the filter state for the URL (MON-TR-6), without the paging position.
func (f trafficFilter) query() string {
	q := f.values()
	return q.Encode()
}

func (f trafficFilter) querySkip(skip int) string {
	q := f.values()
	if skip > 0 {
		q.Set("skip", fmt.Sprintf("%d", skip))
	}
	return q.Encode()
}

func (f trafficFilter) values() url.Values {
	q := url.Values{}
	if f.text != "" {
		q.Set("text", f.text)
	}
	if f.typ != "" {
		q.Set("type", f.typ)
	}
	if f.dir != "" {
		q.Set("dir", f.dir)
	}
	if f.hide {
		q.Set("hide", "1")
	}
	return q
}

func skipFrom(q url.Values) int {
	v := q.Get("skip")
	if v == "" || len(v) > 6 {
		return 0
	}
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	if n < 0 {
		n = 0
	}
	return n
}

// trafficPage renders the full page.
func (s *Server) trafficPage(w http.ResponseWriter, r *http.Request) {
	data := s.trafficView(principalOf(r).Role, r.URL.Query())
	s.render(w, r, http.StatusOK, "traffic", s.pageFor(r, "traffic", "Traffic", "Every packet the radio hears or sends", data))
}

// trafficPartial is what htmx polls every two seconds and what a filter change or a
// "Show 100 more" click replaces (MON-TR-3).
func (s *Server) trafficPartial(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "" {
		// A shared plain link with the filter query goes to the full page (MON-TR-6).
		q := ""
		if r.URL.RawQuery != "" {
			q = "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, "/traffic"+q, http.StatusSeeOther)
		return
	}
	data := s.trafficView(principalOf(r).Role, r.URL.Query())
	var buf bytes.Buffer
	t := s.pages["traffic"]
	for _, part := range []struct {
		name string
		data any
	}{{"traffic-live", data}, {"stalebar", s.staleBar()}, {"nodeblock", s.nodeBlock()}} {
		if err := t.ExecuteTemplate(&buf, part.name, part.data); err != nil {
			http.Error(w, "Template error.", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = buf.WriteTo(w)
}

// csvPage exports every matching row the store holds, not only the visible ones (MON-TR-10).
func (s *Server) csvPage(w http.ResponseWriter, r *http.Request) {
	role := principalOf(r).Role
	allow := s.cfg.ViewerTextChannels
	f := trafficFilterFrom(r.URL.Query())
	matches := s.trafficMatches(role, allow, f)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"kammon-traffic-%s.csv\"", s.now().UTC().Format("20060102T150405")))
	w.Header().Set("Cache-Control", "no-store")

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Time", "Direction", "Type", "Source / destination", "Route", "SNR (dB)", "RSSI (dBm)", "Est. airtime (ms)", "Repeat"})
	for _, ev := range matches {
		row := s.trafficRowFor(role, allow, ev)
		at, _ := ev.At.MarshalText()
		airtime := ""
		if ev.AirtimeMS != nil {
			airtime = fmt.Sprintf("%d", *ev.AirtimeMS)
		}
		snr, rssi := "", ""
		if ev.SNRdB != nil {
			snr = fmt.Sprintf("%+.1f", *ev.SNRdB)
		}
		if ev.RSSIdBm != nil {
			rssi = fmt.Sprintf("%d", *ev.RSSIdBm)
		}
		repeat := ""
		if row.Repeat {
			repeat = "repeat"
		}
		_ = cw.Write([]string{string(at), row.Dir, csvGuard(row.Type), csvGuard(row.Label), csvGuard(row.Route), csvGuard(snr), csvGuard(rssi), csvGuard(airtime), csvGuard(repeat)})
	}
	cw.Flush()
}

// csvGuard prefixes a leading apostrophe to any value a spreadsheet would treat as a formula
// (MON-TR-11), because names come from the radio.
func csvGuard(v string) string {
	if v != "" {
		switch v[0] {
		case '=', '+', '-', '@':
			return "'" + v
		}
	}
	return v
}
