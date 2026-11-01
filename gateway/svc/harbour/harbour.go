// Package harbour serves the portfolio and index capabilities from Harbour's
// views in Snowflake, through the data-source port (§6.4). Each capability
// declares every profile it has had within its version, in order, with the
// lens from each to the next; the gateway serves the last as current. The
// service's code maps each profile's columns to Harbour's: that mapping is
// not part of the contract and may change freely (§3.3.4).
package harbour

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// Service is Harbour behind the front door.
type Service struct {
	source   ports.DataSource
	key      ed25519.PublicKey
	now      func() time.Time
	caps     map[string]*capability
	profiles []*protocol.Profile
	byExact  map[string]*protocol.Profile
}

type capability struct {
	view      string
	filters   func(ports.Request) ([]ports.Filter, error)
	revisions map[string]map[string]ports.SourceColumn // exact id → writer column → source
}

// New returns the service. key is the front door's public key, which every
// call's signature is checked against.
func New(source ports.DataSource, key ed25519.PublicKey) (*Service, error) {
	s := &Service{source: source, key: key, now: time.Now, caps: map[string]*capability{}, byExact: map[string]*protocol.Profile{}}
	if err := s.declare(holdings()); err != nil {
		return nil, err
	}
	if err := s.declare(constituents()); err != nil {
		return nil, err
	}
	return s, nil
}

var _ ports.Service = (*Service)(nil)

func (s *Service) Domains() []string             { return []string{"portfolio", "index"} }
func (s *Service) Profiles() []*protocol.Profile { return s.profiles }

type declaration struct {
	view      string
	filters   func(ports.Request) ([]ports.Filter, error)
	revisions []revision
}

type revision struct {
	profile *protocol.Profile
	sources map[string]ports.SourceColumn
}

func (s *Service) declare(d declaration) error {
	c := &capability{view: d.view, filters: d.filters, revisions: map[string]map[string]ports.SourceColumn{}}
	prev := ""
	for _, r := range d.revisions {
		if prev != "" {
			r.profile.Evolution.Previous = prev
		}
		e, err := protocol.ExactOf(r.profile)
		if err != nil {
			return err
		}
		prev = e.String()
		c.revisions[prev] = r.sources
		s.byExact[prev] = r.profile
		s.profiles = append(s.profiles, r.profile)
		s.caps[r.profile.Pinned().String()] = c
	}
	return nil
}

func (s *Service) Serve(ctx context.Context, call ports.Call, req ports.Request) (ports.Answer, error) {
	if err := ports.VerifyCall(s.key, call, call.Address, s.now()); err != nil {
		return ports.Answer{}, err
	}
	addr, err := protocol.ParseAddress(call.Address)
	if err != nil {
		return ports.Answer{}, err
	}
	c, ok := s.caps[addr.Pinned.String()]
	if !ok {
		return ports.Answer{}, protocol.NewProblem(protocol.CodeNotFound, "this service does not serve "+addr.Pinned.String())
	}
	sources, ok := c.revisions[call.Writer]
	if !ok {
		return ports.Answer{}, fmt.Errorf("no source mapping for %s", call.Writer)
	}
	profile := s.byExact[call.Writer]
	if call.Renew != "" {
		return s.renew(ctx, call)
	}
	writer := profile.Result.Project(names(req["columns"]))
	q := ports.Query{Source: c.view, Tag: tag(call)}
	for _, col := range writer.Columns {
		q.Columns = append(q.Columns, sources[col.Name])
		if col.Key {
			q.OrderBy = append(q.OrderBy, col.Name)
		}
	}
	if q.Filters, err = c.filters(req); err != nil {
		return ports.Answer{}, err
	}
	plan, err := s.source.Plan(ctx, q, writer)
	if err != nil {
		return ports.Answer{}, err
	}
	switch addr.Interaction {
	case "explain":
		ex, err := s.source.Explain(ctx, plan)
		return ports.Answer{Record: ex}, err
	case "fetch":
		if call.Execution == protocol.ExecutionHandoff {
			h, err := s.source.Handoff(ctx, plan, call.Writer)
			if err != nil {
				return ports.Answer{}, err
			}
			return ports.Answer{Handoff: h, QueryID: h.QueryID}, nil
		}
		res, err := s.source.Execute(ctx, plan, call.Writer)
		return ports.Answer{Table: res.Reader, QueryID: res.QueryID}, err
	}
	return ports.Answer{}, protocol.NewProblem(protocol.CodeNotFound, "no interaction "+addr.Interaction)
}

// renew re-issues a handed-off result's links, only for the caller whose call
// produced it: a handoff is a credential bound to its caller (§2.2.1, rule 3).
func (s *Service) renew(ctx context.Context, call ports.Call) (ports.Answer, error) {
	u, err := s.source.Settle(ctx, call.Renew)
	if err != nil || !strings.HasSuffix(u.Tag, "/"+subjectTag(call.Subject)) {
		return ports.Answer{}, protocol.NewProblem(protocol.CodeNotFound, "no result "+call.Renew+" of yours to renew")
	}
	h, err := s.source.Renew(ctx, call.Renew, call.Writer)
	return ports.Answer{Handoff: h}, err
}

func (s *Service) Settle(ctx context.Context, queryID string) (ports.Usage, error) {
	return s.source.Settle(ctx, queryID)
}

// tag is recorded with the query: the request id and a hash of the caller.
func tag(call ports.Call) string { return call.RequestID + "/" + subjectTag(call.Subject) }

func subjectTag(subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return hex.EncodeToString(sum[:6])
}

func names(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func values(v any) []any {
	list, _ := v.([]any)
	return list
}

func asOf(req ports.Request, column string) (ports.Filter, error) {
	v, ok := req["as_of"].(string)
	if !ok {
		return ports.Filter{Source: column, Op: ports.OpLatest}, nil
	}
	t, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return ports.Filter{}, protocol.NewProblem(protocol.CodeInvalidRequest, "as_of must be a date").WithTarget("/as_of")
	}
	return ports.Filter{Source: column, Op: ports.OpEq, Values: []any{t}}, nil
}
