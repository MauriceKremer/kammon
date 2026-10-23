package kam

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The script fixtures are the contract in machine form (docs/kam-api/FeatureKamApi.md §7.6):
// scripts.json is the wire KAM serves today (API-AU-1), scripts-run.json adds the phase-5 run
// object (API-AU-2), script-dry-run.json the dry-run answer (API-AU-3).
func TestScriptFixturesDecodeStrictly(t *testing.T) {
	var scripts []Script
	decodeStrict(t, "scripts.json", &scripts)
	if len(scripts) != 3 || scripts[0].Name != "echo.star" || scripts[2].RuntimeStatus != StatusBROKEN || scripts[2].LastError == "" {
		t.Fatalf("scripts = %+v", scripts)
	}
	for _, script := range scripts {
		if script.Run != nil {
			t.Fatalf("today's list carries no run object: %+v", script)
		}
	}

	var withRun []Script
	decodeStrict(t, "scripts-run.json", &withRun)
	if withRun[0].Run == nil || withRun[0].Run.Runs != 12 || withRun[0].Run.LastReplies != 1 || withRun[1].Run != nil {
		t.Fatalf("run objects = %+v", withRun)
	}

	var dryRun DryRunResponse
	decodeStrict(t, "script-dry-run.json", &dryRun)
	if !dryRun.OK || dryRun.DurationMS != 1 || len(dryRun.Replies) != 1 || dryRun.Replies[0].Channel != "#emmen" {
		t.Fatalf("dry run = %+v", dryRun)
	}
}

func TestHTTPReaderLoadsScriptsList(t *testing.T) {
	mux := http.NewServeMux()
	// KAM's /scripts answers a bare JSON array, no envelope (its internal/api/scripts.go).
	mux.HandleFunc("GET /scripts", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"echo","name":"echo.star","source":"","enabled":true,"timeout_ms":500,
			"runtime_status":"READY","revision":4,"created_at":1760700000,"updated_at":1760882400,
			"run":{"runs":12,"errors":0,"last_run_at":"2026-10-23T14:59:48.000Z","last_duration_ms":2,"last_replies":1}}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	scripts, err := NewHTTP(srv.URL, time.Second).Scripts(context.Background())
	if err != nil || len(scripts) != 1 || scripts[0].ID != "echo" {
		t.Fatalf("scripts = %+v, %v", scripts, err)
	}
	if scripts[0].Run == nil || scripts[0].Run.LastRunAt == nil {
		t.Fatalf("run = %+v", scripts[0].Run)
	}
}

func TestHTTPScriptActionsSendAndMap(t *testing.T) {
	var body map[string]any
	mux := http.NewServeMux()
	check := func(w http.ResponseWriter, r *http.Request, wantPath string) {
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		body = map[string]any{}
		_ = json.Unmarshal(data, &body)
	}
	mux.HandleFunc("GET /scripts/echo", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"echo","name":"echo.star","source":"x","enabled":true,"timeout_ms":500,"runtime_status":"READY","revision":7,"created_at":1,"updated_at":1}`))
	})
	mux.HandleFunc("POST /scripts/validate", func(w http.ResponseWriter, r *http.Request) {
		check(w, r, "/scripts/validate")
		if body["source"] != "def on_group_text(message):\n    pass" || body["id"] == "" || body["timeout_ms"] != float64(300) {
			t.Errorf("validate body = %v", body)
		}
		_, _ = w.Write([]byte(`{"valid":true}`))
	})
	mux.HandleFunc("POST /scripts", func(w http.ResponseWriter, r *http.Request) {
		check(w, r, "/scripts")
		if script, ok := body["script"].(map[string]any); !ok || script["name"] != "new.star" || script["id"] != "new.star" {
			t.Errorf("create body = %v", body)
		}
		if body["expected_revision"] != float64(0) {
			t.Errorf("create expected_revision = %v", body["expected_revision"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"new","name":"new.star","enabled":false,"timeout_ms":500,"runtime_status":"READY","revision":1}`))
	})
	mux.HandleFunc("PUT /scripts/echo", func(w http.ResponseWriter, r *http.Request) {
		check(w, r, "/scripts/echo")
		if body["expected_revision"] != float64(7) {
			t.Errorf("update expected_revision = %v", body["expected_revision"])
		}
		_, _ = w.Write([]byte(`{"id":"echo","name":"echo.star","enabled":true,"timeout_ms":500,"runtime_status":"READY","revision":8}`))
	})
	mux.HandleFunc("DELETE /scripts/echo", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("revision") != "8" {
			t.Errorf("delete revision query = %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /scripts/echo/enable", func(w http.ResponseWriter, r *http.Request) {
		check(w, r, "/scripts/echo/enable")
		if body["expected_revision"] != float64(7) {
			t.Errorf("enable expected_revision = %v", body["expected_revision"])
		}
		_, _ = w.Write([]byte(`{"id":"echo","enabled":true,"revision":8}`))
	})
	mux.HandleFunc("POST /scripts/stale/disable", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"revision is stale"}`, http.StatusConflict)
	})
	mux.HandleFunc("POST /scripts/broken/recover", func(w http.ResponseWriter, r *http.Request) {
		check(w, r, "/scripts/broken/recover")
		if body["timeout_ms"] != float64(600) {
			t.Errorf("recover timeout = %v", body["timeout_ms"])
		}
		_, _ = w.Write([]byte(`{"id":"broken","runtime_status":"READY","revision":4}`))
	})
	mux.HandleFunc("GET /scripts/missing", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"no such script"}`, http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewHTTP(srv.URL, time.Second)
	ctx := context.Background()

	if script, err := client.Script(ctx, "echo"); err != nil || script.Revision != 7 {
		t.Fatalf("script = %+v, %v", script, err)
	}
	if err := client.ValidateScript(ctx, "echo", "def on_group_text(message):\n    pass", 300); err != nil {
		t.Fatalf("validate: %v", err)
	}
	created, err := client.CreateScript(ctx, Script{Name: "new.star", TimeoutMS: 500})
	if err != nil || created.Revision != 1 {
		t.Fatalf("create = %+v, %v", created, err)
	}
	updated, err := client.UpdateScript(ctx, Script{ID: "echo", Name: "echo.star", Enabled: true, TimeoutMS: 500}, 7)
	if err != nil || updated.Revision != 8 {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	if err := client.DeleteScript(ctx, "echo", 8); err != nil {
		t.Fatalf("delete: %v", err)
	}
	enabled, err := client.EnableScript(ctx, "echo", 7)
	if err != nil || !enabled.Enabled {
		t.Fatalf("enable = %+v, %v", enabled, err)
	}
	_, err = client.DisableScript(ctx, "stale", 3)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusConflict || se.Text != "revision is stale" {
		t.Fatalf("stale disable = %v", err)
	}
	recovered, err := client.RecoverScript(ctx, "broken", 600, 3)
	if err != nil || recovered.RuntimeStatus != "READY" {
		t.Fatalf("recover = %+v, %v", recovered, err)
	}
	if _, err := client.Script(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing script = %v", err)
	}
}

func TestHTTPDryRunAndProbe(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /scripts/dry-run", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %s", r.Header.Get("Content-Type"))
		}
		_, _ = w.Write([]byte(`{"ok":false,"duration_ms":1,"replies":[],"error":"Timeout after 500 ms"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := NewHTTP(srv.URL, time.Second)

	// ok:false is a completed test that reported a failure: HTTP 200, not a client error (§7.6.1).
	test, err := client.DryRun(context.Background(), DryRunRequest{
		Source:    "def on_group_text(message):\n    reply(message[\"channel\"], \"pong\")",
		TimeoutMS: 500,
		Message:   DryRunMessage{Channel: "#emmen", Body: "!ping"},
	})
	if err != nil || test.OK || test.DurationMS != 1 || test.Error != "Timeout after 500 ms" || len(test.Replies) != 0 {
		t.Fatalf("dry run = %+v, %v", test, err)
	}
	if err := client.ProbeDryRun(context.Background()); err != nil {
		t.Fatalf("probe against a KAM with the route: %v", err)
	}
}

func TestProbeDryRunDetectsAnOlderKAM(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if err := NewHTTP(srv.URL, time.Second).ProbeDryRun(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("probe on an older KAM = %v, want ErrNotFound", err)
	}
}
