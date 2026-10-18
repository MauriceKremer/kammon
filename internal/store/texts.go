package store

import (
	"slices"
	"time"

	"kammon.bytecoder.nl/internal/kam"
)

const (
	MaxTextPages    = 10
	MaxTextRequests = 32
	TextPageTTL     = 5 * time.Second
)

type TextRequest struct {
	Kind     string
	Channel  string
	RoomID   string
	Limit    int
	BeforeID int64
	Query    string
}

type textKey struct {
	kind, name, query string
	limit             int
	beforeID          int64
}

type textPage struct {
	channel kam.ChannelMessages
	posts   []kam.RoomPost
	updated time.Time
}

// RequestText returns a cached page and queues a refresh when needed. Pages never call KAM.
func (s *Store) RequestText(request TextRequest, now time.Time) (kam.ChannelMessages, []kam.RoomPost, bool, bool) {
	key := keyFor(request)
	s.mu.Lock()
	defer s.mu.Unlock()
	page, exists := s.textPages[key]
	fresh := exists && now.Sub(page.updated) < TextPageTTL
	if !fresh && !s.textPending[key] && len(s.textRequests) < MaxTextRequests {
		s.textPending[key] = true
		s.textRequests = append(s.textRequests, request)
	}
	return page.channel, append([]kam.RoomPost(nil), page.posts...), exists, fresh
}

func (s *Store) TakeTextRequests(limit int) []TextRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 || len(s.textRequests) == 0 {
		return nil
	}
	if limit > len(s.textRequests) {
		limit = len(s.textRequests)
	}
	requests := append([]TextRequest(nil), s.textRequests[:limit]...)
	s.textRequests = append([]TextRequest(nil), s.textRequests[limit:]...)
	return requests
}

func (s *Store) CompleteText(request TextRequest, channel kam.ChannelMessages, posts []kam.RoomPost, now time.Time, err error) {
	key := keyFor(request)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.textPending, key)
	if err != nil {
		return
	}
	if request.Kind == "channel" && channel.Messages == nil {
		channel.Messages = []kam.ChannelMessage{}
	}
	if request.Kind == "room" && posts == nil {
		posts = []kam.RoomPost{}
	}
	s.textPages[key] = textPage{channel: channel, posts: append([]kam.RoomPost(nil), posts...), updated: now}
	s.textOrder = slices.DeleteFunc(s.textOrder, func(existing textKey) bool { return existing == key })
	s.textOrder = append(s.textOrder, key)
	for len(s.textPages) > MaxTextPages {
		oldest := s.textOrder[0]
		s.textOrder = s.textOrder[1:]
		delete(s.textPages, oldest)
	}
}

func keyFor(request TextRequest) textKey {
	name := request.Channel
	if request.Kind == "room" {
		name = request.RoomID
	}
	return textKey{kind: request.Kind, name: name, query: request.Query, limit: request.Limit, beforeID: request.BeforeID}
}
