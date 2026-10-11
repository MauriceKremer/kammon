package store

import (
	"sync"
	"time"

	"kammon.bytecoder.nl/internal/kam"
)

// Limits from docs/requirements/DataStore.md §3.
const (
	EventRingSize = 4096
	BucketCount   = 1440 // minutes in 24 hours
	MaxMarks      = 50
	FreshFor      = 10 * time.Second
	StaleFor      = 2 * time.Minute

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
	roomsAt        time.Time
	notServed      map[string]time.Time
}

func New() *Store {
	return &Store{
		ring:      make([]kam.Event, EventRingSize),
		buckets:   map[int64]*Bucket{},
		clients:   map[string][]kam.RoomClient{},
		notServed: map[string]time.Time{},
	}
}

// Cursor returns what to send to KAM next: the epoch and the last sequence number seen (MON-DS-6).
func (s *Store) Cursor() (epoch string, after uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.epoch, s.cursor
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
		s.addMark(Mark{At: now, Kind: "restart", Note: "KAM restarted"})
		s.epoch = resp.Epoch
		s.cursor = 0
	case resp.Reset && s.cursor > 0:
		s.addMark(Mark{At: now, Kind: "gap", Note: "Some events were missed"})
	}

	oldest := now.Add(-BucketCount*time.Minute).Unix() / 60
	latest := now.Unix() / 60
	for _, ev := range resp.Events {
		if ev.Seq <= s.cursor {
			continue
		}
		s.cursor = ev.Seq
		s.ring[s.next] = ev
		s.next = (s.next + 1) % len(s.ring)
		if s.count < len(s.ring) {
			s.count++
		}
		if s.count == 1 && ev.At.Before(s.historyStart) {
			s.historyStart = ev.At
		}
		if ev.Kind == kam.KindRoom {
			s.roomEventsSeen = true
		}
		// Hard bucket bounds (MON-DS-18): a clock-skewed or future-dated event stays in the ring,
		// but never grows the bucket map beyond the last 1440 minutes.
		if m := ev.At.Unix() / 60; m >= oldest && m <= latest {
			s.add(m, ev)
		}
	}
	for m := range s.buckets {
		if m < oldest {
			delete(s.buckets, m)
		}
	}
	s.eventsAt = now
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
	return freshnessOf(last, now)
}

// freshnessOf applies the MON-DS-13 rule: fresh under 10 s, stale 10 s to 2 min, down beyond that.
func freshnessOf(last, now time.Time) (Freshness, time.Time) {
	switch age := now.Sub(last); {
	case last.IsZero() || age >= StaleFor:
		return Down, last
	case age >= FreshFor:
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
	return freshnessOf(last, now)
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
	w := Window{From: now.Add(-d), To: now, SlotWidth: d / time.Duration(n), Slots: make([]Slot, n), PostsByRoom: map[string]int{}}
	for i := range w.Slots {
		w.Slots[i].Start = w.From.Add(time.Duration(i) * w.SlotWidth)
	}
	slotSec := int64(w.SlotWidth / time.Second)
	for minute, b := range s.buckets {
		start := minute * 60
		// Every minute that overlaps [From, To] counts (MON-DS: rolling windows). A boundary minute
		// counts wholly into the first/last bar, at most one minute outside the exact range.
		if start+60 <= w.From.Unix() || start >= w.To.Unix() {
			continue
		}
		idx := int((start - w.From.Unix()) / slotSec)
		if idx < 0 {
			idx = 0
		}
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
