package main

import (
	"bytes"
	"runtime"
	"testing"
)

func TestVersionIsNonEmpty(t *testing.T) {
	if version == "" {
		t.Fatal("version must not be empty; a local build should report dev")
	}
}

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer
	if err := runVersion(&out); err != nil {
		t.Fatalf("version: %v", err)
	}
	want := "kammon " + version + " " + runtime.GOOS + "/" + runtime.GOARCH + "\n"
	if got := out.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}
