package web

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestDebugTrafficBody(t *testing.T) {
	h := newHarness(t)
	c := h.login(t, "viewer", "viewer")
	resp, _ := c.Get(h.ts.URL + "/traffic")
	buf, _ := io.ReadAll(resp.Body)
	os.WriteFile("/tmp/traffic.html", buf, 0o644)
	t.Logf("has note: %v", strings.Contains(string(buf), "History starts"))
}
