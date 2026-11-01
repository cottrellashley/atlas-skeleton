// Package portfolio is the portfolio domain's capabilities: holdings, the
// list of portfolios, the search that finds a portfolio's code, and its
// valuation dates. Each is a profile and a view model over Harbour's
// views; the domain has no code of its own. history/ keeps every profile a
// served one replaced within its version, so a request written under it is
// still resolved forward, and the gate classifies each change against it.
package portfolio

import (
	"embed"

	"github.com/jhgonline/fi-app-atlas/gateway/views"
)

//go:embed profiles views history
var files embed.FS

// Capabilities are the domain's capabilities, as their owners wrote them.
func Capabilities() ([]views.Files, error) { return views.Read(files) }
