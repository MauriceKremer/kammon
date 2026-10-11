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
)

// Bucket is one minute of events summed per packet class.
type Bucket struct {
	Count     [ClassCount]int
	AirtimeMS [ClassCount]int64
	RX, TX    int
	Repeats   int
	Posts     int
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

	status    kam.Status
	statusAt  time.Time
	eventsAt  time.Time
	channels  []kam.Channel
	rooms     []kam.Room
	clients   map[string][]kam.RoomClient
	roomsAt   time.Time
	notServed map[string]time.Time
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
		if m := ev.At.Unix() / 60; m >= oldest {
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
	s.mu.Unlock()
}

func (s *Store) SetChannels(ch []kam.Channel) {
	s.mu.Lock()
	s.channels = append([]kam.Channel(nil), ch...)
	s.mu.Unlock()
}

func (s *Store) SetRooms(rooms []kam.Room, clients map[string][]kam.RoomClient, now time.Time) {
	s.mu.Lock()
	s.rooms = append([]kam.Room(nil), rooms...)
	s.clients = clients
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

// Freshness reports how recently KAM answered, and when (MON-DS-13).
func (s *Store) Freshness(now time.Time) (Freshness, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	last := s.eventsAt
	if s.statusAt.After(last) {
		last = s.statusAt
	}
	switch age := now.Sub(last); {
	case last.IsZero() || age >= StaleFor:
		return Down, last
	case age >= FreshFor:
		return Stale, last
	default:
		return Fresh, last
	}
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

// Window is the data for the Overview over one time range (docs/requirements/Overview.md §3.2).
type Window struct {
	From, To       time.Time
	Slots          []Slot
	SlotWidth      time.Duration
	Count          [ClassCount]int
	AirtimeMS      [ClassCount]int64
	RX, TX         int
	Repeats        int
	Posts          int
	TotalAirtimeMS int64
	AirtimePercent float64
	PeakPercent    float64
	// HistoryStart is set when the store holds less than the range asked for (MON-OV-5).
	HistoryStart time.Time
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
	w := Window{From: now.Add(-d), To: now, SlotWidth: d / time.Duration(n), Slots: make([]Slot, n)}
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
