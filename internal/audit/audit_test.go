package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAssignsSequenceAndRecentIsNewestFirst(t *testing.T) {
	l := NewMemory("")
	for i := 0; i < 3; i++ {
		if err := l.Append(Record{At: time.Unix(int64(i), 0), Actor: "admin", Action: "x", Outcome: OK}); err != nil {
			t.Fatal(err)
		}
	}
	if got := l.Total(); got != 3 {
		t.Fatalf("total = %d, want 3", got)
	}
	got := l.Recent(0)
	if len(got) != 3 || got[0].Seq != 3 || got[2].Seq != 1 {
		t.Fatalf("recent = %+v, want newest first 3,2,1", got)
	}
}

func TestOpenPersistsAndContinuesSequence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append(Record{Actor: "admin", Action: "user.create", Outcome: OK,
		Changes: map[string]Change{"name": {To: "alice"}}}); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	l2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	if got := l2.Total(); got != 1 {
		t.Fatalf("total after reopen = %d, want 1", got)
	}
	if err := l2.Append(Record{Actor: "admin", Action: "user.delete", Outcome: OK}); err != nil {
		t.Fatal(err)
	}
	if got := l2.Recent(0); got[0].Seq != 2 {
		t.Fatalf("seq after reopen = %d, want 2", got[0].Seq)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "\n"); n != 2 {
		t.Fatalf("lines = %d, want 2", n)
	}
}

func TestTruncatedLastLineIsSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	good := `{"seq":1,"at":"2026-11-26T10:00:00Z","actor":"admin","action":"a","outcome":"ok"}` + "\n"
	if err := os.WriteFile(path, []byte(good+`{"seq":2,`), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if got := l.Total(); got != 1 {
		t.Fatalf("total = %d, want 1 (damaged line skipped)", got)
	}
	if err := l.Append(Record{Actor: "admin", Action: "b", Outcome: OK}); err != nil {
		t.Fatal(err)
	}
	if got := l.Recent(0); got[0].Seq != 2 {
		t.Fatalf("seq = %d, want 2", got[0].Seq)
	}
}

func TestTailIsBounded(t *testing.T) {
	l := NewMemory("")
	l.maxTail = 3
	for i := 0; i < 10; i++ {
		if err := l.Append(Record{Action: "a", Outcome: OK}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(l.Recent(0)); got != 3 {
		t.Fatalf("tail = %d, want 3", got)
	}
	if got := l.Total(); got != 10 {
		t.Fatalf("total = %d, want 10", got)
	}
	if got := l.Recent(2); len(got) != 2 || got[0].Seq != 10 {
		t.Fatalf("recent(2) = %+v, want the two newest", got)
	}
}

func TestMemoryTrailWritesNoFile(t *testing.T) {
	l := NewMemory(DemoNote)
	if err := l.Append(Record{Action: "a", Outcome: OK}); err != nil {
		t.Fatal(err)
	}
	if got := l.Path(); got != "" {
		t.Fatalf("memory trail path = %q, want empty", got)
	}
	if got := l.Note(); got != DemoNote {
		t.Fatalf("note = %q, want the demo note", got)
	}
}

// A record's omitted field stays omitted on disk and in memory (MON-AT-4).
func TestChangeOmitsTheMissingSide(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append(Record{Actor: "admin", Action: "user.delete", Outcome: OK,
		Changes: map[string]Change{"name": {From: "alice"}}}); err != nil {
		t.Fatal(err)
	}
	l.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"to"`) {
		t.Errorf("removed value should carry no \"to\": %s", data)
	}
	if !strings.Contains(string(data), `"from":"alice"`) {
		t.Errorf("removed value should carry its \"from\": %s", data)
	}
}
