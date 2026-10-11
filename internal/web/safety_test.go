package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Radio text is untrusted (AGENTS.md rule 5): template.HTML may only appear in chart.go, which builds markup from numbers.
func TestTemplateHTMLOnlyInTheChart(t *testing.T) {
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if filepath.Base(path) == "chart.go" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), "template.HTML") || strings.Contains(string(data), "template.JS") || strings.Contains(string(data), "template.URL") {
			t.Errorf("%s uses an unescaped template type; only chart.go may", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
