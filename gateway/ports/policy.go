package ports

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/schema"
)

// Policy is one revision of who may do what, and over which data
// (ATLAS-CORE §3, R05). It is a snapshot: the request path reads it, never
// the store behind it (constitution rule 3).
type Policy struct {
	// Revision names this policy; every answer carries it in
	// Atlas-Governance-Revision, and every audit fact records it.
	Revision string `json:"revision"`
	// Roles are the roles each subject holds. A subject is a principal or a
	// group, so a directory group can hold a role.
	Roles map[string][]string `json:"roles"`
	// Grants and Denies, in no order: a deny always wins.
	Grants []Grant `json:"grants"`
	Denies []Deny  `json:"denies"`
	// Scopes are the data each subject may read, by dimension: a subject's
	// portfolios, say. A dimension the caller has no scope for is closed to
	// them; "*" opens a dimension whole.
	Scopes map[string]Scope `json:"scopes"`
	// Limits are the protective limits, per caller and capability.
	Limits []Limit `json:"limits"`
	// HouseRules are the rules a caller is shown (§2.6), under their own
	// revision.
	HouseRules         []HouseRule `json:"house_rules"`
	HouseRulesRevision string      `json:"house_rules_revision"`
}

// Grant lets the holders of a role use interactions of capabilities.
type Grant struct {
	// ID names the grant: describe's held_by.
	ID string `json:"id"`
	// Role is who holds it.
	Role string `json:"role"`
	// Capabilities are the capability ids or pinned ids it covers, or a
	// domain's wildcard ("portfolio.*"). A wildcard reaches only what 06-3
	// rule 3 and ATLAS-ACCESS-002 let a family grant reach: read
	// interactions of a capability whose system is reached as the caller.
	Capabilities []string `json:"capabilities"`
	// Interactions it covers; empty means every interaction.
	Interactions []string `json:"interactions,omitempty"`
}

// Deny refuses interactions of capabilities to a subject (a principal, a
// group or a role, or "*" for every caller), whatever it holds.
type Deny struct {
	ID           string   `json:"id"`
	Subject      string   `json:"subject"`
	Capabilities []string `json:"capabilities"`
	Interactions []string `json:"interactions,omitempty"`
	Reason       string   `json:"reason"`
}

// Scope is the values of each data dimension a subject may read.
type Scope map[string][]string

// Limit bounds a subject's calls of a capability (§6.2.4).
type Limit struct {
	// Subject is a principal, group or role, or "*" for every caller.
	Subject string `json:"subject"`
	// Capability is a capability id, pinned id or wildcard.
	Capability string `json:"capability"`
	// PerMinute is the most calls a minute; 0 is no rate limit.
	PerMinute int `json:"per_minute,omitempty"`
	// Concurrency is the most calls at once; 0 is no limit.
	Concurrency int `json:"concurrency,omitempty"`
	// MaxRows caps a table answer's rows; the answer is truncated beyond it.
	MaxRows int `json:"max_rows,omitempty"`
}

// HouseRule is a rule shown to every caller who can use all of its
// capabilities.
type HouseRule struct {
	Text         string   `json:"text"`
	Capabilities []string `json:"capabilities"`
}

// PolicySource gives the current policy. The gateway holds the last one it
// read for a bounded time and refuses calls when it cannot be renewed in
// time (R13): a stale policy cannot promise that a revocation took hold.
//
// So a source answers the policy its store holds now, or an error when it
// cannot: its store unreachable, its file missing, unreadable or invalid.
// It never answers an older policy as if it were current, because the
// gateway can bound only the staleness it is told of (PR02), and a policy
// it answers names its revision and never changes afterwards. It honours
// ctx, and is safe for concurrent use. porttest.PolicySource is its suite.
type PolicySource interface {
	Policy(ctx context.Context) (*Policy, error)
}

// MaxRevision is the longest revision a policy may name: it travels in a
// header (Atlas-Governance-Revision) and in every audit fact.
const MaxRevision = 128

// PolicyError is a policy that cannot be read, with every reason, so one
// attempt shows its author everything to fix.
type PolicyError struct{ Problems []string }

func (e *PolicyError) Error() string {
	return "ports: the policy is refused: " + strings.Join(e.Problems, "; ")
}

// ParsePolicy reads a policy as every PolicySource and fixture reads one
// (R134): one JSON object, refusing a member it does not know, a policy
// with no revision, and any entry in a form the gateway does not decide by.
// Whether what the policy names exists is the gateway's check against its
// catalogue (core/access.Check), not the reader's.
func ParsePolicy(b []byte) (*Policy, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var p Policy
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("ports: the policy: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("ports: the policy is one JSON object, with nothing after it")
	}
	if problems := p.problems(); len(problems) > 0 {
		return nil, &PolicyError{problems}
	}
	return &p, nil
}

// problems are every rule of the policy's form it breaks.
func (p *Policy) problems() []string {
	var out []string
	bad := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	switch why := revisionProblem(p.Revision); {
	case p.Revision == "":
		bad("a policy names its revision")
	case why != "":
		bad("revision: %s", why)
	}
	if why := revisionProblem(p.HouseRulesRevision); p.HouseRulesRevision != "" && why != "" {
		bad("house_rules_revision: %s", why)
	}
	for _, subject := range sortedKeys(p.Roles) {
		if subject == "" {
			bad("roles: a role is held by a principal or a group, named")
		}
		for _, r := range p.Roles[subject] {
			switch r {
			case "":
				bad("roles[%q]: names its roles", subject)
			case "*":
				bad("roles[%q]: * is not a role: it stands for every caller only as a deny's or a limit's subject", subject)
			}
		}
	}
	ids := map[string]int{}
	for i, g := range p.Grants {
		at := fmt.Sprintf("grants[%d]", i)
		switch first, seen := ids[g.ID]; {
		case g.ID == "":
			bad("%s: has an id", at)
		case seen:
			bad("%s: the id %q is grants[%d]'s", at, g.ID, first)
		default:
			ids[g.ID] = i
		}
		switch g.Role {
		case "":
			bad("%s: names the role that holds it", at)
		case "*":
			bad("%s: is held by a role, and * is not one: grant a role every caller holds, and only reads reach everyone (06-2 §6.2.4)", at)
		}
		entries(at, g.Capabilities, true, bad)
		interactions(at, g.Interactions, bad)
	}
	ids = map[string]int{}
	for i, d := range p.Denies {
		at := fmt.Sprintf("denies[%d]", i)
		switch first, seen := ids[d.ID]; {
		case d.ID == "":
			bad("%s: has an id", at)
		case seen:
			bad("%s: the id %q is denies[%d]'s", at, d.ID, first)
		default:
			ids[d.ID] = i
		}
		if d.Subject == "" {
			bad("%s: names its subject: a principal, a group, a role, or * for every caller", at)
		}
		if strings.TrimSpace(d.Reason) == "" {
			bad("%s: gives its reason, which the caller it refuses is shown", at)
		}
		entries(at, d.Capabilities, true, bad)
		interactions(at, d.Interactions, bad)
	}
	for _, subject := range sortedKeys(p.Scopes) {
		if subject == "" {
			bad("scopes: a scope is a principal's, a group's or a role's, named")
		}
		scope := p.Scopes[subject]
		for _, dim := range sortedKeys(scope) {
			if grammar.PropertyName(dim) != nil || len(dim) > schema.MaxDimension {
				bad("scopes[%q]: %q is not a dimension's name; which dimensions the deployment defines is checked against its catalogue", subject, dim)
			}
			for _, v := range scope[dim] {
				if v == "" {
					bad("scopes[%q][%q]: a value is a code, or * for every one", subject, dim)
				}
			}
		}
	}
	for i, l := range p.Limits {
		at := fmt.Sprintf("limits[%d]", i)
		if l.Subject == "" {
			bad("%s: names its subject: a principal, a group, a role, or * for every caller", at)
		}
		entries(at, []string{l.Capability}, true, bad)
		switch {
		case l.PerMinute < 0 || l.Concurrency < 0 || l.MaxRows < 0:
			bad("%s: per_minute, concurrency and max_rows are not negative", at)
		case l.PerMinute == 0 && l.Concurrency == 0 && l.MaxRows == 0:
			bad("%s: sets per_minute, concurrency or max_rows", at)
		}
	}
	for i, r := range p.HouseRules {
		at := fmt.Sprintf("house_rules[%d]", i)
		if strings.TrimSpace(r.Text) == "" {
			bad("%s: has its text", at)
		}
		entries(at, r.Capabilities, false, bad)
	}
	return out
}

// entries checks a list of capability entries: each a capability id (every
// version), a pinned id, and where families may stand, a namespace
// ("portfolio.*") or every capability ("*").
func entries(at string, list []string, families bool, bad func(string, ...any)) {
	if len(list) == 0 {
		bad("%s: names its capabilities", at)
	}
	for _, e := range list {
		if why := entryProblem(e, families); why != "" {
			bad("%s: %s", at, why)
		}
	}
}

func entryProblem(e string, families bool) string {
	switch {
	case strings.Contains(e, ":"):
		return fmt.Sprintf("%q names an interaction: name the interaction in \"interactions\", and only the capability here", e)
	case e == "*" || strings.HasSuffix(e, ".*"):
		if !families {
			return fmt.Sprintf("%q is a family: name each capability by its id or pinned id", e)
		}
		if e == "*" {
			return ""
		}
		for _, seg := range strings.Split(strings.TrimSuffix(e, ".*"), ".") {
			if grammar.Interaction(seg) != nil {
				return fmt.Sprintf("%q is not a namespace: segments of a-z, 0-9 and -, then .*", e)
			}
		}
		return ""
	case strings.Contains(e, "@"):
		if _, err := grammar.ParsePinned(e); err != nil {
			return fmt.Sprintf("%q is not a pinned id: %v", e, err)
		}
		return ""
	case grammar.CapabilityID(e) != nil:
		return fmt.Sprintf("%q is not a capability id, a pinned id or a namespace", e)
	}
	return ""
}

func interactions(at string, list []string, bad func(string, ...any)) {
	for i, n := range list {
		switch {
		case grammar.Interaction(n) != nil:
			bad("%s: %q is not an interaction name", at, n)
		case slices.Contains(list[:i], n):
			bad("%s: names the interaction %q twice", at, n)
		}
	}
}

func revisionProblem(r string) string {
	if len(r) > MaxRevision {
		return fmt.Sprintf("at most %d characters", MaxRevision)
	}
	for i := 0; i < len(r); i++ {
		if r[i] < 0x20 || r[i] > 0x7e {
			return "printable ASCII only, as the header that carries it is"
		}
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
