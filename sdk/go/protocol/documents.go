package protocol

import (
	"encoding/json"
	"time"
)

// Bootstrap is what /.well-known/atlas answers: the only address a client
// knows in advance, and everything it needs to find the rest (§2.6).
type Bootstrap struct {
	Protocol    string             `json:"protocol"`
	Environment string             `json:"environment"`
	Servers     map[string]Server  `json:"servers"`
	Builtins    map[string]Binding `json:"builtins"`
	SignIn      SignIn             `json:"sign_in"`
}

// Server is a named server bindings refer to.
type Server struct {
	URL string `json:"url"`
}

// SignIn says how to get a token for the gateway. DevToken is set only by a
// gateway run for development, which issues its own tokens.
type SignIn struct {
	Issuer   string `json:"issuer"`
	Audience string `json:"audience"`
	DevToken string `json:"dev_token,omitempty"`
}

// The built-in capabilities' addresses (§2.6). They are the only
// capabilities a client calls without first reading a profile.
const (
	AddressWhoAmI   = "atlas.whoami@v1:fetch"
	AddressCatalog  = "atlas.catalog@v1:fetch"
	AddressSearch   = "atlas.search@v1:fetch"
	AddressDescribe = "atlas.describe@v1:fetch"
)

// DescribeRequest names a capability by id@vN (its current profile) or one
// exact profile by id@vN#fingerprint.
type DescribeRequest struct {
	ID    string `json:"id,omitempty"`
	Exact string `json:"exact,omitempty"`
}

// DescribeAnswer is the profile with what is served beside it: the caller's
// governance and the capability's availability, which never change the
// fingerprint (§3.1.1).
type DescribeAnswer struct {
	Exact        string     `json:"exact"`
	Current      string     `json:"current"`
	Profile      Profile    `json:"profile"`
	Governance   Governance `json:"governance"`
	Availability string     `json:"availability"`
}

// Governance is the caller's own access to one capability.
type Governance struct {
	Revision     string   `json:"revision"`
	Interactions []string `json:"interactions"`
	HeldBy       []string `json:"held_by,omitempty"`
}

// CatalogEntry is one capability in the working view: only what the caller
// can use, here and now (§2.6).
type CatalogEntry struct {
	ID           string   `json:"id"`
	Version      int      `json:"version"`
	Exact        string   `json:"exact"`
	Family       string   `json:"family"`
	Title        string   `json:"title,omitempty"`
	Summary      string   `json:"summary"`
	Interactions []string `json:"interactions"`
	Score        float64  `json:"score,omitempty"`
}

// CatalogPage is a page of the working view.
type CatalogPage struct {
	Items []CatalogEntry `json:"items"`
}

// SearchRequest asks for available capabilities ranked against words.
type SearchRequest struct {
	Words string `json:"words"`
}

// WhoAmI is who the caller is to the gateway.
type WhoAmI struct {
	Principal   string         `json:"principal"`
	Name        string         `json:"name,omitempty"`
	Environment string         `json:"environment"`
	Roles       []string       `json:"roles"`
	Available   map[string]int `json:"available_domains"`
}

// EndLine is the last line of every NDJSON answer and the metadata of an
// Arrow answer's final batch (EndBatch). An answer without it is incomplete
// (§9.6.5).
type EndLine struct {
	Atlas     string   `json:"$atlas"`
	Rows      int64    `json:"rows"`
	Truncated bool     `json:"truncated"`
	Problem   *Problem `json:"problem,omitempty"`
}

// Handoff is a data result the client fetches directly from its source's
// result storage, with no session at the source (§2.2.1, §9.3). It is a
// credential for one result: short-lived, and never stored.
type Handoff struct {
	Kind    string `json:"kind"`
	Format  string `json:"format"`
	Profile string `json:"profile"`
	Rows    int64  `json:"rows"`
	// Source is the format's own description of the chunks' encoding; for
	// snowflake-arrow/1, Snowflake's row type.
	Source  json.RawMessage   `json:"source"`
	First   []byte            `json:"first_chunk,omitempty"`
	Chunks  []Chunk           `json:"chunks"`
	Headers map[string]string `json:"headers,omitempty"`
	Expires time.Time         `json:"expires"`
	// QueryID asks for fresh links when these expire, without running the
	// query again: repeat the call with Atlas-Renew set to it.
	QueryID string `json:"query_id"`
}

// HeaderRenew asks for fresh handoff links for a persisted result.
// Provisional; the spec re-issues links through the operation.
const HeaderRenew = "Atlas-Renew"

// Chunk is one part of a handed-off result.
type Chunk struct {
	URL   string `json:"url"`
	Rows  int64  `json:"rows"`
	Bytes int64  `json:"bytes"`
}
