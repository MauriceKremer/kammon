// Package web embeds the templates and static files into the binary.
package web

import "embed"

//go:embed templates static
var FS embed.FS
