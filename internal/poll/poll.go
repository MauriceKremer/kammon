// Package poll is the one reader of KAM. It writes the store; pages never call KAM (MON-DS-1).
package poll

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"kammon.bytecoder.nl/internal/kam"
	"kammon.bytecoder.nl/internal/store"
)

const (
	defaultTimeout = 3 * time.Second  // the standard per-read timeout (MON-DS-2)
	notServedFor   = 10 * time.Minute // default pause before asking a 404'd read again
	eventsPerPage  = 2000
	maxPages       = 20 // bounds one backfill; the ring holds at most 65536 events
)

// Backoff is the wait after the nth failure in a row: 1, 2, 4, 8 seconds, then 10 (MON-DS-3).
func Backoff(failures int) time.Duration {
	if failures < 1 {
		return 0
	}
	if failures > 4 {
		return 10 * time.Second
	}
	return time.Second << (failures - 1)
}

type Poller struct {
	Reader kam.Reader
	Store  *store.Store
	Now    func() time.Time
	Log    *slog.Logger
	Tick   time.Duration
	// Every overrides how often a named read runs ("events", "status", "rooms", "channels");
	// a missing or non-positive entry keeps the standard interval (docs/requirements/DataStore.md §5).
	Every map[string]time.Duration
	// Timeout overrides the per-read timeout; zero keeps the 3 second default (MON-DS-2).
	Timeout time.Duration
	// NotServed overrides how long the poller stops asking a read that KAM answers with 404;
	// zero keeps 10 minutes.
	NotServed time.Duration
}

type job struct {
	name     string
	every    time.Duration
	run      func(context.Context) error
	failures int
	next     time.Time
}

// Run polls until ctx ends. KAMmon starts and serves pages even when KAM is down (MON-DS-5).
func (p *Poller) Run(ctx context.Context) {
	p.defaults()
	jobs := p.jobs()
	ticker := time.NewTicker(p.Tick)
	defer ticker.Stop()
	for {
		now := p.Now()
		p.TextRequests(ctx)
		for _, j := range jobs {
			if !now.Before(j.next) {
				p.runJob(ctx, j)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// jobs builds the periodic reads with their intervals; Every overrides a read's interval (§5).
func (p *Poller) jobs() []*job {
	return []*job{
		{name: "events", every: p.interval("events", time.Second), run: p.Events},
		{name: "status", every: p.interval("status", 2*time.Second), run: p.Status},
		{name: "rooms", every: p.interval("rooms", 10*time.Second), run: p.Rooms},
		{name: "channels", every: p.interval("channels", 30*time.Second), run: p.Channels},
		{name: "scripts", every: p.interval("scripts", 10*time.Second), run: p.Scripts},
	}
}

func (p *Poller) interval(name string, def time.Duration) time.Duration {
	if d, ok := p.Every[name]; ok && d > 0 {
		return d
	}
	return def
}

func (p *Poller) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return defaultTimeout
}

func (p *Poller) notServedWait() time.Duration {
	if p.NotServed > 0 {
		return p.NotServed
	}
	return notServedFor
}

// TextRequests services bounded on-demand history reads queued by the web layer (MON-DS-21).
func (p *Poller) TextRequests(ctx context.Context) {
	for _, request := range p.Store.TakeTextRequests(4) {
		readCtx, cancel := context.WithTimeout(ctx, p.timeout())
		var channel kam.ChannelMessages
		var posts []kam.RoomPost
		var err error
		switch request.Kind {
		case "channel":
			channel, err = p.Reader.ChannelMessages(readCtx, request.Channel, request.Limit, request.BeforeID, request.Query)
		case "room":
			posts, err = p.Reader.RoomPosts(readCtx, request.RoomID, request.Limit)
		default:
			err = errors.New("unknown text request")
		}
		cancel()
		now := p.Now()
		p.Store.CompleteText(request, channel, posts, now, err)
		p.Store.CountPoll("texts", err, now) // the server block counts every read (MON-SV-3)
		if err != nil {
			p.Log.Warn("KAM text read failed", "kind", request.Kind, "error", err)
		}
	}
}

// Scripts keeps the store's script list current (MON-AU-1). A failure keeps the old snapshot, so
// the page shows the last known state and the stale-data bar (MON-AU-5).
func (p *Poller) Scripts(ctx context.Context) error {
	list, err := p.Reader.Scripts(ctx)
	if err != nil {
		return err
	}
	p.Store.SetScripts(list, p.Now())
	return nil
}

func (p *Poller) defaults() {
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.Log == nil {
		p.Log = slog.Default()
	}
	if p.Tick == 0 {
		p.Tick = 250 * time.Millisecond
	}
}

func (p *Poller) runJob(ctx context.Context, j *job) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	err := j.run(ctx)
	now := p.Now()
	switch {
	case err == nil:
		p.Store.CountPoll(j.name, nil, now) // the server block counts every read (MON-SV-3)
		j.failures, j.next = 0, now.Add(j.every)
	case errors.Is(err, kam.ErrNotFound):
		p.Store.CountPoll(j.name, err, now)
		wait := p.notServedWait()
		p.Store.MarkNotServed(j.name, now.Add(wait))
		j.failures, j.next = 0, now.Add(wait)
	case ctx.Err() != nil && errors.Is(err, context.Canceled):
		// KAMmon stopped the read itself; it says nothing about KAM, so the counters skip it (MON-SV-3).
		j.next = now.Add(j.every)
	default:
		j.failures++
		j.next = now.Add(Backoff(j.failures))
		if j.failures == 1 || j.failures%10 == 0 {
			p.Log.Warn("KAM read failed", "read", j.name, "failures", j.failures, "error", err)
		}
	}
}

// Events asks for new events and repeats until a page comes back short: the first call is the backfill (MON-DS-7).
func (p *Poller) Events(ctx context.Context) error {
	for page := 0; page < maxPages; page++ {
		epoch, after := p.Store.Cursor()
		resp, err := p.Reader.Events(ctx, after, epoch, eventsPerPage)
		if err != nil {
			return err
		}
		p.Store.ApplyEvents(resp, p.Now())
		if len(resp.Events) < eventsPerPage {
			return nil
		}
	}
	return nil
}

func (p *Poller) Status(ctx context.Context) error {
	st, err := p.Reader.Status(ctx)
	if err != nil {
		return err
	}
	p.Store.SetStatus(st, p.Now())
	return nil
}

func (p *Poller) Channels(ctx context.Context) error {
	channels, err := p.Reader.Channels(ctx)
	if err != nil {
		return err
	}
	p.Store.SetChannels(channels, p.Now())
	return nil
}

// Rooms reads the room list and each enabled room's clients. A failure keeps the old snapshot.
func (p *Poller) Rooms(ctx context.Context) error {
	rooms, err := p.Reader.Rooms(ctx)
	if err != nil {
		return err
	}
	clients := make(map[string][]kam.RoomClient, len(rooms))
	for _, room := range rooms {
		if !room.Enabled {
			continue
		}
		list, err := p.Reader.RoomClients(ctx, room.ID)
		if err != nil {
			return err
		}
		clients[room.ID] = list
	}
	p.Store.SetRooms(rooms, clients, p.Now())
	return nil
}
