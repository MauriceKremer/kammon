package web

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/config"
	"kammon.bytecoder.nl/internal/kam"
	"kammon.bytecoder.nl/internal/store"
	webassets "kammon.bytecoder.nl/web"
)

var t0 = time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

type harness struct {
	ts  *httptest.Server
	s   *Server
	st  *store.Store
	now *time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	now := t0
	st := store.New()
	events := []kam.Event{
		{Seq: 1, At: t0.Add(-50 * time.Second), Kind: kam.KindRX, PayloadType: "GRP_TXT", Channel: "#emmen", Name: "SecretSender", AirtimeMS: ptr(300)},
		{Seq: 2, At: t0.Add(-40 * time.Second), Kind: kam.KindRX, PayloadType: "GRP_TXT", Channel: "#public", Name: "OpenSender", AirtimeMS: ptr(300)},
		{Seq: 3, At: t0.Add(-30 * time.Second), Kind: kam.KindRX, PayloadType: "ADVERT", Name: "Canal relay", AirtimeMS: ptr(190)},
		{Seq: 4, At: t0.Add(-20 * time.Second), Kind: kam.KindTX, PayloadType: "ACK", Origin: "room", Room: "lobby", AirtimeMS: ptr(90)},
		{Seq: 5, At: t0.Add(-10 * time.Second), Kind: kam.KindRoom, Room: "lobby", Event: "post"},
	}
	st.ApplyEvents(kam.EventsResponse{Epoch: "e1", FirstSeq: 1, LastSeq: 5, Events: events}, t0)
	status := kam.Status{Epoch: "e1", Now: t0, StartedAt: t0.Add(-86 * time.Hour), NodeName: "kam"}
	status.Radio = kam.Radio{FrequencyHz: 868_000_000, BandwidthHz: 125_000, SpreadingFactor: 8, CodingRate: 5}
	status.DutyCycle.ConfiguredPercent, status.DutyCycle.UsedMS, status.DutyCycle.WindowS = 1.0, 21600, 3600
	status.Rooms.Enabled, status.Rooms.Count = true, 1
	status.Automation.Enabled, status.Automation.ScriptsEnabled, status.Automation.QueueDepth = true, 2, 1
	st.SetStatus(status, t0)
	st.SetChannels([]kam.Channel{{Name: "#emmen", KeyStrategy: "derived", Enabled: true}, {Name: "#public", KeyStrategy: "public", Enabled: true}}, t0)
	st.SetRooms([]kam.Room{{ID: "lobby", Name: "Emmen local", Enabled: true, ClientsRemembered: 9, LastActivityAt: ptr(t0.Add(-12 * time.Second))}}, nil, t0)

	srv, err := New(Deps{Config: config.Demo(), Store: st, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &harness{ts: ts, s: srv, st: st, now: &now}
}

func (h *harness) client(t *testing.T) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (h *harness) login(t *testing.T, name, password string) *http.Client {
	t.Helper()
	c := h.client(t)
	resp, err := c.PostForm(h.ts.URL+"/login", url.Values{"name": {name}, "password": {password}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login %s: status %d", name, resp.StatusCode)
	}
	return c
}

func get(t *testing.T, c *http.Client, target string, headers ...string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), resp.Header
}

func TestUnauthenticatedRequestsAreSentToLogin(t *testing.T) {
	h := newHarness(t)
	c := h.client(t)
	status, _, header := get(t, c, h.ts.URL+"/")
	if status != http.StatusSeeOther || header.Get("Location") != "/login" {
		t.Fatalf("page: %d %q", status, header.Get("Location"))
	}
	status, _, header = get(t, c, h.ts.URL+"/ui/overview", "HX-Request", "true")
	if status != http.StatusUnauthorized || header.Get("HX-Redirect") != "/login" {
		t.Fatalf("partial: %d %q", status, header.Get("HX-Redirect"))
	}
	if status, _, _ = get(t, c, h.ts.URL+"/static/kammon.css"); status != http.StatusOK {
		t.Fatalf("static files are public, got %d", status)
	}
	status, body, _ := get(t, c, h.ts.URL+"/login")
	if status != http.StatusOK || strings.Contains(body, "Emmen") || strings.Contains(body, "Observed") {
		t.Fatalf("the login page must reveal nothing about the node: %d", status)
	}
}

func TestWrongPasswordGivesOneGenericMessage(t *testing.T) {
	h := newHarness(t)
	for _, creds := range [][2]string{{"admin", "nope"}, {"nobody", "admin"}} {
		resp, err := h.client(t).PostForm(h.ts.URL+"/login", url.Values{"name": {creds[0]}, "password": {creds[1]}})
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(body), "Wrong name or password.") {
			t.Fatalf("%v: %d", creds, resp.StatusCode)
		}
	}
}

func TestViewerSeesOverviewButNotAccounts(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	status, body, _ := get(t, c, h.ts.URL+"/")
	if status != http.StatusOK || !strings.Contains(body, "Observed packet airtime") || !strings.Contains(body, "Sign out") {
		t.Fatalf("overview: %d", status)
	}
	if strings.Contains(body, `href="/accounts"`) {
		t.Fatal("the Accounts page must not be offered to a viewer (MON-AX-12)")
	}
	if status, _, _ = get(t, c, h.ts.URL+"/accounts"); status != http.StatusForbidden {
		t.Fatalf("accounts for a viewer: %d", status)
	}
	if status, _, _ = get(t, c, h.ts.URL+"/modem"); status != http.StatusOK {
		t.Fatalf("modem page: %d", status)
	}
}

func TestAdminKeepsPowerFromAnyAddress(t *testing.T) {
	h := newHarness(t)
	u, _ := url.Parse(h.ts.URL)
	// Ask the mux directly, with a peer address from another computer: httptest.NewRequest
	// defaults to 192.0.2.1:1234, which is not a loopback address, and the added forwarding
	// header pretends a proxy is in the path. KAMmon adds no transport rules, so the
	// session's role alone decides (MON-AX-8, MON-AX-23, D31).
	asAdmin := func(target string) (int, string) {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		for _, ck := range h.login(t, "admin", "admin").Jar.Cookies(u) {
			r.AddCookie(ck)
		}
		rec := httptest.NewRecorder()
		h.s.Handler().ServeHTTP(rec, r)
		return rec.Code, rec.Body.String()
	}
	status, body := asAdmin("/")
	if status != http.StatusOK || !strings.Contains(body, `href="/accounts"`) || !strings.Contains(body, "Admin") {
		t.Fatalf("a remote admin keeps the admin pages: %d", status)
	}
	if status, _ = asAdmin("/accounts"); status != http.StatusOK {
		t.Fatalf("a remote admin keeps /accounts: %d", status)
	}

	// A viewer from the same address stays a viewer, and admin routes still answer 403.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	for _, ck := range h.login(t, "viewer", "viewer").Jar.Cookies(u) {
		r.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	h.s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `href="/accounts"`) {
		t.Fatalf("a remote viewer stays a viewer: %d", rec.Code)
	}
	r2 := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	for _, ck := range h.login(t, "viewer", "viewer").Jar.Cookies(u) {
		r2.AddCookie(ck)
	}
	rec2 := httptest.NewRecorder()
	h.s.Handler().ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("a remote viewer on /accounts: %d, want 403 (MON-AX-11)", rec2.Code)
	}
}

func TestViewerNeverReceivesHiddenSenderNames(t *testing.T) {
	h := newHarness(t)
	viewer, admin := h.login(t, "viewer", "viewer"), h.login(t, "admin", "admin")
	for _, target := range []string{"/", "/ui/overview"} {
		_, body, _ := get(t, viewer, h.ts.URL+target)
		if strings.Contains(body, "SecretSender") {
			t.Fatalf("%s leaked a sender on a channel that is not allow-listed", target)
		}
		for _, want := range []string{"OpenSender", "Canal relay", "Sender hidden"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: viewer page should contain %q", target, want)
			}
		}
		_, body, _ = get(t, admin, h.ts.URL+target)
		if !strings.Contains(body, "SecretSender") {
			t.Errorf("%s: the admin should see the sender", target)
		}
	}
}

func TestChannelAndRoomPagesRespectViewerVisibility(t *testing.T) {
	h := newHarness(t)
	viewerClient := h.login(t, "viewer", "viewer")
	adminClient := h.login(t, "admin", "admin")
	clients := []kam.RoomClient{
		{PublicKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Role: "admin", Label: ptr("RoomAdmin"), LastActivityAt: ptr(t0.Add(-48 * time.Hour))},
	}
	h.st.SetRooms([]kam.Room{{ID: "lobby", Name: "Emmen local", Enabled: true, ClientsRemembered: 1}}, map[string][]kam.RoomClient{"lobby": clients}, t0)
	channelText := ptr("private channel message")
	channelSender := ptr("PrivateSender")
	h.st.CompleteText(store.TextRequest{Kind: "channel", Channel: "#emmen", Limit: 50}, kam.ChannelMessages{Channel: "#emmen", Messages: []kam.ChannelMessage{{ID: 2, Sender: channelSender, Body: channelText}}}, nil, t0, nil)
	roomBody := []kam.RoomPost{{ID: 1, Body: "secret room post", AuthorKey: clients[0].PublicKey, CreatedAt: t0}}
	h.st.CompleteText(store.TextRequest{Kind: "room", RoomID: "lobby", Limit: 20}, kam.ChannelMessages{}, roomBody, t0, nil)
	for _, target := range []string{"/channels?channel=%23emmen", "/ui/channel-messages?channel=%23emmen"} {
		_, body, _ := get(t, viewerClient, h.ts.URL+target)
		if strings.Contains(body, "private channel message") || strings.Contains(body, "PrivateSender") || (strings.HasPrefix(target, "/channels") && !strings.Contains(body, "Text hidden")) {
			t.Errorf("viewer channel view leaked text or omitted hidden state: %s", target)
		}
		_, body, _ = get(t, adminClient, h.ts.URL+target)
		if !strings.Contains(body, "private channel message") || !strings.Contains(body, "PrivateSender") {
			t.Errorf("admin channel view omitted stored message: %s", target)
		}
	}
	_, viewerRooms, _ := get(t, viewerClient, h.ts.URL+"/rooms?room=lobby")
	if strings.Contains(viewerRooms, "RoomAdmin") || strings.Contains(viewerRooms, "secret room post") || !strings.Contains(viewerRooms, "Client activity and posts are visible to the administrator.") {
		t.Fatal("viewer room page exposed client or post data")
	}
	_, adminRooms, _ := get(t, adminClient, h.ts.URL+"/rooms?room=lobby")
	if !strings.Contains(adminRooms, "RoomAdmin") || !strings.Contains(adminRooms, "Stale Node") || !strings.Contains(adminRooms, "secret room post") {
		t.Fatal("admin room page omitted client activity or posts")
	}
}

// The Posts metric carries the small 24 hour posts chart (MON-RM-15); its caption made room for
// it, and the chart belongs to the strip that a viewer also sees (MON-RM-12).
func TestRoomMetricStripHasPostsTrend(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/rooms?room=lobby")
	if !strings.Contains(body, `class="posts-trend"`) || !strings.Contains(body, "Accepted posts per hour over the last 24 hours: 1 in total, busiest hour 1.") {
		t.Fatalf("room strip lacks the posts trend: %s", body)
	}
	if strings.Contains(body, "Accepted posts in the event ring") {
		t.Fatal("the removed ring caption is still shown")
	}
}

// The Messages metric carries the small last-hour chart (docs/requirements/Channels.md §3); the
// harness feed puts one #public message 40 s back, so its square is the last minute of the hour.
func TestChannelMetricStripHasTheHourChart(t *testing.T) {
	h := newHarness(t)
	_, body, _ := get(t, h.login(t, "viewer", "viewer"), h.ts.URL+"/channels?channel=%23public")
	for _, want := range []string{
		"Decoded messages over the last 60 minutes: 1 in total, in 1 of the 60 minutes. Every square is a minute with at least one message.",
		`<rect class="c-msgs" x="590.50" y="15.00" width="9.00" height="9.00"/>`,
		"60 min",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("channel strip lacks the hour chart: %s", body)
		}
	}
}

// Minute arithmetic of the messages chart from a fixed event list, including repeats (MON-CH-2):
// two non-repeats one to two minutes back share the same square, a repeat lands in no square, and
// an event at exactly the hour edge still lands in the first minute, so every minute with counted
// messages draws its one square.
func TestChannelMetricMinutesAreTheRollingHour(t *testing.T) {
	h := newHarness(t)
	h.st.ApplyEvents(kam.EventsResponse{Epoch: "e1", FirstSeq: 6, LastSeq: 9, Events: []kam.Event{
		{Seq: 6, At: t0.Add(-61 * time.Second), Kind: kam.KindRX, PayloadType: "GRP_TXT", Channel: "#public"},
		{Seq: 7, At: t0.Add(-61 * time.Second), Kind: kam.KindRX, PayloadType: "GRP_TXT", Channel: "#public", Duplicate: true},
		{Seq: 8, At: t0.Add(-90 * time.Second), Kind: kam.KindRX, PayloadType: "GRP_TXT", Channel: "#public"},
		{Seq: 9, At: t0.Add(-time.Hour), Kind: kam.KindRX, PayloadType: "GRP_TXT", Channel: "#public"},
	}}, t0)
	_, body, _ := get(t, h.login(t, "viewer", "viewer"), h.ts.URL+"/channels?channel=%23public")
	for _, want := range []string{
		"Decoded messages over the last 60 minutes: 4 in total, in 3 of the 60 minutes. Every square is a minute with at least one message.",
		`<rect class="c-msgs" x="0.50" y="15.00" width="9.00" height="9.00"/>`,   // the hour edge, kept
		`<rect class="c-msgs" x="580.50" y="15.00" width="9.00" height="9.00"/>`, // minute 58, two messages, one square
		`<rect class="c-msgs" x="590.50" y="15.00" width="9.00" height="9.00"/>`, // minute 59, one message
	} {
		if !strings.Contains(body, want) {
			t.Errorf("channel minute chart is wrong: %s", body)
		}
	}
	if got := strings.Count(body, `<rect class="c-msgs"`); got != 3 {
		t.Errorf("squares = %d, want one per minute with messages", got)
	}
}

// The strip updates itself every 2 s: the polled metrics partial carries the graph and the numbers,
// and the channel-list counts ride along out of band (MON-CH-13); no reload is needed.
func TestChannelMetricsPartialUpdatesCountersAndList(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/channels?channel=%23public")
	if !strings.Contains(body, `id="channel-metrics" hx-get="/ui/channel-metrics?channel=%23public" hx-trigger="every 2s" hx-swap="outerHTML"`) {
		t.Fatalf("the strip does not poll for its own updates: %s", body)
	}
	if !strings.Contains(body, `id="channel-messages" hx-get="/ui/channel-messages?channel=%23public&amp;q=" hx-trigger="every 10s"`) {
		t.Fatalf("the message list does not poll at half the text cache: %s", body)
	}
	h.st.ApplyEvents(kam.EventsResponse{Epoch: "e1", FirstSeq: 6, LastSeq: 6, Events: []kam.Event{
		{Seq: 6, At: t0.Add(-5 * time.Second), Kind: kam.KindRX, PayloadType: "GRP_TXT", Channel: "#public"},
	}}, t0)
	status, partial, _ := get(t, c, h.ts.URL+"/ui/channel-metrics?channel=%23public")
	if status != http.StatusOK {
		t.Fatalf("metrics partial: %d", status)
	}
	for _, want := range []string{
		`<strong>2</strong>`, // the count moved with the new message
		"Decoded messages over the last 60 minutes: 2 in total, in 1 of the 60 minutes. Every square is a minute with at least one message.",
		`<rect class="c-msgs" x="590.50" y="15.00" width="9.00" height="9.00"/>`, // both in the last minute, one square
		`<nav class="resource-list" aria-label="Channels" id="channel-list" hx-swap-oob="true">`,
		`<strong>#public</strong><span class="mono">2</span>`, // the list count rides along
	} {
		if !strings.Contains(partial, want) {
			t.Errorf("metrics partial is missing %q: %s", want, partial)
		}
	}
	if strings.Contains(partial, "private channel message") || strings.Contains(partial, "SecretSender") {
		t.Error("the metrics partial must not carry message text (MON-CH-8, MON-CH-11)")
	}
}

// The Rooms strip updates itself every 2 s like the Channels one (MON-RM-16): a new post moves the
// numbers, the bar and the room-list count, without a reload. The partial carries numbers only,
// never client keys or post text (MON-RM-12).
func TestRoomMetricsPartialUpdatesCountersAndList(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/rooms?room=lobby")
	if !strings.Contains(body, `id="room-metrics" hx-get="/ui/room-metrics?room=lobby" hx-trigger="every 20s" hx-swap="outerHTML"`) {
		t.Fatalf("the strip does not poll at half the rooms frequency: %s", body)
	}
	h.st.ApplyEvents(kam.EventsResponse{Epoch: "e1", FirstSeq: 6, LastSeq: 6, Events: []kam.Event{
		{Seq: 6, At: t0.Add(-5 * time.Second), Kind: kam.KindRoom, Room: "lobby", Event: "post"},
	}}, t0)
	status, partial, _ := get(t, c, h.ts.URL+"/ui/room-metrics?room=lobby")
	if status != http.StatusOK {
		t.Fatalf("metrics partial: %d", status)
	}
	for _, want := range []string{
		`<strong>2</strong>`, // the post count moved with the new post
		"Accepted posts per hour over the last 24 hours: 2 in total, busiest hour 2.",
		`<nav class="resource-list" aria-label="Rooms" id="room-list" hx-swap-oob="true">`,
		`<span class="mono">2 posts/h</span>`, // the list count rides along
	} {
		if !strings.Contains(partial, want) {
			t.Errorf("room metrics partial is missing %q: %s", want, partial)
		}
	}
	_, adminPage, _ := get(t, h.login(t, "admin", "admin"), h.ts.URL+"/rooms?room=lobby")
	if !strings.Contains(adminPage, `id="room-posts" hx-get="/ui/room-posts?room=lobby" hx-trigger="every 10s"`) {
		t.Errorf("the room feed does not poll at half the text cache: %s", adminPage)
	}
	_, adminBody, _ := get(t, h.login(t, "admin", "admin"), h.ts.URL+"/ui/room-metrics?room=lobby")
	for _, bad := range []string{"secret room post", "0123456789abcdef"} {
		if strings.Contains(adminBody, bad) {
			t.Errorf("the room metrics partial must not carry %q (MON-RM-12)", bad)
		}
	}
}

func TestPartialKeepsPollingAndCarriesOutOfBandParts(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	_, body, header := get(t, c, h.ts.URL+"/ui/overview?range=15m", "HX-Request", "true")
	for _, want := range []string{`id="overview-live"`, `hx-trigger="every 2s"`, `range=15m`, `id="stale-bar"`, `id="node-block"`, "868.000 MHz", "Uptime 3d 14h"} {
		if !strings.Contains(body, want) {
			t.Errorf("partial is missing %q", want)
		}
	}
	if header.Get("Cache-Control") != "no-store" {
		t.Error("partials must not be cached")
	}
	_, body, _ = get(t, c, h.ts.URL+"/ui/overview?range=bogus")
	if !strings.Contains(body, `range=1h`) {
		t.Error("an unknown range falls back to one hour")
	}
}

func TestOverviewNumbers(t *testing.T) {
	h := newHarness(t)
	_, body, _ := get(t, h.login(t, "viewer", "viewer"), h.ts.URL+"/")
	// 880 ms of airtime in an hour is 0.02 percent (shown as 0.0); three packets heard and one sent; one room post.
	for _, want := range []string{">0.0 ", ">4</p>", "3 RX / 1 TX · 0 repeats", "Within limit · 1.0% configured", "/ 1 rooms", "Last activity 12 seconds ago", "9 remembered"} {
		if !strings.Contains(body, want) {
			t.Errorf("overview is missing %q", want)
		}
	}
}

func TestSilentKamShowsTheStaleBar(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	if _, body, _ := get(t, c, h.ts.URL+"/"); strings.Contains(body, "has not answered") {
		t.Fatal("fresh data must not show the bar")
	}
	*h.now = t0.Add(45 * time.Second)
	_, body, _ := get(t, c, h.ts.URL+"/")
	if !strings.Contains(body, "KAM has not answered for 45 s.") || !strings.Contains(body, "slow to answer") {
		t.Fatal("stale data must say so")
	}
}

func TestLogoutNeedsSameOriginAndEndsTheSession(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	req, _ := http.NewRequest(http.MethodPost, h.ts.URL+"/logout", nil)
	req.Header.Set("Origin", "http://evil.example")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site logout: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodPost, h.ts.URL+"/logout", nil)
	req.Header.Set("Origin", h.ts.URL)
	req.Header.Set("HX-Request", "true")
	if resp, err = c.Do(req); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("HX-Redirect") != "/login" {
		t.Fatalf("logout: %d header %q", resp.StatusCode, resp.Header.Get("HX-Redirect"))
	}
	if status, _, _ := get(t, c, h.ts.URL+"/"); status != http.StatusSeeOther {
		t.Fatalf("after logout the overview must redirect, got %d", status)
	}
	// MON-AX-18: a plain post without the htmx header does not reach the change route.
	c2 := h.login(t, "viewer", "viewer")
	req, _ = http.NewRequest(http.MethodPost, h.ts.URL+"/logout", nil)
	req.Header.Set("Origin", h.ts.URL)
	if resp, err = c2.Do(req); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("logout without the htmx header: %d", resp.StatusCode)
	}
}

// Every page the server sends — app pages, the login page — links the shared
// stylesheet at the one version; a bare copy under an old URL is a separate
// cache entry that can hold the old rules and ship pages looking wrong. This
// test keeps the version shared and the cache headers honest: the pages stay
// no-store, the shared static files stay cacheable.
func TestEveryPageLinksTheSharedStylesheetVersion(t *testing.T) {
	h := newHarness(t)
	want := `/static/kammon.css?v=` + AssetVersion
	for _, target := range []string{"/login", "/"} {
		c := h.client(t)
		if target == "/" {
			c = h.login(t, "viewer", "viewer")
		}
		_, body, header := get(t, c, h.ts.URL+target)
		if !strings.Contains(body, want) {
			t.Errorf("%s: page misses shared stylesheet version %s", target, AssetVersion)
		}
		if strings.Contains(body, `/static/kammon.css"`) {
			t.Errorf("%s: page links the stylesheet without the version", target)
		}
		if ref := firstUndefaultedStaticRef(body); ref != "" {
			t.Errorf("%s: page loads a static file without the shared version: %s", target, ref)
		}
		if header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", target, header.Get("Cache-Control"))
		}
	}
}

// firstUndefaultedStaticRef finds the first `src="/static/..."` without a `?v=`
// cache-buster: a separate cache entry in which an old file keeps living. `<use href="/static/...">`
// references to the icon sprite pass on purpose; the sprite rarely changes.
func firstUndefaultedStaticRef(s string) string {
	for i := 0; ; {
		k := strings.Index(s[i:], `src="/static/`)
		if k < 0 {
			return ""
		}
		j := k + i + len(`src="`)
		end := strings.IndexByte(s[j:], '"')
		if end < 0 {
			return ""
		}
		ref := s[j : j+end]
		if !strings.Contains(ref, "?v=") {
			return ref
		}
		i = j + end
	}
}

func TestThemeCookieSetsTheDocumentTheme(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	u, _ := url.Parse(h.ts.URL)
	c.Jar.SetCookies(u, []*http.Cookie{{Name: themeCookie, Value: "light"}})
	if _, body, _ := get(t, c, h.ts.URL+"/"); !strings.Contains(body, `data-theme="light"`) {
		t.Fatal("light theme cookie must reach the page")
	}
	c.Jar.SetCookies(u, []*http.Cookie{{Name: themeCookie, Value: "neon"}})
	if _, body, _ := get(t, c, h.ts.URL+"/"); strings.Contains(body, "data-theme") {
		t.Fatal("an invalid theme value must be ignored")
	}
}

func TestEveryRouteStatesItsAccessLevel(t *testing.T) {
	s, err := New(Deps{Config: config.Demo(), Store: store.New()})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range s.routes() {
		if r.access != public && r.access != viewer && r.access != admin {
			t.Errorf("%s has no access level (MON-AX-11)", r.pattern)
		}
		if r.handler == nil {
			t.Errorf("%s has no handler", r.pattern)
		}
	}
}

// Admin sees the two newest clients per room with the activity indicator and the automation
// counts; a viewer sees neither (MON-OV-9, MON-OV-10).
func TestOverviewRoomsClientsAndAutomation(t *testing.T) {
	h := newHarness(t)
	now := *h.now
	clients := map[string][]kam.RoomClient{"lobby": {
		{PublicKey: "key-mid", LastActivityAt: ptr(now.Add(-30 * time.Minute))},
		{PublicKey: "key-new", LastActivityAt: ptr(now.Add(-12 * time.Second)), Label: ptr("Maurice")},
		{PublicKey: "key-old", LastActivityAt: ptr(now.Add(-48 * time.Hour))},
	}}
	h.st.SetRooms([]kam.Room{{ID: "lobby", Name: "Emmen local", Enabled: true, ClientsRemembered: 9, LastActivityAt: ptr(now.Add(-12 * time.Second))}}, clients, now)

	_, body, _ := get(t, h.login(t, "admin", "admin"), h.ts.URL+"/")
	for _, want := range []string{"Maurice", "key-mid", "ind fresh", "2 scripts ready", "Queue depth 1 · dropped 0", "1 posts last hour"} {
		if !strings.Contains(body, want) {
			t.Errorf("admin overview is missing %q", want)
		}
	}
	if strings.Contains(body, "key-old") {
		t.Error("only the two newest clients per room belong on the page (MON-OV-9)")
	}

	viewer := h.login(t, "viewer", "viewer")
	_, body, _ = get(t, viewer, h.ts.URL+"/")
	if strings.Contains(body, "Maurice") || strings.Contains(body, "key-mid") {
		t.Error("a viewer must not receive client names or keys (MON-OV-9)")
	}
	if !strings.Contains(body, "Client activity and posts are visible to the administrator.") {
		t.Error("the viewer gets the admin-only note (MON-RM-12 wording)")
	}
}

// MON-DS-15: the sidebar shows KAM time corrected by the measured offset, and notes a big one.
func TestNodeBlockShowsKamTime(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/")
	want := "KAM time " + h.now.Local().Format("15:04:05")
	if !strings.Contains(body, want) {
		t.Fatal("the sidebar shows KAM time: " + want)
	}
	if strings.Contains(body, "KAM clock") {
		t.Fatal("a small offset gets no note")
	}

	// A skewed KAM clock is visible in words, not only in a number.
	st, then := h.st.Status()
	st.Now = then.Add(-75 * time.Second)
	h.st.SetStatus(st, then)
	_, body, _ = get(t, c, h.ts.URL+"/")
	if !strings.Contains(body, "KAM clock 1 min behind this computer") {
		t.Errorf("clock note missing: %s", body)
	}
}

// The radio summary is two lines: frequency on its own, then SF, BW and CR.
// It keeps the fraction of a non-whole kHz bandwidth (62500 Hz is "62.5", not
// "62") and shows the coding rate from the status (contract §7.1). Without a
// status snapshot it shows "Freq.: —" and no radio settings.
func TestNodeBlockRadioShowsBandwidthFractionAndCodingRate(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")

	_, body, _ := get(t, c, h.ts.URL+"/")
	if !strings.Contains(body, "Freq.: 868.000 MHz") || !strings.Contains(body, "SF8 / BW125 / CR5") {
		t.Fatalf("a whole kHz keeps no trailing decimal: %s", body)
	}

	st, then := h.st.Status()
	st.Radio = kam.Radio{FrequencyHz: 869_618_000, BandwidthHz: 62_500, SpreadingFactor: 7, CodingRate: 5}
	h.st.SetStatus(st, then)
	_, body, _ = get(t, c, h.ts.URL+"/")
	for _, want := range []string{"Freq.: 869.618 MHz", "SF7 / BW62.5 / CR5"} {
		if !strings.Contains(body, want) {
			t.Fatalf("radio summary missing %q: %s", want, body)
		}
	}

	h.st.SetStatus(kam.Status{}, then)
	_, body, _ = get(t, c, h.ts.URL+"/")
	if !strings.Contains(body, "Freq.: —") || strings.Contains(body, "SF7 / BW62.5 / CR5") {
		t.Fatalf("waiting state shows no radio settings: %s", body)
	}
}

// Fonts and the icon sprite are self-hosted: no CDN reference anywhere in the assets (slice 1).
func TestAssetsAreVendoredAndServed(t *testing.T) {
	h := newHarness(t)
	res, err := webassets.FS.ReadFile("static/icons.svg")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res), `<symbol id="sun"`) {
		t.Error("the sprite misses its icons")
	}
	for _, woff2 := range []string{"hanken-grotesk-5.3.0-latin-wght-normal.woff2", "syncopate-5.3.0-latin-700-normal.woff2"} {
		buf, err := webassets.FS.ReadFile("static/fonts/" + woff2)
		if err != nil {
			t.Fatalf("%s: %v", woff2, err)
		}
		if string(buf[:4]) != "wOF2" {
			t.Errorf("%s is not woff2", woff2)
		}
	}
	c := h.login(t, "viewer", "viewer")
	if status, _, _ := get(t, c, h.ts.URL+"/static/icons.svg"); status != 200 {
		t.Errorf("sprite status %d", status)
	}
	if status, _, _ := get(t, c, h.ts.URL+"/static/fonts/syncopate-5.3.0-latin-700-normal.woff2"); status != 200 {
		t.Errorf("font status %d", status)
	}
}

// Without room-post events (KAM phase 1) the counts must not be a measured zero (MON-OV-8,
// honest labels); packets without measured airtime say so in the caption (MON-OV-2).
func TestOverviewUnavailableCountsSaySo(t *testing.T) {
	// New store without the room-post event from the scaffold feed.
	st := store.New()
	events := []kam.Event{
		{Seq: 1, At: t0.Add(-40 * time.Second), Kind: kam.KindRX, PayloadType: "GRP_TXT", Channel: "#public", Name: "OpenSender", AirtimeMS: ptr(300)},
		{Seq: 2, At: t0.Add(-30 * time.Second), Kind: kam.KindRX, PayloadType: "TRACE"},
	}
	st.ApplyEvents(kam.EventsResponse{Epoch: "e1", FirstSeq: 1, LastSeq: 2, Events: events}, t0)
	status := kam.Status{Epoch: "e1", Now: t0, NodeName: "kam"}
	status.Rooms.Enabled, status.Rooms.Count = true, 1
	st.SetStatus(status, t0)
	st.SetRooms([]kam.Room{{ID: "lobby", Name: "Emmen local", Enabled: true, ClientsRemembered: 3}}, nil, t0)

	srv, err := New(Deps{Config: config.Demo(), Store: st, Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	// The two servers do not share sessions, so log in on this one.
	jar, _ := cookiejar.New(nil)
	vlogin := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := vlogin.PostForm(ts.URL+"/login", url.Values{"name": {"admin"}, "password": {"admin"}})
	if err != nil || resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("fresh server login: %v %v", err, resp.StatusCode)
	}
	_, body, _ := get(t, vlogin, ts.URL+"/")
	for _, want := range []string{"Unavailable: this KAM sends no room-post events yet", "1 without measured airtime, so the percentage is too low"} {
		if !strings.Contains(body, want) {
			t.Errorf("overview is missing %q", want)
		}
	}
}

// The pause control from MON-OV-11 with its icon and title, wired to the script.
func TestOverviewPauseControl(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/ui/overview?range=15m", "HX-Request", "true")
	for _, want := range []string{
		`id="pause"`, `aria-pressed="false"`, "Pause live refresh", `href="/static/icons.svg#pause"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overview partial is missing %q", want)
		}
	}
	_, body, _ = get(t, c, h.ts.URL+"/")
	if !strings.Contains(body, `src="/static/live.js?v=`+AssetVersion+`"`) {
		t.Error("the page loads the pause script")
	}
}

// Old room and status snapshots announce their age on the page instead of looking current (MON-DS-13).
func TestOverviewMarksOldSnapshots(t *testing.T) {
	h := newHarness(t)
	// The event stream keeps answering while the status and room snapshots fall behind: the
	// overall answer (the stale bar) stays quiet, the per-snapshot blocks say how old they are.
	now := t0.Add(3 * time.Minute)
	*h.now = now
	h.st.ApplyEvents(kam.EventsResponse{Epoch: "e1", FirstSeq: 10, LastSeq: 10, Events: []kam.Event{{Seq: 10, At: now.Add(-time.Second), Kind: kam.KindRX, PayloadType: "TRACE"}}}, now)
	h.st.SetStatus(kam.Status{Epoch: "e1", Now: t0, NodeName: "kam"}, t0)
	h.st.SetRooms([]kam.Room{{ID: "lobby", Name: "Emmen local", Enabled: true, ClientsRemembered: 9}}, nil, t0)

	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/")
	if !strings.Contains(body, fmt.Sprintf("Rooms from %s · KAM not answering.", t0.Local().Format("15:04:05"))) {
		t.Errorf("old room snapshot says so: %s", body)
	}
	if !strings.Contains(body, "KAM not answering") || !strings.Contains(body, `class="dot down"`) {
		t.Errorf("node block judges the status snapshot: %s", body)
	}
	if strings.Contains(body, "has not answered") {
		t.Error("the event stream still answers, so the page-wide bar stays hidden")
	}
}

// A stalled event stream warns by the metric strip even when KAM answers status and rooms
// (MON-DS-13 per snapshot). The room block announces its age in the same situation.
func TestOverviewMarksOldEventFeed(t *testing.T) {
	h := newHarness(t)
	*h.now = t0.Add(90 * time.Second)
	h.st.SetStatus(kam.Status{Epoch: "e1", Now: t0, NodeName: "kam"}, *h.now)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/")
	if !strings.Contains(body, fmt.Sprintf("Packets from %s · KAM slow to answer.", t0.Local().Format("15:04:05"))) {
		t.Error("the event feed says how old its data is")
	}
	if !strings.Contains(body, fmt.Sprintf("Rooms from %s · KAM slow to answer.", t0.Local().Format("15:04:05"))) {
		t.Error("the room snapshot says how old its data is")
	}
	_, body, _ = get(t, c, h.ts.URL+"/")
	if strings.Contains(body, "has not answered") {
		t.Error("KAM answered, so the page-wide bar stays hidden")
	}
}

// MON-TR-1..4: the Traffic page builds from the ring alone, rows newest first, with the summary,
// history note and the footnote.
func TestTrafficPageBasics(t *testing.T) {
	h := newHarness(t)
	viewer := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, viewer, h.ts.URL+"/traffic")
	for _, want := range []string{
		`id="traffic-live"`, `hx-trigger="every 2s"`, `hx-get="/ui/traffic?"`,
		"OpenSender", "Sender hidden → #emmen", "KAM → Emmen local",
		"Export CSV", `href="/traffic.csv?"`,
		"History starts at " + t0.Add(-50*time.Second).Local().Format("15:04") + ".",
		"Observed packets only. Unattributed or undecryptable payloads keep their visible packet type.",
		"matching · 3 RX · 1 TX",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("traffic page is missing %q", want)
		}
	}
}

// MON-TR-5, 6, 8: filters combine with AND, live in the URL, and dashes stand for missing values.
func TestTrafficFilters(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/traffic?type=GRP_TXT&dir=rx")
	if !strings.Contains(body, "Sender hidden → #emmen") || !strings.Contains(body, "OpenSender → #public") {
		t.Error("both group-text rows match type and direction (MON-TR-5)")
	}
	if strings.Contains(body, "Canal relay") || strings.Contains(body, "KAM → Emmen local") {
		t.Error("the other packet types stayed out")
	}
	_, body, _ = get(t, c, h.ts.URL+"/traffic?text=public&dir=rx")
	if !strings.Contains(body, "OpenSender → #public") || strings.Contains(body, "Sender hidden") {
		t.Error("text and direction select together")
	}
	_, body, _ = get(t, c, h.ts.URL+"/traffic?text=public&dir=tx")
	if !strings.Contains(body, "No packets match this filter.") {
		t.Error("an unmatchable filter says so")
	}
}

// MON-TR-5, §6: a viewer cannot find a hidden sender by searching, in the page or in the CSV.
func TestTrafficTextFilterNeverMatchesHiddenFields(t *testing.T) {
	h := newHarness(t)
	viewer := h.login(t, "viewer", "viewer")
	admin := h.login(t, "admin", "admin")
	for _, target := range []string{"/traffic?text=secret", "/traffic.csv?text=secret"} {
		_, body, _ := get(t, viewer, h.ts.URL+target)
		if strings.Contains(body, "SecretSender") {
			t.Errorf("%s: the viewer found a hidden sender", target)
		}
		_, body, _ = get(t, admin, h.ts.URL+target)
		if !strings.Contains(body, "SecretSender") {
			t.Errorf("%s: the admin filtered on the visible sender", target)
		}
	}
}

// MON-TR-10, 11: CSV with the right columns, escaping and the formula guard.
func TestTrafficCSV(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	_, body, header := get(t, c, h.ts.URL+"/traffic.csv")
	if !strings.HasPrefix(header.Get("Content-Type"), "text/csv") {
		t.Errorf("content type %q", header.Get("Content-Type"))
	}
	if !strings.Contains(header.Get("Content-Disposition"), "attachment; filename=\"kammon-traffic-") {
		t.Errorf("file name %q", header.Get("Content-Disposition"))
	}
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if strings.Contains(lines[0], "Source / destination") == false {
		t.Errorf("csv header = %q", lines[0])
	}
	if len(lines) != 5 { // four packet events, no room event
		t.Fatalf("csv rows = %d, body = %q", len(lines), body)
	}
	for _, line := range lines {
		for _, cell := range strings.Split(line, ",") {
			if len(cell) > 0 && (cell[0] == '=' || cell[0] == '@') {
				t.Errorf("an unguarded formula slipped into %q", line)
			}
		}
	}
	// A plain label survives as it is; csv quotes only when a field needs it.
	if !strings.Contains(body, "Canal relay") {
		t.Errorf("escaping for a name with a space: %q", body)
	}
}

// MON-TR-2, 12: 100 rows first, "Show 100 more" up to the ring, then the history note.
func TestTrafficPaging(t *testing.T) {
	h := newHarness(t)
	base := t0.Add(-10 * time.Minute)
	var events []kam.Event
	for i := 0; i < 150; i++ {
		events = append(events, kam.Event{Seq: uint64(i + 1), At: base.Add(time.Duration(i) * time.Second), Kind: kam.KindRX, PayloadType: "TRACE"})
	}
	h.st.ApplyEvents(kam.EventsResponse{Epoch: "e1", FirstSeq: 1, LastSeq: 150, Events: events}, t0)

	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/traffic")
	firstCount := strings.Count(body, "mon\">15")
	if !strings.Contains(body, "Show 100 more") {
		t.Fatalf("the paging control is missing (rows=%d)", firstCount)
	}
	_, body, _ = get(t, c, h.ts.URL+"/ui/traffic?skip=100", "HX-Request", "true")
	if strings.Contains(body, "Show 100 more") {
		t.Error("no page after the last one")
	}
	if !strings.Contains(body, "History starts at "+base.Local().Format("15:04")) {
		t.Errorf("history note on the last page: %s", body)
	}
}

// MON-TR-11: a value that a spreadsheet would parse as a formula gets a leading apostrophe.
func TestTrafficCSVFormulaGuard(t *testing.T) {
	h := newHarness(t)
	events := []kam.Event{
		{Seq: 10, At: t0.Add(-time.Minute), Kind: kam.KindRX, PayloadType: "ADVERT", Name: "=EVIL"},
		{Seq: 11, At: t0.Add(-time.Minute + time.Second), Kind: kam.KindRX, PayloadType: "ADVERT", Name: "+RADIO"},
	}
	h.st.ApplyEvents(kam.EventsResponse{Epoch: "e1", FirstSeq: 1, LastSeq: 2, Events: events}, t0)
	c := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, c, h.ts.URL+"/traffic.csv?type=ADVERT")
	if !strings.Contains(body, "'=EVIL") || !strings.Contains(body, "'+RADIO") {
		t.Errorf("formula values must carry the apostrophe guard: %q", body)
	}
}

// MON-TR-14: every row carries a hidden detail line with the full record of the packet. The
// details never show SNR or RSSI (D19) and never show more of a sender's name than the row label
// does, so a viewer sees the same hidden senders here as everywhere else (MON-AX-14).
func TestTrafficRowDetails(t *testing.T) {
	h := newHarness(t)
	events := []kam.Event{
		{Seq: 6, At: t0.Add(-2 * time.Second), Kind: kam.KindRX, PayloadType: "GRP_TXT", Route: "flood", Hops: 2,
			Len: 87, AirtimeMS: ptr(312), Duplicate: true, Channel: "#emmen", Name: "SecretSender", SNRdB: ptr(8.5), RSSIdBm: ptr(-97)},
		{Seq: 7, At: t0.Add(-time.Second), Kind: kam.KindTX, PayloadType: "ACK", Route: "direct", Len: 8, AirtimeMS: ptr(74), OK: ptr(true), Origin: "automation"},
		{Seq: 8, At: t0, Kind: kam.KindTX, PayloadType: "GRP_TXT", Route: "flood", Len: 70, OK: ptr(false), Origin: "other"},
	}
	h.st.ApplyEvents(kam.EventsResponse{Epoch: "e1", FirstSeq: 6, LastSeq: 8, Events: events}, t0)

	viewer := h.login(t, "viewer", "viewer")
	_, body, _ := get(t, viewer, h.ts.URL+"/traffic")
	for _, want := range []string{
		`class="row-toggle" aria-expanded="false" aria-controls="detail-6"`,
		`<tr class="traffic-detail" id="detail-6" hidden>`,
		`<dt>Source / destination</dt><dd>Sender hidden → #emmen</dd>`,
		`<dt>Channel</dt><dd>#emmen</dd>`,
		`<dt>Packet length</dt><dd>87 bytes</dd>`,
		`<dt>Route</dt><dd>flood · 2 hops</dd>`,
		`<dt>Est. airtime</dt><dd>312 ms</dd>`,
		`<dt>Repeat</dt><dd>Yes</dd>`,
		`<dt>Send result</dt><dd>Abandoned</dd>`, // the TX with no result recorded
		`<dt>Send origin</dt><dd>Room</dd>`,
		`<dt>Room</dt><dd>Emmen local</dd>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the row details are missing %q", want)
		}
	}
	if strings.Contains(body, "SecretSender") {
		t.Error("the viewer reads a hidden sender from the row details")
	}
	if strings.Contains(body, "<dt>Sender</dt>") {
		t.Error("the row details add a sender line the row label does not show")
	}

	admin := h.login(t, "admin", "admin")
	_, body, _ = get(t, admin, h.ts.URL+"/traffic")
	for _, want := range []string{
		"SecretSender → #emmen", // admins see the sender in the label, as before
		`<dt>Send result</dt><dd>Sent</dd>`,
		`<dt>Send result</dt><dd>Failed</dd>`,
		`<dt>Send origin</dt><dd>Automation</dd>`,
		`<dt>Send origin</dt><dd>Other</dd>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the admin's row details are missing %q", want)
		}
	}
	for _, banned := range []string{"SNR", "RSSI", "dBm", "8.5", "-97"} {
		if strings.Contains(body, banned) {
			t.Errorf("%q shows in the row details, but SNR and RSSI are never displayed (D19)", banned)
		}
	}
}

// MON-AX-6: the fifth failed login starts delaying answers by two seconds; a same-host key
// survives changing ports.
func TestLoginSlowdownAfterFiveFailures(t *testing.T) {
	clock := t0
	var pauses []time.Duration
	srv, err := New(Deps{Config: config.Demo(), Store: store.New(), Now: func() time.Time { return clock }, Sleep: func(d time.Duration) { pauses = append(pauses, d) }})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	post := func(c *http.Client) {
		resp, err := c.PostForm(ts.URL+"/login", url.Values{"name": {"admin"}, "password": {"nope"}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	c := &http.Client{}
	for i := 0; i < 4; i++ {
		post(c) // four failures in the minute: none is slowed (MON-AX-6)
	}
	if len(pauses) != 0 {
		t.Fatalf("delays before the fifth failure: %v", pauses)
	}
	post(c)
	if len(pauses) != 0 {
		t.Fatalf("the fifth failure is not slowed: %v", pauses)
	}
	post(c)
	if len(pauses) != 1 || pauses[0] != 2*time.Second {
		t.Fatalf("delays = %v, want the sixth attempt to wait 2 s", pauses)
	}

	// The slowdown keys on the host part: a fresh port from the same machine is the same attacker.
	var pauses2 []time.Duration
	srv2, _ := New(Deps{Config: config.Demo(), Store: store.New(), Now: func() time.Time { return clock }, Sleep: func(d time.Duration) { pauses2 = append(pauses2, d) }})
	ts2 := httptest.NewServer(srv2.Handler())
	t.Cleanup(ts2.Close)
	c2 := &http.Client{}
	for i := 0; i < 5; i++ {
		resp, err := c2.PostForm(ts2.URL+"/login", url.Values{"name": {"admin"}, "password": {"nope"}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		c2.CloseIdleConnections() // force new connections with new source ports
	}
	resp, _ := c2.PostForm(ts2.URL+"/login", url.Values{"name": {"admin"}, "password": {"nope"}})
	resp.Body.Close()
	if len(pauses2) != 1 {
		t.Fatalf("a port-changing client still earned one delay: %v", pauses2)
	}
}

// The manuals left the interface (D23): nothing links to /manuals/ and no
// route serves them — including the login page, whose retired theme initializer
// is gone with them. The sidebar is one plain list of the navigation items (D26):
// no collapsible section, no Workspace or Help label.
func TestManualsAreGoneFromTheInterface(t *testing.T) {
	h := newHarness(t)
	_, body, _ := get(t, h.login(t, "viewer", "viewer"), h.ts.URL+"/")
	if n := strings.Count(body, `<nav class="nav">`); n != 1 {
		t.Errorf("the app sidebar has %d navigation lists, want one", n)
	}
	for _, gone := range []string{
		`<details class="nav-section"`,
		"nav-label",
		"nav-manuals",
		"/manuals/",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("the overview page still mentions %q", gone)
		}
	}
	_, body, _ = get(t, h.client(t), h.ts.URL+"/login")
	if strings.Contains(body, "/manuals/") {
		t.Error("the login page still links the manuals")
	}
	if status, _, _ := get(t, h.client(t), h.ts.URL+"/manuals/"); status != http.StatusNotFound {
		t.Errorf("/manuals/: %d, want 404", status)
	}
	if status, _, _ := get(t, h.client(t), h.ts.URL+"/static/theme-init.js"); status != http.StatusNotFound {
		t.Errorf("/static/theme-init.js: %d, want 404", status)
	}
}

// The server block sits under the node block in the sidebar and is for the administrator alone;
// a viewer is sent neither the block nor its numbers, on the page or in a live refresh (MON-SV-1).
// Its numbers are KAM's own counters from the status snapshot (MON-SV-2).
func TestServerStatsAreAdminOnly(t *testing.T) {
	h := newHarness(t)
	st, at := h.st.Status()
	st.TX.Submitted, st.TX.Sent, st.TX.Completed, st.TX.Queued = 2126, 2123, 2123, 2
	st.TX.Rejected, st.TX.Evicted, st.TX.Deferred, st.TX.GateTimeouts = 3, 1, 2, 5
	st.Persistence = map[string]struct {
		Depth   int    `json:"depth"`
		Dropped uint64 `json:"dropped"`
	}{"adverts": {Depth: 2, Dropped: 4}, "group_texts": {Depth: 312, Dropped: 6}}
	st.EventRing.Capacity, st.EventRing.FirstSeq, st.EventRing.LastSeq = 8192, 5931, 8123
	h.st.SetStatus(st, at)

	_, body, _ := get(t, h.login(t, "viewer", "viewer"), h.ts.URL+"/")
	if strings.Contains(body, "server-stats") || strings.Contains(body, "Event ring") {
		t.Error("a viewer must not see server statistics")
	}

	status, body, _ := get(t, h.login(t, "viewer", "viewer"), h.ts.URL+"/ui/traffic", "HX-Request", "true")
	if status != http.StatusOK || strings.Contains(body, "server-stats") {
		t.Errorf("a viewer's live refresh carries %q (status %d)", "server-stats", status)
	}

	_, body, _ = get(t, h.login(t, "admin", "admin"), h.ts.URL+"/")
	for _, want := range []string{
		"<strong>Server</strong>",
		"2,123 sent · 2 queued",
		"3 rejected · 1 evicted · 2 deferred",
		"5 gate timeouts · 10 ring drops",
		"Event ring 2,193 / 8,192",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the admin's sidebar lacks %q", want)
		}
	}

	status, body, _ = get(t, h.login(t, "admin", "admin"), h.ts.URL+"/ui/overview", "HX-Request", "true")
	if status != http.StatusOK || !strings.Contains(body, `id="server-stats"`) {
		t.Errorf("the admin's overview refresh lacks the server block (status %d)", status)
	}
}

// Before KAM has answered, the block shows nothing rather than zeroes (MON-SV-3).
func TestServerStatsWaitForKAM(t *testing.T) {
	h := newHarness(t)
	st, _ := h.st.Status()
	st.Epoch = ""
	h.st.SetStatus(st, t0)

	_, body, _ := get(t, h.login(t, "admin", "admin"), h.ts.URL+"/")
	if strings.Contains(body, "server-stats") {
		t.Error("waiting for KAM must hide the server block, not show zeroes")
	}
}
