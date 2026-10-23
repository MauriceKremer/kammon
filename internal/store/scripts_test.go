package store

import (
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/kam"
)

var ts = time.Date(2026, 10, 23, 15, 0, 0, 0, time.UTC)

func TestSetScriptsStripsSourceAndKeepsOrder(t *testing.T) {
	st := New()
	list := []kam.Script{
		{ID: "echo", Name: "echo.star", Source: "def on_group_text(message):\n    pass", Enabled: true, Revision: 4},
		{ID: "status", Name: "status.star", Revision: 1},
	}
	st.SetScripts(list, t0)
	got, truncated := st.Scripts()
	if truncated != 0 || len(got) != 2 || got[0].Name != "echo.star" {
		t.Fatalf("scripts = %+v, %d", got, truncated)
	}
	for _, script := range got {
		if script.Source != "" {
			t.Fatalf("the stored list holds a source: %+v", script)
		}
	}
	// The store only copies: editing the returned slice must not change the snapshot.
	got[0].Name = "changed"
	again, _ := st.Scripts()
	if again[0].Name != "echo.star" {
		t.Fatalf("Scripts leaked the snapshot: %+v", again[0])
	}
}

func TestScriptListTruncatesAtTheHardLimit(t *testing.T) {
	st := New()
	list := make([]kam.Script, MaxScripts+7)
	for i := range list {
		list[i] = kam.Script{ID: "s", Name: "script.star", Revision: int64(i)}
	}
	st.SetScripts(list, t0)
	got, truncated := st.Scripts()
	if len(got) != MaxScripts || truncated != 7 {
		t.Fatalf("len = %d truncated = %d, want %d and 7", len(got), truncated, MaxScripts)
	}
}

func TestScriptSnapshotFreshness(t *testing.T) {
	st := New()
	if f, last := st.SnapshotFreshness("scripts", t0); f != Down || !last.IsZero() {
		t.Fatalf("no snapshot yet: %v %v", f, last)
	}
	st.SetScripts([]kam.Script{{ID: "echo"}}, t0.Add(-30*time.Second))
	if f, last := st.SnapshotFreshness("scripts", t0); f != Stale || last.IsZero() {
		t.Fatalf("a 30 s old list is stale, got %v", f)
	}
	st.SetScripts(nil, t0)
	if f, _ := st.SnapshotFreshness("scripts", t0); f != Fresh {
		t.Fatalf("fresh list: %v", f)
	}
	// The other snapshots' freshness stays untouched by script reads (behaviour table §6).
	if f, _ := st.SnapshotFreshness("channels", t0); f != Down {
		t.Fatalf("channels must stay down, got %v", f)
	}
}
