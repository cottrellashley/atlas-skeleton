package protocol

import (
	"errors"
	"fmt"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/resolve"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// Problem is the one shape of every failure (§4.1): RFC 9457 with Atlas's
// members. Its code is the contract; its status and retryable follow from
// the code, by the registry or by the capability's declared errors.
type Problem struct {
	Type       string    `json:"type"`
	Title      string    `json:"title"`
	Status     int       `json:"status"`
	Detail     string    `json:"detail,omitempty"`
	Instance   string    `json:"instance,omitempty"`
	Code       string    `json:"code"`
	Retryable  bool      `json:"retryable"`
	RetryAfter int       `json:"retry_after,omitempty"`
	Target     string    `json:"target,omitempty"`
	Next       string    `json:"next,omitempty"`
	Hop        string    `json:"hop,omitempty"`
	Estimate   *Estimate `json:"estimate,omitempty"`
	Details    []Problem `json:"details,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
}

// Estimate is a call's estimated cost, carried by confirmation_required. A
// person who accepts it repeats the call with Atlas-Confirm set to Amount
// (§9.7). Amount is a decimal string (§9.1), with at most three fraction
// digits so that it fits a Structured Field Decimal (assumption A-001).
type Estimate struct {
	Amount string `json:"amount"`
	Unit   string `json:"unit"`
}

func (p *Problem) Error() string {
	if p.Target != "" {
		return fmt.Sprintf("%s: %s (%s)", p.Code, p.Detail, p.Target)
	}
	return p.Code + ": " + p.Detail
}

// WithTarget names what the problem is about: a parameter, a field pointer,
// a resource name, a header.
func (p *Problem) WithTarget(t string) *Problem { p.Target = t; return p }

// WithNext names the step a person or an agent can take.
func (p *Problem) WithNext(n string) *Problem { p.Next = n; return p }

// WithRetryAfter says how many seconds to wait before repeating.
func (p *Problem) WithRetryAfter(seconds int) *Problem { p.RetryAfter = seconds; return p }

// WithDetails adds the problems behind this one.
func (p *Problem) WithDetails(d ...Problem) *Problem { p.Details = append(p.Details, d...); return p }

// WithEstimate attaches the cost a person is asked to confirm.
func (p *Problem) WithEstimate(e Estimate) *Problem { p.Estimate = &e; return p }

// The problem registry (§9.3.3).
const (
	CodeInvalidDocument          = "invalid_document"
	CodeInvalidRequest           = "invalid_request"
	CodeUnauthenticated          = "unauthenticated"
	CodeNotGranted               = "not_granted"
	CodeDenied                   = "denied"
	CodeBudgetExceeded           = "budget_exceeded"
	CodeApprovalRequired         = "approval_required"
	CodeConfirmationRequired     = "confirmation_required"
	CodeNotFound                 = "not_found"
	CodeNotInEnvironment         = "not_in_environment"
	CodeIdempotencyConflict      = "idempotency_conflict"
	CodeInProgress               = "in_progress"
	CodeResumeGap                = "resume_gap"
	CodeRetired                  = "retired"
	CodePreconditionFailed       = "precondition_failed"
	CodeCriticalTraitUnsupported = "critical_trait_unsupported"
	CodeResolutionFailed         = "resolution_failed"
	CodeUpgradeRequired          = "upgrade_required"
	CodeLimitExceeded            = "limit_exceeded"
	CodeOutputLimitExceeded      = "output_limit_exceeded"
	CodeInternal                 = "internal"
	CodeUpstreamFailed           = "upstream_failed"
	CodeUnavailable              = "unavailable"
	CodeDeadlineExceeded         = "deadline_exceeded"
)

// ProblemTypeBase prefixes every problem's type: the type of code c is
// ProblemTypeBase + c.
const ProblemTypeBase = "https://atlas.jhg.com/problems/"

// Registered is one row of the registry.
type Registered struct {
	Code      string
	Status    int
	Retryable bool
	Title     string
}

// Registry is §9.3.3, in its order. output_limit_exceeded's status is 200
// because it travels in an end line or a final frame, never as an answer's
// status.
var Registry = []Registered{
	{CodeInvalidDocument, 400, false, "Invalid document"},
	{CodeInvalidRequest, 400, false, "Invalid request"},
	{CodeUnauthenticated, 401, false, "Unauthenticated"},
	{CodeNotGranted, 403, false, "Not granted"},
	{CodeDenied, 403, false, "Denied"},
	{CodeBudgetExceeded, 403, false, "Budget exceeded"},
	{CodeApprovalRequired, 403, false, "Approval required"},
	{CodeConfirmationRequired, 428, false, "Confirmation required"},
	{CodeNotFound, 404, false, "Not found"},
	{CodeNotInEnvironment, 404, false, "Not in this environment"},
	{CodeIdempotencyConflict, 409, false, "Idempotency key reused"},
	{CodeInProgress, 409, true, "In progress"},
	{CodeResumeGap, 409, false, "Cannot resume"},
	{CodeRetired, 410, false, "Retired"},
	{CodePreconditionFailed, 412, false, "Precondition failed"},
	{CodeCriticalTraitUnsupported, 422, false, "Critical trait unsupported"},
	{CodeResolutionFailed, 422, false, "Resolution failed"},
	{CodeUpgradeRequired, 426, false, "Upgrade required"},
	{CodeLimitExceeded, 429, true, "Limit exceeded"},
	{CodeOutputLimitExceeded, 200, false, "Output limit exceeded"},
	{CodeInternal, 500, false, "Internal error"},
	{CodeUpstreamFailed, 502, true, "Upstream failed"},
	{CodeUnavailable, 503, true, "Unavailable"},
	{CodeDeadlineExceeded, 504, false, "Deadline exceeded"},
}

var registry = func() map[string]Registered {
	m := make(map[string]Registered, len(Registry))
	for _, r := range Registry {
		m[r.Code] = r
	}
	return m
}()

// Lookup returns a code's row of the registry.
func Lookup(code string) (Registered, bool) {
	r, ok := registry[code]
	return r, ok
}

// Known reports whether code is in the registry. A client treats a code it
// does not know by the problem's status and retryable (§9.3.3).
func Known(code string) bool {
	_, ok := registry[code]
	return ok
}

// NewProblem builds the problem for a registered code. An unregistered code
// is a defect in the caller and becomes internal, so the gateway never sends
// a code outside the registry and the capability's declared errors.
func NewProblem(code, detail string) *Problem {
	r, ok := registry[code]
	if !ok {
		r = registry[CodeInternal]
	}
	return &Problem{Type: ProblemTypeBase + r.Code, Title: r.Title, Status: r.Status, Code: r.Code, Retryable: r.Retryable, Detail: detail}
}

// ExitCode is the CLI's exit status for a problem's code (§9.7.6). Every
// code it does not name, a capability's own among them, is 1.
func ExitCode(code string) int {
	switch code {
	case CodeInvalidRequest:
		return 2
	case CodeUnauthenticated:
		return 3
	case CodeNotGranted, CodeDenied, CodeBudgetExceeded, CodeApprovalRequired, CodeConfirmationRequired:
		return 4
	case CodeNotFound, CodeNotInEnvironment, CodeRetired:
		return 5
	case CodeLimitExceeded, CodeUnavailable, CodeInProgress:
		return 6
	case CodeDeadlineExceeded:
		return 7
	case CodeResolutionFailed, CodeCriticalTraitUnsupported, CodeUpgradeRequired:
		return 8
	}
	return 1
}

// Declared is one member of a profile's errors: a code the capability adds
// to the registry (§3.1).
type Declared struct {
	Code        string `json:"code"`
	Status      int    `json:"status"`
	Retryable   bool   `json:"retryable"`
	Description string `json:"description"`
	Next        string `json:"next"`
}

// Problem builds the problem for a declared code.
func (d Declared) Problem(detail string) *Problem {
	return &Problem{Type: ProblemTypeBase + d.Code, Title: d.Description, Status: d.Status, Code: d.Code, Retryable: d.Retryable, Detail: detail, Next: d.Next}
}

// Check reports whether a problem is one the gateway may send: its code is
// a problem-code, its type names it, and a registered code has its
// registered status and retryable. declared lists the capability's own
// codes.
func (p *Problem) Check(declared ...Declared) error {
	if err := grammar.ProblemCode(p.Code); err != nil {
		return err
	}
	if p.Type != ProblemTypeBase+p.Code {
		return fmt.Errorf("problem %s: type %q is not %s%s", p.Code, p.Type, ProblemTypeBase, p.Code)
	}
	status, retryable, ok := p.Status, p.Retryable, false
	if r, known := registry[p.Code]; known {
		status, retryable, ok = r.Status, r.Retryable, true
	}
	for _, d := range declared {
		if !ok && d.Code == p.Code {
			status, retryable, ok = d.Status, d.Retryable, true
		}
	}
	switch {
	case !ok:
		return fmt.Errorf("problem %s: not in the registry nor declared", p.Code)
	case p.Status != status:
		return fmt.Errorf("problem %s: status %d, registered %d", p.Code, p.Status, status)
	case p.Retryable != retryable:
		return fmt.Errorf("problem %s: retryable %v, registered %v", p.Code, p.Retryable, retryable)
	}
	for i := range p.Details {
		if err := p.Details[i].Check(declared...); err != nil {
			return fmt.Errorf("problem %s, detail %d: %w", p.Code, i, err)
		}
	}
	return nil
}

// InvalidRequest is the problem for a request that fails its schema
// (§9.2.5): every failure in details, the first one's pointer as target.
func InvalidRequest(failures []schema.Failure) *Problem {
	p := NewProblem(CodeInvalidRequest, "the request is not valid against its schema")
	for i, f := range failures {
		if i == 0 {
			p.Target = f.Pointer
			p.Detail = fmt.Sprintf("%s: %s", f.Keyword, f.Message)
		}
		d := NewProblem(CodeInvalidRequest, f.Keyword+": "+f.Message).WithTarget(f.Pointer)
		p.Details = append(p.Details, *d)
	}
	if len(failures) > 1 {
		p.Detail = fmt.Sprintf("%s, and %d more", p.Detail, len(failures)-1)
	}
	return p
}

// ResolutionFailed is the problem for a payload that cannot be resolved
// (§9.5): resolution_failed, its target the field.
func ResolutionFailed(err error) *Problem {
	var e *resolve.Error
	if errors.As(err, &e) {
		return NewProblem(CodeResolutionFailed, e.Message).WithTarget(e.Target)
	}
	return NewProblem(CodeResolutionFailed, err.Error())
}
