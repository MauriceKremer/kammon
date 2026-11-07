// Script resource and script actions. The shapes follow KAM's own script
// routes (its internal/api/scripts.go, untouched by the contract, API-AU-1)
// and the phase-5 additions in docs/kam-api/FeatureKamApi.md §7.6 (D28: the
// script routes share the monitor listener, so one base URL serves all).
package kam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Script is one automation script as KAM serves it. CreatedAt and UpdatedAt are Unix seconds.
// Run is present only on a KAM with the phase-5 additions; it lives in KAM's memory.
// TimerIntervalMinutes describes a periodic script and is read-only here: the script's source
// decides it (KAM ignores what a write asks for), and 0 means the script has no timer.
type Script struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Source        string     `json:"source"`
	Enabled       bool       `json:"enabled"`
	TimeoutMS     int64      `json:"timeout_ms"`
	RuntimeStatus string     `json:"runtime_status"`
	LastError     string     `json:"last_error,omitempty"`
	TimerInterval int64      `json:"timer_interval_minutes"`
	Revision      int64      `json:"revision"`
	CreatedAt     int64      `json:"created_at"`
	UpdatedAt     int64      `json:"updated_at"`
	Run           *ScriptRun `json:"run,omitempty"`
}

// ScriptRun is how often a script ran, how long the last run took and how many replies it
// queued (API-AU-2). KAM resets it on restart; a missing run means "no run information".
type ScriptRun struct {
	Runs           uint64     `json:"runs"`
	Errors         uint64     `json:"errors"`
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	LastDurationMS int64      `json:"last_duration_ms"`
	LastReplies    int        `json:"last_replies"`
}

// The runtime states KAM reports (the words the pages show, MON-AU-2).
const (
	StatusREADY  = "READY"
	StatusBROKEN = "BROKEN"
	StatusFROZEN = "FROZEN"
)

// DryRunMessage is the made-up message a dry run runs against (§7.6). Only Channel is required;
// the rest are fixture values, not measurements, and KAM applies its own defaults.
type DryRunMessage struct {
	Channel          string   `json:"channel"`
	Sender           string   `json:"sender,omitempty"`
	Body             string   `json:"body,omitempty"`
	HopCount         int      `json:"hop_count,omitempty"`
	HopStations      []string `json:"hop_stations,omitempty"`
	TimestampUnix    *int64   `json:"timestamp_unix,omitempty"`
	HasRxMeta        bool     `json:"has_rx_meta,omitempty"`
	SNRQuarterDB     *int     `json:"snr_quarter_db,omitempty"`
	RSSIdBm          *int     `json:"rssi_dbm,omitempty"`
	TimeDriftSeconds *int     `json:"time_drift_seconds,omitempty"`
}

// DryRunRequest is the body of POST /scripts/dry-run (API-AU-3): unsaved editor text, a test
// timeout of its own (1-3000 ms; it never changes a live script's timeout) and the fixture.
type DryRunRequest struct {
	Source    string        `json:"source"`
	TimeoutMS int64         `json:"timeout_ms,omitempty"`
	Message   DryRunMessage `json:"message"`
}

// DryRunReply is one reply the dry run captured but did not send.
type DryRunReply struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

// DryRunResponse carries ok false for compile failures, script exceptions, timeouts and reply
// limits. HTTP 200 means the test ran and reported its outcome; "Nothing was sent." holds in
// every case (MON-AU-17).
type DryRunResponse struct {
	OK         bool          `json:"ok"`
	DurationMS int64         `json:"duration_ms"`
	Replies    []DryRunReply `json:"replies"`
	Error      string        `json:"error,omitempty"`
}

// StatusError is a non-2xx answer from KAM whose body text is meant for the console. The web
// layer shows this text (KAM's compile messages carry line and column, MON-AU-7) and picks its
// own words for the statuses it owns, such as 409 (MON-AU-9).
type StatusError struct {
	Status int
	Text   string
}

func (e *StatusError) Error() string { return e.Text }

// Admin is what a KAMmon request may call in KAM: script management, validate and the dry run.
// The poller never holds an Admin (MON-DS-1); admin actions are the calls from a request
// (AGENTS rule 3), and opening a script loads it directly with GET /scripts/{id} (MON-AU-6).
type Admin interface {
	Script(ctx context.Context, id string) (Script, error)
	ValidateScript(ctx context.Context, id, source string, timeoutMS int64) error
	CreateScript(ctx context.Context, script Script) (Script, error)
	UpdateScript(ctx context.Context, script Script, expectedRevision int64) (Script, error)
	DeleteScript(ctx context.Context, id string, revision int64) error
	EnableScript(ctx context.Context, id string, expectedRevision int64) (Script, error)
	DisableScript(ctx context.Context, id string, expectedRevision int64) (Script, error)
	RecoverScript(ctx context.Context, id string, timeoutMS, expectedRevision int64) (Script, error)
	DryRun(ctx context.Context, test DryRunRequest) (DryRunResponse, error)
	// ProbeDryRun reports whether KAM offers POST /scripts/dry-run. It sends an empty object:
	// a KAM with the route answers 400 or 422 (nothing is executed); an older one answers 404,
	// or 405 when its /scripts/{id} routes cover the path (MON-AU-20).
	ProbeDryRun(ctx context.Context) error
}

func (h *HTTP) Scripts(ctx context.Context) ([]Script, error) {
	var out []Script
	err := h.get(ctx, "/scripts", nil, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (h *HTTP) Script(ctx context.Context, id string) (Script, error) {
	var out Script
	err := h.get(ctx, "/scripts/"+url.PathEscape(id), nil, &out)
	return out, err
}

// scriptMutation is the wire shape KAM reads for script writes (its ScriptMutation).
type scriptMutation struct {
	Script           Script `json:"script"`
	ExpectedRevision int64  `json:"expected_revision"`
}

func (h *HTTP) ValidateScript(ctx context.Context, id, source string, timeoutMS int64) error {
	body := struct {
		ID        string `json:"id"`
		Source    string `json:"source"`
		TimeoutMS int64  `json:"timeout_ms"`
	}{ID: id, Source: source, TimeoutMS: timeoutMS}
	return h.request(ctx, http.MethodPost, "/scripts/validate", body, nil)
}

// CreateScript posts to /scripts; KAM ignores expected_revision on a create (revision 0 is what
// a create means) and answers 201. KAM's create takes the id from the body as well (its
// APIdetails example carries id and name): a script without an id goes out with the name as
// its id (D32).
func (h *HTTP) CreateScript(ctx context.Context, script Script) (Script, error) {
	if strings.TrimSpace(script.ID) == "" {
		script.ID = script.Name
	}
	return h.scriptRequest(ctx, http.MethodPost, "", scriptMutation{Script: script})
}

func (h *HTTP) UpdateScript(ctx context.Context, script Script, expectedRevision int64) (Script, error) {
	return h.scriptRequest(ctx, http.MethodPut, script.ID, scriptMutation{Script: script, ExpectedRevision: expectedRevision})
}

// scriptRequest sends a script write and decodes the saved script create, update, enable,
// disable and recover answer with.
func (h *HTTP) scriptRequest(ctx context.Context, method, id string, body scriptMutation) (Script, error) {
	path := "/scripts"
	if id != "" {
		path += "/" + url.PathEscape(id)
	}
	var out Script
	if err := h.request(ctx, method, path, body, &out); err != nil {
		return Script{}, err
	}
	return out, nil
}

func (h *HTTP) DeleteScript(ctx context.Context, id string, revision int64) error {
	return h.request(ctx, http.MethodDelete, "/scripts/"+url.PathEscape(id)+"?revision="+fmt.Sprint(revision), nil, nil)
}

// scriptAction posts the small body of enable, disable and recover.
func (h *HTTP) scriptAction(ctx context.Context, id, action string, body any) (Script, error) {
	var out Script
	err := h.request(ctx, http.MethodPost, "/scripts/"+url.PathEscape(id)+"/"+action, body, &out)
	return out, err
}

func (h *HTTP) EnableScript(ctx context.Context, id string, expectedRevision int64) (Script, error) {
	return h.scriptAction(ctx, id, "enable", struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}{ExpectedRevision: expectedRevision})
}

func (h *HTTP) DisableScript(ctx context.Context, id string, expectedRevision int64) (Script, error) {
	return h.scriptAction(ctx, id, "disable", struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}{ExpectedRevision: expectedRevision})
}

func (h *HTTP) RecoverScript(ctx context.Context, id string, timeoutMS, expectedRevision int64) (Script, error) {
	return h.scriptAction(ctx, id, "recover", struct {
		ExpectedRevision int64 `json:"expected_revision"`
		TimeoutMS        int64 `json:"timeout_ms"`
	}{ExpectedRevision: expectedRevision, TimeoutMS: timeoutMS})
}

func (h *HTTP) DryRun(ctx context.Context, test DryRunRequest) (DryRunResponse, error) {
	var out DryRunResponse
	err := h.request(ctx, http.MethodPost, "/scripts/dry-run", test, &out)
	return out, err
}

// ProbeDryRun sends an empty body: a KAM with the dry-run route cannot execute anything without
// a fixture and answers 400 or 422; a KAM without the route answers 404, or 405 when its
// /scripts/{id} routes cover the path (MON-AU-20). Only these two answers hide the button; any
// other error is a problem of its own and is returned as such, so the button never lies but a
// busy or disabled KAM still says so on the click.
func (h *HTTP) ProbeDryRun(ctx context.Context) error {
	err := h.request(ctx, http.MethodPost, "/scripts/dry-run", struct{}{}, nil)
	var se *StatusError
	if err == nil || (errors.As(err, &se) && se.Status != http.StatusNotFound && se.Status != http.StatusMethodNotAllowed) {
		return nil
	}
	return err
}

// request is the shared call for the script actions: a JSON body in, optional JSON out.
func (h *HTTP) request(ctx context.Context, method, path string, body, out any) error {
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("kam %s: %w", path, err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var failure struct {
			Error string `json:"error"`
		}
		text := fmt.Sprintf("status %d", resp.StatusCode)
		if json.Unmarshal(data, &failure) == nil && failure.Error != "" {
			text = failure.Error
		}
		return &StatusError{Status: resp.StatusCode, Text: text}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("kam %s: %w", path, err)
	}
	return nil
}
