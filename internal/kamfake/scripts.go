// The script surface of the fake KAM (docs/kam-api/FeatureKamApi.md §7.6, its
// FeatureUnifiedApi.md): list, get, create, update, delete, validate, enable,
// disable, recover with revision checks, plus the two phase-5 additions: the
// run object and a bounded dry run. The fake "compiles" with one simple check
// and "runs" a script by matching reply literals against the fixture, which is
// enough for demo mode and the tests (MON-AU-1..20).
package kamfake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kammon.bytecoder.nl/internal/kam"
)

// Starter is the template a new script begins with (Automations.md §3.1).
const Starter = "def on_group_text(message):\n    if message[\"body\"] == \"!ping\":\n        reply(message[\"channel\"], \"pong\")\n"

const echoSource = "def on_group_text(message):\n    if message[\"body\"] == \"!ping\":\n        reply(message[\"channel\"], \"pong\")\n"

const welcomeSource = "def on_group_text(message):\n    if message[\"body\"] == \"!hello\":\n        reply(message[\"channel\"], \"Hello from Emmen!\")\n"

const brokenSource = "def on_group_text(message)\n    reply(message[\"channel\"], \"missing colon\")\n"

const frozenSource = "def on_group_text(message):\n    while True:\n        pass\n"

type fakeScript struct {
	id, name, source string
	enabled          bool
	timeoutMS        int64
	status           string
	lastError        string
	revision         int64
	createdAt        int64
	updatedAt        int64
	// Run information (API-AU-2): in memory, reset by a KAM restart.
	runs, errors   uint64
	lastRunAt      time.Time
	lastDurationMS int64
	lastReplies    int
}

func seedScripts(now time.Time) []fakeScript {
	return []fakeScript{
		{id: "echo", name: "echo.star", source: echoSource, enabled: true, timeoutMS: 500, status: kam.StatusREADY, revision: 4,
			createdAt: now.Unix(), updatedAt: now.Unix(), runs: 12, lastRunAt: now.Add(-2 * time.Second), lastDurationMS: 2, lastReplies: 1},
		{id: "welcome", name: "welcome.star", source: welcomeSource, enabled: true, timeoutMS: 500, status: kam.StatusREADY, revision: 2,
			createdAt: now.Unix(), updatedAt: now.Unix(), runs: 5, lastRunAt: now.Add(-9 * time.Minute), lastDurationMS: 1, lastReplies: 1},
		{id: "status", name: "status.star", source: welcomeSource, enabled: false, timeoutMS: 500, status: kam.StatusREADY, revision: 1,
			createdAt: now.Unix(), updatedAt: now.Unix()},
		{id: "broken", name: "broken.star", source: brokenSource, enabled: false, timeoutMS: 500, status: kam.StatusBROKEN,
			lastError: "SyntaxError: line 1, column 27: expected ':'", revision: 3, createdAt: now.Unix(), updatedAt: now.Unix()},
		{id: "frozen", name: "frozen.star", source: frozenSource, enabled: true, timeoutMS: 250, status: kam.StatusFROZEN,
			lastError: "Timeout after 250 ms: the script took too long and KAM paused it", revision: 2, createdAt: now.Unix(), updatedAt: now.Unix(),
			runs: 4, errors: 4, lastRunAt: now.Add(-3 * time.Minute), lastDurationMS: 250, lastReplies: 0},
	}
}

// SetScriptsAPI turns the script surface off, as KAM does with automation off: the routes stay
// registered and answer 503 (its D9).
func (f *Fake) SetScriptsAPI(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.automationOn = on
}

// SetDryRun turns the phase-5 dry-run route on or off; off, an older KAM answers 405 from the
// /scripts/{id} routes that cover the path (MON-AU-20).
func (f *Fake) SetDryRun(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dryRunOn = on
}

// SetRunInfo turns the run object on or off, which only a KAM with phase 5 sends (API-AU-2).
func (f *Fake) SetRunInfo(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runInfoOn = on
}

// SetDryRunBusy makes the next dry-run request answer 409: one test at a time (§7.6.1).
func (f *Fake) SetDryRunBusy(busy bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dryRunBusy = busy
}

func (f *Fake) wireScript(s fakeScript) kam.Script {
	out := kam.Script{
		ID: s.id, Name: s.name, Source: s.source, Enabled: s.enabled, TimeoutMS: s.timeoutMS,
		RuntimeStatus: s.status, LastError: s.lastError, Revision: s.revision,
		CreatedAt: s.createdAt, UpdatedAt: s.updatedAt,
	}
	if f.runInfoOn {
		out.Run = &kam.ScriptRun{Runs: s.runs, Errors: s.errors, LastDurationMS: s.lastDurationMS, LastReplies: s.lastReplies}
		if !s.lastRunAt.IsZero() {
			out.Run.LastRunAt = ptr(s.lastRunAt.UTC())
		}
	}
	return out
}

func (f *Fake) findScript(id string) (int, bool) {
	for i, script := range f.scripts {
		if script.id == id {
			return i, true
		}
	}
	return 0, false
}

func (f *Fake) nameTaken(name string, exceptID string) bool {
	for _, script := range f.scripts {
		if script.name == name && script.id != exceptID {
			return true
		}
	}
	return false
}

// fakeCompile is the fake compiler: a script must define on_group_text(message): on one line
// (Automations.md §3.1). The message carries a line and column like KAM's own (MON-AU-7).
func fakeCompile(source string) string {
	for i, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "def on_group_text(") {
			if !strings.HasSuffix(trimmed, ":") {
				return fmt.Sprintf("SyntaxError: line %d, column %d: expected ':'", i+1, len(trimmed)+1)
			}
			return ""
		}
	}
	return "SyntaxError: line 1, column 1: a script defines on_group_text(message)"
}

// The fake dry-run engine: reply literals become captured replies (never sent).
var (
	guardRe   = regexp.MustCompile(`message\["body"\]\s*==\s*"([^"\\]*)`)
	replyRe   = regexp.MustCompile(`reply\(\s*([^,]+),\s*"((?:[^"\\]|\\.)*)"\s*\)`)
	channelRe = regexp.MustCompile(`^"([^"]*)"$`)
	escapeRe  = regexp.MustCompile(`\\(.)`)
)

func fakeDryRun(test kam.DryRunRequest) kam.DryRunResponse {
	out := kam.DryRunResponse{Replies: []kam.DryRunReply{}}
	if text := fakeCompile(test.Source); text != "" {
		out.Error = text
		return out
	}
	// A guard is a comparison against the fixture's body; when one guards the reply and the
	// fixture does not match it, the script stays silent (like the live engine would).
	if guards := guardRe.FindAllStringSubmatch(test.Source, -1); len(guards) > 0 {
		matched := false
		for _, guard := range guards {
			if guard[1] == test.Message.Body {
				matched = true
				break
			}
		}
		if !matched {
			if strings.Contains(test.Source, "while True") {
				out.Error = "Timeout after " + strconv.FormatInt(testTimeout(test), 10) + " ms"
				return out
			}
			// A completed run that stays silent is a success with no replies.
			out.DurationMS = 1
			out.OK = true
			return out
		}
	}
	if strings.Contains(test.Source, "while True") {
		out.Error = "Timeout after " + strconv.FormatInt(testTimeout(test), 10) + " ms"
		return out
	}
	for _, m := range replyRe.FindAllStringSubmatch(test.Source, -1) {
		channel := test.Message.Channel
		if c := channelRe.FindStringSubmatch(strings.TrimSpace(m[1])); c != nil {
			channel = c[1]
		}
		out.Replies = append(out.Replies, kam.DryRunReply{Channel: channel, Text: escapeRe.ReplaceAllString(m[2], "$1")})
	}
	if len(out.Replies) > 16 {
		out.Replies = nil
		out.Error = "the test captured more than 16 replies"
		return out
	}
	total := 0
	for _, reply := range out.Replies {
		total += len(reply.Channel) + len(reply.Text)
	}
	if total > 4096 {
		out.Replies = nil
		out.Error = "the test captured more than 4096 bytes of replies"
		return out
	}
	out.DurationMS = 2
	out.OK = true
	return out
}

// testTimeout is the timeout the dry run reports in its error: the requested test timeout,
// default 500 ms (§7.6.1).
func testTimeout(test kam.DryRunRequest) int64 {
	if test.TimeoutMS > 0 {
		return test.TimeoutMS
	}
	return 500
}

func fakeDecode(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeFakeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func writeFakeError(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": text})
}

func writeFakeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// scriptsDisabled answers 503 when automation is off; the caller holds f.mu.
func (f *Fake) scriptsDisabled(w http.ResponseWriter) bool {
	if f.automationOn {
		return false
	}
	writeFakeError(w, http.StatusServiceUnavailable, "automation is disabled")
	return true
}

// fakeSave applies a create (id "") or update and answers with the saved script.
// The caller holds f.mu.
func (f *Fake) fakeSave(w http.ResponseWriter, id string, incoming kam.Script, expectedRevision int64) {
	if text := fakeCompile(incoming.Source); text != "" {
		writeFakeError(w, http.StatusUnprocessableEntity, text)
		return
	}
	if strings.TrimSpace(incoming.ID) == "" || strings.TrimSpace(incoming.Name) == "" {
		// KAM's own required check and words: its create takes the id from the body too (D32).
		writeFakeError(w, http.StatusUnprocessableEntity, "script id and name are required")
		return
	}
	if id == "" {
		for _, script := range f.scripts {
			if script.id == incoming.ID {
				writeFakeError(w, http.StatusConflict, "automation script revision conflict")
				return
			}
		}
	}
	if f.nameTaken(incoming.Name, id) {
		writeFakeError(w, http.StatusConflict, "automation script name already in use")
		return
	}
	if id == "" {
		// A create: revision 0 is what a create means (wire contract); the id is the client's,
		// KAMmon's is the name as typed (D32).
		f.scripts = append(f.scripts, fakeScript{
			id: incoming.ID, name: incoming.Name, source: incoming.Source, enabled: incoming.Enabled,
			timeoutMS: incoming.TimeoutMS, status: kam.StatusREADY, revision: 1,
			createdAt: f.now().Unix(), updatedAt: f.now().Unix(),
		})
		writeFakeJSON(w, http.StatusCreated, f.wireScript(f.scripts[len(f.scripts)-1]))
		return
	}
	idx, exists := f.findScript(id)
	if !exists {
		writeFakeError(w, http.StatusNotFound, "no such script")
		return
	}
	saved := &f.scripts[idx]
	if saved.revision != expectedRevision {
		writeFakeError(w, http.StatusConflict, "revision "+strconv.FormatInt(expectedRevision, 10)+" is stale, current is "+strconv.FormatInt(saved.revision, 10))
		return
	}
	saved.name, saved.source, saved.enabled, saved.timeoutMS = incoming.Name, incoming.Source, incoming.Enabled, incoming.TimeoutMS
	saved.revision++
	saved.updatedAt = f.now().Unix()
	writeFakeJSON(w, http.StatusOK, f.wireScript(*saved))
}

func (f *Fake) scriptMutation(w http.ResponseWriter, r *http.Request) (mutation struct {
	Script           kam.Script `json:"script"`
	ExpectedRevision int64      `json:"expected_revision"`
}, ok bool) {
	if !fakeDecode(w, r, &mutation) {
		return mutation, false
	}
	return mutation, true
}

func (f *Fake) handleScriptList(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scriptsDisabled(w) {
		return
	}
	out := make([]kam.Script, 0, len(f.scripts))
	for _, script := range f.scripts {
		out = append(out, f.wireScript(script))
	}
	writeFakeJSON(w, http.StatusOK, out)
}

func (f *Fake) handleScriptGet(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scriptsDisabled(w) {
		return
	}
	if idx, ok := f.findScript(r.PathValue("id")); ok {
		writeFakeJSON(w, http.StatusOK, f.wireScript(f.scripts[idx]))
		return
	}
	writeFakeError(w, http.StatusNotFound, "no such script")
}

func (f *Fake) handleScriptCreate(w http.ResponseWriter, r *http.Request) {
	mutation, ok := f.scriptMutation(w, r)
	if !ok {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scriptsDisabled(w) {
		return
	}
	f.fakeSave(w, "", mutation.Script, 0)
}

func (f *Fake) handleScriptUpdate(w http.ResponseWriter, r *http.Request) {
	mutation, ok := f.scriptMutation(w, r)
	if !ok {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scriptsDisabled(w) {
		return
	}
	f.fakeSave(w, r.PathValue("id"), mutation.Script, mutation.ExpectedRevision)
}

func (f *Fake) handleScriptDelete(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
	if err != nil || revision <= 0 {
		writeFakeError(w, http.StatusBadRequest, "positive revision query parameter is required")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scriptsDisabled(w) {
		return
	}
	idx, exists := f.findScript(r.PathValue("id"))
	if !exists {
		writeFakeError(w, http.StatusNotFound, "no such script")
		return
	}
	if f.scripts[idx].revision != revision {
		writeFakeError(w, http.StatusConflict, "revision "+strconv.FormatInt(revision, 10)+" is stale")
		return
	}
	f.scripts = append(f.scripts[:idx], f.scripts[idx+1:]...)
	w.WriteHeader(http.StatusNoContent)
}

func (f *Fake) handleScriptValidate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID        string `json:"id"`
		Source    string `json:"source"`
		TimeoutMS int64  `json:"timeout_ms"`
	}
	if !fakeDecode(w, r, &request) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scriptsDisabled(w) {
		return
	}
	if text := fakeCompile(request.Source); text != "" {
		writeFakeError(w, http.StatusUnprocessableEntity, text)
		return
	}
	writeFakeJSON(w, http.StatusOK, map[string]bool{"valid": true})
}

func (f *Fake) handleScriptEnabled(w http.ResponseWriter, r *http.Request, want bool) {
	var request struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if !fakeDecode(w, r, &request) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scriptsDisabled(w) {
		return
	}
	idx, exists := f.findScript(r.PathValue("id"))
	if !exists {
		writeFakeError(w, http.StatusNotFound, "no such script")
		return
	}
	if request.ExpectedRevision <= 0 {
		writeFakeError(w, http.StatusBadRequest, "positive expected_revision is required")
		return
	}
	saved := &f.scripts[idx]
	if saved.revision != request.ExpectedRevision {
		writeFakeError(w, http.StatusConflict, "revision "+strconv.FormatInt(request.ExpectedRevision, 10)+" is stale, current is "+strconv.FormatInt(saved.revision, 10))
		return
	}
	saved.enabled = want
	saved.revision++
	saved.updatedAt = f.now().Unix()
	writeFakeJSON(w, http.StatusOK, f.wireScript(*saved))
}

func (f *Fake) handleScriptRecover(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ExpectedRevision int64 `json:"expected_revision"`
		TimeoutMS        int64 `json:"timeout_ms"`
	}
	if !fakeDecode(w, r, &request) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scriptsDisabled(w) {
		return
	}
	idx, exists := f.findScript(r.PathValue("id"))
	if !exists {
		writeFakeError(w, http.StatusNotFound, "no such script")
		return
	}
	saved := &f.scripts[idx]
	if saved.revision != request.ExpectedRevision {
		writeFakeError(w, http.StatusConflict, "revision "+strconv.FormatInt(request.ExpectedRevision, 10)+" is stale, current is "+strconv.FormatInt(saved.revision, 10))
		return
	}
	if saved.status != kam.StatusFROZEN && saved.status != kam.StatusBROKEN {
		writeFakeError(w, http.StatusUnprocessableEntity, "recover wants a FROZEN or BROKEN script")
		return
	}
	saved.status, saved.lastError = kam.StatusREADY, ""
	if request.TimeoutMS > 0 {
		saved.timeoutMS = request.TimeoutMS
	}
	saved.revision++
	saved.updatedAt = f.now().Unix()
	writeFakeJSON(w, http.StatusOK, f.wireScript(*saved))
}

func (f *Fake) handleScriptDryRun(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scriptsDisabled(w) {
		return
	}
	if !f.dryRunOn {
		// An older KAM registers no dry-run route; its /scripts/{id} routes cover the path, so
		// the mux answers the method itself, with plain words and no JSON (MON-AU-20).
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte("Method Not Allowed\n"))
		return
	}
	if f.dryRunBusy {
		writeFakeError(w, http.StatusConflict, "another dry-run test is running")
		return
	}
	var test kam.DryRunRequest
	if !fakeDecode(w, r, &test) {
		return
	}
	if test.Message.Channel == "" {
		writeFakeError(w, http.StatusUnprocessableEntity, "message requires a channel")
		return
	}
	enabled := map[string]bool{"#emmen": true, "#public": true}
	if !enabled[test.Message.Channel] {
		writeFakeError(w, http.StatusUnprocessableEntity, "message channel must be an enabled configured channel")
		return
	}
	if test.TimeoutMS < 1 || test.TimeoutMS > 3000 {
		if test.TimeoutMS != 0 {
			writeFakeError(w, http.StatusUnprocessableEntity, "timeout_ms must be between 1 and 3000")
			return
		}
		test.TimeoutMS = 500 // the default when omitted (§7.6.1)
	}
	writeFakeJSON(w, http.StatusOK, fakeDryRun(test))
}
