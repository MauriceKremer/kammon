package store

import (
	"fmt"
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

func TestTextRequestCacheQueuesOnceAndExpires(t *testing.T) {
	s := New()
	request := TextRequest{Kind: "channel", Channel: "#emmen", Limit: 50}
	if _, _, exists, fresh := s.RequestText(request, t0); exists || fresh {
		t.Fatal("a cache miss must be reported")
	}
	s.RequestText(request, t0)
	if requests := s.TakeTextRequests(10); len(requests) != 1 {
		t.Fatalf("duplicate cache miss queued %d requests", len(requests))
	}
	page := kam.ChannelMessages{Channel: "#emmen", Messages: []kam.ChannelMessage{{ID: 4, Body: ptr("signal")}}}
	s.CompleteText(request, page, nil, t0, nil)
	got, _, exists, fresh := s.RequestText(request, t0.Add(4*time.Second))
	if !exists || !fresh || len(got.Messages) != 1 || *got.Messages[0].Body != "signal" {
		t.Fatalf("cached page: %+v, exists=%v fresh=%v", got, exists, fresh)
	}
	s.RequestText(request, t0.Add(TextPageTTL))
	if requests := s.TakeTextRequests(10); len(requests) != 1 {
		t.Fatalf("expired page queued %d refreshes", len(requests))
	}
}

func TestFreshnessLimitsAreConfigurable(t *testing.T) {
	s := New(WithFreshness(2*time.Second, 5*time.Second))
	s.ApplyEvents(reply("e1", 1, false, rx(1, t0, "GRP_TXT", 300)), t0)
	if f, _ := s.Freshness(t0.Add(time.Second)); f != Fresh {
		t.Fatalf("1 s old: %v, want fresh", f)
	}
	if f, at := s.Freshness(t0.Add(3 * time.Second)); f != Stale || !at.Equal(t0) {
		t.Fatalf("3 s old: %v at %v, want stale at %v", f, at, t0)
	}
	if f, _ := s.Freshness(t0.Add(6 * time.Second)); f != Down {
		t.Fatalf("6 s old: %v, want down", f)
	}
	zero := New(WithFreshness(0, time.Second)) // nonsense values keep the defaults
	zero.ApplyEvents(reply("e1", 1, false, rx(1, t0, "GRP_TXT", 300)), t0)
	if f, _ := zero.Freshness(t0.Add(45 * time.Second)); f != Stale {
		t.Fatalf("defaults kept: 45 s old: %v, want stale", f)
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
	// Minute alignment: with KAMmon at 15:00:00 the window covers 14:01:00 to 15:01:00 on KAM's
	// clock, so the event at 14:00 (a full minute before the window) is not in it. 11 of 12
	// minutes carry 3000 ms: 33000/3600000 = 0.9166 percent.
	if len(w.Slots) != 60 || w.TotalAirtimeMS != 33000 || w.AirtimePercent < 0.913 || w.AirtimePercent > 0.920 {
		t.Fatalf("1h window: slots=%d airtime=%d percent=%.4f", len(w.Slots), w.TotalAirtimeMS, w.AirtimePercent)
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

func TestWindowMarksOnTheAxis(t *testing.T) {
	s := New()
	s.ApplyEvents(reply("e1", 1, false, rx(1, t0.Add(-20*time.Minute), "GRP_TXT", 100)), t0.Add(-20*time.Minute))
	s.ApplyEvents(reply("e2", 2, false), t0.Add(-10*time.Minute))                      // restart mark (MON-OV-5)
	s.ApplyEvents(reply("e3", 20, false), t0.Add(-9*time.Minute-500*time.Millisecond)) // restart again, same bar
	// First data of the new epoch arrives with a reset header, but the restart mark already covers it:
	s.ApplyEvents(reply("e3", 30, true, rx(30, t0.Add(-9*time.Minute), "GRP_TXT", 100)), t0.Add(-9*time.Minute))
	// Same epoch later with a reset header: events were missed (MON-DS-10).
	s.ApplyEvents(reply("e3", 40, true, rx(40, t0.Add(-8*time.Minute), "GRP_TXT", 100)), t0.Add(-8*time.Minute)) // gap mark

	w := s.Window(t0, 15*time.Minute)
	want := []WindowMark{{Slot: 4, Kind: "restart"}, {Slot: 6, Kind: "gap"}}
	if len(w.Marks) != len(want) {
		t.Fatalf("marks = %v, want %v", w.Marks, want)
	}
	for i, m := range want {
		if w.Marks[i] != m {
			t.Fatalf("marks[%d] = %v, want %v", i, w.Marks[i], m)
		}
	}
	if older := s.Window(t0, 5*time.Minute).Marks; len(older) != 0 {
		t.Fatalf("marks before the range must stay out: %v", older)
	}
}

// MON-DS-15: the store remembers KAM's clock offset from the newest status answer.
func TestClockOffsetFromStatus(t *testing.T) {
	s := New()
	s.SetStatus(kam.Status{Epoch: "e1", Now: t0.Add(5 * time.Second)}, t0)
	if got := s.ClockOffset(); got != 5*time.Second {
		t.Fatalf("offset = %v, want 5 s", got)
	}
	s.SetStatus(kam.Status{Epoch: "e1", Now: t0.Add(-90 * time.Second)}, t0)
	if got := s.ClockOffset(); got != -90*time.Second {
		t.Fatalf("offset = %v, want -90 s", got)
	}
	// KAM without a now is not a 60-year-old clock (MON-DS-15).
	s.SetStatus(kam.Status{Epoch: "e1"}, t0)
	if got := s.ClockOffset(); got != 0 {
		t.Fatalf("offset without a KAM now = %v, want 0", got)
	}
}

// MON-DS-18: events dated in the future (clock skew) must not grow the bucket map past 1440.
func TestFutureEventsStayOutOfBuckets(t *testing.T) {
	s := New()
	future := rx(1, t0.Add(3*time.Hour), "GRP_TXT", 100)
	s.ApplyEvents(reply("e1", 1, false, future, rx(2, t0.Add(-time.Minute), "GRP_TXT", 100)), t0)

	s.mu.RLock()
	n := len(s.buckets)
	s.mu.RUnlock()
	if n != 1 {
		t.Fatalf("buckets = %d, want only the normal minute (got future bucket)", n)
	}
	if got := s.Recent(2); len(got) != 2 || got[0].Seq != 2 || got[1].Seq != 1 {
		t.Fatalf("the ring keeps the skewed event too, recent = %+v", got)
	}
	w := s.Window(t0, 15*time.Minute)
	if w.RX != 1 || w.AirtimeMS[ClassGroupText] != 100 {
		t.Fatalf("window = %+v", w)
	}
	// A later poll on its own clock still cleans up; the bucket map stays bounded.
	s.ApplyEvents(reply("e1", 2, false, rx(3, t0.Add(30*time.Minute).Add(-time.Minute), "GRP_TXT", 100)), t0.Add(30*time.Minute))
	s.mu.RLock()
	n = len(s.buckets)
	s.mu.RUnlock()
	if n != 2 {
		t.Fatalf("buckets after skew = %d", n)
	}
}

// MON-DS-18: room snapshot limits are hard: 4 rooms, 100 clients each.
func TestRoomSnapshotLimits(t *testing.T) {
	s := New()
	rooms := make([]kam.Room, 6)
	clients := map[string][]kam.RoomClient{}
	for i := range rooms {
		id := fmt.Sprintf("r%d", i)
		rooms[i].ID, rooms[i].Name, rooms[i].Enabled = id, id, true
		list := make([]kam.RoomClient, 150)
		for j := range list {
			list[j].PublicKey = fmt.Sprintf("%s-c%d", id, j)
		}
		clients[id] = list
	}
	clients["ghost"] = []kam.RoomClient{{PublicKey: "x"}}
	s.SetRooms(rooms, clients, t0)

	stored, storedClients := s.Rooms()
	if len(stored) != MaxRooms {
		t.Fatalf("rooms = %d, want %d", len(stored), MaxRooms)
	}
	if got := storedClients["r0"]; len(got) != MaxClientsPerRoom {
		t.Fatalf("clients in r0 = %d, want %d", len(got), MaxClientsPerRoom)
	}
	if got := storedClients["ghost"]; got != nil {
		t.Fatalf("clients for a room that KAM no longer lists = %v", got)
	}
	if len(storedClients) != MaxRooms {
		t.Fatalf("client lists = %d, want %d", len(storedClients), MaxRooms)
	}
}

// MON-DS-13 per snapshot: events keep flowing while status is silent, and the two judgements
// stay separate.
func TestSnapshotFreshnessIsPerJob(t *testing.T) {
	s := New()
	s.ApplyEvents(reply("e1", 1, false, rx(1, t0.Add(-time.Minute), "GRP_TXT", 100)), t0)
	if f, _ := s.SnapshotFreshness("status", t0); f != Down {
		t.Fatalf("no status answers yet, status freshness = %v", f)
	}
	s.SetStatus(kam.Status{Epoch: "e1", Now: t0}, t0)
	if f, _ := s.SnapshotFreshness("status", t0); f != Fresh {
		t.Fatalf("status freshness = %v, want fresh", f)
	}
	// Forty seconds later the events stream is stale and the status snapshot is newer but stale too.
	now := t0.Add(40 * time.Second)
	if f, _ := s.SnapshotFreshness("events", now); f != Stale {
		t.Fatalf("events freshness = %v, want stale", f)
	}
	if f, _ := s.SnapshotFreshness("status", now); f != Stale {
		t.Fatalf("status freshness = %v, want stale", f)
	}
	// The overall answer (for the stale bar) takes the newest of all jobs.
	if f, _ := s.Freshness(now); f != Stale {
		t.Fatalf("overall freshness = %v, want stale", f)
	}
}

// MON-DS: rolling windows count every bucket that overlaps the range, so a packet inside the
// last hour survives a range boundary that falls mid-minute.
func TestWindowMinuteAligned(t *testing.T) {
	s := New()
	now := t0.Add(30 * time.Second) // 15:00:30: the window becomes 14:01:00 to 15:01:00
	s.ApplyEvents(reply("e1", 1, false,
		rx(1, now.Add(-time.Hour), "GRP_TXT", 100),                // 14:00:30, one minute before the window
		rx(2, now.Add(-time.Hour+61*time.Second), "GRP_TXT", 200), // 14:01:31, the first minute in it
	), now)
	w := s.Window(now, time.Hour)
	if w.From.Format("15:04") != "14:01" || w.To.Format("15:04") != "15:01" {
		t.Fatalf("window edges = %v .. %v, want 14:01 .. 15:01", w.From, w.To)
	}
	if w.Slots[0].AirtimeMS[ClassGroupText] != 200 {
		t.Fatalf("the first bar holds exactly its minute, not the boundary minute: %+v", w.Slots[0])
	}
}

// MON-DS: packets without measured airtime are counted instead of being silent weight.
func TestAirtimeUnknownIsTrackedAndWindowed(t *testing.T) {
	s := New()
	s.ApplyEvents(reply("e1", 1, false,
		kam.Event{Seq: 1, At: t0.Add(-time.Minute), Kind: kam.KindRX, PayloadType: "TRACE"},
		rx(2, t0.Add(-time.Minute), "GRP_TXT", 200),
	), t0)
	if got := s.Window(t0, 15*time.Minute).AirtimeUnknown; got != 1 {
		t.Fatalf("AirtimeUnknown = %d, want 1", got)
	}
}

// KAM before the room phase sends no room events; the Overview must not show a measured zero.
func TestRoomEventsSeen(t *testing.T) {
	s := New()
	if s.RoomEventsSeen() {
		t.Fatal("before any events, room events are unseen")
	}
	s.ApplyEvents(reply("e1", 1, false, rx(1, t0, "GRP_TXT", 100)), t0)
	if s.RoomEventsSeen() {
		t.Fatal("rx and tx events are not room events")
	}
	s.ApplyEvents(reply("e1", 2, false, kam.Event{Seq: 2, At: t0, Kind: kam.KindRoom, Room: "lobby", Event: "post"}), t0)
	if !s.RoomEventsSeen() {
		t.Fatal("a room event marks the store")
	}
}

// MON-DS-18: hard cap of exactly 1440 minute buckets.
func TestBucketCapIsExact(t *testing.T) {
	s := New()
	base := t0.Add(-2 * time.Hour)
	for i := 0; i < 1445; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		s.ApplyEvents(reply("e1", uint64(i+1), false, rx(uint64(i+1), at, "TRACE", 1)), at)
	}
	s.mu.RLock()
	n := len(s.buckets)
	s.mu.RUnlock()
	if n != BucketCount {
		t.Fatalf("bucket map holds %d, want the documented hard limit %d", n, BucketCount)
	}
}

// MON-DS-18: when KAMmon's clock turns out to be behind KAM's, the buckets that were future
// minutes go instead of lingering for hours.
func TestBucketsPrunedWhenClockMovesBack(t *testing.T) {
	s := New()
	ahead := t0.Add(30 * time.Minute)
	s.ApplyEvents(reply("e1", 1, false, rx(1, ahead, "TRACE", 100)), ahead)
	if s.Window(ahead, time.Hour).RX != 1 {
		t.Fatal("the forward clock bucketed the event")
	}
	s.ApplyEvents(reply("e1", 2, false), t0)
	s.mu.RLock()
	n := len(s.buckets)
	s.mu.RUnlock()
	if n != 0 {
		t.Fatalf("buckets = %d, the minute in KAM's future went", n)
	}
}

// MON-OV-5: history starts at the oldest timestamp the store keeps, whatever the sequence order.
func TestHistoryStartTracksOlderEvents(t *testing.T) {
	s := New()
	s.ApplyEvents(reply("e1", 1, false, rx(1, t0, "TRACE", 1)), t0)
	s.ApplyEvents(reply("e1", 2, false, rx(2, t0.Add(-30*time.Minute), "TRACE", 1)), t0)
	if got := s.HistoryStart(); !got.Equal(t0.Add(-30 * time.Minute)) {
		t.Fatalf("history starts at %v, want the older timestamp", got)
	}
}

// The ring wraps at 4096 events; history then starts at the oldest kept one.
func TestHistoryStartAfterRingWrap(t *testing.T) {
	s := New()
	events := make([]kam.Event, 0, EventRingSize+1)
	for i := 0; i <= EventRingSize; i++ {
		events = append(events, rx(uint64(i+1), t0.Add(time.Duration(i)*time.Second), "TRACE", 1))
	}
	s.ApplyEvents(reply("e1", 1, false, events...), t0)
	if got := s.HistoryStart(); !got.Equal(t0.Add(time.Second)) {
		t.Fatalf("after the wrap history starts at %v, want the oldest kept event", got)
	}
}

// Room-event availability is per KAM run (MON-OV-8): until the new KAM sends its own room event,
// the answer is "Unavailable", not a measured zero.
func TestRoomEventsSeenPerEpoch(t *testing.T) {
	s := New()
	s.ApplyEvents(reply("e1", 1, false, kam.Event{Seq: 1, At: t0, Kind: kam.KindRoom, Room: "r", Event: "post"}), t0)
	if !s.RoomEventsSeen() {
		t.Fatal("the room event marks availability")
	}
	s.ApplyEvents(reply("e2", 5, false, rx(5, t0, "GRP_TXT", 1)), t0)
	if s.RoomEventsSeen() {
		t.Fatal("after a KAM restart the new room availability is its own")
	}
}

// One consistent KAM clock: with KAM 35 minutes ahead of KAMmon its events still reach the window,
// because the window is drawn on KAM's clock (MON-DS-10, MON-DS-15).
func TestKAMAheadOfKAMmonKeepsEvents(t *testing.T) {
	s := New()
	kamAhead := t0.Add(35 * time.Minute)
	s.SetStatus(kam.Status{Epoch: "e1", Now: kamAhead}, t0)
	s.ApplyEvents(reply("e1", 1, false, rx(1, kamAhead, "GRP_TXT", 100)), t0)
	w := s.Window(t0, time.Hour)
	if w.From.Format("15:04") != "14:36" || w.To.Format("15:04") != "15:36" {
		t.Fatalf("window = %v .. %v, want 14:36 .. 15:36 on KAM's clock", w.From, w.To)
	}
	if w.RX != 1 || w.AirtimeMS[ClassGroupText] != 100 {
		t.Fatalf("the ahead-KAM event is in the chart: %+v", w)
	}
}

// PostHours sums accepted posts per hour for one room, matched by id or name like the last-hour
// count (MON-RM-1); unknown rooms, posts without a room and hours outside the day count nowhere.
func TestPostHoursSumsPerRoomPerHour(t *testing.T) {
	s := New()
	seq := uint64(0)
	post := func(at time.Time, room string) kam.Event {
		seq++
		return kam.Event{Seq: seq, At: at, Kind: kam.KindRoom, Event: "post", Room: room}
	}
	plain := func(at time.Time) kam.Event {
		seq++
		return kam.Event{Seq: seq, At: at, Kind: kam.KindRX, PayloadType: "GRP_TXT", AirtimeMS: ptr(100)}
	}
	events := []kam.Event{
		plain(t0.Add(-time.Minute)),              // a packet is not a post
		post(t0.Add(-90*time.Minute), "L1"),      // hour 21, matched by id
		post(t0.Add(-91*time.Minute), "Emerald"), // hour 21, matched by name
		post(t0.Add(-90*time.Minute), "other"),   // another room
		post(t0.Add(-90*time.Minute), ""),        // no room: counts for none
		post(t0.Add(20*time.Second), "L1"),       // hour 23, the current KAM hour
		post(t0.Add(20*time.Second), "L1"),
		post(t0.Add(20*time.Second), "Emerald"),
		plain(t0.Add(-24 * time.Hour).Add(30 * time.Minute)), // kept history, but before the day: not a bar
	}
	s.ApplyEvents(kam.EventsResponse{Epoch: "e1", Events: events}, t0)

	hours, full := s.PostHours(t0, "L1", "L1", "Emerald") // duplicate key counts once
	if hours[23] != 3 || hours[21] != 2 {
		t.Fatalf("hours 21/23 = %d/%d, want 2/3: %v", hours[21], hours[23], hours)
	}
	for i, n := range hours {
		if i != 21 && i != 23 && n != 0 {
			t.Errorf("hour %d holds %d posts", i, n)
		}
	}
	if !full {
		t.Error("history reaches back past the day start, so the chart may call itself complete")
	}
}

// A young history is not a quiet day: PostHours says the store holds less than a day, so a flat
// chart is labelled as young instead of claiming no posts (MON-RM-15).
func TestPostHoursSaysHistoryIsYoung(t *testing.T) {
	s := New()
	s.ApplyEvents(kam.EventsResponse{Epoch: "e1", Events: []kam.Event{
		{Seq: 1, At: t0, Kind: kam.KindRoom, Event: "post", Room: "L1"},
	}}, t0)
	hours, full := s.PostHours(t0, "L1")
	if hours[23] != 1 {
		t.Fatalf("newest hour = %d, want 1: %v", hours[23], hours)
	}
	if full {
		t.Error("a store older than minutes must not claim a full day of history")
	}
}
