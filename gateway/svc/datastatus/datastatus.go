// Package datastatus is data.status: how fresh each source is, from the
// platform's load register. One profile and one view model, and the
// profiles it replaced (history/).
package datastatus

import (
	"embed"

	"atlas.local/gateway/views"
)

//go:embed profiles views history
var files embed.FS

// Capabilities are the domain's capabilities.
func Capabilities() ([]views.Files, error) { return views.Read(files) }
