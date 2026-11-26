package web

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"kammon.bytecoder.nl/internal/audit"
)

// auditPageSize is how many records the page opens with; "Show more" doubles it up to the
// bounded tail (MON-AT-16).
const auditPageSize = 100

type auditChangeRow struct {
	Field string
	Text  string
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
func (s *Server) auditView(limit int) auditData {
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
	for _, r := range recs {
		data.Records = append(data.Records, auditRowFor(r))
	}
	return data
}

func auditRowFor(r audit.Record) auditRow {
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
		row.Changes = append(row.Changes, changeRowFor(f, r.Changes[f]))
	}
	return row
}

func changeRowFor(field string, c audit.Change) auditChangeRow {
	from, to := auditValue(c.From), auditValue(c.To)
	text := ""
	switch {
	case from != "" && to != "":
		text = from + " → " + to
	case to != "":
		text = to
	case from != "":
		text = from + " → (removed)"
	}
	return auditChangeRow{Field: field, Text: text}
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
	data := s.auditView(auditLimit(r))
	s.render(w, r, http.StatusOK, "audit", s.pageFor(r, "audit", "Audit trail", "Who changed what, and when", data))
}

// auditPartial answers "Show more" with the next page of records (MON-AT-16).
func (s *Server) auditPartial(w http.ResponseWriter, r *http.Request) {
	s.execute(w, http.StatusOK, s.pages["audit"], "audit-table", s.auditView(auditLimit(r)))
}
