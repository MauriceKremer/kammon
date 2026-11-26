package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"kammon.bytecoder.nl/internal/audit"
	"kammon.bytecoder.nl/internal/kam"
)

const (
	// auditPageSize is how many records the page opens with; "Show more" doubles it up to the
	// bounded tail (MON-AT-16).
	auditPageSize = 100
	// maxJournalLookups bounds the per-page KAM journal checks (MON-AT-29).
	maxJournalLookups = 16
	// journalTimeout bounds one journal read made from an audit page request.
	journalTimeout = 2 * time.Second
)

type auditPart struct {
	Text string
	Link string
}

type auditChangeRow struct {
	Field string
	Parts []auditPart
}

type auditRow struct {
	Seq     uint64
	At      string
	Actor   string
	Role    string
	Action  string
	Target  string
	Changes []auditChangeRow
	Outcome string
	Failed  bool
	Detail  string
	Remote  string
}

type auditData struct {
	Records   []auditRow
	Shown     int
	Total     int
	NextLimit int
	Note      string
	Capped    bool
}

// auditVersionData is one superseded revision opened from the trail (MON-AT-27).
type auditVersionData struct {
	Error       string
	Found       bool
	ScriptID    string
	Fingerprint string
	Name        string
	Revision    int64
	ArchivedAt  string
	Enabled     bool
	TimeoutMS   int64
	Source      string
}

// auditLimit reads ?limit= into a bounded page size.
func auditLimit(r *http.Request) int {
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return auditPageSize
}

// auditView builds the page from the trail's bounded tail only, never the file (MON-AT-16, 20).
func (s *Server) auditView(ctx context.Context, limit int) auditData {
	data := auditData{Note: s.auditLog.Note(), Total: s.auditLog.Total()}
	if limit <= 0 {
		limit = auditPageSize
	}
	if limit > audit.MaxTail {
		limit = audit.MaxTail
	}
	recs := s.auditLog.Recent(limit)
	data.Shown = len(recs)
	data.Capped = data.Total > audit.MaxTail
	if data.Shown < data.Total && data.Shown < audit.MaxTail {
		next := data.Shown * 2
		if next <= data.Shown {
			next = data.Shown + 1
		}
		if next > audit.MaxTail {
			next = audit.MaxTail
		}
		data.NextLimit = next
	}
	// Ask KAM's journal once per script that has a source fingerprint on this page (MON-AT-26).
	known := s.knownFingerprints(ctx, sourceScriptIDs(recs))
	for _, r := range recs {
		data.Records = append(data.Records, auditRowFor(r, known))
	}
	return data
}

// sourceScriptIDs lists the distinct script ids whose source change carries a fingerprint.
func sourceScriptIDs(recs []audit.Record) []string {
	seen := map[string]bool{}
	var ids []string
	for _, r := range recs {
		if r.Target == nil || r.Target.Type != "script" || r.Target.ID == "" {
			continue
		}
		change, ok := r.Changes["source"]
		if !ok {
			continue
		}
		if !isFingerprint(auditValue(change.From)) && !isFingerprint(auditValue(change.To)) {
			continue
		}
		if !seen[r.Target.ID] {
			seen[r.Target.ID] = true
			ids = append(ids, r.Target.ID)
		}
	}
	return ids
}

// knownFingerprints asks KAM for each script's journalled fingerprints, bounded and read once per
// page. A KAM that is down or has automations disabled stops the lookups; the trail still renders
// without links (MON-AT-26, 28, 29).
func (s *Server) knownFingerprints(ctx context.Context, ids []string) map[string]map[string]bool {
	known := map[string]map[string]bool{}
	if s.kam == nil {
		return known
	}
	for i, id := range ids {
		if i >= maxJournalLookups {
			break
		}
		cctx, cancel := context.WithTimeout(ctx, journalTimeout)
		fps, err := s.kam.Journal(cctx, id)
		cancel()
		if err != nil {
			break
		}
		set := make(map[string]bool, len(fps))
		for _, fp := range fps {
			set[fp] = true
		}
		known[id] = set
	}
	return known
}

func auditRowFor(r audit.Record, known map[string]map[string]bool) auditRow {
	scriptID := ""
	if r.Target != nil && r.Target.Type == "script" {
		scriptID = r.Target.ID
	}
	row := auditRow{
		Seq: r.Seq, At: r.At.Local().Format("2006-01-02 15:04:05"),
		Actor: r.Actor, Role: r.Role, Action: r.Action,
		Target: targetWords(r.Target), Outcome: r.Outcome,
		Failed: r.Outcome == audit.Failed, Detail: r.Detail, Remote: r.Remote,
	}
	fields := make([]string, 0, len(r.Changes))
	for f := range r.Changes {
		fields = append(fields, f)
	}
	slices.Sort(fields)
	for _, f := range fields {
		row.Changes = append(row.Changes, changeRowFor(f, r.Changes[f], scriptID, known))
	}
	return row
}

func changeRowFor(field string, c audit.Change, scriptID string, known map[string]map[string]bool) auditChangeRow {
	from, to := auditValue(c.From), auditValue(c.To)
	row := auditChangeRow{Field: field}
	// A source change stores KAM's journal fingerprints; a known one becomes a link to the
	// journalled revision (MON-AT-26).
	if field == "source" && (isFingerprint(from) || isFingerprint(to)) {
		if from != "" {
			row.Parts = append(row.Parts, auditPart{Text: from, Link: versionLink(scriptID, from, known)})
		}
		if from != "" && to != "" {
			row.Parts = append(row.Parts, auditPart{Text: " → "})
		}
		if to != "" {
			row.Parts = append(row.Parts, auditPart{Text: to, Link: versionLink(scriptID, to, known)})
		}
		return row
	}
	text := ""
	switch {
	case from != "" && to != "":
		text = from + " → " + to
	case to != "":
		text = to
	case from != "":
		text = from + " → (removed)"
	}
	row.Parts = []auditPart{{Text: text}}
	return row
}

func versionLink(scriptID, fingerprint string, known map[string]map[string]bool) string {
	if scriptID == "" || !known[scriptID][fingerprint] {
		return ""
	}
	return "/audit/version?script=" + url.QueryEscape(scriptID) + "&fingerprint=" + url.QueryEscape(fingerprint)
}

// isFingerprint reports whether s is one of KAM's 16-character lowercase hex source ids.
func isFingerprint(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// auditValue words one stored value for the page. A number may arrive as int64 from a fresh
// record or as float64 after a reload, so both are handled.
func auditValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}

func targetWords(t *audit.Target) string {
	if t == nil {
		return ""
	}
	label := t.Name
	if label == "" {
		label = t.ID
	}
	switch {
	case t.Type == "script" && t.ID != "" && t.Name != "" && t.ID != t.Name:
		return "script " + t.Name + " (" + t.ID + ")"
	case t.Type != "" && label != "":
		return t.Type + " " + label
	case label != "":
		return label
	default:
		return t.Type
	}
}

// auditPage shows the trail, newest first, read-only (MON-AT-14, 15).
func (s *Server) auditPage(w http.ResponseWriter, r *http.Request) {
	data := s.auditView(r.Context(), auditLimit(r))
	s.render(w, r, http.StatusOK, "audit", s.pageFor(r, "audit", "Audit trail", "Who changed what, and when", data))
}

// auditPartial answers "Show more" with the next page of records (MON-AT-16).
func (s *Server) auditPartial(w http.ResponseWriter, r *http.Request) {
	s.execute(w, http.StatusOK, s.pages["audit"], "audit-table", s.auditView(r.Context(), auditLimit(r)))
}

// auditVersionPage opens one superseded revision from KAM's journal, read-only (MON-AT-27, 28).
func (s *Server) auditVersionPage(w http.ResponseWriter, r *http.Request) {
	scriptID := r.URL.Query().Get("script")
	fingerprint := r.URL.Query().Get("fingerprint")
	data := auditVersionData{ScriptID: scriptID, Fingerprint: fingerprint}
	switch {
	case s.kam == nil:
		data.Error = "KAMmon is not connected to KAM."
	case scriptID == "" || fingerprint == "":
		data.Error = "Open a revision from the audit trail."
	default:
		ctx, cancel := context.WithTimeout(r.Context(), kamActionTimeout)
		defer cancel()
		version, err := s.kam.JournalVersion(ctx, scriptID, fingerprint)
		var se *kam.StatusError
		switch {
		case err == nil:
			data.Found = true
			data.Name = version.Name
			data.Revision = version.Revision
			data.ArchivedAt = time.Unix(version.ArchivedAt, 0).Local().Format("2006-01-02 15:04:05")
			data.Enabled = version.Enabled
			data.TimeoutMS = version.TimeoutMS
			data.Source = version.Source
		case errors.Is(err, kam.ErrNotFound):
			data.Error = "KAM's journal has no revision with this fingerprint. It may be the current revision, or older than the journal."
		case errors.As(err, &se) && se.Status == http.StatusServiceUnavailable:
			data.Error = "Automations are disabled on KAM."
		default:
			data.Error = "KAMmon could not read KAM's journal."
		}
	}
	s.render(w, r, http.StatusOK, "audit-version", s.pageFor(r, "audit", "Journalled revision", "A superseded version from KAM's journal", data))
}
