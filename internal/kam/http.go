package kam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ErrNotFound means KAM answered 404: it is too old to offer this endpoint, and the UI says "Needs a newer KAM".
var ErrNotFound = errors.New("kam: endpoint not found")

// Reader is everything the poller reads from KAM. It is read-only (MON-DS-4).
type Reader interface {
	Status(ctx context.Context) (Status, error)
	Events(ctx context.Context, after uint64, epoch string, limit int) (EventsResponse, error)
	Channels(ctx context.Context) ([]Channel, error)
	ChannelMessages(ctx context.Context, name string, limit int, beforeID int64, query string) (ChannelMessages, error)
	Rooms(ctx context.Context) ([]Room, error)
	RoomClients(ctx context.Context, roomID string) ([]RoomClient, error)
	RoomPosts(ctx context.Context, roomID string, limit int) ([]RoomPost, error)
}

// HTTP reads from KAM's monitor listener.
type HTTP struct {
	base string
	hc   *http.Client
}

// NewHTTP returns a reader for the KAM API at base, for example http://127.0.0.1:8766.
// Each request has its own timeout (MON-DS-2).
func NewHTTP(base string, timeout time.Duration) *HTTP {
	return &HTTP{base: base, hc: &http.Client{Timeout: timeout}}
}

func (h *HTTP) get(ctx context.Context, path string, query url.Values, out any) error {
	target := h.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := h.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &failure) == nil && failure.Error != "" {
			return fmt.Errorf("kam %s: %d %s", path, resp.StatusCode, failure.Error)
		}
		return fmt.Errorf("kam %s: status %d", path, resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("kam %s: %w", path, err)
	}
	return nil
}

func (h *HTTP) Status(ctx context.Context) (Status, error) {
	var out Status
	err := h.get(ctx, "/v1/status", nil, &out)
	return out, err
}

func (h *HTTP) Events(ctx context.Context, after uint64, epoch string, limit int) (EventsResponse, error) {
	q := url.Values{"after": {strconv.FormatUint(after, 10)}, "limit": {strconv.Itoa(limit)}}
	if epoch != "" {
		q.Set("epoch", epoch)
	}
	var out EventsResponse
	err := h.get(ctx, "/v1/events", q, &out)
	return out, err
}

func (h *HTTP) Channels(ctx context.Context) ([]Channel, error) {
	var out struct {
		Channels []Channel `json:"channels"`
	}
	err := h.get(ctx, "/v1/channels", nil, &out)
	return out.Channels, err
}

func (h *HTTP) ChannelMessages(ctx context.Context, name string, limit int, beforeID int64, search string) (ChannelMessages, error) {
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if beforeID > 0 {
		query.Set("before_id", strconv.FormatInt(beforeID, 10))
	}
	if search != "" {
		query.Set("q", search)
	}
	var out ChannelMessages
	err := h.get(ctx, "/v1/channels/"+url.PathEscape(name)+"/messages", query, &out)
	return out, err
}

func (h *HTTP) Rooms(ctx context.Context) ([]Room, error) {
	var out struct {
		Rooms []Room `json:"rooms"`
	}
	err := h.get(ctx, "/v1/rooms", nil, &out)
	return out.Rooms, err
}

func (h *HTTP) RoomClients(ctx context.Context, roomID string) ([]RoomClient, error) {
	var out struct {
		Clients []RoomClient `json:"clients"`
	}
	err := h.get(ctx, "/v1/rooms/"+url.PathEscape(roomID)+"/clients", nil, &out)
	return out.Clients, err
}

func (h *HTTP) RoomPosts(ctx context.Context, roomID string, limit int) ([]RoomPost, error) {
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	var out struct {
		Posts []RoomPost `json:"posts"`
	}
	err := h.get(ctx, "/v1/rooms/"+url.PathEscape(roomID)+"/posts", query, &out)
	return out.Posts, err
}
