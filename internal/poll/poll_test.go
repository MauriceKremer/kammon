package poll

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/kam"
	"kammon.bytecoder.nl/internal/kamfake"
	"kammon.bytecoder.nl/internal/store"
)

var t0 = time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)

func TestBackoffSequence(t *testing.T) {
	want := []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second}
	for n, w := range want {
		if got := Backoff(n); got != w {
			t.Errorf("Backoff(%d) = %v, want %v", n, got, w)
		}
	}
}

func TestJobsTakeTheirIntervalsFromEvery(t *testing.T) {
	_, p, _ := fixture(t)
	p.Every = map[string]time.Duration{"events": 5 * time.Second, "channels": time.Minute, "status": -time.Second}
	p.Timeout = 9 * time.Second
	got := map[string]time.Duration{}
	for _, j := range p.jobs() {
		got[j.name] = j.every
	}
	want := map[string]time.Duration{
		"events":   5 * time.Second,
		"status":   2 * time.Second, // a non-positive override is ignored
		"rooms":    10 * time.Second,
		"channels": time.Minute,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s interval = %v, want %v", name, got[name], w)
		}
	}
	if p.timeout() != 9*time.Second {
		t.Fatalf("timeout = %v, want 9s", p.timeout())
	}
	zero := &Poller{}
	if zero.timeout() != defaultTimeout {
		t.Fatalf("default timeout = %v", zero.timeout())
	}
}

func fixture(t *testing.T) (*kamfake.Fake, *Poller, *time.Time) {
	t.Helper()
	now := t0
	fake := kamfake.New(func() time.Time { return now }, 1)
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)
	p := &Poller{Reader: kam.NewHTTP(srv.URL, time.Second), Store: store.New(), Now: func() time.Time { return now }}
	p.defaults()
	return fake, p, &now
}

func TestBackfillFillsTheStoreFromTheWholeRing(t *testing.T) {
	_, p, _ := fixture(t)
	if err := p.Events(context.Background()); err != nil {
		t.Fatal(err)
	}
	epoch, after := p.Store.Cursor()
	status, err := p.Reader.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if epoch != status.Epoch || after != status.EventRing.LastSeq {
		t.Fatalf("cursor %q/%d, KAM %q/%d", epoch, after, status.Epoch, status.EventRing.LastSeq)
	}
	if w := p.Store.Window(t0, time.Hour); w.RX == 0 || w.TotalAirtimeMS == 0 {
		t.Fatalf("empty window after backfill: %+v", w)
	}
}

func TestOnDemandTextReadsPopulateTheStore(t *testing.T) {
	_, p, now := fixture(t)
	channelRequest := store.TextRequest{Kind: "channel", Channel: "#emmen", Limit: 50}
	roomRequest := store.TextRequest{Kind: "room", RoomID: "lobby", Limit: 20}
	p.Store.RequestText(channelRequest, *now)
	p.Store.RequestText(roomRequest, *now)
	p.TextRequests(context.Background())
	channel, _, exists, fresh := p.Store.RequestText(channelRequest, *now)
	if !exists || !fresh || channel.Channel != "#emmen" || len(channel.Messages) == 0 {
		t.Fatalf("channel history cache: %+v, exists=%v fresh=%v", channel, exists, fresh)
	}
	_, posts, exists, fresh := p.Store.RequestText(roomRequest, *now)
	if !exists || !fresh || len(posts) == 0 || posts[0].Body == "" {
		t.Fatalf("room posts cache: %+v, exists=%v fresh=%v", posts, exists, fresh)
	}
}

func TestPollingContinuesAndSurvivesAKamRestart(t *testing.T) {
	fake, p, now := fixture(t)
	ctx := context.Background()
	if err := p.Events(ctx); err != nil {
		t.Fatal(err)
	}
	_, before := p.Store.Cursor()

	*now = now.Add(20 * time.Second)
	if err := p.Events(ctx); err != nil {
		t.Fatal(err)
	}
	if _, after := p.Store.Cursor(); after <= before {
		t.Fatalf("cursor did not advance: %d -> %d", before, after)
	}

	fake.Restart()
	*now = now.Add(20 * time.Second)
	if err := p.Events(ctx); err != nil {
		t.Fatal(err)
	}
	marks := p.Store.Marks()
	if len(marks) != 1 || marks[0].Kind != "restart" {
		t.Fatalf("marks = %v", marks)
	}
}

type notFoundReader struct{ kam.Reader }

func (notFoundReader) Channels(context.Context) ([]kam.Channel, error) { return nil, kam.ErrNotFound }

func TestNotFoundStopsAskingForTenMinutes(t *testing.T) {
	_, p, now := fixture(t)
	p.Reader = notFoundReader{p.Reader}
	j := &job{name: "channels", every: 30 * time.Second, run: p.Channels}
	p.runJob(context.Background(), j)
	if !p.Store.NotServed("channels", *now) || !j.next.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("not-served=%v next=%v", p.Store.NotServed("channels", *now), j.next)
	}
}

func TestNotFoundStopsAskingForTheConfiguredWait(t *testing.T) {
	_, p, now := fixture(t)
	p.Reader = notFoundReader{p.Reader}
	p.NotServed = 2 * time.Minute
	j := &job{name: "channels", every: 30 * time.Second, run: p.Channels}
	p.runJob(context.Background(), j)
	if !p.Store.NotServed("channels", *now) || !j.next.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("not-served=%v next=%v", p.Store.NotServed("channels", *now), j.next)
	}
}

type failingReader struct{ kam.Reader }

func (failingReader) Status(context.Context) (kam.Status, error) {
	return kam.Status{}, errors.New("boom")
}

func TestFailuresBackOffAndOnlyThatReadSuffers(t *testing.T) {
	_, p, now := fixture(t)
	p.Reader = failingReader{p.Reader}
	status := &job{name: "status", every: 2 * time.Second, run: p.Status}
	events := &job{name: "events", every: time.Second, run: p.Events}
	for i, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		p.runJob(context.Background(), status)
		if got := status.next.Sub(*now); got != want {
			t.Fatalf("failure %d: wait %v, want %v", i+1, got, want)
		}
	}
	p.runJob(context.Background(), events)
	if events.failures != 0 {
		t.Fatalf("events job must be unaffected: %+v", events)
	}
	if f, _ := p.Store.Freshness(*now); f != store.Fresh {
		t.Fatalf("events succeeded, so the store is fresh; got %v", f)
	}
}

func TestRunStopsWhenTheContextEnds(t *testing.T) {
	_, p, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if epoch, _ := p.Store.Cursor(); epoch != "" {
			break
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
	if epoch, _ := p.Store.Cursor(); epoch == "" {
		t.Fatal("the first loop pass should have backfilled")
	}
}
