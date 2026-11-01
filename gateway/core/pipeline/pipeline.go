// Package pipeline is the front door's one pipeline (§6.1.2). Every call,
// through every edge, passes the same steps in the same order:
// authenticate, resolve, authorise, validate, limit, route, execute, then
// settle and audit. Each position follows from what the step needs (§6.2.3):
// a caller without a grant learns nothing about a request's shape, and an
// invalid request spends no limit. Every call, refused or not, leaves exactly
// one audit record (ATLAS-ACCESS-004).
package pipeline

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/core/builtins"
	"github.com/jhgonline/fi-app-atlas/gateway/core/catalog"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/jwt"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/problem"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// DefaultDeadline is how long a call may take when its caller says nothing.
const DefaultDeadline = 30 * time.Second

// Pipeline holds what the front door decides with.
type Pipeline struct {
	Verifier    *jwt.Verifier
	Snapshot    func() *catalog.Snapshot
	Limits      ports.Limits
	Audit       ports.AuditSink
	Key         ed25519.PrivateKey
	Environment string
	Now         func() time.Time
}

// Request is one call as an edge hands it over.
type Request struct {
	Token     string
	Address   string
	Body      []byte
	Expect    string // the profile the request was written under
	Execution string // the caller's preference
	Renew     string // a handed-off result to re-issue links for
	Features  protocol.Features
	RequestID string
	Client    string
}

// Outcome is a decided call's answer. The edge sends it, then calls Finish.
type Outcome struct {
	// Writer is the exact profile the answer is written under; empty for a
	// built-in in atlas/0.1.
	Writer string
	Answer ports.Answer
	finish func(rows int64, err error)
}

// Finish settles and audits the call. For a stream it runs before the end
// line, so the record is durable before the answer ends.
func (o *Outcome) Finish(rows int64, err error) { o.finish(rows, err) }

// Handle decides one call and, if it is allowed, runs it.
func (p *Pipeline) Handle(ctx context.Context, r Request) (*Outcome, error) {
	start := p.Now()
	snap := p.Snapshot()
	rec := ports.AuditRecord{Time: start, RequestID: r.RequestID, Address: r.Address, Expected: r.Expect, Governance: snap.Revision}
	refuse := func(step string, err error) (*Outcome, error) {
		prob := problem.From(err)
		rec.Decision, rec.RefusedAt, rec.Outcome = prob.Code, step, "refused"
		rec.DurationMS = millis(p.Now().Sub(start))
		if aerr := p.Audit.Write(ctx, rec); aerr != nil {
			return nil, protocol.NewProblem(protocol.CodeInternal, "the call could not be audited")
		}
		return nil, prob
	}

	// 1. Authenticate: the identity from a validated token only.
	if r.Token == "" {
		return refuse("authenticate", protocol.NewProblem(protocol.CodeUnauthenticated, "sign in: the call carries no token"))
	}
	claims, err := p.Verifier.Verify(ctx, r.Token)
	if err != nil {
		return refuse("authenticate", protocol.NewProblem(protocol.CodeUnauthenticated, "the token is not valid for this gateway"))
	}
	who := principal(claims)
	rec.Caller = who.Subject

	// 2. Resolve: the address, its profile and its governance.
	addr, err := protocol.ParseAddress(r.Address)
	if err != nil {
		return refuse("resolve", protocol.NewProblem(protocol.CodeNotFound, err.Error()))
	}
	if protocol.IsBuiltin(addr.ID) {
		return p.builtin(ctx, snap, who, addr, r, &rec, start, refuse)
	}
	entry, ok := snap.Current(addr.Pinned)
	if !ok {
		return refuse("resolve", protocol.NewProblem(protocol.CodeNotFound, "no capability "+addr.Pinned.String()))
	}
	inter, ok := entry.Profile.Interactions[addr.Interaction]
	if !ok {
		return refuse("resolve", protocol.NewProblem(protocol.CodeNotFound, fmt.Sprintf("%s has no interaction %s", addr.Pinned, addr.Interaction)))
	}
	rec.Writer = entry.Exact

	// 3. Authorise: deny, then grants (ATLAS-ACCESS-009).
	d := snap.Governance.Decide(who, addr, inter.Effect)
	if d.Code != "" {
		return refuse("authorise", protocol.NewProblem(d.Code, "you may not call "+addr.String()).
			WithNext("call atlas.access for "+addr.Pinned.String()+" to see how to get it"))
	}

	// 4. Validate: resolve the request to the current profile, fill its
	// defaults, then check it (ATLAS-ACCESS-010).
	req, err := p.validate(snap, entry, r)
	if err != nil {
		return refuse("validate", err)
	}
	rec.RequestHash = hashRequest(req)

	// 5. Limit: reserved before the work starts.
	res, err := p.Limits.Reserve(ctx, who.Subject+"|"+addr.String(), d.Rate, catalog.RateWindow)
	if err != nil {
		return refuse("limit", err)
	}

	// 6. Route: to the service, with a signed call.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = start.Add(DefaultDeadline)
	}
	execution := chooseExecution(inter, r)
	call := ports.SignCall(p.Key, ports.Call{
		Address: addr.String(), Subject: who.Subject, RequestID: r.RequestID, Deadline: deadline,
		Writer: entry.Exact, Execution: execution, Renew: r.Renew,
	})
	rec.Execution = execution

	// 7. Execute.
	ans, err := entry.Service.Serve(ctx, call, ports.Request(req))
	if err != nil {
		return refuse("execute", err)
	}

	rec.Decision = "allowed"
	out := &Outcome{Writer: entry.Exact, Answer: ans}
	out.finish = func(rows int64, ferr error) {
		// 8. Settle and audit, from the source's own record where it has one.
		_ = p.Limits.Settle(ctx, res)
		if ans.QueryID != "" {
			if u, err := entry.Service.Settle(ctx, ans.QueryID); err == nil {
				rec.QueryID = u.QueryID
				if ans.Handoff != nil {
					rows = u.Rows
				}
			}
		}
		rec.Rows, rec.Outcome = rows, "ok"
		if ferr != nil {
			rec.Outcome = problem.From(ferr).Code
		}
		rec.DurationMS = millis(p.Now().Sub(start))
		_ = p.Audit.Write(ctx, rec)
	}
	return out, nil
}

func (p *Pipeline) validate(snap *catalog.Snapshot, entry *catalog.Entry, r Request) (map[string]any, error) {
	body := r.Body
	if len(body) == 0 {
		body = []byte("{}")
	}
	req, err := protocol.DecodeRecord(body)
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, "the request body is not a JSON object: "+err.Error())
	}
	if r.Expect != "" && r.Expect != entry.Exact {
		chain, err := protocol.BuildChain(snap.Source(), r.Expect, entry.Exact)
		if err != nil {
			return nil, err
		}
		if req, err = protocol.ResolveRequest(chain, req, entry.Profile.Request); err != nil {
			return nil, err
		}
	}
	if err := protocol.ApplyDefaults(entry.Profile.Request, req); err != nil {
		return nil, err
	}
	if failures := protocol.Validate(entry.Profile.Request, req); len(failures) > 0 {
		prob := protocol.NewProblem(protocol.CodeInvalidRequest, failures[0].Message).WithTarget(failures[0].Pointer)
		for _, f := range failures {
			prob.Details = append(prob.Details, protocol.Problem{Code: protocol.CodeInvalidRequest, Detail: f.Message, Target: f.Pointer})
		}
		return nil, prob
	}
	// A Projectable request names columns as its writer knew them; map each,
	// by name or alias, to the current profile's column (§3.3.4).
	if cols, ok := req["columns"].([]any); ok && entry.Profile.HasTrait(protocol.TraitProjectable) && entry.Profile.Result != nil {
		mapped := make([]any, len(cols))
		for i, c := range cols {
			name, _ := c.(string)
			col, ok := entry.Profile.Result.Lookup(name)
			if !ok {
				return nil, protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("%q is not a column of %s", name, entry.Profile.Pinned())).
					WithTarget("/columns/" + strconv.Itoa(i))
			}
			mapped[i] = col.Name
		}
		req["columns"] = mapped
	}
	return req, nil
}

// chooseExecution serves the caller the way it asked where the interaction
// offers it and the client declared it can take it; otherwise the gateway
// streams, which every client can read (§2.2.1.7). Size-based choice comes
// with explain estimates.
func chooseExecution(inter protocol.Interaction, r Request) string {
	handoff := inter.Offers(protocol.ExecutionHandoff) && r.Features.Has(protocol.LevelHandoff) && len(r.Features.Handoff) > 0
	if handoff && (r.Renew != "" || r.Execution == protocol.ExecutionHandoff) {
		return protocol.ExecutionHandoff
	}
	return protocol.ExecutionGateway
}

func (p *Pipeline) builtin(ctx context.Context, snap *catalog.Snapshot, who catalog.Principal, addr protocol.Address, r Request, rec *ports.AuditRecord, start time.Time, refuse func(string, error) (*Outcome, error)) (*Outcome, error) {
	var answer any
	var err error
	switch addr.String() {
	case protocol.AddressWhoAmI:
		answer = builtins.WhoAmI(snap, who, p.Environment)
	case protocol.AddressCatalog:
		answer = builtins.Catalog(snap, who)
	case protocol.AddressSearch:
		var q protocol.SearchRequest
		if err = json.Unmarshal(nonEmpty(r.Body), &q); err == nil {
			answer = builtins.Search(snap, who, q.Words)
		}
	case protocol.AddressDescribe:
		var q protocol.DescribeRequest
		if err = json.Unmarshal(nonEmpty(r.Body), &q); err == nil {
			answer, err = builtins.Describe(snap, who, q)
		}
	default:
		return refuse("resolve", protocol.NewProblem(protocol.CodeNotFound, "no built-in "+addr.String()))
	}
	if err != nil {
		if _, isProblem := err.(*protocol.Problem); !isProblem {
			err = protocol.NewProblem(protocol.CodeInvalidRequest, err.Error())
		}
		return refuse("validate", err)
	}
	rec.Decision, rec.Outcome = "allowed", "ok"
	out := &Outcome{Answer: ports.Answer{Record: answer}}
	out.finish = func(int64, error) {
		rec.DurationMS = millis(p.Now().Sub(start))
		_ = p.Audit.Write(ctx, *rec)
	}
	return out, nil
}

func principal(c *jwt.Claims) catalog.Principal {
	p := catalog.Principal{Subject: c.ObjectID, Name: c.Email}
	if p.Subject == "" {
		p.Subject = c.Subject
	}
	if raw, ok := c.Raw["groups"]; ok {
		_ = json.Unmarshal(raw, &p.Groups)
	}
	return p
}

// hashRequest records the request as a hash of its canonical form, never
// the request itself (§6.2.8).
func hashRequest(req map[string]any) string {
	b, _ := json.Marshal(req)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func nonEmpty(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

func millis(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
