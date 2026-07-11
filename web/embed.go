// Package web bundles Greenlight's server-rendered templates and static assets
// into the binary via go:embed.
package web

import "embed"

// Templates holds the HTML templates under templates/.
//
//go:embed templates/*.html
var Templates embed.FS

// Static holds CSS/JS/icons served under /static/.
//
//go:embed static
var Static embed.FS
