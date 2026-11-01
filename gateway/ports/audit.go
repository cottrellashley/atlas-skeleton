package ports

import (
	"context"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// Fact is one immutable fact of an invocation's history (R09): its
// admission or refusal, its outcome, its settlement. The facts of one call
// share its RequestID, and a reader joins them; none is ever rewritten.
// A fact holds no token, key, body or query string (MECH-014): a request
// is recorded by the digest of its canonical form.
type Fact struct {
	Kind FactKind
	At   time.Time

	RequestID string
	Attempt   int    // 1, or the attempt of a retried call
	Operation string // the operation that owns the work, when there is one

	// Who, as authentication established it: empty for a refusal before
	// any identity was proved, which never trusts a claimed one.
	Principal   string
	CallerKind  string
	Application string
	// Claimed is an identity a header on the request named (06-2:35,
	// x-atlas-on-behalf-of): recorded, and never trusted for anything
	// (ATLAS-ACCESS-007).
	Claimed string
	// Client is the calling client, as its Atlas-Client header names it
	// (§6.2.8: the client and edge).
	Client protocol.Client

	Environment    string
	PolicyRevision string
	Address        string // portfolio.holdings@v1:fetch
	Exact          string // the profile it ran under: the writer's (§6.2.8)
	// Expected is the profile the request was written under, as
	// Atlas-Profile-Expect named it: "" when it named none (§6.2.8: the
	// writer and expected fingerprints).
	Expected      string
	RequestDigest string // sha256 of the normalised request's RFC 8785 form

	// Step is the step of ATLAS-CORE §8 a refusal was refused at, or a
	// failed outcome failed at (ATLAS-MECH-003).
	Step Step
	// Code is the problem a refusal or a failed outcome ended with, or
	// CodeCancelled for a call whose caller went away.
	Code string
	// Rows and Truncated are a table outcome's.
	Rows      int64
	Truncated bool
	// Usage is what a settlement charges, by unit, and Source the source's
	// own record of the work, copied in at settlement (§6.2.8).
	Usage  map[string]string
	Source *Usage
	// Duration is from admission to the outcome.
	Duration time.Duration
}

// FactKind is which fact of the history it is.
type FactKind string

// The kinds of fact.
const (
	Admission  FactKind = "admission"  // the call was admitted, before its work was dispatched
	Refusal    FactKind = "refusal"    // the call was refused before dispatch
	Outcome    FactKind = "outcome"    // the work ended: answered, or failed
	Settlement FactKind = "settlement" // what the call used was charged
)

// Step is a step of the one execution path (ATLAS-CORE §8), as a fact
// names it.
type Step string

// The steps, in their order.
const (
	// StepTransport is before identity: the protocol a client declares,
	// the replica's room for another call, a deadline already passed.
	StepTransport Step = "transport"
	// StepIdentity is §8 step 1: the token, and the policy that gives the
	// caller's context.
	StepIdentity Step = "identity"
	// StepAddress is step 2: the address, its lifecycle, its critical
	// traits and the interaction's deadline.
	StepAddress Step = "address"
	// StepAccess is step 3: visibility, denies and grants.
	StepAccess Step = "access"
	// StepNegotiate is the representation chosen for the call, once its
	// access is decided and before its request is validated or admitted
	// (HANDOFF.md C2, R3-07): an Accept the binding cannot answer is
	// refused here, and no work runs.
	StepNegotiate Step = "negotiate"
	// StepRequest is step 4's validation, resolution and normalisation.
	StepRequest Step = "request"
	// StepScope is step 4's data scope over the values a request names.
	StepScope Step = "scope"
	// StepLimits is step 6's protective limits: rate and concurrency.
	StepLimits Step = "limits"
	// StepDispatch is step 7: the upstream's bulkhead, then the provider.
	StepDispatch Step = "dispatch"
	// StepAnswer is step 8: the answer held to its contract, the caller's
	// scope and current access, and the output cap.
	StepAnswer Step = "answer"
	// StepSettle is step 9's settlement, from the source's own record.
	StepSettle Step = "settle"
)

// CodeCancelled is the code of a fact whose call's caller went away before
// it ended: nothing more was answered (06-7 §6.7.3 logs it as 499). It is
// the audit's own, never a problem's on the wire.
const CodeCancelled = "cancelled"

// AuditSink records facts durably. An admission that cannot be recorded is
// not dispatched (ATLAS-CORE §8 step 7). Record honours ctx: it returns
// once ctx ends, recorded or not. It is called from every call's
// goroutine at once.
type AuditSink interface {
	Record(ctx context.Context, f Fact) error
}
