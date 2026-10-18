package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"kammon.bytecoder.nl/internal/config"
	"kammon.bytecoder.nl/internal/kam"
	"kammon.bytecoder.nl/internal/store"
	"kammon.bytecoder.nl/internal/view"
)

type channelLink struct {
	Name, KeyType, State string
	Count                int
	Selected             bool
}

type channelMetric struct{ Label, Value, Caption string }
type eventRow struct {
	Time, Route string
	Hops        int
}
type messageRow struct {
	Time, Sender, Body string
	Hops               int
}

type channelsData struct {
	Channels                   []channelLink
	Selected, KeyType, State   string
	Query                      string
	Metrics                    []channelMetric
	CanRead, Hidden            bool
	Events                     []eventRow
	Messages                   []messageRow
	MessageURL, OlderURL       string
	NextBeforeID               *int64
	CacheMessage, ActivityNote string
	Unreadable                 int
}

func (s *Server) channelsPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "channels", s.pageFor(r, "channels", "Channels", "Decoded conversations and stored messages", s.channels(r)))
}

func (s *Server) channels(r *http.Request) channelsData {
	data := channelsData{Query: r.URL.Query().Get("q")}
	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	channels := s.store.Channels()
	selected := r.URL.Query().Get("channel")
	for _, channel := range channels {
		if channel.Name == selected {
			data.Selected = selected
			break
		}
	}
	if data.Selected == "" && len(channels) > 0 {
		data.Selected = channels[0].Name
	}
	now := s.now()
	metricsNow := now.Add(s.store.ClockOffset())
	if status, _ := s.store.Status(); !status.Now.IsZero() {
		metricsNow = status.Now
	}
	for _, channel := range channels {
		count, _, _ := s.channelMetrics(channel.Name, metricsNow)
		keyType := channel.KeyStrategy
		state := "Enabled"
		if !channel.Enabled {
			state = "Disabled"
		}
		data.Channels = append(data.Channels, channelLink{Name: channel.Name, KeyType: keyType, State: state, Count: count, Selected: channel.Name == data.Selected})
		if channel.Name == data.Selected {
			data.KeyType, data.State = keyType, state
		}
	}
	for _, event := range s.store.Recent(store.EventRingSize) {
		if event.Kind == kam.KindRX && event.PayloadType == "GRP_TXT" && event.Channel == "" && !event.At.Before(metricsNow.Add(-time.Hour)) {
			data.Unreadable++
		}
	}
	if data.Selected == "" {
		return data
	}
	data.CanRead = view.CanReadChannelText(principalOf(r).Role, s.cfg.ViewerTextChannels, data.Selected)
	count, airtime, last := s.channelMetrics(data.Selected, metricsNow)
	lastText := "No decoded message in the last hour"
	if !last.IsZero() {
		lastText = "Last decoded message " + ago(metricsNow.Sub(last))
	}
	data.Metrics = []channelMetric{
		{Label: "Messages · last hour", Value: thousands(count), Caption: "Decoded group texts; repeats excluded"},
		{Label: "Last decoded message", Value: lastText, Caption: "From observed RX events"},
		{Label: "Estimated airtime · last hour", Value: fmt.Sprintf("%.1f s", float64(airtime)/1000), Caption: "All attributed RX packets, including repeats"},
	}
	data.ActivityNote = "Airtime is attributed to decoded RX packets, not total radio occupancy. Messages KAM cannot decrypt are not assigned to a channel here."
	if data.CanRead {
		data.MessageURL = channelMessagesURL(data.Selected, data.Query, beforeID)
		page, _, exists, fresh := s.store.RequestText(store.TextRequest{Kind: "channel", Channel: data.Selected, Limit: 50, BeforeID: beforeID, Query: data.Query}, now)
		if exists {
			data.NextBeforeID = page.NextBeforeID
			for _, message := range page.Messages {
				sender, body := "Unknown sender", ""
				if message.Sender != nil {
					sender = *message.Sender
				}
				if message.Body != nil {
					body = *message.Body
				}
				data.Messages = append(data.Messages, messageRow{Time: message.ReceivedAt.Local().Format("15:04:05"), Sender: sender, Body: body, Hops: message.HopCount})
			}
			if !fresh {
				data.CacheMessage = "Stored messages may be out of date."
			}
		} else {
			freshness, _ := s.store.SnapshotFreshness("channels", now)
			if freshness == store.Down {
				data.CacheMessage = "Messages are unavailable right now."
			} else {
				data.CacheMessage = "Loading stored messages from KAM."
			}
		}
		if data.NextBeforeID != nil {
			data.OlderURL = channelMessagesURL(data.Selected, data.Query, *data.NextBeforeID)
		}
	} else {
		data.Hidden = true
		for _, event := range s.store.Recent(store.EventRingSize) {
			if event.Kind != kam.KindRX || event.PayloadType != "GRP_TXT" || event.Channel != data.Selected || event.At.Before(metricsNow.Add(-time.Hour)) {
				continue
			}
			data.Events = append(data.Events, eventRow{Time: event.At.Local().Format("15:04:05"), Route: event.Route, Hops: event.Hops})
			if len(data.Events) == 50 {
				break
			}
		}
	}
	return data
}

func (s *Server) channelMessagesPartial(w http.ResponseWriter, r *http.Request) {
	data := s.channels(r)
	if !data.CanRead {
		http.Error(w, "Not found.", http.StatusNotFound)
		return
	}
	t := s.pages["channels"]
	s.execute(w, http.StatusOK, t, "channel-messages", data)
}

func channelMessagesURL(channel, query string, beforeID int64) string {
	values := url.Values{"channel": {channel}, "q": {query}}
	if beforeID > 0 {
		values.Set("before_id", strconv.FormatInt(beforeID, 10))
	}
	return "/ui/channel-messages?" + values.Encode()
}

func (s *Server) channelMetrics(name string, now time.Time) (int, int64, time.Time) {
	count := 0
	var airtime int64
	var last time.Time
	for _, event := range s.store.Recent(store.EventRingSize) {
		if event.Kind != kam.KindRX || event.Channel != name || event.At.Before(now.Add(-time.Hour)) {
			continue
		}
		if event.PayloadType == "GRP_TXT" && !event.Duplicate {
			count++
			if event.At.After(last) {
				last = event.At
			}
		}
		if event.AirtimeMS != nil {
			airtime += int64(*event.AirtimeMS)
		}
	}
	return count, airtime, last
}

type roomLink struct {
	ID, Name, Activity string
	Selected           bool
	Remembered         int
	Posts              int
}

type clientRow struct {
	Label, Key, Role, PushState, LastHeard string
	Indicator                              view.Indicator
}

type postRow struct{ Author, Time, Body string }

type roomsData struct {
	Rooms                           []roomLink
	Selected, PublicKey, Name       string
	EmptyMessage                    string
	ClientsRemembered               int
	HeardLastHour, PostsLastHour    int
	LastActivity                    string
	DeliveryAcked, DeliveryTimedOut int
	PausedClients                   int
	Counters                        []string
	Clients                         []clientRow
	Posts                           []postRow
	IsAdmin                         bool
	CacheMessage, FeedURL           string
	Metrics                         []channelMetric
}

func (s *Server) roomsPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "rooms", s.pageFor(r, "rooms", "Rooms", "Activity and retained posts for hosted rooms", s.rooms(r)))
}

func (s *Server) rooms(r *http.Request) roomsData {
	data := roomsData{IsAdmin: principalOf(r).Role == config.RoleAdmin}
	rooms, clients := s.store.Rooms()
	selected := r.URL.Query().Get("room")
	for _, room := range rooms {
		if room.ID == selected && room.Enabled {
			data.Selected = selected
		}
	}
	if data.Selected == "" {
		for _, room := range rooms {
			if room.Enabled {
				data.Selected = room.ID
				break
			}
		}
	}
	status, _ := s.store.Status()
	nowKAM := status.Now
	if nowKAM.IsZero() {
		nowKAM = s.now().Add(s.store.ClockOffset())
	}
	hourEvents := s.store.Recent(store.EventRingSize)
	for _, room := range rooms {
		if !room.Enabled {
			continue
		}
		posts := 0
		for _, event := range hourEvents {
			if event.Kind == kam.KindRoom && event.Event == "post" && (event.Room == room.ID || event.Room == room.Name) && !event.At.Before(nowKAM.Add(-time.Hour)) {
				posts++
			}
		}
		activity := "No activity recorded"
		if room.LastActivityAt != nil {
			activity = "Last heard " + ago(nowKAM.Sub(*room.LastActivityAt))
		}
		data.Rooms = append(data.Rooms, roomLink{ID: room.ID, Name: room.Name, Activity: activity, Remembered: room.ClientsRemembered, Posts: posts, Selected: room.ID == data.Selected})
		if room.ID == data.Selected {
			data.Name, data.PublicKey, data.ClientsRemembered, data.PostsLastHour = room.Name, room.PublicKey, room.ClientsRemembered, posts
			data.LastActivity = activity
		}
	}
	if data.Selected == "" {
		switch {
		case s.store.NotServed("rooms", s.now()):
			data.EmptyMessage = "Needs a newer KAM."
		case !status.Rooms.Enabled:
			data.EmptyMessage = "Rooms are not enabled."
		case len(data.Rooms) == 0:
			data.EmptyMessage = "No enabled rooms."
		default:
			data.EmptyMessage = "Waiting for KAM's room list."
		}
		return data
	}
	var heard int
	for _, client := range clients[data.Selected] {
		if client.LastActivityAt != nil && !client.LastActivityAt.Before(nowKAM.Add(-time.Hour)) && !client.LastActivityAt.After(nowKAM) {
			heard++
		}
		if client.PushState == "paused" {
			data.PausedClients++
		}
	}
	data.HeardLastHour = heard
	for _, event := range hourEvents {
		if event.Kind != kam.KindRoom || event.Room != data.Selected || event.At.Before(nowKAM.Add(-time.Hour)) {
			continue
		}
		if event.Event == "push_acked" {
			data.DeliveryAcked++
		}
		if event.Event == "push_timeout" {
			data.DeliveryTimedOut++
		}
	}
	if data.IsAdmin {
		for _, client := range clients[data.Selected] {
			label := client.PublicKey
			if client.Label != nil && *client.Label != "" {
				label = *client.Label
			}
			lastHeard := "Unknown"
			if client.LastActivityAt != nil {
				lastHeard = client.LastActivityAt.Local().Format("2006-01-02 15:04:05")
			}
			data.Clients = append(data.Clients, clientRow{Label: label, Key: client.PublicKey, Role: client.Role, PushState: client.PushState, LastHeard: lastHeard, Indicator: view.Activity(nowKAM, client.LastActivityAt)})
		}
		data.FeedURL = "/ui/room-posts?room=" + url.QueryEscape(data.Selected)
		_, posts, exists, fresh := s.store.RequestText(store.TextRequest{Kind: "room", RoomID: data.Selected, Limit: 20}, s.now())
		if !exists {
			data.CacheMessage = "Loading recent posts from KAM."
		} else {
			if !fresh {
				data.CacheMessage = "Stored posts may be out of date."
			}
			labels := map[string]string{}
			for _, client := range clients[data.Selected] {
				if client.Label != nil && *client.Label != "" {
					labels[client.PublicKey] = *client.Label
				}
			}
			for _, post := range posts {
				author := post.AuthorKey
				if label := labels[author]; label != "" {
					author = label
				} else if len(author) > 12 {
					author = author[:12]
				}
				data.Posts = append(data.Posts, postRow{Author: author, Time: post.CreatedAt.Local().Format("15:04:05"), Body: post.Body})
			}
		}
	}
	for _, room := range rooms {
		if room.ID == data.Selected {
			for _, key := range []string{"logins_ok", "logins_failed", "logins_rate_limited", "posts_accepted", "posts_rejected"} {
				data.Counters = append(data.Counters, fmt.Sprintf("%s %s", strings.ReplaceAll(key, "_", " "), thousands(int(room.Counters[key]))))
			}
			break
		}
	}
	data.Metrics = []channelMetric{
		{Label: "Last activity", Value: data.LastActivity, Caption: "Radio activity, not a live connection"},
		{Label: "Posts · last hour", Value: thousands(data.PostsLastHour), Caption: "Accepted posts in the event ring"},
		{Label: "Remembered clients", Value: thousands(data.ClientsRemembered), Caption: fmt.Sprintf("%d heard in the last hour", data.HeardLastHour)},
	}
	return data
}

func (s *Server) roomPostsPartial(w http.ResponseWriter, r *http.Request) {
	if principalOf(r).Role != config.RoleAdmin {
		http.Error(w, "Not found.", http.StatusNotFound)
		return
	}
	data := s.rooms(r)
	s.execute(w, http.StatusOK, s.pages["rooms"], "room-posts", data)
}
