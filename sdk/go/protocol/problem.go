package protocol

import "fmt"

// Problem is the one shape of every failure: RFC 9457 with Atlas's members
// (§4.1). The code is the contract; the status is transport detail.
type Problem struct {
	Type       string    `json:"type"`
	Title      string    `json:"title"`
	Status     int       `json:"status"`
	Detail     string    `json:"detail,omitempty"`
	Code       string    `json:"code"`
	Retryable  bool      `json:"retryable"`
	RetryAfter int       `json:"retry_after,omitempty"`
	Target     string    `json:"target,omitempty"`
	Next       string    `json:"next,omitempty"`
	Details    []Problem `json:"details,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
}

func (p *Problem) Error() string {
	if p.Target != "" {
		return fmt.Sprintf("%s: %s (%s)", p.Code, p.Detail, p.Target)
	}
	return p.Code + ": " + p.Detail
}

// WithTarget names what the problem is about: a parameter, a field path.
func (p *Problem) WithTarget(t string) *Problem { p.Target = t; return p }

// WithNext names the step a person or an agent can take.
func (p *Problem) WithNext(n string) *Problem { p.Next = n; return p }

// WithRetryAfter says how many seconds to wait before repeating.
func (p *Problem) WithRetryAfter(seconds int) *Problem { p.RetryAfter = seconds; return p }

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

type registered struct {
	status    int
	retryable bool
	title     string
}

var registry = map[string]registered{
	CodeInvalidDocument:          {400, false, "Invalid document"},
	CodeInvalidRequest:           {400, false, "Invalid request"},
	CodeUnauthenticated:          {401, false, "Unauthenticated"},
	CodeNotGranted:               {403, false, "Not granted"},
	CodeDenied:                   {403, false, "Denied"},
	CodeBudgetExceeded:           {403, false, "Budget exceeded"},
	CodeApprovalRequired:         {403, false, "Approval required"},
	CodeConfirmationRequired:     {428, false, "Confirmation required"},
	CodeNotFound:                 {404, false, "Not found"},
	CodeNotInEnvironment:         {404, false, "Not in this environment"},
	CodeIdempotencyConflict:      {409, false, "Idempotency key reused"},
	CodeInProgress:               {409, true, "In progress"},
	CodeRetired:                  {410, false, "Retired"},
	CodePreconditionFailed:       {412, false, "Precondition failed"},
	CodeCriticalTraitUnsupported: {422, false, "Critical trait unsupported"},
	CodeResolutionFailed:         {422, false, "Resolution failed"},
	CodeUpgradeRequired:          {426, false, "Upgrade required"},
	CodeLimitExceeded:            {429, true, "Limit exceeded"},
	CodeOutputLimitExceeded:      {200, false, "Output limit exceeded"},
	CodeInternal:                 {500, false, "Internal error"},
	CodeUpstreamFailed:           {502, true, "Upstream failed"},
	CodeUnavailable:              {503, true, "Unavailable"},
	CodeDeadlineExceeded:         {504, false, "Deadline exceeded"},
}

// NewProblem builds the problem for a registered code. An unregistered code
// is a defect and becomes internal.
func NewProblem(code, detail string) *Problem {
	r, ok := registry[code]
	if !ok {
		code, r = CodeInternal, registry[CodeInternal]
	}
	return &Problem{Type: "about:blank", Title: r.title, Status: r.status, Code: code, Retryable: r.retryable, Detail: detail}
}

// Known reports whether code is in the registry. A client treats a code it
// does not know by its status and retryable member (§9.3.3).
func Known(code string) bool {
	_, ok := registry[code]
	return ok
}
