package main

import (
	"path/filepath"
	"testing"

	"kammon.bytecoder.nl/internal/audit"
	"kammon.bytecoder.nl/internal/config"
)

// TestOpenAuditDemoIsMemoryOnly checks MON-AT-12: demo mode keeps the trail in memory.
func TestOpenAuditDemoIsMemoryOnly(t *testing.T) {
	l := openAudit(config.Demo(), true)
	if l.Path() != "" || l.Note() != audit.DemoNote {
		t.Fatalf("demo audit = path %q, note %q", l.Path(), l.Note())
	}
}

// TestOpenAuditPersists checks the real path is opened append-only.
func TestOpenAuditPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kammon.audit.jsonl")
	cfg := config.Demo()
	cfg.AuditFile = path
	l := openAudit(cfg, false)
	defer l.Close()
	if l.Path() != path || l.Note() != "" {
		t.Fatalf("persistent audit = path %q, note %q", l.Path(), l.Note())
	}
}

// TestOpenAuditFallsBackToMemory checks MON-AT-19's startup case: a file that cannot be opened
// leaves the app running with an in-memory trail and an honest note.
func TestOpenAuditFallsBackToMemory(t *testing.T) {
	cfg := config.Demo()
	cfg.AuditFile = filepath.Join(t.TempDir(), "missing-dir", "kammon.audit.jsonl")
	l := openAudit(cfg, false)
	if l.Path() != "" || l.Note() != audit.UnopenedNote {
		t.Fatalf("fallback audit = path %q, note %q", l.Path(), l.Note())
	}
}
