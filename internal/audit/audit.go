// Package audit keeps KAMmon's append-only trail of administrator changes and sign-in
// activity (docs/requirements/AuditTrail.md): one JSON Lines record per event, written to
// disk and read back read-only. It stores no passwords, password hashes or script source.
package audit

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"
)

// MaxTail is how many of the newest records the trail keeps in memory for the page. The file
// keeps every record; the memory copy is bounded (DataStore.md §3, MON-AT-11, MON-AT-20).
const MaxTail = 1000

// Outcome values (MON-AT-2).
const (
	OK     = "ok"
	Failed = "failed"
)

// DemoNote is shown when the trail is kept in memory because KAMmon runs in demo mode.
const DemoNote = "Demo mode: the audit trail is kept in memory and is lost on restart."

// UnopenedNote is shown when a persistent trail could not be opened.
const UnopenedNote = "The audit trail file could not be opened; records are kept in memory only."

// Change is one field's before and after value. From is absent for a new value and To for a
// removed one (MON-AT-4). Secrets are never stored here (MON-AT-5, MON-AT-6).
type Change struct {
	From any `json:"from,omitempty"`
	To   any `json:"to,omitempty"`
}

// Target names what an action acted on. A sign-in record has no target (MON-AT-25).
type Target struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// Record is one line of the trail (MON-AT-3).
type Record struct {
	Seq     uint64            `json:"seq"`
	At      time.Time         `json:"at"`
	Actor   string            `json:"actor"`
	Role    string            `json:"role"`
	Action  string            `json:"action"`
	Target  *Target           `json:"target,omitempty"`
	Changes map[string]Change `json:"changes,omitempty"`
	Outcome string            `json:"outcome"`
	Detail  string            `json:"detail"`
	Remote  string            `json:"remote,omitempty"`
}

// Log is the trail. A persistent Log appends to a file; a memory Log keeps records for the
// life of the process (demo mode, or a file that could not be opened).
type Log struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	note    string
	seq     uint64
	total   int
	tail    []Record
	maxTail int
}

// Open reads an existing trail at path and opens it for appending. A missing file starts an
// empty trail. A trailing line left half-written by an interrupted append is skipped and
// reported, never treated as a fatal error (MON-AT-11).
func Open(path string) (*Log, error) {
	l := &Log{maxTail: MaxTail, path: path}
	if err := l.load(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	l.file = f
	return l, nil
}

// NewMemory makes a trail that writes nothing to disk.
func NewMemory(note string) *Log {
	return &Log{note: note, maxTail: MaxTail}
}

// Note explains a memory-only trail to the page; it is empty for a persistent trail.
func (l *Log) Note() string { return l.note }

// Path returns the file path, or "" for a memory trail.
func (l *Log) Path() string { return l.path }

// load reads the file's records into the tail and takes the last sequence number and the
// total count. Lines that do not parse are skipped and reported.
func (l *Log) load(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	skipped := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			skipped++
			continue
		}
		l.total++
		if rec.Seq > l.seq {
			l.seq = rec.Seq
		}
		l.appendTail(rec)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if skipped > 0 {
		slog.Warn("audit trail: skipped damaged lines", "path", path, "lines", skipped)
	}
	return nil
}

// Append adds one record, assigning the next sequence number. For a persistent trail it writes
// the line and syncs it before returning, so a record is whole before the next action proceeds
// (MON-AT-9). A write error leaves the trail unchanged and is returned for the caller to report
// (MON-AT-19).
func (l *Log) Append(rec Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec.Seq = l.seq + 1
	if rec.At.IsZero() {
		rec.At = time.Now()
	}
	if l.file != nil {
		line, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		line = append(line, '\n')
		if _, err := l.file.Write(line); err != nil {
			return err
		}
		if err := l.file.Sync(); err != nil {
			return err
		}
	}
	l.seq = rec.Seq
	l.total++
	l.appendTail(rec)
	return nil
}

// appendTail keeps the newest maxTail records, so memory stays bounded (MON-AT-20).
func (l *Log) appendTail(rec Record) {
	if len(l.tail) == l.maxTail {
		copy(l.tail, l.tail[1:])
		l.tail[len(l.tail)-1] = rec
		return
	}
	l.tail = append(l.tail, rec)
}

// Recent returns up to limit of the newest records, newest first. A limit of zero or less
// means every record held in memory.
func (l *Log) Recent(limit int) []Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(l.tail)
	if limit > 0 && limit < n {
		n = limit
	}
	out := make([]Record, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, l.tail[len(l.tail)-1-i])
	}
	return out
}

// Total is the number of records the trail holds, including those no longer in memory.
func (l *Log) Total() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.total
}

// Close closes the file of a persistent trail.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}
