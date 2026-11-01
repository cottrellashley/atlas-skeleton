// Package protocol is the Go implementation of what crosses the wire between
// an Atlas client and the Atlas Gateway (the spec's §9).
//
// This skeleton speaks a provisional version, atlas/0.1. It keeps the
// invariants the architecture rests on: every answer names the exact profile
// it was written under, clients learn everything from what the gateway
// serves, readers resolve what a writer sent to the profile they know, and
// every failure is one problem document. It does not yet implement §9's
// exact documents, grammars or algorithms. Those are designed after the
// proofs, from what the proofs turned out to need.
package protocol

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Version is the protocol version this package speaks.
const Version = "atlas/0.1"

// Headers a call or an answer may carry (§4.1, §9.6.3).
const (
	HeaderClient         = "Atlas-Client"
	HeaderClientFeatures = "Atlas-Client-Features"
	HeaderProfileExpect  = "Atlas-Profile-Expect"
	HeaderProfile        = "Atlas-Profile"
	HeaderDeadline       = "Atlas-Deadline"
	HeaderExecution      = "Atlas-Execution"
	HeaderErrorCode      = "Atlas-Error-Code"
	HeaderRequestID      = "X-Request-Id"
)

// Media types (§9.1.7).
const (
	MediaJSON        = "application/json"
	MediaProblem     = "application/problem+json"
	MediaNDJSON      = "application/x-ndjson"
	MediaArrowStream = "application/vnd.apache.arrow.stream"
	MediaHandoff     = "application/vnd.atlas.handoff+json"
)

// Executions of a data result (§2.2): who does the work and moves the bytes.
const (
	ExecutionGateway = "gateway"
	ExecutionHandoff = "handoff"
)

// Levels a client may declare (§9.C). Broker is deferred and not offered.
const (
	LevelCore      = "core"
	LevelStreaming = "streaming"
	LevelColumnar  = "columnar"
	LevelHandoff   = "handoff"
)

// Features is what a client declares it implements, sent as
// Atlas-Client-Features so the gateway serves it the next way down where it
// lacks something (§3.3.11).
type Features struct {
	Levels  []string // core, streaming, columnar, handoff
	Handoff []string // chunk formats it decodes, such as snowflake-arrow/1
}

// Has reports whether the features include the level.
func (f Features) Has(level string) bool { return contains(f.Levels, level) }

// Decodes reports whether the client decodes the handoff format.
func (f Features) Decodes(format string) bool { return contains(f.Handoff, format) }

// String renders the features as a Structured Field dictionary of inner
// lists: levels=(core columnar), handoff=(snowflake-arrow/1).
func (f Features) String() string {
	var parts []string
	if len(f.Levels) > 0 {
		parts = append(parts, "levels=("+strings.Join(f.Levels, " ")+")")
	}
	if len(f.Handoff) > 0 {
		parts = append(parts, "handoff=("+strings.Join(f.Handoff, " ")+")")
	}
	return strings.Join(parts, ", ")
}

// ParseFeatures reads Atlas-Client-Features. Members it does not know are
// ignored, as the tolerant reader requires (§9.7.10); a client that declares
// nothing is served at Core.
func ParseFeatures(s string) Features {
	var f Features
	for _, member := range strings.Split(s, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(member), "=")
		if !ok {
			continue
		}
		value = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(value), "("), ")")
		items := strings.Fields(value)
		switch strings.TrimSpace(key) {
		case "levels":
			f.Levels = items
		case "handoff":
			f.Handoff = items
		}
	}
	if !f.Has(LevelCore) {
		f.Levels = append([]string{LevelCore}, f.Levels...)
	}
	return f
}

// FormatDeadline writes an absolute deadline as an RFC 9651 Date: @seconds.
func FormatDeadline(t time.Time) string { return "@" + strconv.FormatInt(t.Unix(), 10) }

// ParseDeadline reads an RFC 9651 Date.
func ParseDeadline(s string) (time.Time, error) {
	v, ok := strings.CutPrefix(strings.TrimSpace(s), "@")
	if !ok {
		return time.Time{}, fmt.Errorf("deadline %q is not an RFC 9651 date (@seconds)", s)
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("deadline %q is not an RFC 9651 date (@seconds)", s)
	}
	return time.Unix(n, 0), nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
