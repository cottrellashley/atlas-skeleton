// Package problem writes Atlas problem documents over HTTP (§4.1, §9.3.3). It
// is the only way the gateway and the kit answer a failure: one shape, the
// code in the body and in Atlas-Error-Code, no internal detail in the text.
package problem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"atlas.local/sdk/go/protocol"
)

// StatusClientClosedRequest is what the access log records for a caller that
// went away; nothing is sent, because nobody is listening.
const StatusClientClosedRequest = 499

// From converts any error into a problem. A *protocol.Problem passes through;
// a deadline or an oversized body maps to its registered code; anything else
// becomes internal, so that no detail of the failure leaks to the caller.
func From(err error) *protocol.Problem {
	if err == nil {
		return nil
	}
	var p *protocol.Problem
	if errors.As(err, &p) {
		return p
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return protocol.NewProblem(protocol.CodeDeadlineExceeded, "the call did not complete before its deadline")
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("the request body is larger than %d bytes", mbe.Limit))
	}
	return protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
}

// Write sends err as a problem document, with Atlas-Error-Code and, where the
// problem gives one, Retry-After.
func Write(w http.ResponseWriter, requestID string, err error) {
	p := From(err)
	if p == nil {
		return
	}
	out := *p
	if out.RequestID == "" {
		out.RequestID = requestID
	}
	h := w.Header()
	h.Set("Content-Type", protocol.MediaProblem)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set(protocol.HeaderErrorCode, out.Code)
	if out.RetryAfter > 0 {
		h.Set("Retry-After", strconv.Itoa(out.RetryAfter))
	}
	w.WriteHeader(out.Status)
	_ = json.NewEncoder(w).Encode(&out)
}
