package web

import (
	"fmt"
	"time"

	"kammon.bytecoder.nl/internal/config"
	"kammon.bytecoder.nl/internal/kam"
	"kammon.bytecoder.nl/internal/store"
	"kammon.bytecoder.nl/internal/view"
)

type rangeOption struct {
	Key, Label string
	Dur        time.Duration
}

var ranges = []rangeOption{
	{"15m", "15m", 15 * time.Minute},
	{"1h", "1h", time.Hour},
	{"24h", "24h", 24 * time.Hour},
}

func pickRange(key string) rangeOption {
	for _, r := range ranges {
		if r.Key == key {
			return r
		}
	}
	return ranges[1]
}

type rangeLink struct {
	Key, Label string
	Active     bool
}

type metric struct {
	Label, Value, Unit, Caption string
	Warn                        bool
}

type classRow struct{ Key, Name, Count, Percent string }

type packetRow struct{ Time, Type, TypeClass, Label, SNR string }

type roomRow struct {
	Name, Activity string
	Remembered     int
}

type overviewData struct {
	Range, RangeLabel string
	Ranges            []rangeLink
	Metrics           []metric
	Chart             chartMarkup
	ChartAlt          string
	AxisTop           string
	SlotText          string
	Labels            []string
	HistoryNote       string
	Marks             []string
	Classes           []classRow
	Recent            []packetRow
	Rooms             []roomRow
	RoomsNote         string
}

// overview builds the Overview from the store alone (MON-OV-1).
func (s *Server) overview(role config.Role, rangeKey string) overviewData {
	now := s.now()
	opt := pickRange(rangeKey)
	w := s.store.Window(now, opt.Dur)
	hour := s.store.Window(now, time.Hour)
	status, _ := s.store.Status()
	haveStatus := status.Epoch != ""

	chart, axisTop := chartSVG(w)
	data := overviewData{
		Range: opt.Key, RangeLabel: opt.Label, Chart: chart,
		AxisTop:  fmt.Sprintf("%.1f%%", axisTop),
		SlotText: fmt.Sprintf("%d bars of %s", len(w.Slots), slotWidthText(w.SlotWidth)),
		ChartAlt: fmt.Sprintf("Observed packet airtime over the last %s: average %.1f%%, busiest bar %.1f%%. %d marks.", opt.Label, w.AirtimePercent, w.PeakPercent, len(w.Marks)),
	}
	for _, r := range ranges {
		data.Ranges = append(data.Ranges, rangeLink{Key: r.Key, Label: r.Label, Active: r.Key == opt.Key})
	}
	for i := 0; i <= 4; i++ {
		layout := "15:04"
		if opt.Dur > 24*time.Hour-time.Second {
			layout = "02 15:04"
		}
		data.Labels = append(data.Labels, w.From.Add(time.Duration(i)*opt.Dur/4).Local().Format(layout))
	}
	if !w.HistoryStart.IsZero() {
		data.HistoryNote = "History starts at " + w.HistoryStart.Local().Format("15:04") + "."
	}
	for _, m := range s.store.Marks() {
		if m.At.Before(w.From) {
			continue
		}
		switch m.Kind {
		case "restart":
			data.Marks = append(data.Marks, "KAM restarted at "+m.At.Local().Format("15:04")+".")
		case "gap":
			data.Marks = append(data.Marks, "Some events were missed at "+m.At.Local().Format("15:04")+".")
		}
	}

	data.Metrics = []metric{
		{Label: "Observed packet airtime", Value: fmt.Sprintf("%.1f", w.AirtimePercent), Unit: "%", Caption: "Received + transmitted packets"},
		dutyMetric(status, haveStatus),
		{Label: "Packets observed", Value: thousands(w.RX + w.TX), Caption: fmt.Sprintf("%s RX / %s TX · %s repeats", thousands(w.RX), thousands(w.TX), thousands(w.Repeats))},
		{Label: "Room posts · last hour", Value: thousands(hour.Posts), Caption: "Accepted posts, all rooms"},
	}
	if haveStatus && status.Rooms.Enabled {
		data.Metrics[3].Unit = fmt.Sprintf("/ %d rooms", status.Rooms.Count)
	}

	var total int
	for _, n := range w.Count {
		total += n
	}
	for _, c := range []store.Class{store.ClassGroupText, store.ClassAdvert, store.ClassRoom, store.ClassOther} {
		pct := 0.0
		if total > 0 {
			pct = float64(w.Count[c]) / float64(total) * 100
		}
		data.Classes = append(data.Classes, classRow{Key: c.Key(), Name: c.String(), Count: thousands(w.Count[c]), Percent: fmt.Sprintf("%.0f%%", pct)})
	}

	allow := s.cfg.ViewerTextChannels
	for _, ev := range s.store.Recent(120) {
		if len(data.Recent) == 6 {
			break
		}
		if ev.Kind != kam.KindRX && ev.Kind != kam.KindTX {
			continue
		}
		data.Recent = append(data.Recent, packetRowFor(role, allow, ev))
	}

	rooms, _ := s.store.Rooms()
	for _, room := range rooms {
		if !room.Enabled {
			continue
		}
		activity := "No activity recorded"
		if room.LastActivityAt != nil {
			activity = "Last activity " + ago(now.Sub(*room.LastActivityAt))
		}
		data.Rooms = append(data.Rooms, roomRow{Name: room.Name, Activity: activity, Remembered: room.ClientsRemembered})
	}
	switch {
	case s.store.NotServed("rooms", now):
		data.RoomsNote = "Needs a newer KAM."
	case haveStatus && !status.Rooms.Enabled:
		data.RoomsNote = "Rooms are not enabled."
	default:
		data.RoomsNote = "No rooms yet."
	}
	return data
}

func packetRowFor(role config.Role, allow []string, ev kam.Event) packetRow {
	row := packetRow{
		Time:  ev.At.Local().Format("15:04:05"),
		Type:  ev.PayloadType,
		Label: view.PacketLabel(role, allow, ev),
		SNR:   "—",
	}
	switch ev.PayloadType {
	case "ADVERT":
		row.TypeClass = "advert"
	case "ACK":
		row.TypeClass = "ack"
	}
	if ev.SNRdB != nil {
		row.SNR = fmt.Sprintf("%+.1f", *ev.SNRdB)
	}
	if ev.Duplicate {
		row.Label += " (repeat)"
	}
	if ev.Kind == kam.KindTX {
		row.Type = "TX " + row.Type
	}
	return row
}

func dutyMetric(st kam.Status, have bool) metric {
	m := metric{Label: "Node TX duty cycle", Value: "—", Caption: "Waiting for KAM"}
	if !have || st.DutyCycle.WindowS == 0 {
		return m
	}
	used := float64(st.DutyCycle.UsedMS) / float64(st.DutyCycle.WindowS*1000) * 100
	limit := st.DutyCycle.ConfiguredPercent
	m.Value, m.Unit = fmt.Sprintf("%.1f", used), "%"
	switch {
	case limit <= 0:
		m.Caption = "No limit configured"
	case used > limit:
		m.Caption, m.Warn = fmt.Sprintf("Above the %.1f%% limit", limit), true
	case used >= 0.8*limit:
		m.Caption, m.Warn = fmt.Sprintf("Close to the %.1f%% limit", limit), true
	default:
		m.Caption = fmt.Sprintf("Within limit · %.1f%% configured", limit)
	}
	return m
}

func slotWidthText(d time.Duration) string {
	minutes := int(d / time.Minute)
	if minutes == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", minutes)
}
