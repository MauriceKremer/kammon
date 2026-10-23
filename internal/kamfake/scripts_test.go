package kamfake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/kam"
)

type scriptHarness struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	fake   *Fake
}

func newScriptHarness(t *testing.T) *scriptHarness {
	t.Helper()
	fake := New(func() time.Time { return t0s }, 1)
	server := httptest.NewServer(fake.Handler())
	t.Cleanup(server.Close)
	return &scriptHarness{t: t, server: server, client: server.Client(), fake: fake}
}

var t0s = time.Date(2026, 10, 23, 15, 0, 0, 0, time.UTC)

// callJSON sends one script request and decodes the answer: the body of an error, or the
// decoded output.
func (h *scriptHarness) call(method, path string, body any) (int, map[string]any) {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return resp.StatusCode, out
}

func (h *scriptHarness) scriptBody(id string) map[string]any {
	status, body := h.call(http.MethodGet, "/scripts/"+id, nil)
	if status != http.StatusOK {
		h.t.Fatalf("GET %s: %d %v", id, status, body)
	}
	return body
}

// The client of the tests is the kammon KAM client itself: the fake must speak the same wire
// the client speaks (docs/kam-api/FeatureKamApi.md §7.6).
func (h *scriptHarness) admin() *kam.HTTP {
	return kam.NewHTTP(h.server.URL, time.Second)
}

func TestFakeScriptListAndRunInfo(t *testing.T) {
	h := newScriptHarness(t)
	admin := h.admin()
	scripts, err := admin.Scripts(context.Background())
	if err != nil || len(scripts) != 5 {
		t.Fatalf("scripts = %+v, %v", scripts, err)
	}
	if scripts[0].Name != "echo.star" || !scripts[0].Enabled || scripts[0].Revision != 4 {
		t.Fatalf("echo = %+v", scripts[0])
	}
	// API-AU-2: the run object rides along (echo ran in the seed).
	if scripts[0].Run == nil || scripts[0].Run.Runs != 12 {
		t.Fatalf("echo run = %+v", scripts[0].Run)
	}
	// A broken and a frozen script carry their last error (MON-AU-2).
	states := map[string]string{}
	for _, script := range scripts {
		states[script.ID] = script.RuntimeStatus
	}
	if states["broken"] != kam.StatusBROKEN || states["frozen"] != kam.StatusFROZEN {
		t.Fatalf("states = %v", states)
	}

	// An older KAM: no run information and no dry run.
	h.fake.SetRunInfo(false)
	scripts, err = admin.Scripts(context.Background())
	if err != nil || len(scripts) == 0 || scripts[0].Run != nil {
		t.Fatalf("old list = %+v, %v", scripts, err)
	}
}

func TestFakeScriptValidate(t *testing.T) {
	h := newScriptHarness(t)
	admin := h.admin()
	ctx := context.Background()
	if err := admin.ValidateScript(ctx, "echo", Starter, 500); err != nil {
		t.Fatalf("validate ok: %v", err)
	}
	err := admin.ValidateScript(ctx, "echo", brokenSource, 500)
	var se *kam.StatusError
	if !errors.As(err, &se) || se.Status != 422 || !strings.Contains(se.Text, "line 1") {
		t.Fatalf("validate error = %v", err)
	}
}

func TestFakeScriptSaveAndRevisionRules(t *testing.T) {
	h := newScriptHarness(t)
	admin := h.admin()
	ctx := context.Background()

	created, err := admin.CreateScript(ctx, kam.Script{Name: "ping.star", Source: Starter, TimeoutMS: 500})
	if err != nil || created.Revision != 1 || created.ID != "ping.star" || created.Source != Starter {
		t.Fatalf("create = %+v, %v", created, err)
	}
	// A second script with the same id, and one with the same name but a fresh id, are both
	// conflicts, in KAM's own words: its answers say 409 when the id or name is already in use.
	if _, err := admin.CreateScript(ctx, kam.Script{ID: "ping.star", Name: "ping.star", Source: Starter, TimeoutMS: 500}); !conflictOf(err) || !strings.Contains(err.Error(), "revision conflict") {
		t.Fatalf("duplicate create = %v", err)
	}
	if _, err := admin.CreateScript(ctx, kam.Script{ID: "ping-2", Name: "ping.star", Source: Starter, TimeoutMS: 500}); !conflictOf(err) || !strings.Contains(err.Error(), "name already in use") {
		t.Fatalf("name-taken create = %v", err)
	}
	// A create takes its id from the body: KAM answers its own refusal without one (D32).
	status, body := h.call(http.MethodPost, "/scripts", map[string]any{
		"script":            map[string]any{"name": "no-id.star", "source": Starter, "timeout_ms": 500},
		"expected_revision": 0,
	})
	if status != http.StatusUnprocessableEntity || body["error"] != "script id and name are required" {
		t.Fatalf("create without an id: %d %v", status, body)
	}

	saved, err := admin.UpdateScript(ctx, kam.Script{ID: "ping.star", Name: "ping.star", Source: "def on_group_text(message):\n    pass\n", TimeoutMS: 400}, 1)
	if err != nil || saved.Revision != 2 || saved.TimeoutMS != 400 {
		t.Fatalf("update = %+v, %v", saved, err)
	}
	// A stale revision is a 409 (MON-AU-9).
	if _, err := admin.UpdateScript(ctx, kam.Script{ID: "ping.star", Name: "ping.star", Source: Starter}, 1); !conflictOf(err) {
		t.Fatalf("stale update = %v", err)
	}
	// An invalid source is a 422 and changes nothing.
	err = admin.ValidateScript(ctx, "ping.star", brokenSource, 500)
	if err == nil {
		t.Fatal("invalid source must not compile")
	}
	if list, _ := admin.Scripts(ctx); len(list[list0Index(t, list, "ping.star")].Source) == 0 {
		t.Fatalf("the list lost the source?")
	}
}

func list0Index(t *testing.T, list []kam.Script, id string) int {
	t.Helper()
	for i, script := range list {
		if script.ID == id {
			return i
		}
	}
	t.Fatalf("script %s not in the list", id)
	return 0
}

func conflictOf(err error) bool {
	var se *kam.StatusError
	return errors.As(err, &se) && se.Status == http.StatusConflict
}

func TestFakeScriptEnableDisableRecoverDelete(t *testing.T) {
	h := newScriptHarness(t)
	admin := h.admin()
	ctx := context.Background()

	// Enable and disable take the revision and bump it (MON-AU-10).
	enabled, err := admin.EnableScript(ctx, "status", 1)
	if err != nil || !enabled.Enabled || enabled.Revision != 2 {
		t.Fatalf("enable = %+v, %v", enabled, err)
	}
	if _, err := admin.DisableScript(ctx, "status", 2); err != nil {
		t.Fatalf("disable: %v", err)
	}
	// A stale revision is a 409.
	if _, err := admin.DisableScript(ctx, "status", 1); !conflictOf(err) {
		t.Fatalf("stale disable = %v", err)
	}

	// Recover clears FROZEN (MON-AU-11); a READY script does not recover.
	recovered, err := admin.RecoverScript(ctx, "frozen", 600, 2)
	if err != nil || recovered.RuntimeStatus != kam.StatusREADY || recovered.LastError != "" || recovered.TimeoutMS != 600 {
		t.Fatalf("recover = %+v, %v", recovered, err)
	}
	if _, err := admin.RecoverScript(ctx, "echo", 0, 4); err == nil {
		t.Fatal("recovering a READY script must fail")
	}

	// Delete takes the revision and is gone after (MON-AU-12).
	if err := admin.DeleteScript(ctx, "welcome", 2); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := admin.Script(ctx, "welcome"); !errors.Is(err, kam.ErrNotFound) {
		t.Fatalf("get after delete = %v", err)
	}
	if err := admin.DeleteScript(ctx, "echo", 1); !conflictOf(err) {
		t.Fatalf("stale delete = %v", err)
	}
}

func TestFakeScriptRestartKeepsScriptsAndResetsRuns(t *testing.T) {
	h := newScriptHarness(t)
	admin := h.admin()
	ctx := context.Background()
	before, _ := admin.Scripts(ctx)
	h.fake.Restart()
	after, _ := admin.Scripts(ctx)
	if len(after) != len(before) || after[0].Revision != before[0].Revision || after[0].Name != before[0].Name {
		t.Fatalf("restart changed the scripts: %d → %d", len(before), len(after))
	}
	if after[0].Run != nil && after[0].Run.Runs != 0 {
		t.Fatalf("restart must reset the run counters: %+v", after[0].Run)
	}
}

func TestFakeScriptDryRun(t *testing.T) {
	h := newScriptHarness(t)
	admin := h.admin()
	ctx := context.Background()
	fixture := kam.DryRunMessage{Channel: "#emmen", Sender: "test", Body: "!ping"}
	test := kam.DryRunRequest{Source: echoSource, TimeoutMS: 500, Message: fixture}
	test2 := kam.DryRunRequest{Source: "def on_group_text(message):\n    if message[\"body\"] == \"!ping\":\n        reply(\"#field\", \"secret\")\n", Message: kam.DryRunMessage{Channel: "#emmen", Body: "!ping"}}

	// ok: replies are captured, never sent (API-AU-3).
	out, err := admin.DryRun(ctx, test)
	if err != nil || !out.OK || out.DurationMS != 2 || len(out.Replies) != 1 || out.Replies[0].Channel != "#emmen" || out.Replies[0].Text != "pong" {
		t.Fatalf("dry run = %+v, %v", out, err)
	}
	// A literal channel in the reply stays the reply's channel.
	out, err = admin.DryRun(ctx, test2)
	if err != nil || !out.OK || len(out.Replies) != 1 || out.Replies[0].Channel != "#field" {
		t.Fatalf("literal channel dry run = %+v, %v", out, err)
	}
	// A compile error reads ok:false with the compiler's words (§7.6.1).
	out, err = admin.DryRun(ctx, kam.DryRunRequest{Source: brokenSource, Message: fixture})
	if err != nil || out.OK || out.Error == "" {
		t.Fatalf("compile dry run = %+v, %v", out, err)
	}
	// The frozen sample times out (the fake marks it with a busy loop).
	out, err = admin.DryRun(ctx, kam.DryRunRequest{Source: frozenSource, TimeoutMS: 300, Message: fixture})
	if err != nil || out.OK || !strings.Contains(out.Error, "Timeout after 300 ms") {
		t.Fatalf("timeout dry run = %+v, %v", out, err)
	}
	// A guard that does not match stays silent.
	quiet, err := admin.DryRun(ctx, kam.DryRunRequest{Source: echoSource, Message: kam.DryRunMessage{Channel: "#emmen", Body: "!hello"}})
	if err != nil || !quiet.OK || len(quiet.Replies) != 0 {
		t.Fatalf("quiet run = %+v, %v", quiet, err)
	}
}

func TestFakeScriptDryRunLimitsAndOlderKAM(t *testing.T) {
	h := newScriptHarness(t)
	fixture := kam.DryRunMessage{Channel: "#emmen", Body: "!ping"}
	raw := func(body any) (int, map[string]any) { return h.call(http.MethodPost, "/scripts/dry-run", body) }

	// Validation before anything runs (§7.6.1).
	if status, _ := raw(kam.DryRunRequest{Source: Starter, TimeoutMS: 4000, Message: fixture}); status != http.StatusUnprocessableEntity {
		t.Fatalf("timeout 4000: %d", status)
	}
	if status, _ := raw(kam.DryRunRequest{Source: Starter, Message: kam.DryRunMessage{Channel: "#closed"}}); status != http.StatusUnprocessableEntity {
		t.Fatalf("unknown channel: %d", status)
	}
	if status, _ := raw(map[string]any{"source": Starter, "message": map[string]any{"channel": "#emmen", "wat": 1}}); status != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", status)
	}

	// An older KAM has no dry-run route (MON-AU-20).
	h.fake.SetDryRun(false)
	if status, _ := raw(kam.DryRunRequest{Source: Starter, Message: fixture}); status != http.StatusNotFound {
		t.Fatalf("older KAM: %d", status)
	}
	if err := h.admin().ProbeDryRun(context.Background()); !errors.Is(err, kam.ErrNotFound) {
		t.Fatalf("probe on the old fake = %v", err)
	}
}

func TestFakeScriptBusyAndDisabled(t *testing.T) {
	h := newScriptHarness(t)
	fixture := kam.DryRunMessage{Channel: "#emmen", Body: "!ping"}

	// One test at a time: the next gets 409 (§7.6.1).
	h.fake.SetDryRunBusy(true)
	if status, body := h.call(http.MethodPost, "/scripts/dry-run", kam.DryRunRequest{Source: Starter, Message: fixture}); status != http.StatusConflict || !strings.Contains(body["error"].(string), "dry-run") {
		t.Fatalf("busy = %d %v", status, body)
	}
	h.fake.SetDryRunBusy(false)

	// Automation off: every script route answers 503 (KAM's D9).
	h.fake.SetScriptsAPI(false)
	admin := h.admin()
	ctx := context.Background()
	if _, err := admin.Scripts(ctx); err == nil || !strings.Contains(err.Error(), "automation is disabled") {
		t.Fatalf("list while disabled = %v", err)
	}
	if err := admin.ValidateScript(ctx, "echo", Starter, 500); err == nil {
		t.Fatal("validate while disabled must fail")
	}
	if _, err := admin.Script(ctx, "echo"); err == nil {
		t.Fatal("get while disabled must fail")
	}
}
