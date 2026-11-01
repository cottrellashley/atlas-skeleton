// Adapted from github.com/cottrellashley/cloud-gateway-patterns-go at 44ba81a
// (patterns/telemetry/writer.go), MIT licence: see sdk/go/THIRD_PARTY_NOTICES.md.
// Changes: import paths; later edits are marked ATLAS.

package telemetry

import (
	"net/http"

	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/middleware"
)

func wrapWriter(w http.ResponseWriter) *middleware.ResponseWriter { return middleware.Wrap(w) }
