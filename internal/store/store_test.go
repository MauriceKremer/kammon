package store

import (
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/kam"
)

var t0 = time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func rx(seq uint64, at time.Time, payload string, airtime int) kam.Event {
	return kam.Event{Seq: seq, At: at, Kind: kam.KindRX, PayloadType: payload, AirtimeMS: ptr(airtime)}
}

func reply(epoch string, first uint64, reset bool, events ...kam.Event) kam.EventsResponse {
	last := first
	if n := len(events); n > 0 {
		last = events[n-1].Seq
	}
	return kam.EventsResponse{Epoch: epoch, FirstSeq: first, LastSeq: last, Reset: reset, Events: events}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		ev   kam.Event
		want Class
	}{
		{"group text rx", kam.Event{Kind: kam.KindRX, PayloadType: "GRP_TXT"}, ClassGroupText},
		{"group text tx", kam.Event{Kind: kam.KindTX, PayloadType: "GRP_TXT", Origin: "automation"}, ClassGroupText},
		{"advert", kam.Event{Kind: kam.KindRX, PayloadType: "ADVERT"}, ClassAdvert},
		{"peer packet for a room", kam.Event{Kind: kam.KindRX, PayloadType: "REQ", Room: "lobby"}, ClassRoom},
		{"peer packet for someone else", kam.Event{Kind: kam.KindRX, PayloadType: "REQ"}, ClassOther},
		{"room ack sent", kam.Event{Kind: kam.KindTX, PayloadType: "ACK", Origin: "room"}, ClassRoom},
		{"trace", kam.Event{Kind: kam.KindRX, PayloadType: "TRACE"}, ClassOther},
		{"unknown", kam.Event{Kind: kam.KindRX, PayloadType: "UNKNOWN_12"}, ClassOther},
	}
	for _, c := range cases {
		if got := Classify(c.ev); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBackfillThenCaughtUpDoesNotDoubleCount(t *testing.T) {
	s := New()
	s.ApplyEvents(reply("e1", 1, false, rx(1, t0.Add(-5*time.Minute), "GRP_TXT", 300), rx(2, t0.Add(-4*time.Minute), "ADVERT", 200)), t0)
	if epoch, after := s.Cursor(); epoch != "e1" || after != 2 {
		t.Fatalf("cursor = %q %d", epoch, after)
	}
	s.ApplyEvents(reply("e1", 1, false, rx(2, t0.Add(-4*time.Minute), "ADVERT", 200)), t0)
	w := s.Window(t0, time.Hour)
	if w.Count[ClassGroupText] != 1 || w.Count[ClassAdvert] != 1 || w.RX != 2 {
		t.Fatalf("window = %+v", w)
	}
	if len(s.Marks()) != 0 {
		t.Fatalf("a normal poll must not add marks: %v", s.Marks())
	}
}

func TestFirstRequestResetFlagIsIgnoredButAGapIsMarked(t *testing.T) {
	s := New()
	s.ApplyEvents(reply("e1", 500, true, rx(500, t0.Add(-time.Minute), "GRP_TXT", 100)), t0)
	if len(s.Marks()) != 0 {
		t.Fatalf("first contact is not a gap: %v", s.Marks())
	}
	s.ApplyEvents(reply("e1", 900, true, rx(900, t0, "GRP_TXT", 100)), t0.Add(time.Minute))
	marks := s.Marks()
	if len(marks) != 1 || marks[0].Kind != "gap" {
		t.Fatalf("marks = %v", marks)
	}
	if w := s.Window(t0.Add(time.Minute), time.Hour); w.Count[ClassGroupText] != 2 {
		t.Fatalf("both events must be counted: %+v", w)
	}
}

func TestNewEpochAddsRestartMarkAndAcceptsLowerSequenceNumbers(t *testing.T) {
	s := New()
	s.ApplyEvents(reply("e1", 1, false, rx(1, t0.Add(-2*time.Minute), "GRP_TXT", 100), rx(2, t0.Add(-time.Minute), "GRP_TXT", 100)), t0)
	s.ApplyEvents(reply("e2", 1, true, rx(1, t0, "ADVERT", 100)), t0.Add(time.Second))
	if epoch, after := s.Cursor(); epoch != "e2" || after != 1 {
		t.Fatalf("cursor = %q %d", epoch, after)
	}
	if marks := s.Marks(); len(marks) != 1 || marks[0].Kind != "restart" {
		t.Fatalf("marks = %v", s.Marks())
	}
	if w := s.Window(t0.Add(time.Second), time.Hour); w.RX != 3 {
		t.Fatalf("buckets must survive a KAM restart: %+v", w)
	}
}

func TestWindowArithmeticAndSlots(t *testing.T) {
	s := New()
	// 36 000 ms of airtime in the last hour is exactly 1 percent.
	events := make([]kam.Event, 0, 12)
	for i := 0; i < 12; i++ {
		events = append(events, rx(uint64(i+1), t0.Add(-time.Duration(i+1)*5*time.Minute), "GRP_TXT", 3000))
	}
	s.ApplyEvents(reply("e1", 1, false, events...), t0)

	w := s.Window(t0, time.Hour)
	if len(w.Slots) != 60 || w.TotalAirtimeMS != 36000 || w.AirtimePercent < 0.99 || w.AirtimePercent > 1.01 {
		t.Fatalf("1h window: slots=%d airtime=%d percent=%.3f", len(w.Slots), w.TotalAirtimeMS, w.AirtimePercent)
	}
	if got := len(s.Window(t0, 15*time.Minute).Slots); got != 15 {
		t.Fatalf("15m slots = %d", got)
	}
	day := s.Window(t0, 24*time.Hour)
	if len(day.Slots) != 60 || day.SlotWidth != 24*time.Minute {
		t.Fatalf("24h slots = %d width %v", len(day.Slots), day.SlotWidth)
	}
	if day.HistoryStart.IsZero() {
		t.Fatal("24h window with about 1 hour of history must report where history starts")
	}
	if !w.HistoryStart.IsZero() && w.HistoryStart.Before(w.From) {
		t.Fatalf("history start %v is before the window", w.HistoryStart)
	}
}

func TestRepeatsAndPostsAreCounted(t *testing.T) {
	s := New()
	dup := rx(1, t0.Add(-time.Minute), "GRP_TXT", 300)
	dup.Duplicate = true
	post := kam.Event{Seq: 2, At: t0.Add(-time.Minute), Kind: kam.KindRoom, Room: "lobby", Event: "post"}
	tx := kam.Event{Seq: 3, At: t0.Add(-time.Minute), Kind: kam.KindTX, PayloadType: "ACK", Origin: "room", AirtimeMS: ptr(90)}
	s.ApplyEvents(reply("e1", 1, false, dup, post, tx), t0)
	w := s.Window(t0, time.Hour)
	if w.Repeats != 1 || w.Posts != 1 || w.RX != 1 || w.TX != 1 || w.Count[ClassRoom] != 1 {
		t.Fatalf("window = %+v", w)
	}
}

func TestRingKeepsTheNewestEventsOnly(t *testing.T) {
	s := New()
	var events []kam.Event
	for i := 1; i <= EventRingSize+100; i++ {
		events = append(events, rx(uint64(i), t0.Add(-time.Hour), "GRP_TXT", 1))
	}
	s.ApplyEvents(reply("e1", 1, false, events...), t0)
	recent := s.Recent(EventRingSize + 500)
	if len(recent) != EventRingSize || recent[0].Seq != EventRingSize+100 || recent[len(recent)-1].Seq != 101 {
		t.Fatalf("recent: len=%d newest=%d oldest=%d", len(recent), recent[0].Seq, recent[len(recent)-1].Seq)
	}
}

func TestEventsOlderThanADayAreNotBucketed(t *testing.T) {
	s := New()
	s.ApplyEvents(reply("e1", 1, false, rx(1, t0.Add(-25*time.Hour), "GRP_TXT", 100), rx(2, t0.Add(-time.Minute), "GRP_TXT", 100)), t0)
	if w := s.Window(t0, 24*time.Hour); w.RX != 1 {
		t.Fatalf("window = %+v", w)
	}
}

func TestFreshness(t *testing.T) {
	s := New()
	if f, _ := s.Freshness(t0); f != Down {
		t.Fatalf("before the first answer = %v", f)
	}
	s.SetStatus(kam.Status{}, t0)
	for _, c := range []struct {
		age  time.Duration
		want Freshness
	}{{5 * time.Second, Fresh}, {30 * time.Second, Stale}, {3 * time.Minute, Down}} {
		if f, at := s.Freshness(t0.Add(c.age)); f != c.want || !at.Equal(t0) {
			t.Errorf("age %v: %v %v", c.age, f, at)
		}
	}
}

func TestNotServedExpires(t *testing.T) {
	s := New()
	s.MarkNotServed("rooms", t0.Add(10*time.Minute))
	if !s.NotServed("rooms", t0) || s.NotServed("rooms", t0.Add(11*time.Minute)) {
		t.Fatal("not-served window is wrong")
	}
}
