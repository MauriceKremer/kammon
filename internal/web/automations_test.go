// Tests of the Automations page and its admin actions against a fake KAM (Automations.md §7).
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
	"kammon.bytecoder.nl/internal/kamfake"
	"kammon.bytecoder.nl/internal/store"
)

// scriptHarness is the Automations page backed by a fake KAM, exactly as demo mode wires them:
// one client for the poller's reads (simulated here by Store.SetScripts) and the pages' admin
// actions (MON-AU-1, AGENTS rule 3).
type scriptHarness struct {
	ts     *httptest.Server
	fake   *kamfake.Fake
	kam    *kam.HTTP
	st     *store.Store
	now    *time.Time
	client *http.Client
}

func newScriptHarness(t *testing.T) *scriptHarness {
	t.Helper()
	now := t0
	st := store.New()
	status := kam.Status{Epoch: "e1", Now: t0, StartedAt: t0.Add(-24 * time.Hour), NodeName: "kam"}
	status.Automation.Enabled = true
	st.SetStatus(status, t0)

	fake := kamfake.New(func() time.Time { return now }, 9)
	fakeTS := httptest.NewServer(fake.Handler())
	t.Cleanup(fakeTS.Close)

	// The poller's snapshot, as the running poller would have written it (the store strips the
	// source, MON-AU-4).
	client := kam.NewHTTP(fakeTS.URL, 3*time.Second)
	list, err := client.Scripts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	st.SetScripts(list, t0)

	srv, err := New(Deps{Config: config.Demo(), Store: st, Now: func() time.Time { return now }, KAM: client})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	return &scriptHarness{
		ts: ts, fake: fake, kam: client, st: st, now: &now,
		client: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (h *scriptHarness) login(t *testing.T, name, password string) {
	t.Helper()
	resp, err := h.client.PostForm(h.ts.URL+"/login", url.Values{"name": {name}, "password": {password}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login %s: status %d", name, resp.StatusCode)
	}
}

func (h *scriptHarness) get(t *testing.T, target string) (int, string) {
	t.Helper()
	status, body, _ := get(t, h.client, h.ts.URL+target)
	return status, body
}

// formPost calls an editor action the way the browser's htmx does: the Origin of this host and
// the HX-Request header the guard requires (MON-AX-18).
func (h *scriptHarness) formPost(t *testing.T, method, target string, form url.Values) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(method, h.ts.URL+target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", h.ts.URL)
	req.Header.Set("HX-Request", "true")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), resp.Header
}

func TestAutomationsViewerSeesListButNoEditor(t *testing.T) {
	h := newScriptHarness(t)
	h.login(t, "viewer", "viewer")
	status, body := h.get(t, "/automations")
	if status != http.StatusOK {
		t.Fatalf("automations page: %d", status)
	}
	for _, want := range []string{"echo.star", "welcome.star", "Frozen", "rev 4", "12 runs"} {
		if !strings.Contains(body, want) {
			t.Errorf("the list misses %q: %s", want, body)
		}
	}
	if strings.Contains(body, "def on_group_text") || strings.Contains(body, "textarea") {
		t.Error("a viewer never sees script source (MON-AU-4)")
	}
	// MON-AU-3: the run line names time, duration and replies (the hour is KAM's clock, in words).
	if !strings.Contains(body, "last run ") || !strings.Contains(body, "· 2 ms · 1 reply") {
		t.Errorf("the run line is missing: %s", body)
	}
}

func TestAutomationsAdminOpensEditorWithSource(t *testing.T) {
	h := newScriptHarness(t)
	h.login(t, "admin", "admin")
	status, body := h.get(t, "/automations?script=echo")
	if status != http.StatusOK {
		t.Fatalf("editor page: %d", status)
	}
	for _, want := range []string{
		"echo.star", `id="script-source"`, "on_group_text", "Revision 4",
		`name="name" value="echo.star"`,
		`hx-post="/ui/script-validate"`, ">Save</button>", ">Dry run</button>",
		`name="fx_channel" value="#emmen"`, `name="fx_body" value="!ping"`,
		"Delete", "Reset",
		"Replies are real radio transmissions and count against the duty cycle.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the editor misses %q: %s", want, body)
		}
	}
	// The frozen sample names its last error and carries a recover row (MON-AU-2, 11).
	_, frozen := h.get(t, "/automations?script=frozen")
	if !strings.Contains(frozen, "KAM paused this script") || !strings.Contains(frozen, `name="recover_timeout"`) {
		t.Errorf("the frozen script misses the recover row: %s", frozen)
	}
	// A BROKEN script shows KAM's last error in the console without a recover row (MON-AU-2).
	_, broken := h.get(t, "/automations?script=broken")
	if !strings.Contains(broken, "KAM could not run this script") || strings.Contains(broken, `name="recover_timeout"`) {
		t.Errorf("the broken script shows the wrong console: %s", broken)
	}
	// ?new=1 is the starter editor with nothing of the delete row (Automations.md §3.1, MON-AU-8).
	// The name field is here, empty: the save names the script (MON-AU-8).
	_, starter := h.get(t, "/automations?new=1")
	if !strings.Contains(starter, `id="script-source"`) || !strings.Contains(starter, "!ping") || strings.Contains(starter, "hx-delete") {
		t.Errorf("the starter editor is not a new script: %s", starter)
	}
	if !strings.Contains(starter, `id="script-name"`) {
		t.Errorf("the starter editor misses the name field (MON-AU-8): %s", starter)
	}
}

// The editor's button order (Automations.md §3): Validate sits under the script source, and
// Save and Delete sit at the bottom of the page, after the test-message form and the reference.
func TestAutomationsEditorButtonPlacement(t *testing.T) {
	h := newScriptHarness(t)
	h.login(t, "admin", "admin")
	_, body := h.get(t, "/automations?script=echo")
	last, lastName := -1, ""
	for _, want := range []struct{ name, mark string }{
		{"the script source", `id="script-source"`},
		{"validate", `>Validate</button>`},
		{"the settings row", `id="script-name"`},
		{"the console", `id="console"`},
		{"the test-message form", `id="fixture-form"`},
		{"the fixture's text field", `name="fx_body"`},
		{"the dry run", `>Dry run</button>`},
		{"the dry run's result", `id="dry-run-result"`},
		{"save", `>Save</button>`},
		{"delete", `>Delete</button>`},
	} {
		at := strings.Index(body, want.mark)
		if at < 0 {
			t.Fatalf("the editor misses %s", want.name)
		}
		if at < last {
			t.Errorf("%s comes before %s; the order is source, Validate, settings, console, test message, Dry run, Save, Delete (Automations.md §3)", want.name, lastName)
		}
		last, lastName = at, want.name
	}
	// Save and Delete live outside the editor form: each carries the form's values explicitly,
	// so a save and a delete carry the text, name, revision and timeout (MON-AU-8, MON-AU-12).
	// The dry run sends its own form and the editor's unsaved source (MON-AU-15).
	for _, want := range []string{
		`hx-post="/ui/script-save" hx-include="#script-editor"`,
		`hx-delete="/ui/script" hx-include="#script-editor"`,
		`hx-include="#script-source"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the editor misses %s: %s", want, body)
		}
	}
}

func TestAutomationsValidateShowsKamVerbatim(t *testing.T) {
	h := newScriptHarness(t)
	h.login(t, "admin", "admin")
	form := url.Values{
		"id": {"echo"}, "name": {"echo.star"}, "revision": {"4"}, "enabled": {"true"},
		"source": {"def on_group_text(message)\n    reply(message[\"channel\"], \"x\")\n"}, "timeout": {"500"},
	}
	status, body, _ := h.formPost(t, http.MethodPost, "/ui/script-validate", form)
	if status != http.StatusOK || !strings.Contains(body, `class="fail"`) || !strings.Contains(body, "column 27") {
		t.Fatalf("broken validate: %d %s", status, body)
	}
	form.Set("source", kamfake.Starter)
	_, body, _ = h.formPost(t, http.MethodPost, "/ui/script-validate", form)
	if !strings.Contains(body, "KAM accepts this script.") {
		t.Errorf("ok validate: %s", body)
	}
	// The timeout field takes whole milliseconds, one or more (MON-AU-13).
	form.Set("timeout", "0")
	_, body, _ = h.formPost(t, http.MethodPost, "/ui/script-validate", form)
	if !strings.Contains(body, "The timeout must be a whole number of milliseconds, one or more.") {
		t.Errorf("zero timeout accepted: %s", body)
	}
}

func TestAutomationsSaveCreatesUpdatesAndKeepsTextOnConflict(t *testing.T) {
	h := newScriptHarness(t)
	h.login(t, "admin", "admin")
	// A create carries revision 0 and no id (MON-AU-8), and never enables by itself (MON-AU-10).
	create := url.Values{
		"id": {""}, "revision": {"0"}, "enabled": {"false"}, "name": {"pinger.star"},
		"source": {kamfake.Starter}, "timeout": {"250"},
	}
	status, body, header := h.formPost(t, http.MethodPost, "/ui/script-save", create)
	if status != http.StatusOK || !strings.Contains(body, "Saved revision 1.") || !strings.Contains(body, `data-saved="true"`) {
		t.Fatalf("create: %d %s", status, body)
	}
	if header.Get("HX-Replace-Url") != "/automations?script=pinger.star" {
		t.Errorf("create replace url = %q", header.Get("HX-Replace-Url"))
	}
	if !strings.Contains(body, `name="id" value="pinger.star"`) {
		t.Error("the editor must become the created script's editor, so no second save creates a duplicate")
	}
	// A create is not changed elsewhere: no revision can be stale, so its 409 is KAM's own words
	// about a taken id (MON-AU-9, D32).
	_, body, _ = h.formPost(t, http.MethodPost, "/ui/script-save", create)
	if !strings.Contains(body, "revision conflict") || strings.Contains(body, "Changed elsewhere") {
		t.Errorf("duplicate create: %s", body)
	}
	saved, err := h.kam.Scripts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range saved {
		if script.ID == "pinger.star" && script.Enabled {
			t.Fatal("a save must not enable the script (MON-AU-10)")
		}
	}
	// An update carries the remembered revision and KAM bumps it (MON-AU-6, 8).
	update := url.Values{
		"id": {"echo"}, "revision": {"4"}, "enabled": {"true"}, "name": {"echo.star"},
		"source": {kamfake.Starter}, "timeout": {"500"},
	}
	_, body, _ = h.formPost(t, http.MethodPost, "/ui/script-save", update)
	if !strings.Contains(body, "Saved revision 5.") {
		t.Errorf("update: %s", body)
	}
	// The same revision again is a 409: KAMmon's own words, and the answer carries no source, so
	// the user's text is never lost (MON-AU-9).
	_, body, _ = h.formPost(t, http.MethodPost, "/ui/script-save", update)
	if !strings.Contains(body, "Changed elsewhere. Reload to see the latest version.") || strings.Contains(body, `id="script-source"`) {
		t.Errorf("conflict: %s", body)
	}
}

func TestAutomationsDryRunConsole(t *testing.T) {
	h := newScriptHarness(t)
	h.login(t, "admin", "admin")
	form := url.Values{
		"source":     {kamfake.Starter},
		"fx_channel": {"#emmen"}, "fx_sender": {"PA3M"}, "fx_body": {"!ping"},
		"fx_hops": {"2"}, "fx_snr": {"8.5"}, "fx_rssi": {"-97"}, "fx_drift": {"0"}, "fx_timeout": {"500"},
	}
	status, body, _ := h.formPost(t, http.MethodPost, "/ui/script-dry-run", form)
	if status != http.StatusOK {
		t.Fatalf("dry run: %d", status)
	}
	// The dry run answers into its own result below the button (MON-AU-15), not the editor's
	// console, and the box keeps the live region.
	for _, want := range []string{"id=\"dry-run-result\"", `role="status" aria-live="polite"`, "Would send to #emmen: pong", "Nothing was sent.", "The test ran in 2 ms."} {
		if !strings.Contains(body, want) {
			t.Errorf("dry run console misses %q: %s", want, body)
		}
	}
	// A guard that does not match stays silent: still a successful test (MON-AU-16).
	form.Set("fx_body", "!hello")
	_, body, _ = h.formPost(t, http.MethodPost, "/ui/script-dry-run", form)
	if !strings.Contains(body, "The script stayed silent.") || strings.Contains(body, `class="fail"`) || !strings.Contains(body, "Nothing was sent.") {
		t.Errorf("quiet dry run: %s", body)
	}
	// A timeout reads the test's own error (MON-AU-16).
	form.Set("fx_body", "")
	form.Set("source", "def on_group_text(message):\n    while True:\n        pass\n")
	form.Set("fx_timeout", "300")
	_, body, _ = h.formPost(t, http.MethodPost, "/ui/script-dry-run", form)
	if !strings.Contains(body, "The test failed: Timeout after 300 ms") || !strings.Contains(body, "Nothing was sent.") {
		t.Errorf("timeout dry run: %s", body)
	}
	// The test timeout has its own 1-3000 ms range (MON-AU-15).
	form.Set("source", kamfake.Starter)
	form.Set("fx_body", "!ping")
	form.Set("fx_timeout", "4000")
	_, body, _ = h.formPost(t, http.MethodPost, "/ui/script-dry-run", form)
	if !strings.Contains(body, "The test timeout must be between 1 and 3000 ms.") {
		t.Errorf("out-of-range timeout: %s", body)
	}
}

func TestAutomationsDryRunNeedsNewerKam(t *testing.T) {
	h := newScriptHarness(t)
	h.fake.SetDryRun(false)
	h.login(t, "admin", "admin")
	// Opening the editor probes the route: the button is absent and a line says why (MON-AU-20).
	_, body := h.get(t, "/automations?script=echo")
	if strings.Contains(body, ">Dry run</button>") {
		t.Error("an older KAM must not be offered the dry run button (MON-AU-20)")
	}
	if !strings.Contains(body, "Needs a newer KAM.") {
		t.Errorf("an older KAM editor misses the note: %s", body)
	}
	// Belt and braces: a dry run that still reaches an older KAM says the same (D30).
	form := url.Values{"source": {kamfake.Starter}, "fx_channel": {"#emmen"}, "fx_body": {"!ping"}}
	_, body, _ = h.formPost(t, http.MethodPost, "/ui/script-dry-run", form)
	if !strings.Contains(body, "Needs a newer KAM.") || !strings.Contains(body, "Nothing was sent.") {
		t.Errorf("dry run on an older KAM: %s", body)
	}
}

func TestAutomationsToggleEnableDisable(t *testing.T) {
	h := newScriptHarness(t)
	h.login(t, "admin", "admin")
	// A toggle goes through KAM's own endpoints and bumps the revision (MON-AU-10).
	form := url.Values{"id": {"echo"}, "revision": {"4"}, "enabled": {"true"}, "timeout": {"500"}}
	status, body, _ := h.formPost(t, http.MethodPost, "/ui/script-toggle", form)
	if status != http.StatusOK || !strings.Contains(body, "Disabled. Revision 5 is current.") {
		t.Fatalf("disable: %d %s", status, body)
	}
	if !strings.Contains(body, `name="enabled" value="false"`) || !strings.Contains(body, `name="revision" value="5"`) {
		t.Errorf("the oob state misses the new truth: %s", body)
	}
	// The out-of-band list shows the acted script's true state, not the store's one-poll-old copy.
	oobList := body[strings.Index(body, `id="script-list"`):]
	if !strings.Contains(oobList, "Disabled") {
		t.Errorf("the oob list must show the fresh state for echo: %s", oobList)
	}
	// The checkbox comes back as the button: checking it enables, unchecking disables.
	check := url.Values{"id": {"echo"}, "revision": {"5"}, "enabled": {"false"}, "timeout": {"500"}, "enabled-box": {"on"}, "name": {"echo.star"}, "source": {kamfake.Starter}}
	_, body, _ = h.formPost(t, http.MethodPost, "/ui/script-toggle", check)
	if !strings.Contains(body, "Enabled. Revision 6 is current.") {
		t.Errorf("enable: %s", body)
	}
	// A stale revision is refused with KAM's words, and the control restores to KAMmon's best
	// knowledge until the next poll (MON-AU-9's words belong to saves).
	stale := url.Values{"id": {"echo"}, "revision": {"4"}, "enabled": {"true"}, "timeout": {"500"}, "enabled-box": {"on"}}
	_, body, _ = h.formPost(t, http.MethodPost, "/ui/script-toggle", stale)
	if !strings.Contains(body, "stale") || !strings.Contains(body, "Revision 4") {
		t.Errorf("stale toggle: %s", body)
	}
}

func TestAutomationsRecoverAndDelete(t *testing.T) {
	h := newScriptHarness(t)
	h.login(t, "admin", "admin")
	// Recover (MON-AU-11): the frozen sample comes back, with an optional new timeout, revision 3.
	form := url.Values{"id": {"frozen"}, "revision": {"2"}, "recover_timeout": {"600"}}
	status, body, _ := h.formPost(t, http.MethodPost, "/ui/script-recover", form)
	if status != http.StatusOK || !strings.Contains(body, "Recovered.") || !strings.Contains(body, `name="revision" value="3"`) {
		t.Fatalf("recover: %d %s", status, body)
	}
	if !strings.Contains(body, `name="timeout" value="600"`) {
		t.Errorf("the new timeout must reach the editor: %s", body)
	}
	// A recover of a script that runs no more is refused, in KAM's verbatim words.
	form.Set("revision", "3")
	_, body, _ = h.formPost(t, http.MethodPost, "/ui/script-recover", form)
	if !strings.Contains(body, "recover wants a FROZEN or BROKEN script") {
		t.Errorf("second recover: %s", body)
	}
	// Delete (MON-AU-12): the browser asked first; KAMmon deletes and the page follows.
	del := url.Values{"id": {"welcome"}, "revision": {"2"}}
	status, _, header := h.formPost(t, http.MethodDelete, "/ui/script", del)
	if status != http.StatusOK || header.Get("HX-Redirect") != "/automations" {
		t.Fatalf("delete: %d %q", status, header.Get("HX-Redirect"))
	}
	if _, err := h.kam.Script(t.Context(), "welcome"); err == nil {
		t.Fatal("the script is still on KAM")
	}
}

func TestAutomationsListPollAndBars(t *testing.T) {
	h := newScriptHarness(t)
	h.login(t, "viewer", "viewer")
	status, body := h.get(t, "/ui/script-list")
	if status != http.StatusOK {
		t.Fatalf("list partial: %d", status)
	}
	if !strings.Contains(body, `id="script-list" hx-get="/ui/script-list" hx-trigger="every 10s"`) {
		t.Errorf("the list does not poll: %s", body)
	}
	if strings.Contains(body, "def on_group_text") {
		t.Error("the list partial never carries source (MON-AU-4)")
	}
	// MON-AU-5: the page's bar speaks about the script list alone. A slow script read is stale.
	h.st.SetScripts([]kam.Script{{ID: "echo", Name: "echo.star", Enabled: true, TimeoutMS: 500, RuntimeStatus: kam.StatusREADY, Revision: 9, CreatedAt: t0.Unix(), UpdatedAt: t0.Unix()}}, t0.Add(-time.Minute))
	*h.now = t0.Add(30 * time.Second)
	_, body = h.get(t, "/automations")
	if !strings.Contains(body, "The script list is out of date.") {
		t.Errorf("a stale script list misses its bar: %s", body)
	}
	// A KAM without the script interface says so on the page (MON-AU-20 wording).
	*h.now = t0
	h.st.MarkNotServed("scripts", t0.Add(time.Hour))
	_, body = h.get(t, "/automations")
	if !strings.Contains(body, "Needs a newer KAM.") {
		t.Errorf("a not-served scripts read misses the newer-KAM line: %s", body)
	}
	// A KAM with automation off says so, and that cause wins over the bar's other words.
	h2 := newScriptHarness(t)
	h2.login(t, "viewer", "viewer")
	st := kam.Status{Epoch: "e1", Now: t0, StartedAt: t0.Add(-time.Hour), NodeName: "kam"}
	st.Automation.Enabled = false
	h2.st.SetStatus(st, t0)
	_, body = h2.get(t, "/automations")
	if !strings.Contains(body, "Automation is disabled on KAM.") {
		t.Errorf("automation off misses its line: %s", body)
	}
}

func TestAutomationsViewerCannotEdit(t *testing.T) {
	h := newScriptHarness(t)
	h.login(t, "viewer", "viewer")
	form := url.Values{"id": {"echo"}, "revision": {"4"}, "source": {kamfake.Starter}, "timeout": {"500"}}
	if status, _, _ := h.formPost(t, http.MethodPost, "/ui/script-validate", form); status != http.StatusForbidden {
		t.Fatalf("viewer validate: %d", status)
	}
	// A plain post without the htmx header does not reach a change route (MON-AX-18).
	req, _ := http.NewRequest(http.MethodPost, h.ts.URL+"/ui/script-validate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("post without the htmx header: %d", resp.StatusCode)
	}
}

// A KAMmon without a client for KAM keeps the list and the editor says so; the actions answer
// with a console and never panic (nil tolerance, AGENTS rule 1).
func TestAutomationsWithoutKamClient(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "admin", "admin")
	status, body, _ := get(t, c, h.ts.URL+"/automations?script=echo")
	if status != http.StatusOK || !strings.Contains(body, "KAMmon is not connected to KAM.") || strings.Contains(body, "textarea") {
		t.Fatalf("no-client editor: %d %s", status, body)
	}
	form := url.Values{"id": {"echo"}, "source": {kamfake.Starter}, "timeout": {"500"}}
	req, _ := http.NewRequest(http.MethodPost, h.ts.URL+"/ui/script-validate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", h.ts.URL)
	req.Header.Set("HX-Request", "true")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), "KAMmon is not connected to KAM.") {
		t.Fatalf("no-client validate: %d %s", resp.StatusCode, string(raw))
	}
}

// fixtureMessage reads the form's fixture into the wire message: SNR is given in dB and travels
// as quarter dB (Automations.md §3.1), and a filled signal field means the message carries
// receiving data.
func TestFixtureMessageConvertsSNRToQuarterDB(t *testing.T) {
	form := url.Values{
		"fx_channel": {"#emmen"}, "fx_sender": {"PA3M"}, "fx_body": {"!ping"},
		"fx_hops": {"2"}, "fx_snr": {"8.5"}, "fx_rssi": {"-97"}, "fx_drift": {"3"},
	}
	message, problem := fixtureMessage(form)
	if problem != "" {
		t.Fatalf("fixture: %s", problem)
	}
	if message.SNRQuarterDB == nil || *message.SNRQuarterDB != 34 || !message.HasRxMeta || message.HopCount != 2 ||
		message.RSSIdBm == nil || *message.RSSIdBm != -97 || message.TimeDriftSeconds == nil || *message.TimeDriftSeconds != 3 {
		t.Fatalf("fixture message = %+v", message)
	}
	// An empty field means not set: KAM applies its own defaults.
	empty, problem := fixtureMessage(url.Values{"fx_channel": {"#public"}})
	if problem != "" || empty.SNRQuarterDB != nil || empty.HasRxMeta || empty.HopCount != 0 {
		t.Fatalf("empty fixture = %+v (%s)", empty, problem)
	}
	// A channel is required.
	if _, problem := fixtureMessage(url.Values{}); problem == "" {
		t.Fatal("the test needs a channel")
	}
}
