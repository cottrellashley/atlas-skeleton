// Includes MIT-licensed serving utilities; see sdk/go/THIRD_PARTY_NOTICES.md.
// Changes: import paths; later edits are marked ATLAS.

package telemetry

import (
	"net/http"

	"atlas.local/sdk/go/kit/middleware"
)

func wrapWriter(w http.ResponseWriter) *middleware.ResponseWriter { return middleware.Wrap(w) }
