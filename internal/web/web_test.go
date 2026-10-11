package web

import (
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
)

var t0 = time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

type harness struct {
	ts  *httptest.Server
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
	st.SetRooms([]kam.Room{{ID: "lobby", Name: "Emmen local", Enabled: true, ClientsRemembered: 9, LastActivityAt: ptr(t0.Add(-12 * time.Second))}}, nil, t0)

	srv, err := New(Deps{Config: config.Demo(), Store: st, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &harness{ts: ts, st: st, now: &now}
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

func TestAdminPowerNeedsALocalRequest(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "admin", "admin")
	status, body, _ := get(t, c, h.ts.URL+"/")
	if status != http.StatusOK || !strings.Contains(body, `href="/accounts"`) || !strings.Contains(body, "Admin") {
		t.Fatalf("local admin: %d", status)
	}
	if status, _, _ = get(t, c, h.ts.URL+"/accounts"); status != http.StatusOK {
		t.Fatalf("local admin on /accounts: %d", status)
	}
	// The same session arriving through a proxy is a viewer (MON-AX-9, MON-AX-10).
	status, _, _ = get(t, c, h.ts.URL+"/accounts", "X-Forwarded-For", "203.0.113.9")
	if status != http.StatusForbidden {
		t.Fatalf("admin behind a proxy on /accounts: %d", status)
	}
	_, body, _ = get(t, c, h.ts.URL+"/", "X-Forwarded-For", "203.0.113.9")
	if !strings.Contains(body, "not a local request") || strings.Contains(body, `href="/accounts"`) {
		t.Fatal("a proxied admin gets the view-only notice and no admin navigation")
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
	if resp, err = c.Do(req); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if status, _, _ := get(t, c, h.ts.URL+"/"); status != http.StatusSeeOther {
		t.Fatalf("after logout the overview must redirect, got %d", status)
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
