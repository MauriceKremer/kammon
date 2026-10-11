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
	readTimeout   = 3 * time.Second
	notServedFor  = 10 * time.Minute
	eventsPerPage = 2000
	maxPages      = 20 // bounds one backfill; the ring holds at most 65536 events
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
	jobs := []*job{
		{name: "events", every: time.Second, run: p.Events},
		{name: "status", every: 2 * time.Second, run: p.Status},
		{name: "rooms", every: 10 * time.Second, run: p.Rooms},
		{name: "channels", every: 30 * time.Second, run: p.Channels},
	}
	ticker := time.NewTicker(p.Tick)
	defer ticker.Stop()
	for {
		now := p.Now()
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
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	err := j.run(ctx)
	now := p.Now()
	switch {
	case err == nil:
		j.failures, j.next = 0, now.Add(j.every)
	case errors.Is(err, kam.ErrNotFound):
		p.Store.MarkNotServed(j.name, now.Add(notServedFor))
		j.failures, j.next = 0, now.Add(notServedFor)
	case ctx.Err() != nil && errors.Is(err, context.Canceled):
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
