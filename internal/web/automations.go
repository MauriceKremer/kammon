// automations.go builds the Automations page (docs/requirements/Automations.md). The script list
// comes from the store, where the poller keeps it (MON-AU-1); a viewer sees that list and nothing
// more (MON-AU-4). The editor and every script action are admin-only and talk to KAM directly,
// one call per request (AGENTS rule 3): the editor loads a script with GET /scripts/{id} (MON-AU-6,
// D29) and the buttons validate, dry-run, save, enable, disable, recover and delete.
// The dry run degrades on an older KAM: a 404 for the route hides the button and says why
// (MON-AU-20, D30).
package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
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

// starterSource is the template a new script begins with (Automations.md §3.1): a one-message,
// one-reply script that the beginner can validate, test and save.
const starterSource = "def on_group_text(message):\n    if message[\"body\"] == \"!ping\":\n        reply(message[\"channel\"], \"pong\")\n"

const (
	// kamActionTimeout bounds one KAM call made from a request; a 3000 ms dry run fits under it.
	kamActionTimeout = 5 * time.Second
	// maxScriptForm bounds one editor action's form, source text included (bounded memory).
	maxScriptForm = 64 << 10
)

// consoleLine is one line of the action console; Class styles it ("fail", or none for plain text).
type consoleLine struct{ Text, Class string }

// scriptConsole is the answer of an editor action as the page shows it. Saved marks a save that
// succeeded, so editor.js drops its unsaved-changes note (MON-AU-14).
type scriptConsole struct {
	Lines []consoleLine
	Saved bool
}

// scriptState is the loaded script's mutable state (Automations.md §3, §4.2). Action answers
// carry it out of band, so the hidden revision, the enabled control, the timeout and the title
// stay truthful: a save then carries the revision KAM last answered with (MON-AU-8).
type scriptState struct {
	ID, Revision, Timeout string
	Enabled               bool
	Title, Words, Dot     string
}

// scriptRow is one entry of the script list as a role may see it (MON-AU-2, 3, 4). No source ever.
type scriptRow struct {
	ID, Name, State, Dot, Revision, RunsNote, RunInfo, Error string
	Selected                                                 bool
}

type editorData struct {
	Open bool
	// New is a script that is not on KAM yet: the starter with revision 0 (MON-AU-8).
	New       bool
	ID, Name  string
	Source    string
	Enabled   bool
	TimeoutMS string
	Revision  string
	// Frozen and Broken carry the state the page answers to (MON-AU-2, 11); StateName is the
	// list's word for it.
	Frozen, Broken bool
	StateName      string
	// LastError is the script's last error, for the Frozen and Broken rows (MON-AU-2).
	LastError string
	// LoadError is why the editor holds nothing (KAM's words where it has any, MON-AU-7).
	LoadError string
	// NotConnected is a KAMmon without a client for KAM's script calls.
	NotConnected bool
	// RunSummary is the last run line, empty when this KAM sends no run object (MON-AU-3).
	RunSummary string
	// DryRunSupported is false when KAM answered 404 for the dry-run route (MON-AU-20, D30).
	DryRunSupported bool
	// DryRunResult is the box under the Dry run button at editor open: empty, a live region for
	// the next test's replies (MON-AU-15).
	DryRunResult scriptConsole
	Console      scriptConsole
}

type automationsData struct {
	Scripts       []scriptRow
	TruncatedNote string
	EmptyMessage  string
	CanEdit       bool
	// Editor is open for an administrator who picked a script or asked for a new one.
	Editor editorData
}

// scriptStateWords is the honest state pair of a script: the word the page shows and the dot
// class that comes with it (MON-AU-2). Meaning never rides on colour alone.
func scriptStateWords(script kam.Script) (words, dot string) {
	switch script.RuntimeStatus {
	case kam.StatusREADY:
		if !script.Enabled {
			return "Disabled", ""
		}
		return "Ready", "fresh"
	case kam.StatusFROZEN:
		return "Frozen", "stale"
	case kam.StatusBROKEN:
		return "Broken", "down"
	default:
		// An unknown state carries as it is, like every other protocol value.
		return script.RuntimeStatus, "unknown"
	}
}

// replyWord is "1 reply" or "n replies".
func replyWord(n int) string {
	if n == 1 {
		return "1 reply"
	}
	return fmt.Sprintf("%d replies", n)
}

// runsWord is the run information of a script (API-AU-2): "12 runs · 1 error" for the state line
// and "last run 14:59:48 · 2 ms · 1 reply" for the detail, or nothing when this KAM sends no run
// object (MON-AU-3).
func runsWord(sc kam.Script) (note, info string) {
	if sc.Run == nil {
		return "", ""
	}
	if sc.Run.Runs == 0 || sc.Run.LastRunAt == nil {
		return "No runs recorded.", ""
	}
	note = fmt.Sprintf("%s runs", thousands(int(sc.Run.Runs)))
	if sc.Run.Errors > 0 {
		note += fmt.Sprintf(" · %s error", thousands(int(sc.Run.Errors)))
	}
	info = fmt.Sprintf("last run %s · %d ms · %s", sc.Run.LastRunAt.Local().Format("15:04:05"), sc.Run.LastDurationMS, replyWord(sc.Run.LastReplies))
	return note, info
}

// scriptRowFor renders one stored script for the list: the state word, the revision KAM has, and
// the run information when this KAM sends it (MON-AU-2, 3).
func (s *Server) scriptRowFor(script kam.Script, selected bool) scriptRow {
	words, dot := scriptStateWords(script)
	note, info := runsWord(script)
	return scriptRow{
		ID: script.ID, Name: script.Name, State: words, Dot: dot,
		Revision: strconv.FormatInt(script.Revision, 10),
		RunsNote: note, RunInfo: info,
		Error: script.LastError, Selected: selected,
	}
}

// scriptListData is the list state for a full page, a poll or an out-of-band answer; selectedID
// marks the script the editor is open on, so the master-detail pair keeps matching. An action's
// answer passes the script KAM just saved, so the list shows the acted script's true state — the
// store's copy is at most one poll old, and a created script is not in it yet.
func (s *Server) scriptListData(role config.Role, selectedID string, fresh *kam.Script) automationsData {
	data := automationsData{CanEdit: view.CanEditScripts(role), EmptyMessage: s.scriptEmptyWords()}
	list, more := s.store.Scripts()
	if fresh != nil {
		next := make([]kam.Script, 0, len(list)+1)
		patched := false
		for _, script := range list {
			if script.ID == fresh.ID {
				next, patched = append(next, *fresh), true
				continue
			}
			next = append(next, script)
		}
		if !patched {
			next = append([]kam.Script{*fresh}, next...)
		}
		list = next
	}
	rows := make([]scriptRow, 0, len(list))
	for _, script := range list {
		rows = append(rows, s.scriptRowFor(script, data.CanEdit && script.ID == selectedID))
	}
	data.Scripts = rows
	if more > 0 {
		data.TruncatedNote = fmt.Sprintf("%s more scripts are not shown.", thousands(more))
	}
	return data
}

// scriptEmptyWords is what an empty list says: an empty list can wait for a first answer, name a
// KAM without the script interface, or be a real empty list (MON-AU-5, 20).
func (s *Server) scriptEmptyWords() string {
	now := s.now()
	if f, _ := s.store.SnapshotFreshness("scripts", now); f == store.Fresh {
		return "No scripts on this KAM yet."
	}
	if st, _ := s.store.Status(); st.Epoch != "" && !st.Automation.Enabled {
		return "Automation is disabled on KAM."
	}
	if s.store.NotServed("scripts", now) {
		return "Waiting for KAM's script list. Needs a newer KAM."
	}
	return "Waiting for KAM's script list."
}

// automationsView builds the page: the list for every role, the editor for an administrator only
// (MON-AU-4). A viewer asking for an editor gets the plain list and, true to the visibility
// rules, no source is fetched for them.
func (s *Server) automationsView(r *http.Request, role config.Role, q url.Values) automationsData {
	data := s.scriptListData(role, q.Get("script"), nil)
	if !data.CanEdit {
		return data
	}
	switch {
	case q.Get("new") != "":
		data.Editor = s.newEditor(r)
	default:
		if id := q.Get("script"); id != "" {
			data.Editor = s.openEditor(r, id)
		}
	}
	return data
}

// scriptBar is the page's own stale bar: the script list is judged on its own read alone, the way
// other pages judge their snapshots (docs/requirements/Automations.md §6, MON-AU-5). It replaces
// the shared bar on this page and names the cause where KAMmon knows one.
func (s *Server) scriptBar() staleBar {
	now := s.now()
	if st, _ := s.store.Status(); st.Epoch != "" && !st.Automation.Enabled {
		return staleBar{Show: true, Text: "Automation is disabled on KAM."}
	}
	if s.store.NotServed("scripts", now) {
		return staleBar{Show: true, Text: "This KAM offers no script interface. Needs a newer KAM."}
	}
	f, last := s.store.SnapshotFreshness("scripts", now)
	switch {
	case f == store.Fresh:
		return staleBar{}
	case last.IsZero():
		return staleBar{Show: true, Text: "Waiting for KAM's script interface."}
	case f == store.Down:
		return staleBar{Show: true, Down: true, Text: fmt.Sprintf("KAM's script interface has not answered for %s. Showing data from %s.", shortAge(now.Sub(last)), last.Local().Format("15:04:05"))}
	default:
		return staleBar{Show: true, Text: fmt.Sprintf("The script list is out of date. Showing data from %s.", last.Local().Format("15:04:05"))}
	}
}

// automationsPage is GET /automations. The page-level bar speaks about scripts (MON-AU-5).
func (s *Server) automationsPage(w http.ResponseWriter, r *http.Request) {
	role := principalOf(r).Role
	data := s.automationsView(r, role, r.URL.Query())
	pg := s.pageFor(r, "automations", "Automations", "Starlark scripts the node runs on channel messages", data)
	pg.Stale = s.scriptBar()
	s.render(w, r, http.StatusOK, "automations", pg)
}

// scriptListPartial is what htmx fetches every 10 seconds (MON-AU-1); the stale bar and node
// block ride along out of band, as on the other pages.
func (s *Server) scriptListPartial(w http.ResponseWriter, r *http.Request) {
	data := s.scriptListData(principalOf(r).Role, r.URL.Query().Get("script"), nil)
	var buf bytes.Buffer
	t := s.pages["automations"]
	parts := append([]partial{{"script-list", data}, {"stalebar", s.scriptBar()}}, s.sideParts(r)...)
	for _, part := range parts {
		if err := t.ExecuteTemplate(&buf, part.name, part.data); err != nil {
			http.Error(w, "Template error.", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = buf.WriteTo(w)
}

// newEditor is the editor for a script that is not on KAM yet (Automations.md §3.1): the starter,
// revision 0 is what a create means (MON-AU-8).
func (s *Server) newEditor(r *http.Request) editorData {
	return editorData{
		Open: true, New: true, Source: starterSource,
		Enabled: false, TimeoutMS: "500", Revision: "0",
		StateName: "Not on KAM yet", DryRunSupported: s.probeDryRun(r),
	}
}

// openEditor loads one script with GET /scripts/{id} straight from an admin request (MON-AU-6,
// D29): the store keeps no source. A KAMmon without a client, or a KAM that will not answer,
// renders a notice instead of an editor that pretends to hold the script.
func (s *Server) openEditor(r *http.Request, id string) editorData {
	ed := editorData{Open: true, ID: id, DryRunSupported: s.probeDryRun(r)}
	if s.kam == nil {
		ed.NotConnected = true
		return ed
	}
	ctx, cancel := context.WithTimeout(r.Context(), kamActionTimeout)
	defer cancel()
	script, err := s.kam.Script(ctx, id)
	if err != nil {
		text, isErr := kamErrorWords(err)
		if !isErr {
			text = "KAMmon could not load this script."
		}
		ed.LoadError = "KAMmon could not load this script. " + text
		return ed
	}
	words, _ := scriptStateWords(script)
	ed.Name = script.Name
	ed.Source = script.Source
	ed.Enabled = script.Enabled
	ed.TimeoutMS = strconv.FormatInt(script.TimeoutMS, 10)
	ed.Revision = strconv.FormatInt(script.Revision, 10)
	ed.StateName = words
	ed.Frozen = script.RuntimeStatus == kam.StatusFROZEN
	ed.Broken = script.RuntimeStatus == kam.StatusBROKEN
	ed.LastError = script.LastError
	_, ed.RunSummary = runsWord(script)
	switch {
	case ed.Frozen:
		ed.Console = scriptConsole{Lines: []consoleLine{{Text: "KAM paused this script: " + script.LastError, Class: "fail"}}}
	case ed.Broken:
		ed.Console = scriptConsole{Lines: []consoleLine{{Text: "KAM could not run this script: " + script.LastError, Class: "fail"}}}
	}
	return ed
}

// probeDryRun asks KAM whether the dry-run route exists: an empty POST costs KAM one validation
// and nothing else (MON-AU-20, D30). The answer is not remembered; the next editor open probes
// again.
func (s *Server) probeDryRun(r *http.Request) bool {
	if s.kam == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), kamActionTimeout)
	defer cancel()
	return s.kam.ProbeDryRun(ctx) == nil
}

// scriptReply is an answer to an editor action: the console (the swap target) plus, out of band,
// the parts of the page the action changed (docs/DESIGN.md §7.2). Redirect and Replace move the
// browser when the page itself must follow, as a delete or a create does.
type scriptReply struct {
	Console    scriptConsole
	State      *scriptState
	List       bool
	SelectedID string
	// Fresh is the script KAM just answered with, for the list's one true row.
	Fresh    *kam.Script
	Redirect string
	Replace  string
	// Fixture answers the Dry run button: its console swaps into the test-message form's own
	// result below that button, not into the editor's console (MON-AU-15).
	Fixture bool
}

// writeScriptReply writes the console and the out-of-band parts. Every answer swaps: a failure
// is console text, never a missing swap (the console keeps role=status).
func (s *Server) writeScriptReply(w http.ResponseWriter, r *http.Request, reply scriptReply) {
	t := s.pages["automations"]
	var buf bytes.Buffer
	name := "console"
	if reply.Fixture {
		name = "fixture-console"
	}
	if err := t.ExecuteTemplate(&buf, name, reply.Console); err != nil {
		http.Error(w, "Template error.", http.StatusInternalServerError)
		return
	}
	if reply.State != nil {
		if err := t.ExecuteTemplate(&buf, "script-state-oob", *reply.State); err != nil {
			http.Error(w, "Template error.", http.StatusInternalServerError)
			return
		}
	}
	if reply.List {
		data := s.scriptListData(principalOf(r).Role, reply.SelectedID, reply.Fresh)
		if err := t.ExecuteTemplate(&buf, "script-list-oob", data); err != nil {
			http.Error(w, "Template error.", http.StatusInternalServerError)
			return
		}
	}
	if reply.Redirect != "" {
		w.Header().Set("HX-Redirect", reply.Redirect)
	}
	if reply.Replace != "" {
		w.Header().Set("HX-Replace-Url", reply.Replace)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = buf.WriteTo(w)
}

// kamErrorWords turns a script-call error into console words (MON-AU-7): KAM's own text verbatim
// where it has one, KAMmon's words when the call got no answer to judge.
func kamErrorWords(err error) (string, bool) {
	var se *kam.StatusError
	switch {
	case err == nil:
		return "", false
	case errors.As(err, &se):
		return se.Text, true
	case errors.Is(err, kam.ErrNotFound):
		return "No script with that id. The list shows what KAM has.", true
	default:
		return "KAMmon could not reach KAM.", true
	}
}

// scriptForm reads a bounded form of an editor action; on a failure it answers with a console and
// returns nil, so the console stays live for another try.
func (s *Server) scriptForm(w http.ResponseWriter, r *http.Request) url.Values {
	r.Body = http.MaxBytesReader(w, r.Body, maxScriptForm)
	if err := r.ParseForm(); err != nil {
		s.writeScriptReply(w, r, scriptReply{Console: scriptConsole{Lines: []consoleLine{{Text: "The form did not arrive whole. Press again.", Class: "fail"}}}})
		return nil
	}
	return url.Values(r.PostForm)
}

// notConnectedConsole is the console of a KAMmon without a client for KAM.
func notConnectedConsole() scriptConsole {
	return scriptConsole{Lines: []consoleLine{{Text: "KAMmon is not connected to KAM.", Class: "fail"}}}
}

// confirmConsole says an error verbatim, or confirms with KAMmon's own words.
func confirmConsole(ok string, err error) scriptConsole {
	if text, isErr := kamErrorWords(err); isErr {
		return scriptConsole{Lines: []consoleLine{{Text: text, Class: "fail"}}}
	}
	return scriptConsole{Lines: []consoleLine{{Text: ok}}}
}

// timeoutField parses a milliseconds field: an empty field keeps the default; a whole number of
// at least one millisecond is right, anything else is refused in words (MON-AU-13).
func timeoutField(raw string, def int64) (int64, string) {
	if strings.TrimSpace(raw) == "" {
		return def, ""
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || strings.Contains(raw, ".") {
		return 0, "The timeout must be a whole number of milliseconds, one or more."
	}
	return n, ""
}

// stateOf reads the loaded script's state from the form's hidden inputs. An empty id or a
// revision of 0 is the unsaved script (MON-AU-8).
func stateOf(form url.Values) scriptState {
	revision := form.Get("revision")
	if revision == "" {
		revision = "0"
	}
	return scriptState{ID: form.Get("id"), Revision: revision, Timeout: form.Get("timeout"), Enabled: form.Get("enabled") == "true"}
}

// stateFromScript is the truth after KAM answered: the returned script's revision, timeout and
// enabled state, with the list's state words.
func stateFromScript(saved kam.Script) *scriptState {
	words, dot := scriptStateWords(saved)
	return &scriptState{
		ID: saved.ID, Revision: strconv.FormatInt(saved.Revision, 10),
		Timeout: strconv.FormatInt(saved.TimeoutMS, 10), Enabled: saved.Enabled,
		Title: saved.Name, Words: words, Dot: dot,
	}
}

// stateFromStore restores the loaded script's state after a refused action: the list holds the
// last known words, at most one poll old. It is nil for a script the list never saw.
func (s *Server) stateFromStore(id, revision, timeout string, enabled bool) *scriptState {
	list, _ := s.store.Scripts()
	for _, script := range list {
		if script.ID != id {
			continue
		}
		state := stateFromScript(script)
		state.Revision, state.Timeout, state.Enabled = revision, timeout, enabled
		return state
	}
	return nil
}

// scriptValidateSubmit answers the Validate button (MON-AU-7): KAM's message verbatim, because
// it carries the line and column.
func (s *Server) scriptValidateSubmit(w http.ResponseWriter, r *http.Request) {
	form := s.scriptForm(w, r)
	if form == nil {
		return
	}
	if s.kam == nil {
		s.writeScriptReply(w, r, scriptReply{Console: notConnectedConsole()})
		return
	}
	timeout, problem := timeoutField(form.Get("timeout"), 500)
	if problem != "" {
		s.writeScriptReply(w, r, scriptReply{Console: scriptConsole{Lines: []consoleLine{{Text: problem, Class: "fail"}}}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), kamActionTimeout)
	defer cancel()
	err := s.kam.ValidateScript(ctx, form.Get("id"), form.Get("source"), timeout)
	s.writeScriptReply(w, r, scriptReply{Console: confirmConsole("KAM accepts this script.", err)})
}

func timeoutOf(raw string, def int64) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if n < 1 {
		return def
	}
	return n
}

// fixtureMessage reads the made-up message of a dry run (MON-AU-15, §3). An empty field means
// "not set"; KAM applies its own default. A filled signal field means the message carries
// receiving data — has_rx_meta then says so.
func fixtureMessage(form url.Values) (kam.DryRunMessage, string) {
	message := kam.DryRunMessage{Channel: strings.TrimSpace(form.Get("fx_channel"))}
	if message.Channel == "" {
		return message, "The test needs a channel, such as #emmen."
	}
	message.Sender = form.Get("fx_sender")
	message.Body = form.Get("fx_body")
	whole := func(name, what string) (int, string) {
		raw := strings.TrimSpace(form.Get(name))
		if raw == "" {
			return 0, ""
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return 0, what + " must be a whole number."
		}
		return n, ""
	}
	hops, problem := whole("fx_hops", "Hops")
	if problem == "" {
		message.HopCount = hops
	}
	var hasMeta bool
	if snr := strings.TrimSpace(form.Get("fx_snr")); problem == "" && snr != "" {
		f, err := strconv.ParseFloat(snr, 64)
		if err != nil {
			return message, "SNR must be a number, such as 8.5."
		}
		// The wire carries quarter dB (Automations.md §3.1); the form speaks dB.
		message.SNRQuarterDB = ptrOf(int(math.Round(f * 4)))
		hasMeta = true
	}
	if raw := strings.TrimSpace(form.Get("fx_rssi")); problem == "" && raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return message, "RSSI must be a whole number."
		}
		message.RSSIdBm = ptrOf(n)
		hasMeta = true
	}
	message.HasRxMeta = hasMeta
	if drift, _ := whole("fx_drift", "Clock drift"); problem == "" && form.Get("fx_drift") != "" {
		message.TimeDriftSeconds = ptrOf(drift)
	}
	return message, problem
}

// ptr is a small helper for the optional fixture fields.
func ptrOf[T any](v T) *T { return &v }

// scriptDryRunSubmit answers the Dry run button (MON-AU-15, 16, 17): the unsaved editor text, a
// test timeout of its own, and the fixture. The console always says nothing was sent.
func (s *Server) scriptDryRunSubmit(w http.ResponseWriter, r *http.Request) {
	form := s.scriptForm(w, r)
	if form == nil {
		return
	}
	sent := consoleLine{Text: "Nothing was sent."}
	if s.kam == nil {
		console := notConnectedConsole()
		console.Lines = append(console.Lines, sent)
		s.writeScriptReply(w, r, scriptReply{Console: console, Fixture: true})
		return
	}
	message, problem := fixtureMessage(form)
	if problem != "" {
		s.writeScriptReply(w, r, scriptReply{Console: scriptConsole{Lines: []consoleLine{{Text: problem, Class: "fail"}, sent}}, Fixture: true})
		return
	}
	test := kam.DryRunRequest{Source: form.Get("source"), Message: message}
	if raw := strings.TrimSpace(form.Get("fx_timeout")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		// KAM answers 422 out of range (§7.6.1); KAMmon refuses the obvious ones first (MON-AU-15).
		if err != nil || n < 1 || n > 3000 {
			s.writeScriptReply(w, r, scriptReply{Console: scriptConsole{Lines: []consoleLine{{Text: "The test timeout must be between 1 and 3000 ms.", Class: "fail"}, sent}}, Fixture: true})
			return
		}
		test.TimeoutMS = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), kamActionTimeout)
	defer cancel()
	out, err := s.kam.DryRun(ctx, test)
	if err != nil {
		console := dryRunErrorConsole(err)
		console.Lines = append(console.Lines, sent)
		s.writeScriptReply(w, r, scriptReply{Console: console, Fixture: true})
		return
	}
	var lines []consoleLine
	if out.Error != "" {
		lines = []consoleLine{{Text: "The test failed: " + out.Error, Class: "fail"}}
	} else {
		lines = []consoleLine{{Text: fmt.Sprintf("The test ran in %d ms.", out.DurationMS)}}
		for _, reply := range out.Replies {
			lines = append(lines, consoleLine{Text: "Would send to " + reply.Channel + ": " + reply.Text})
		}
		if len(out.Replies) == 0 {
			lines = append(lines, consoleLine{Text: "The script stayed silent."})
		}
	}
	lines = append(lines, sent)
	s.writeScriptReply(w, r, scriptReply{Console: scriptConsole{Lines: lines}, Fixture: true})
}

// dryRunErrorConsole is the console of a dry run that could not run at all: an older KAM misses
// the route — 404, or 405 from an older KAM whose /scripts/{id} routes cover the path
// (MON-AU-20, D30) — and a busy KAM says one test at a time (§7.6.1).
func dryRunErrorConsole(err error) scriptConsole {
	var se *kam.StatusError
	missing := errors.Is(err, kam.ErrNotFound)
	if !missing && errors.As(err, &se) {
		missing = se.Status == http.StatusNotFound || se.Status == http.StatusMethodNotAllowed
	}
	switch {
	case missing:
		return scriptConsole{Lines: []consoleLine{{Text: "Needs a newer KAM. This KAM has no dry-run interface.", Class: "fail"}}}
	case errors.As(err, &se) && (se.Status == http.StatusUnprocessableEntity || se.Status == http.StatusBadRequest):
		return scriptConsole{Lines: []consoleLine{{Text: "KAM refused the test: " + se.Text, Class: "fail"}}}
	case errors.As(err, &se) && se.Status == http.StatusConflict:
		return scriptConsole{Lines: []consoleLine{{Text: "KAM is running another test. Try again in a moment.", Class: "fail"}}}
	}
	text, _ := kamErrorWords(err)
	return scriptConsole{Lines: []consoleLine{{Text: text, Class: "fail"}}}
}

// scriptSaveSubmit answers the Save button (MON-AU-8, 9, 10): the enabled state rides from the
// loaded value, never from the checkbox — a toggle goes to KAM's own endpoints. The user's text
// stays in the editor whatever the answer (MON-AU-9).
func (s *Server) scriptSaveSubmit(w http.ResponseWriter, r *http.Request) {
	form := s.scriptForm(w, r)
	if form == nil {
		return
	}
	if s.kam == nil {
		s.writeScriptReply(w, r, scriptReply{Console: notConnectedConsole()})
		return
	}
	loaded := stateOf(form)
	name := strings.TrimSpace(form.Get("name"))
	if name == "" {
		s.writeScriptReply(w, r, scriptReply{Console: scriptConsole{Lines: []consoleLine{{Text: "A script needs a name.", Class: "fail"}}}})
		return
	}
	timeout, problem := timeoutField(form.Get("timeout"), 500)
	if problem != "" {
		s.writeScriptReply(w, r, scriptReply{Console: scriptConsole{Lines: []consoleLine{{Text: problem, Class: "fail"}}}})
		return
	}
	script := kam.Script{ID: loaded.ID, Name: name, Source: form.Get("source"), Enabled: loaded.Enabled, TimeoutMS: timeout}
	ctx, cancel := context.WithTimeout(r.Context(), kamActionTimeout)
	defer cancel()
	var saved kam.Script
	var err error
	isCreate := loaded.ID == "" || loaded.Revision == "0"
	if isCreate {
		saved, err = s.kam.CreateScript(ctx, script)
	} else {
		rev, revErr := strconv.ParseInt(loaded.Revision, 10, 64)
		if revErr != nil {
			s.writeScriptReply(w, r, scriptReply{Console: scriptConsole{Lines: []consoleLine{{Text: "Reload the page: the loaded revision is not a whole number.", Class: "fail"}}}})
			return
		}
		saved, err = s.kam.UpdateScript(ctx, script, rev)
	}
	if err != nil {
		s.writeScriptReply(w, r, scriptReply{Console: saveErrorConsole(err, isCreate)})
		return
	}
	reply := scriptReply{
		Console:    scriptConsole{Lines: []consoleLine{{Text: "Saved revision " + strconv.FormatInt(saved.Revision, 10) + "."}}, Saved: true},
		State:      stateFromScript(saved),
		List:       true,
		SelectedID: saved.ID,
		Fresh:      &saved,
	}
	if isCreate {
		// The script is on KAM now: the editor becomes an editor of it, so no second Save can
		// create a duplicate.
		reply.Replace = "/automations?script=" + url.QueryEscape(saved.ID)
	}
	s.writeScriptReply(w, r, reply)
}

// saveErrorConsole is the console of a save that failed (MON-AU-9): an update's 409 is changed
// elsewhere; a create has no revision to be stale, so its 409 is KAM's own words about a taken
// id or name (D32). The other statuses carry KAM's text verbatim and leave the editor as it is.
func saveErrorConsole(err error, isCreate bool) scriptConsole {
	var se *kam.StatusError
	switch {
	case !isCreate && errors.As(err, &se) && se.Status == http.StatusConflict:
		return scriptConsole{Lines: []consoleLine{{Text: "Changed elsewhere. Reload to see the latest version.", Class: "fail"}}}
	}
	text, _ := kamErrorWords(err)
	return scriptConsole{Lines: []consoleLine{{Text: text, Class: "fail"}}}
}

// unsavedConsole is the console of an action on a script that is not on KAM yet.
func unsavedConsole() scriptConsole {
	return scriptConsole{Lines: []consoleLine{{Text: "Save the script first.", Class: "fail"}}}
}

// revisionConsole is the console of an action whose loaded revision is unusable.
func revisionConsole() scriptConsole {
	return scriptConsole{Lines: []consoleLine{{Text: "Reload the page: the loaded revision is not a whole number.", Class: "fail"}}}
}

// scriptToggleSubmit answers the enabled control (MON-AU-10): on is enable, off is disable, each
// through KAM's own endpoints with the loaded revision. A save never toggles it.
func (s *Server) scriptToggleSubmit(w http.ResponseWriter, r *http.Request) {
	form := s.scriptForm(w, r)
	if form == nil {
		return
	}
	if s.kam == nil {
		s.writeScriptReply(w, r, scriptReply{Console: notConnectedConsole()})
		return
	}
	loaded := stateOf(form)
	rev, revErr := strconv.ParseInt(loaded.Revision, 10, 64)
	if loaded.ID == "" || loaded.Revision == "0" {
		s.writeScriptReply(w, r, scriptReply{Console: unsavedConsole()})
		return
	}
	if revErr != nil || rev < 1 {
		s.writeScriptReply(w, r, scriptReply{Console: revisionConsole()})
		return
	}
	enable := form.Has("enabled-box")
	ctx, cancel := context.WithTimeout(r.Context(), kamActionTimeout)
	defer cancel()
	var saved kam.Script
	var err error
	if enable {
		saved, err = s.kam.EnableScript(ctx, loaded.ID, rev)
	} else {
		saved, err = s.kam.DisableScript(ctx, loaded.ID, rev)
	}
	if err != nil {
		reply := scriptReply{Console: confirmConsole("", err)}
		if state := s.stateFromStore(loaded.ID, loaded.Revision, loaded.Timeout, loaded.Enabled); state != nil {
			reply.State = state
		}
		s.writeScriptReply(w, r, reply)
		return
	}
	action := "Disabled"
	if enable {
		action = "Enabled"
	}
	s.writeScriptReply(w, r, scriptReply{
		Console:    scriptConsole{Lines: []consoleLine{{Text: action + ". Revision " + strconv.FormatInt(saved.Revision, 10) + " is current."}}},
		State:      stateFromScript(saved),
		List:       true,
		SelectedID: saved.ID,
		Fresh:      &saved,
	})
}

// scriptRecoverSubmit answers the Recover button (MON-AU-11): it runs the script on KAM again,
// with an optional new timeout, and bumps the revision.
func (s *Server) scriptRecoverSubmit(w http.ResponseWriter, r *http.Request) {
	form := s.scriptForm(w, r)
	if form == nil {
		return
	}
	if s.kam == nil {
		s.writeScriptReply(w, r, scriptReply{Console: notConnectedConsole()})
		return
	}
	loaded := stateOf(form)
	if loaded.ID == "" || loaded.Revision == "0" {
		s.writeScriptReply(w, r, scriptReply{Console: unsavedConsole()})
		return
	}
	rev, revErr := strconv.ParseInt(loaded.Revision, 10, 64)
	if revErr != nil || rev < 1 {
		s.writeScriptReply(w, r, scriptReply{Console: revisionConsole()})
		return
	}
	// An empty field keeps the current timeout; the wire carries 0 for that (MON-AU-11).
	newTimeout := int64(0)
	if raw := strings.TrimSpace(form.Get("recover_timeout")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 {
			s.writeScriptReply(w, r, scriptReply{Console: scriptConsole{Lines: []consoleLine{{Text: "The new timeout must be a whole number of milliseconds, one or more.", Class: "fail"}}}})
			return
		}
		newTimeout = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), kamActionTimeout)
	defer cancel()
	saved, err := s.kam.RecoverScript(ctx, loaded.ID, newTimeout, rev)
	if err != nil {
		s.writeScriptReply(w, r, scriptReply{Console: saveErrorConsole(err, false), List: true, SelectedID: loaded.ID})
		return
	}
	reply := scriptReply{
		Console:    scriptConsole{Lines: []consoleLine{{Text: "Recovered. KAM runs this script again. Revision " + strconv.FormatInt(saved.Revision, 10) + " is current."}}},
		State:      stateFromScript(saved),
		List:       true,
		SelectedID: saved.ID,
		Fresh:      &saved,
	}
	s.writeScriptReply(w, r, reply)
}

// deleteForm reads the DELETE's own body: ParseForm reads the body of POST, PUT and PATCH only,
// so the delete's id and revision would arrive unread (MON-AU-12).
func (s *Server) deleteForm(w http.ResponseWriter, r *http.Request) url.Values {
	r.Body = http.MaxBytesReader(w, r.Body, maxScriptForm)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeScriptReply(w, r, scriptReply{Console: scriptConsole{Lines: []consoleLine{{Text: "The form did not arrive whole. Press again.", Class: "fail"}}}})
		return nil
	}
	values, err := url.ParseQuery(string(raw))
	if err != nil {
		s.writeScriptReply(w, r, scriptReply{Console: scriptConsole{Lines: []consoleLine{{Text: "The form did not arrive whole. Press again.", Class: "fail"}}}})
		return nil
	}
	return values
}

// scriptDeleteSubmit answers the Delete button (MON-AU-12): delete needs KAM's own confirmation
// first, and the page follows with a full reload, which closes the editor.
func (s *Server) scriptDeleteSubmit(w http.ResponseWriter, r *http.Request) {
	form := s.deleteForm(w, r)
	if form == nil {
		return
	}
	if s.kam == nil {
		s.writeScriptReply(w, r, scriptReply{Console: notConnectedConsole()})
		return
	}
	loaded := stateOf(form)
	if loaded.ID == "" || loaded.Revision == "0" {
		s.writeScriptReply(w, r, scriptReply{Console: unsavedConsole()})
		return
	}
	rev, revErr := strconv.ParseInt(loaded.Revision, 10, 64)
	if revErr != nil || rev < 1 {
		s.writeScriptReply(w, r, scriptReply{Console: revisionConsole()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), kamActionTimeout)
	defer cancel()
	if err := s.kam.DeleteScript(ctx, loaded.ID, rev); err != nil {
		s.writeScriptReply(w, r, scriptReply{Console: saveErrorConsole(err, false), List: true, SelectedID: loaded.ID})
		return
	}
	s.writeScriptReply(w, r, scriptReply{Console: scriptConsole{}, Redirect: "/automations"})
}
