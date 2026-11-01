package edge

import (
	"net/http"
	"strings"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// The headers a browser page may send, and read, across origins: the
// protocol's own (§9.6.3).
var (
	corsAllow = strings.Join([]string{
		protocol.HeaderAuthorization, protocol.HeaderContentType, protocol.HeaderAccept,
		protocol.HeaderClient, protocol.HeaderClientFeatures, protocol.HeaderProfileExpect,
		protocol.HeaderDeadline, protocol.HeaderIdempotencyKey, protocol.HeaderIfMatch,
		protocol.HeaderIfNoneMatch, protocol.HeaderExecution, protocol.HeaderInteractive,
		protocol.HeaderConfirm, protocol.HeaderTraceparent, protocol.HeaderTracestate,
	}, ", ")
	corsExpose = strings.Join([]string{
		protocol.HeaderRequestID, protocol.HeaderProfile, protocol.HeaderGovernanceRevision,
		protocol.HeaderLimitRemaining, protocol.HeaderErrorCode, protocol.HeaderDeprecation,
		protocol.HeaderSunset, protocol.HeaderRetryAfter, protocol.HeaderOperationLocation,
		protocol.HeaderETag, protocol.HeaderTraceparent, protocol.HeaderTracestate,
	}, ", ")
)

// cors lets a page on one of the deployment's named origins call the
// gateway, and no other (§6.2.9): never a wildcard, never credentials, as
// the token travels in Authorization and never in a cookie. A preflight
// from a named origin is answered here; one from any other origin is a
// request like any other that is not a POST: not_found.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Add("Vary", "Origin")
		if !s.origins[strings.ToLower(origin)] {
			next.ServeHTTP(w, r)
			return
		}
		h.Set("Access-Control-Allow-Origin", origin)
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			h.Set("Access-Control-Allow-Methods", "POST, GET")
			h.Set("Access-Control-Allow-Headers", corsAllow)
			h.Set("Access-Control-Max-Age", "600")
			h.Set(protocol.HeaderCacheControl, protocol.NoStore)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.Set("Access-Control-Expose-Headers", corsExpose)
		next.ServeHTTP(w, r)
	})
}
