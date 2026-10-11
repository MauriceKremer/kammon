package kamfake

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/kam"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func get(t *testing.T, h http.Handler, target string, out any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d: %s", target, rec.Code, rec.Body)
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields() // the fake must match the contract types exactly
	if err := dec.Decode(out); err != nil {
		t.Fatalf("%s: %v", target, err)
	}
}

func TestFakeAnswersMatchTheContractTypes(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)}
	h := New(c.now, 1).Handler()
	var status kam.Status
	get(t, h, "/v1/status", &status)
	var channels struct{ Channels []kam.Channel }
	get(t, h, "/v1/channels", &channels)
	var rooms struct{ Rooms []kam.Room }
	get(t, h, "/v1/rooms", &rooms)
	var clients struct {
		Room    string
		Clients []kam.RoomClient
	}
	get(t, h, "/v1/rooms/lobby/clients", &clients)
	if status.Radio.Source != "modem_echo" || len(channels.Channels) != 3 || len(clients.Clients) != 3 {
		t.Fatalf("unexpected answers: %+v %+v %+v", status, channels, clients)
	}
}

func TestEventsCursorSemantics(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)}
	f := New(c.now, 1)
	h := f.Handler()

	var first kam.EventsResponse
	get(t, h, "/v1/events?after=0&limit=500", &first)
	if first.Reset || len(first.Events) != 500 || first.Events[0].Seq != first.FirstSeq {
		t.Fatalf("first page: reset=%v n=%d first=%d", first.Reset, len(first.Events), first.FirstSeq)
	}
	var next kam.EventsResponse
	get(t, h, "/v1/events?after="+itoa(first.Events[499].Seq)+"&epoch="+first.Epoch+"&limit=2000", &next)
	if next.Reset || next.Events[0].Seq != first.Events[499].Seq+1 {
		t.Fatalf("second page does not continue: %+v", next.Events[0].Seq)
	}

	// Caught up: nothing new when the clock has not moved.
	var caughtUp kam.EventsResponse
	get(t, h, "/v1/events?after="+itoa(next.LastSeq)+"&epoch="+first.Epoch+"&limit=2000", &caughtUp)
	if len(caughtUp.Events) != 0 || caughtUp.Reset {
		t.Fatalf("caught up: %+v", caughtUp)
	}

	// Time passes and events appear.
	c.t = c.t.Add(10 * time.Second)
	var more kam.EventsResponse
	get(t, h, "/v1/events?after="+itoa(next.LastSeq)+"&epoch="+first.Epoch, &more)
	if len(more.Events) == 0 {
		t.Fatal("expected new events after 10 s")
	}

	// A different epoch sets reset and starts at first_seq.
	f.Restart()
	var restarted kam.EventsResponse
	get(t, h, "/v1/events?after="+itoa(more.LastSeq)+"&epoch="+first.Epoch, &restarted)
	if !restarted.Reset || restarted.Epoch == first.Epoch {
		t.Fatalf("restart: reset=%v epoch=%q", restarted.Reset, restarted.Epoch)
	}
}

func itoa(n uint64) string {
	var b [20]byte
	i := len(b)
	for ; n >= 10; n /= 10 {
		i--
		b[i] = byte('0' + n%10)
	}
	i--
	b[i] = byte('0' + n)
	return string(b[i:])
}
