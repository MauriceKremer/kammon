package main

import (
	"fmt"
	"io"
	"runtime"
)

// version is the KAMmon release, set by the release build with
// -ldflags "-X main.version=<tag>". A plain local build reports "dev".
var version = "dev"

// runVersion implements "kammon -version" (KAMmon has no subcommands). It
// prints the version and the platform the binary was built for, so a
// downloaded artifact can say what it is (PLAN.md DST-2).
func runVersion(stdout io.Writer) error {
	_, err := fmt.Fprintf(stdout, "kammon %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
	return err
}
