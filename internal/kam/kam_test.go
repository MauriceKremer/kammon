package kam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// The fixtures are the contract in machine form: decoding them with unknown fields rejected
// fails when the contract gains a field these types do not know yet.
func decodeStrict(t *testing.T, name string, out any) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/kam/" + name)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func TestFixturesDecodeStrictly(t *testing.T) {
	var status Status
	decodeStrict(t, "status.json", &status)
	if status.Radio.Source != "modem_echo" || status.EventRing.Capacity != 8192 {
		t.Fatalf("status = %+v", status)
	}

	// events.json is recorded from the live node: four real RX events, all with SNR.
	var events EventsResponse
	decodeStrict(t, "events.json", &events)
	if len(events.Events) != 4 || events.Events[0].SNRdB == nil || events.Events[0].Duplicate || events.Events[0].Kind != "rx" {
		t.Fatalf("recorded events = %+v", events)
	}
	// D14: the recorded page was complete, so the continuation cursor sits on the ring head.
	if events.NextAfter != events.LastSeq {
		t.Fatalf("next_after %d does not follow the served page (last_seq %d)", events.NextAfter, events.LastSeq)
	}

	// events-traits.json covers the fields the live ring had not produced yet (see its README).
	var traits EventsResponse
	decodeStrict(t, "events-traits.json", &traits)
	if !traits.Events[1].Duplicate || traits.Events[2].OK == nil || traits.Events[3].Kind != "room" {
		t.Fatalf("trait events = %+v", traits.Events)
	}

	var channels struct{ Channels []Channel }
	decodeStrict(t, "channels.json", &channels)
	if len(channels.Channels) != 3 || channels.Channels[2].LastMessageAt != nil {
		t.Fatalf("channels = %+v", channels)
	}

	var rooms struct{ Rooms []Room }
	decodeStrict(t, "rooms.json", &rooms)
	if rooms.Rooms[0].Counters["push_timeouts"] != 9 {
		t.Fatalf("rooms = %+v", rooms)
	}

	var clients struct {
		Room    string
		Clients []RoomClient
	}
	decodeStrict(t, "room_clients.json", &clients)
	if clients.Clients[2].LastActivityAt != nil || clients.Clients[0].Label == nil {
		t.Fatalf("clients = %+v", clients)
	}
}

func TestHTTPReaderMapsStatusCodes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"busy"}`, http.StatusServiceUnavailable)
	})
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") != "7" || r.URL.Query().Get("epoch") != "e1" || r.URL.Query().Get("limit") != "50" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"epoch":"e1","first_seq":1,"last_seq":7,"reset":false,"events":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	reader := NewHTTP(srv.URL, time.Second)

	if _, err := reader.Status(context.Background()); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("503 should be an ordinary error, got %v", err)
	}
	if _, err := reader.Channels(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing endpoint should be ErrNotFound, got %v", err)
	}
	if resp, err := reader.Events(context.Background(), 7, "e1", 50); err != nil || resp.Epoch != "e1" {
		t.Fatalf("events = %+v, %v", resp, err)
	}
}
