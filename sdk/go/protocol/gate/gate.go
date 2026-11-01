// Package gate is the check every profile passes twice (§3.3.12): in the
// gate on every pull request, against the last published profiles, and in
// the gateway when a service registers, against the profiles it serves. A
// breaking change within a version is refused in both, so it can never
// reach callers even if it escaped review.
//
// Lint holds one profile to the rules a profile keeps on its own, Ready is
// the agent-readiness check (§3.1.4a), and Classify names every difference
// from the last published profile safe, dangerous or breaking (§3.3.3 to
// §3.3.7).
package gate

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/profile"
)

// Class is what a difference means for the readers and writers of a
// version (§3.3.2).
type Class int

// The classes, in order of severity.
const (
	Safe      Class = iota // no correct reader or writer can notice
	Dangerous              // readers keep working, by aliases and lenses; announced in the changelog
	Breaking               // some reader or writer would fail or misread: a new version
)

func (c Class) String() string { return [...]string{"safe", "dangerous", "breaking"}[c] }

// Finding is one difference, or one rule a profile breaks: where it is in
// the new profile, its class, and why, in words a person acts on.
type Finding struct {
	Class   Class
	Pointer string
	Rule    string
}

func (f Finding) String() string { return fmt.Sprintf("%s %s: %s", f.Class, f.Pointer, f.Rule) }

// Report is the gate's answer for one capability.
type Report struct {
	Findings []Finding
}

// Refused reports whether the profile may not be served: a lint failure, or
// a breaking difference within its version.
func (r Report) Refused() bool {
	return slices.ContainsFunc(r.Findings, func(f Finding) bool { return f.Class == Breaking })
}

// Worst is the most severe class found.
func (r Report) Worst() Class {
	worst := Safe
	for _, f := range r.Findings {
		worst = max(worst, f.Class)
	}
	return worst
}

// Reason is every refusal, one per line, for a registration's readiness.
func (r Report) Reason() string {
	var lines []string
	for _, f := range r.Findings {
		if f.Class == Breaking {
			lines = append(lines, f.Pointer+": "+f.Rule)
		}
	}
	return strings.Join(lines, "\n")
}

// MinimumSunset is the least time between a deprecation and its sunset
// (§3.3.9).
const MinimumSunset = 90 * 24 * time.Hour

// Catalogue answers what the gate needs to know beyond the profile: whether
// a referenced capability exists.
type Catalogue interface {
	Has(p grammar.Pinned) bool
}

// Check runs the gate for one changed profile (§3.3.12 steps 1 to 6):
// lint it, find the last published profile of its version, classify every
// difference, refuse a breaking one, require a changelog entry for a
// dangerous one, and check that every alias, lens and deprecation is
// consistent. previous is the last published profile of the same id@vN, or
// nil for a new capability or version.
func Check(p, previous *profile.Profile, cat Catalogue) Report {
	var r Report
	r.Findings = append(r.Findings, Lint(p, cat)...)
	if previous == nil {
		if p.Version > 1 && p.Evolution.PreviousVersion == "" {
			r.Findings = append(r.Findings, Finding{Breaking, "/evolution/previous_version", fmt.Sprintf("@v%d names the version it replaces", p.Version)})
		}
		return r
	}
	if previous.ID != p.ID || previous.Version != p.Version {
		r.Findings = append(r.Findings, Finding{Breaking, "/identity", fmt.Sprintf("%s@v%d is classified only against its own version, not %s@v%d", p.ID, p.Version, previous.ID, previous.Version)})
		return r
	}
	if previous.FP.Value == p.FP.Value {
		return r // prose only: a new revision, the same contract
	}
	diffs := Classify(previous, p)
	r.Findings = append(r.Findings, diffs...)
	if pp := p.Evolution.PreviousProfile; pp == nil || *pp != previous.Exact() {
		r.Findings = append(r.Findings, Finding{Breaking, "/evolution/previous_profile", fmt.Sprintf("names the profile it replaces, %s", previous.Exact())})
	}
	dangerous := slices.ContainsFunc(diffs, func(f Finding) bool { return f.Class == Dangerous })
	if dangerous && len(p.Evolution.Changelog) <= len(previous.Evolution.Changelog) {
		r.Findings = append(r.Findings, Finding{Breaking, "/evolution/changelog", "a dangerous change is announced in a new changelog entry"})
	}
	r.Findings = append(r.Findings, lensConsistent(previous, p)...)
	return r
}
