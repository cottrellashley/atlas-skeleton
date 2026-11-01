package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// Governance is who may use what, and their limits (§6.2.4). It is served
// beside profiles with its own revision, and never changes a fingerprint.
type Governance struct {
	Revision string `json:"revision"`
	Roles    []Role `json:"roles"`
}

// Role is a named set of grants with a limit. Members are principals or
// group ids; Atlas never stores who is in a group.
type Role struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
	// Grants and Denies name a domain (portfolio), a capability version
	// (portfolio.holdings@v1) or one interaction
	// (portfolio.holdings@v1:fetch).
	Grants        []string `json:"grants"`
	Denies        []string `json:"denies,omitempty"`
	RatePerMinute int      `json:"rate_per_minute,omitempty"`
}

// Principal is who is calling, from a validated token only.
type Principal struct {
	Subject string
	Name    string
	Groups  []string
}

// LoadGovernance reads governance from a JSON file.
func LoadGovernance(path string) (*Governance, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var g Governance
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, fmt.Errorf("governance %s: %w", path, err)
	}
	return &g, nil
}

// Decision is the outcome of the access steps (§6.2.3): deny, then grants.
type Decision struct {
	Code   string // "" when allowed, else denied or not_granted
	HeldBy []string
	Rate   int
}

// Decide answers whether the principal may use the interaction. A deny always
// wins; a domain grant reaches only interactions whose effect is read
// (ATLAS-ACCESS-001, -002).
func (g *Governance) Decide(p Principal, addr protocol.Address, effect string) Decision {
	var d Decision
	for _, r := range g.rolesOf(p) {
		for _, deny := range r.Denies {
			if reaches(deny, addr, effect) {
				return Decision{Code: protocol.CodeDenied}
			}
		}
		for _, grant := range r.Grants {
			if reaches(grant, addr, effect) {
				d.HeldBy = append(d.HeldBy, r.Name)
				if r.RatePerMinute > 0 && (d.Rate == 0 || r.RatePerMinute > d.Rate) {
					d.Rate = r.RatePerMinute
				}
			}
		}
	}
	if len(d.HeldBy) == 0 {
		d.Code = protocol.CodeNotGranted
	}
	return d
}

// RoleNames returns the names of the principal's roles.
func (g *Governance) RoleNames(p Principal) []string {
	var out []string
	for _, r := range g.rolesOf(p) {
		out = append(out, r.Name)
	}
	return out
}

// RateWindow is the period rates are counted over.
const RateWindow = time.Minute

func (g *Governance) rolesOf(p Principal) []Role {
	var out []Role
	for _, r := range g.Roles {
		if slices.Contains(r.Members, p.Subject) || slices.ContainsFunc(p.Groups, func(gid string) bool { return slices.Contains(r.Members, gid) }) {
			out = append(out, r)
		}
	}
	return out
}

// reaches reports whether a grant or deny names the address.
func reaches(pattern string, addr protocol.Address, effect string) bool {
	switch {
	case strings.Contains(pattern, ":"):
		return pattern == addr.String()
	case strings.Contains(pattern, "@v"):
		return pattern == addr.Pinned.String()
	default: // a domain: read interactions only
		return pattern == protocol.Domain(addr.ID) && effect == "read"
	}
}
