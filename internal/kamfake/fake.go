// Package kamfake is a fake KAM monitor API with generated traffic. It backs -demo mode and the tests.
// It follows docs/kam-api/FeatureMonitorApi.md: event ring, sequence numbers, epoch and reset.
package kamfake

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"kammon.bytecoder.nl/internal/kam"
)

const ringSize = 8192

type Fake struct {
	mu       sync.Mutex
	now      func() time.Time
	rng      *rand.Rand
	epoch    string
	started  time.Time
	seq      uint64
	ring     []kam.Event
	last     time.Time
	lastRoom time.Time
}

// New returns a fake whose clock is now. It starts with half an hour of traffic already in the ring.
func New(now func() time.Time, seed int64) *Fake {
	f := &Fake{now: now, rng: rand.New(rand.NewSource(seed)), started: now().Add(-3 * 24 * time.Hour)}
	f.restart(30 * time.Minute)
	return f
}

// Restart behaves like a KAM restart: a new epoch and an empty ring.
func (f *Fake) Restart() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restart(0)
	f.started = f.now()
}

func (f *Fake) restart(history time.Duration) {
	f.epoch = fmt.Sprintf("%016x", f.rng.Uint64())
	f.seq = 0
	f.ring = f.ring[:0]
	f.last = f.now().Add(-history)
}

var (
	senders  = []string{"PA3M", "MeshMike", "LoRaLab", "Scout", "Hilda"}
	repeats  = []string{"Emmen repeater", "Canal relay", "North ridge", "Dalen relay"}
	channels = []string{"#emmen", "#emmen", "#public", "#makers"}
	clients  = []string{
		"ed8342bc" + padKey, "72f188ca" + padKey, "b590f2e8" + padKey,
	}
)

const padKey = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func ptr[T any](v T) *T { return &v }

// advance generates events for every second up to now. Callers hold f.mu.
func (f *Fake) advance() {
	now := f.now()
	if now.Sub(f.last) > 45*time.Minute {
		f.last = now.Add(-45 * time.Minute)
	}
	for f.last.Before(now) {
		f.last = f.last.Add(time.Second)
		for n := f.rng.Intn(3); n > 0; n-- {
			f.emit(f.generate(f.last.Add(time.Duration(f.rng.Intn(900)) * time.Millisecond)))
		}
	}
}

func (f *Fake) generate(at time.Time) kam.Event {
	r := f.rng.Float64()
	length := 40 + f.rng.Intn(80)
	rxEvent := func(payload string) kam.Event {
		return kam.Event{
			At: at, Kind: kam.KindRX, PayloadType: payload, Route: []string{"flood", "flood", "direct"}[f.rng.Intn(3)],
			Hops: f.rng.Intn(4), Len: length,
			SNRdB: ptr(float64(f.rng.Intn(140)-20) / 10), RSSIdBm: ptr(-120 + f.rng.Intn(35)),
			AirtimeMS: ptr(50 + length*3), Duplicate: f.rng.Float64() < 0.2,
		}
	}
	switch {
	case r < 0.45:
		ev := rxEvent("GRP_TXT")
		ev.Channel, ev.Name = channels[f.rng.Intn(len(channels))], senders[f.rng.Intn(len(senders))]
		return ev
	case r < 0.70:
		ev := rxEvent("ADVERT")
		ev.Name, ev.AdvertRole, ev.Route = repeats[f.rng.Intn(len(repeats))], "repeater", "flood"
		return ev
	case r < 0.82:
		ev := rxEvent([]string{"REQ", "TXT_MSG", "ACK", "PATH"}[f.rng.Intn(4)])
		ev.Room, ev.DestHash, ev.Route, ev.Duplicate = "lobby", "8e", "direct", false
		return ev
	case r < 0.88:
		ev := rxEvent([]string{"TRACE", "CONTROL", "UNKNOWN_12"}[f.rng.Intn(3)])
		ev.Duplicate = false
		return ev
	case r < 0.94:
		if f.rng.Intn(2) == 0 {
			return kam.Event{At: at, Kind: kam.KindTX, PayloadType: "ACK", Route: "direct", Len: 8, AirtimeMS: ptr(74), OK: ptr(true), Origin: "room", Room: "lobby"}
		}
		return kam.Event{At: at, Kind: kam.KindTX, PayloadType: "GRP_TXT", Route: "flood", Len: 70, AirtimeMS: ptr(260), OK: ptr(true), Origin: "automation"}
	case r < 0.98:
		return kam.Event{At: at, Kind: kam.KindRoom, Room: "lobby", Event: []string{"post", "login_ok", "push_acked"}[f.rng.Intn(3)],
			Client: clients[f.rng.Intn(len(clients))], Role: "read_write"}
	default:
		return kam.Event{At: at, Kind: kam.KindScript, Script: "echo", OK: ptr(true), DurationMS: 1 + f.rng.Intn(4), Replies: 1}
	}
}

func (f *Fake) emit(ev kam.Event) {
	f.seq++
	ev.Seq = f.seq
	if ev.Kind == kam.KindRoom {
		f.lastRoom = ev.At
	}
	if len(f.ring) == ringSize {
		copy(f.ring, f.ring[1:])
		f.ring = f.ring[:ringSize-1]
	}
	f.ring = append(f.ring, ev)
}

func (f *Fake) firstSeq() uint64 {
	if len(f.ring) == 0 {
		return f.seq + 1
	}
	return f.ring[0].Seq
}

// Handler serves the monitor API.
func (f *Fake) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", f.status)
	mux.HandleFunc("GET /v1/events", f.events)
	mux.HandleFunc("GET /v1/channels", f.channels)
	mux.HandleFunc("GET /v1/channels/{name}/messages", f.channelMessages)
	mux.HandleFunc("GET /v1/rooms", f.rooms)
	mux.HandleFunc("GET /v1/rooms/{id}/clients", f.roomClients)
	mux.HandleFunc("GET /v1/rooms/{id}/posts", f.roomPosts)
	return mux
}

func (f *Fake) channelMessages(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != "#emmen" && name != "#public" && name != "#field" {
		http.Error(w, `{"error":"channel not found"}`, http.StatusNotFound)
		return
	}
	texts := []kam.ChannelMessage{
		{ID: 410, ReceivedAt: f.now().UTC().Add(-2 * time.Second), Sender: ptr("PA3M"), Body: ptr("Signal check from the north path"), HopCount: 2, SNRdB: ptr(8.5), RSSIdBm: ptr(-97)},
		{ID: 409, ReceivedAt: f.now().UTC().Add(-50 * time.Second), Sender: ptr("Scout"), Body: ptr("All clear at the bridge"), HopCount: 1, SNRdB: ptr(4.25), RSSIdBm: ptr(-103)},
		{ID: 408, ReceivedAt: f.now().UTC().Add(-3 * time.Minute), Sender: ptr("LoRaLab"), Body: ptr("Testing the new antenna"), HopCount: 0},
	}
	search := r.URL.Query().Get("q")
	if search != "" {
		filtered := texts[:0]
		for _, item := range texts {
			if item.Sender != nil && containsFold(*item.Sender, search) || item.Body != nil && containsFold(*item.Body, search) {
				filtered = append(filtered, item)
			}
		}
		texts = filtered
	}
	limit, _ := strconv.Atoi(orDefault(r.URL.Query().Get("limit"), "50"))
	if limit < 1 {
		limit = 50
	}
	if before, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64); before > 0 {
		filtered := texts[:0]
		for _, item := range texts {
			if item.ID < before {
				filtered = append(filtered, item)
			}
		}
		texts = filtered
	}
	texts = texts[:min(limit, len(texts))]
	writeJSON(w, kam.ChannelMessages{Channel: name, Messages: texts})
}

func containsFold(value, query string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(query))
}

// Serve listens on a free loopback port until ctx ends and returns the base URL.
func Serve(ctx context.Context, f *Fake) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	srv := &http.Server{Handler: f.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *Fake) events(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	after, err := strconv.ParseUint(orDefault(q.Get("after"), "0"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"after must be a number"}`, http.StatusBadRequest)
		return
	}
	limit, err := strconv.Atoi(orDefault(q.Get("limit"), "1000"))
	if err != nil || limit < 1 {
		http.Error(w, `{"error":"limit must be a positive number"}`, http.StatusBadRequest)
		return
	}
	limit = min(limit, 2000)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.advance()
	first := f.firstSeq()
	// A first request (after=0) never sets reset; the reply simply starts at first_seq.
	reset := (after > 0 && q.Get("epoch") != "" && q.Get("epoch") != f.epoch) || (after > 0 && after+1 < first)
	start := after
	if reset || after+1 < first {
		start = first - 1
	}
	out := kam.EventsResponse{Epoch: f.epoch, FirstSeq: first, LastSeq: f.seq, Reset: reset, Events: []kam.Event{}}
	if len(f.ring) > 0 && start+1 >= first {
		idx := int(start + 1 - first)
		if idx < len(f.ring) {
			end := min(len(f.ring), idx+limit)
			out.Events = append(out.Events, f.ring[idx:end]...)
		}
	}
	// D14: the poller continues from the largest sequence number this page served.
	out.NextAfter = f.seq
	if n := len(out.Events); n > 0 {
		out.NextAfter = out.Events[n-1].Seq
	}
	writeJSON(w, out)
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func (f *Fake) status(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.advance()
	var st kam.Status
	st.APIVersion, st.Epoch, st.Now, st.StartedAt, st.NodeName = 1, f.epoch, f.now().UTC(), f.started.UTC(), "kam"
	st.Modem.Link, st.Modem.DeviceName, st.Modem.FirmwareVersion = "up", "Emmen KISS", 1
	st.Radio = kam.Radio{FrequencyHz: 868_000_000, BandwidthHz: 125_000, SpreadingFactor: 8, CodingRate: 5, TxPowerDBm: ptr(uint8(14)), Source: "modem_echo"}
	st.DutyCycle.ConfiguredPercent, st.DutyCycle.UsedMS, st.DutyCycle.WindowS = 1.0, 21600, 3600
	st.Rooms.Enabled, st.Rooms.Count = true, 1
	st.Automation.Enabled, st.Automation.ScriptsEnabled = true, 2
	st.EventRing.Capacity, st.EventRing.FirstSeq, st.EventRing.LastSeq = ringSize, f.firstSeq(), f.seq
	writeJSON(w, st)
}

func (f *Fake) channels(w http.ResponseWriter, r *http.Request) {
	now := f.now().UTC().Truncate(time.Second)
	writeJSON(w, map[string]any{"channels": []kam.Channel{
		{Name: "#emmen", KeyStrategy: "derived", Enabled: true, StoredMessages: 3120, LastMessageAt: ptr(now.Add(-2 * time.Second))},
		{Name: "#public", KeyStrategy: "public", Enabled: true, StoredMessages: 2410, LastMessageAt: ptr(now.Add(-14 * time.Second))},
		{Name: "#field", KeyStrategy: "explicit", Enabled: false},
	}})
}

func (f *Fake) rooms(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.advance()
	var lastActivity *time.Time
	if !f.lastRoom.IsZero() {
		lastActivity = ptr(f.lastRoom.UTC())
	}
	writeJSON(w, map[string]any{"rooms": []kam.Room{{
		ID: "lobby", Name: "Emmen local", Enabled: true, PublicKey: "8e2cb91a" + padKey, MaxClients: 20,
		ClientsRemembered: 3, LastActivityAt: lastActivity, RetainedPosts: 412,
		Counters: map[string]uint64{"logins_ok": 41, "posts_accepted": 131, "pushes_sent": 320, "pushes_acked": 311, "push_timeouts": 9},
	}}})
}

func (f *Fake) roomClients(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") != "lobby" {
		http.Error(w, `{"error":"room not found"}`, http.StatusNotFound)
		return
	}
	now := f.now().UTC().Truncate(time.Second)
	writeJSON(w, map[string]any{"room": "lobby", "clients": []kam.RoomClient{
		{PublicKey: clients[0], Role: "admin", LastActivityAt: ptr(now.Add(-12 * time.Second)), PushState: "idle", Label: ptr("Maurice")},
		{PublicKey: clients[1], Role: "read_write", LastActivityAt: ptr(now.Add(-2 * time.Minute)), PushState: "idle", Label: ptr("PA3M")},
		{PublicKey: clients[2], Role: "read_write", LastActivityAt: ptr(now.Add(-48 * time.Hour)), PushState: "paused"},
	}})
}

func (f *Fake) roomPosts(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") != "lobby" {
		http.Error(w, `{"error":"room not found"}`, http.StatusNotFound)
		return
	}
	now := f.now().UTC().Truncate(time.Second)
	writeJSON(w, map[string]any{"room": "lobby", "posts": []kam.RoomPost{
		{ID: 91, CreatedAt: now.Add(-time.Minute), AuthorKey: clients[0], Body: "Hello Emmen"},
		{ID: 90, CreatedAt: now.Add(-5 * time.Minute), AuthorKey: clients[1], Body: "Radio check"},
	}})
}
