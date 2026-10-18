package store

import (
	"sync"
	"time"

	"kammon.bytecoder.nl/internal/kam"
)

// Limits from docs/requirements/DataStore.md §3. The freshness limits are the defaults;
// kammon.yml can set its own (D24).
const (
	EventRingSize = 4096
	BucketCount   = 1440 // minutes in 24 hours
	MaxMarks      = 50
	FreshFor      = 10 * time.Second // a snapshot older than this is no longer fresh (MON-DS-13)
	StaleFor      = 2 * time.Minute  // a snapshot older than this counts as down (MON-DS-13)

	// Hard limits from docs/requirements/DataStore.md §3 (MON-DS-18).
	MaxRooms              = 4
	MaxClientsPerRoom     = 100
	MaxRoomNamesPerBucket = 64
)

// Bucket is one minute of events summed per packet class.
type Bucket struct {
	Count     [ClassCount]int
	AirtimeMS [ClassCount]int64
	RX, TX    int
	Repeats   int
	Posts     int
	// AirtimeUnknown counts packets whose airtime was not measured (MON-DS: honest percentage).
	AirtimeUnknown int
	PostsByRoom    map[string]int // bounded by MaxRoomNamesPerBucket (MON-DS-18)
}

// Mark labels a place in the history where something happened.
type Mark struct {
	At   time.Time
	Kind string // "restart" or "gap"
	Note string
}

// Freshness says how old the newest answer from KAM is (MON-DS-13).
type Freshness int

const (
	Down Freshness = iota
	Stale
	Fresh
)

func (f Freshness) String() string { return [...]string{"down", "stale", "up"}[f] }

type Store struct {
	mu sync.RWMutex

	// Freshness limits of the MON-DS-13 rule, set only at construction (Option).
	freshFor time.Duration // a snapshot younger than this is fresh
	staleFor time.Duration // a snapshot older than this is down

	epoch  string
	cursor uint64

	ring  []kam.Event
	next  int
	count int

	buckets      map[int64]*Bucket // key: unix minute
	marks        []Mark
	historyStart time.Time

	status         kam.Status
	statusAt       time.Time
	clockOffset    time.Duration
	eventsAt       time.Time
	channelsAt     time.Time
	roomEventsSeen bool
	channels       []kam.Channel
	rooms          []kam.Room
	clients        map[string][]kam.RoomClient
	textPages      map[textKey]textPage
	textOrder      []textKey
	textRequests   []TextRequest
	textPending    map[textKey]bool
	roomsAt        time.Time
	notServed      map[string]time.Time
}

// Option tunes a Store at construction.
type Option func(*Store)

// WithFreshness sets the MON-DS-13 age limits: a snapshot older than staleAfter is no longer
// fresh, and one older than downAfter counts as down. Zero or nonsense values keep the defaults
// (FreshFor, StaleFor).
func WithFreshness(staleAfter, downAfter time.Duration) Option {
	return func(s *Store) {
		if staleAfter > 0 && downAfter > staleAfter {
			s.freshFor, s.staleFor = staleAfter, downAfter
		}
	}
}

func New(opts ...Option) *Store {
	s := &Store{
		freshFor:    FreshFor,
		staleFor:    StaleFor,
		ring:        make([]kam.Event, EventRingSize),
		buckets:     map[int64]*Bucket{},
		clients:     map[string][]kam.RoomClient{},
		textPages:   map[textKey]textPage{},
		textPending: map[textKey]bool{},
		notServed:   map[string]time.Time{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Cursor returns what to send to KAM next: the epoch and the last sequence number seen (MON-DS-6).
func (s *Store) Cursor() (epoch string, after uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.epoch, s.cursor
}

// kamNow is KAM's clock at the moment KAMmon's clock says now. Must hold s.mu.
func (s *Store) kamNow(now time.Time) time.Time {
	return now.Add(s.clockOffset)
}

// ApplyEvents adds a reply from GET /v1/events (MON-DS-7..12). KAMmon keeps its buckets across a KAM
// restart or a gap and adds a mark so the screen can say so.
func (s *Store) ApplyEvents(resp kam.EventsResponse, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case s.epoch == "":
		s.epoch = resp.Epoch
		s.historyStart = now
	case resp.Epoch != s.epoch:
		s.addMark(Mark{At: s.kamNow(now), Kind: "restart", Note: "KAM restarted"})
		s.epoch = resp.Epoch
		s.cursor = 0
		// Availability is per KAM run: until the new KAM sends a room event, the truth is
		// "this KAM sends no room events yet", not a measured zero.
		s.roomEventsSeen = false
	case resp.Reset && s.cursor > 0:
		s.addMark(Mark{At: s.kamNow(now), Kind: "gap", Note: "Some events were missed"})
	}

	// Events are bucketed by their own KAM timestamps (MON-DS-10). The band follows KAM's clock
	// (one consistent clock for charts), not KAMmon's, so a KAM clock ahead never loses events.
	kamMinute := s.kamNow(now).Unix() / 60
	latest := kamMinute + 2 // a little slack: events may carry KAM's clock of a moment later
	oldest := kamMinute - (BucketCount - 1)
	wrapped := false
	for _, ev := range resp.Events {
		if ev.Seq <= s.cursor {
			continue
		}
		s.cursor = ev.Seq
		s.ring[s.next] = ev
		s.next = (s.next + 1) % len(s.ring)
		if s.count < len(s.ring) {
			s.count++
		} else {
			wrapped = true
		}
		if ev.At.Before(s.historyStart) {
			s.historyStart = ev.At
		}
		if ev.Kind == kam.KindRoom {
			s.roomEventsSeen = true
		}
		// Hard bucket bounds (MON-DS-18): anything outside the band stays in the ring, and the
		// bucket map is pruned to the band after each poll, including when the clock moves back.
		if m := ev.At.Unix() / 60; m >= oldest && m <= latest {
			s.add(m, ev)
		}
	}
	if wrapped {
		// The ring just dropped its oldest entries; recompute where history truly starts from what
		// is still kept, since a later sequence number can carry an earlier timestamp.
		minAt := time.Time{}
		for _, kept := range s.ring {
			if kept.At.IsZero() {
				continue
			}
			if minAt.IsZero() || kept.At.Before(minAt) {
				minAt = kept.At
			}
		}
		if !minAt.IsZero() {
			s.historyStart = minAt
		}
	}
	s.pruneBuckets(oldest, latest)
	s.eventsAt = now
}

// pruneBuckets keeps the bucket map within the band and the documented cap of 1440 minutes
// (MON-DS-18): future buckets go when the clock moves back, and when the cap is exceeded the
// oldest entries drop. Must hold s.mu.
func (s *Store) pruneBuckets(oldest, latest int64) {
	for m := range s.buckets {
		if m < oldest || m > latest {
			delete(s.buckets, m)
		}
	}
	for len(s.buckets) > BucketCount {
		smallest := int64(1 << 62)
		for m := range s.buckets {
			if m < smallest {
				smallest = m
			}
		}
		delete(s.buckets, smallest)
	}
}

func (s *Store) add(minute int64, ev kam.Event) {
	b := s.buckets[minute]
	if b == nil {
		b = &Bucket{}
		s.buckets[minute] = b
	}
	switch ev.Kind {
	case kam.KindRX, kam.KindTX:
		c := Classify(ev)
		b.Count[c]++
		if ev.AirtimeMS != nil {
			b.AirtimeMS[c] += int64(*ev.AirtimeMS)
		} else {
			// The percentage only counts measured airtime; missing measurements are tracked so the
			// metric can say the percentage is too low instead of quietly calling them silent.
			b.AirtimeUnknown++
		}
		if ev.Kind == kam.KindRX {
			b.RX++
			if ev.Duplicate {
				b.Repeats++
			}
		} else {
			b.TX++
		}
	case kam.KindRoom:
		if ev.Event == "post" {
			b.Posts++
			// Per-room posts for the Overview room lines (MON-OV-8). Capped so a stream of unknown
			// room names cannot grow one bucket without bound.
			if ev.Room != "" && len(b.PostsByRoom) < MaxRoomNamesPerBucket {
				if b.PostsByRoom == nil {
					b.PostsByRoom = map[string]int{}
				}
				b.PostsByRoom[ev.Room]++
			}
		}
	}
}

func (s *Store) addMark(m Mark) {
	s.marks = append(s.marks, m)
	if len(s.marks) > MaxMarks {
		s.marks = s.marks[len(s.marks)-MaxMarks:]
	}
}

func (s *Store) SetStatus(st kam.Status, now time.Time) {
	s.mu.Lock()
	s.status, s.statusAt = st, now
	// KAM's clock minus our own, for MON-DS-15. Zero time means KAM left "now" out; treat as no offset.
	if st.Now.IsZero() {
		s.clockOffset = 0
	} else {
		s.clockOffset = st.Now.Sub(now)
	}
	s.mu.Unlock()
}

// ClockOffset is KAM's now minus KAMmon's own clock from the newest status answer (MON-DS-15).
func (s *Store) ClockOffset() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clockOffset
}

func (s *Store) SetChannels(ch []kam.Channel, now time.Time) {
	s.mu.Lock()
	s.channels = append([]kam.Channel(nil), ch...)
	s.channelsAt = now
	s.mu.Unlock()
}

func (s *Store) SetRooms(rooms []kam.Room, clients map[string][]kam.RoomClient, now time.Time) {
	s.mu.Lock()
	// Hard limits from docs/requirements/DataStore.md §3 (MON-DS-18): 4 rooms, 100 clients each.
	// Keep the first entries as they come from KAM; drop the rest.
	if len(rooms) > MaxRooms {
		rooms = rooms[:MaxRooms]
	}
	s.rooms = append([]kam.Room(nil), rooms...)
	known := map[string]bool{}
	for _, room := range s.rooms {
		known[room.ID] = true
	}
	for id, list := range clients {
		if !known[id] {
			continue
		}
		if len(list) > MaxClientsPerRoom {
			list = list[:MaxClientsPerRoom]
		}
		s.clients[id] = append([]kam.RoomClient(nil), list...)
	}
	for id := range s.clients {
		if !known[id] {
			delete(s.clients, id)
		}
	}
	s.roomsAt = now
	s.mu.Unlock()
}

// MarkNotServed remembers that KAM answered 404 for an endpoint, until the given time (design §8.3).
func (s *Store) MarkNotServed(name string, until time.Time) {
	s.mu.Lock()
	s.notServed[name] = until
	s.mu.Unlock()
}

// NotServed reports whether KAM is known not to offer the endpoint right now.
func (s *Store) NotServed(name string, now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return now.Before(s.notServed[name])
}

// Freshness reports how recently KAM answered anything at all; the bar at the top of the page
// uses it (MON-DS-13). For one snapshot, use SnapshotFreshness.
func (s *Store) Freshness(now time.Time) (Freshness, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	last := s.eventsAt
	if s.statusAt.After(last) {
		last = s.statusAt
	}
	if s.roomsAt.After(last) {
		last = s.roomsAt
	}
	if s.channelsAt.After(last) {
		last = s.channelsAt
	}
	return s.freshnessOf(last, now)
}

// freshnessOf applies the MON-DS-13 rule: fresh under freshFor, stale up to staleFor, down
// beyond that. The defaults are 10 s and 2 min.
func (s *Store) freshnessOf(last, now time.Time) (Freshness, time.Time) {
	switch age := now.Sub(last); {
	case last.IsZero() || age >= s.staleFor:
		return Down, last
	case age >= s.freshFor:
		return Stale, last
	default:
		return Fresh, last
	}
}

// SnapshotFreshness reports how recently one named snapshot was answered (MON-DS-13). Names are
// the poller jobs: "events", "status", "channels", "rooms". Successful event polling says
// nothing about the status or room snapshots: they are judged separately.
func (s *Store) SnapshotFreshness(name string, now time.Time) (Freshness, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var last time.Time
	switch name {
	case "events":
		last = s.eventsAt
	case "status":
		last = s.statusAt
	case "channels":
		last = s.channelsAt
	case "rooms":
		last = s.roomsAt
	default:
		return Down, time.Time{}
	}
	return s.freshnessOf(last, now)
}

// RoomEventsSeen reports whether the event ring has ever held a room event. KAM before the room
// phase sends none: counts of room posts would then be a measured zero, not a fact about rooms.
func (s *Store) RoomEventsSeen() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.roomEventsSeen
}

func (s *Store) Status() (kam.Status, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status, s.statusAt
}

func (s *Store) Channels() []kam.Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]kam.Channel(nil), s.channels...)
}

func (s *Store) Rooms() ([]kam.Room, map[string][]kam.RoomClient) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clients := make(map[string][]kam.RoomClient, len(s.clients))
	for id, list := range s.clients {
		clients[id] = append([]kam.RoomClient(nil), list...)
	}
	return append([]kam.Room(nil), s.rooms...), clients
}

func (s *Store) Marks() []Mark {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Mark(nil), s.marks...)
}

// Recent returns up to n events, newest first.
func (s *Store) Recent(n int) []kam.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n > s.count {
		n = s.count
	}
	out := make([]kam.Event, 0, n)
	for i := 0; i < n; i++ {
		idx := (s.next - 1 - i + len(s.ring)) % len(s.ring)
		out = append(out, s.ring[idx])
	}
	return out
}

// Slot is one bar of the airtime chart.
type Slot struct {
	Start     time.Time
	AirtimeMS [ClassCount]int64
	Count     [ClassCount]int
}

// WindowMark is a gap or KAM-restart mark for the chart axis (MON-OV-5).
type WindowMark struct {
	Slot int    // the bar the mark sits on
	Kind string // "restart" or "gap"
}

// Window is the data for the Overview over one time range (docs/requirements/Overview.md §3.2).
type Window struct {
	From, To  time.Time
	Slots     []Slot
	SlotWidth time.Duration
	Count     [ClassCount]int
	AirtimeMS [ClassCount]int64
	RX, TX    int
	Repeats   int
	Posts     int
	// AirtimeUnknown counts packets in the range whose airtime was not measured. The airtime
	// percentage excludes them, so the caption must say they exist instead of showing silence.
	AirtimeUnknown int
	TotalAirtimeMS int64
	// PostsByRoom are the accepted posts per room name or id over the window (MON-OV-8).
	PostsByRoom    map[string]int
	AirtimePercent float64
	PeakPercent    float64
	// HistoryStart is set when the store holds less than the range asked for (MON-OV-5).
	HistoryStart time.Time
	// Marks are the restart and gap marks inside the range, deduplicated per bar.
	Marks []WindowMark
}

// Window sums the buckets in [now-d, now). At most 60 slots: 15m gives 15 one-minute slots, 1h 60, 24h 60 of 24 minutes.
// Window sums one minute-aligned stretch of buckets over the range (docs/requirements/Overview.md
// §3.2). Both edges sit on minute boundaries of KAM's clock, so every bar holds an exact set of
// minutes: no bar double-counts a boundary minute, and no packet inside the range is lost. The end
// of the window is the current minute of KAM's clock, so freshly bucketed events land in the newest
// bar and the chart can lag KAMmon's clock by up to a minute — the axis says what it covers. At
// most 60 slots: 15m gives 15 one-minute bars, 1h 60, 24h 60 of 24 minutes.
func (s *Store) Window(now time.Time, d time.Duration) Window {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := int(d / time.Minute)
	if n > 60 {
		n = 60
	}
	if n < 1 {
		n = 1
	}
	// One consistent clock: the buckets are keyed by their own KAM timestamps (MON-DS-10), so the
	// window is drawn on KAM's clock too.
	w := Window{From: s.kamNow(now).Truncate(time.Minute).Add(time.Minute).Add(-d), SlotWidth: d / time.Duration(n), Slots: make([]Slot, n), PostsByRoom: map[string]int{}}
	w.To = w.From.Add(d)
	for i := range w.Slots {
		w.Slots[i].Start = w.From.Add(time.Duration(i) * w.SlotWidth)
	}
	slotSec := int64(w.SlotWidth / time.Second)
	for minute, b := range s.buckets {
		start := minute * 60
		if start < w.From.Unix() || start >= w.To.Unix() {
			continue
		}
		idx := int((start - w.From.Unix()) / slotSec)
		if idx >= n {
			idx = n - 1
		}
		for c := Class(0); c < ClassCount; c++ {
			w.Slots[idx].AirtimeMS[c] += b.AirtimeMS[c]
			w.Slots[idx].Count[c] += b.Count[c]
			w.AirtimeMS[c] += b.AirtimeMS[c]
			w.Count[c] += b.Count[c]
			w.TotalAirtimeMS += b.AirtimeMS[c]
		}
		w.RX += b.RX
		w.TX += b.TX
		w.Repeats += b.Repeats
		w.Posts += b.Posts
		w.AirtimeUnknown += b.AirtimeUnknown
		for name, n := range b.PostsByRoom {
			w.PostsByRoom[name] += n
		}
	}
	w.AirtimePercent = percent(w.TotalAirtimeMS, d)
	for _, sl := range w.Slots {
		var total int64
		for _, v := range sl.AirtimeMS {
			total += v
		}
		if p := percent(total, w.SlotWidth); p > w.PeakPercent {
			w.PeakPercent = p
		}
	}
	if !s.historyStart.IsZero() && s.historyStart.After(w.From) {
		w.HistoryStart = s.historyStart
	}
	seen := map[WindowMark]bool{}
	for _, m := range s.marks {
		if m.At.Before(w.From) || !m.At.Before(w.To) {
			continue
		}
		idx := int(m.At.Sub(w.From) / w.SlotWidth)
		if idx >= n {
			idx = n - 1
		}
		mk := WindowMark{Slot: idx, Kind: m.Kind}
		if !seen[mk] {
			seen[mk] = true
			w.Marks = append(w.Marks, mk)
		}
	}
	return w
}

func percent(ms int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(ms) / float64(d/time.Millisecond) * 100
}

// HistoryStart is when KAMmon's history begins, or the zero time before the first answer.
func (s *Store) HistoryStart() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.historyStart
}

// OldestEventAt is the oldest timestamp the event ring keeps, zero when it is empty; the Traffic
// page uses it for "History starts at HH:MM" (MON-TR-12).
func (s *Store) OldestEventAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.count == 0 {
		return time.Time{}
	}
	oldest := time.Time{}
	for i := 0; i < s.count; i++ {
		at := s.ring[(s.next-1-i+len(s.ring))%len(s.ring)].At
		if oldest.IsZero() || at.Before(oldest) {
			oldest = at
		}
	}
	return oldest
}
