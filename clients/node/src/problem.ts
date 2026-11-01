// Problems (§9.3.3, §9.7.6): the registry of codes, and the thrown
// AtlasProblem that carries every member of a problem document as a
// property. A problem this client raises itself has the same shape as one
// the gateway sends, with the registry's status, retryable and title, so
// that Node and the Go client give the same code for the same failure.

import type { Json } from './ijson.ts';

export type Obj = { [k: string]: Json };

/** Every problem's type is this base followed by its code. */
export const PROBLEM_BASE = 'https://atlas.jhg.com/problems/';

export interface Registered { status: number; retryable: boolean; title: string }

/** The registry (§9.3.3), in its order. output_limit_exceeded travels in an end line, with status 200. */
export const REGISTRY: ReadonlyMap<string, Registered> = new Map([
  ['invalid_document', { status: 400, retryable: false, title: 'Invalid document' }],
  ['invalid_request', { status: 400, retryable: false, title: 'Invalid request' }],
  ['unauthenticated', { status: 401, retryable: false, title: 'Unauthenticated' }],
  ['not_granted', { status: 403, retryable: false, title: 'Not granted' }],
  ['denied', { status: 403, retryable: false, title: 'Denied' }],
  ['budget_exceeded', { status: 403, retryable: false, title: 'Budget exceeded' }],
  ['approval_required', { status: 403, retryable: false, title: 'Approval required' }],
  ['confirmation_required', { status: 428, retryable: false, title: 'Confirmation required' }],
  ['not_found', { status: 404, retryable: false, title: 'Not found' }],
  ['not_in_environment', { status: 404, retryable: false, title: 'Not in this environment' }],
  ['idempotency_conflict', { status: 409, retryable: false, title: 'Idempotency key reused' }],
  ['in_progress', { status: 409, retryable: true, title: 'In progress' }],
  ['resume_gap', { status: 409, retryable: false, title: 'Cannot resume' }],
  ['retired', { status: 410, retryable: false, title: 'Retired' }],
  ['precondition_failed', { status: 412, retryable: false, title: 'Precondition failed' }],
  ['critical_trait_unsupported', { status: 422, retryable: false, title: 'Critical trait unsupported' }],
  ['resolution_failed', { status: 422, retryable: false, title: 'Resolution failed' }],
  ['upgrade_required', { status: 426, retryable: false, title: 'Upgrade required' }],
  ['limit_exceeded', { status: 429, retryable: true, title: 'Limit exceeded' }],
  ['output_limit_exceeded', { status: 200, retryable: false, title: 'Output limit exceeded' }],
  ['internal', { status: 500, retryable: false, title: 'Internal error' }],
  ['upstream_failed', { status: 502, retryable: true, title: 'Upstream failed' }],
  ['unavailable', { status: 503, retryable: true, title: 'Unavailable' }],
  ['deadline_exceeded', { status: 504, retryable: false, title: 'Deadline exceeded' }],
]);

/** §9.7.6's exit codes, for a program that reports a problem the way the CLI does. */
export function exitCode(code: string): number {
  switch (code) {
    case 'invalid_request': return 2;
    case 'unauthenticated': return 3;
    case 'not_granted': case 'denied': case 'budget_exceeded': case 'approval_required': case 'confirmation_required': return 4;
    case 'not_found': case 'not_in_environment': case 'retired': return 5;
    case 'limit_exceeded': case 'unavailable': case 'in_progress': return 6;
    case 'deadline_exceeded': return 7;
    case 'resolution_failed': case 'critical_trait_unsupported': case 'upgrade_required': return 8;
  }
  return 1;
}

/** Error's own members, which a problem's member never replaces. */
const RESERVED = new Set(['name', 'message', 'stack', 'cause', 'problem']);

function str(v: Json | undefined): string | undefined {
  return typeof v === 'string' ? v : undefined;
}

/** A problem a call was answered with, or that this client raised (§9.7.6): every member of its document is a property. */
export class AtlasProblem extends Error {
  /** The problem document, every member as it came. */
  readonly problem: Obj;
  readonly type: string;
  readonly code: string;
  readonly status: number;
  readonly retryable: boolean;
  readonly title?: string;
  readonly detail?: string;
  readonly instance?: string;
  readonly target?: string;
  readonly next?: string;
  readonly retry_after?: number;
  readonly request_id?: string;
  readonly details: Obj[];
  readonly estimate?: Obj;

  constructor(problem: Obj) {
    const code = typeof problem.code === 'string' && problem.code !== '' ? problem.code : 'internal';
    const detail = str(problem.detail) ?? str(problem.title) ?? '';
    const target = typeof problem.target === 'string' && problem.target !== '' ? ` (${problem.target})` : '';
    super(`${code}: ${detail}${target}`);
    this.name = 'AtlasProblem';
    const registered = REGISTRY.get(code);
    this.problem = problem;
    this.code = code;
    this.type = str(problem.type) ?? PROBLEM_BASE + code;
    this.status = typeof problem.status === 'number' ? problem.status : registered?.status ?? 500;
    this.retryable = typeof problem.retryable === 'boolean' ? problem.retryable : registered?.retryable ?? false;
    this.title = str(problem.title);
    this.detail = str(problem.detail);
    this.instance = str(problem.instance);
    this.target = str(problem.target);
    this.next = str(problem.next);
    this.request_id = str(problem.request_id);
    this.retry_after = typeof problem.retry_after === 'number' ? problem.retry_after : undefined;
    const e = problem.estimate;
    this.estimate = e !== null && typeof e === 'object' && !Array.isArray(e) ? e : undefined;
    this.details = Array.isArray(problem.details) ? (problem.details.filter((d) => d !== null && typeof d === 'object' && !Array.isArray(d)) as Obj[]) : [];
    for (const [k, v] of Object.entries(problem)) {
      if (!RESERVED.has(k) && !Object.hasOwn(this, k)) Object.defineProperty(this, k, { value: v, enumerable: true });
    }
  }
}

const NEXT: Record<string, string> = {
  invalid_request: 'Correct the request at target; atlas describe gives its schema.',
  invalid_document: "The answer breaks the protocol: report it to the gateway's owner, with its request id.",
  unauthenticated: 'Sign in: atlas login.',
  not_granted: 'Call atlas.whoami@v1:fetch for your scope, or ask its owner for access.',
  not_found: 'atlas search finds capabilities; atlas describe lists their interactions and bindings.',
  critical_trait_unsupported: 'Use a client that implements the trait.',
  resolution_failed: "Call the capability's current profile, or upgrade this client.",
  limit_exceeded: 'Wait, then repeat the call.',
  upstream_failed: 'Repeat the call; if it fails again, report it with its request id.',
  unavailable: 'Repeat the call shortly.',
  deadline_exceeded: 'Give the call a longer deadline, or ask for less.',
  internal: 'Report it with its request id.',
};

/** A problem this client raises itself: the registry's status, retryable and title for its code. */
export function problem(code: string, detail: string, extra: Obj = {}): AtlasProblem {
  const r = REGISTRY.get(code);
  return new AtlasProblem({
    type: PROBLEM_BASE + code, title: r?.title ?? 'Internal error', status: r?.status ?? 500, code, retryable: r?.retryable ?? false,
    detail, next: NEXT[code] ?? NEXT.internal, ...extra,
  });
}

/**
 * The problem for an answer that failed without a problem document: its
 * code by its status, as the Go client reads one (sdk/go/call.go), so that
 * an ingress's HTML page is the same problem in every client (A-167).
 */
export function problemForStatus(status: number, requestId?: string): AtlasProblem {
  const code = ({ 400: 'invalid_request', 401: 'unauthenticated', 403: 'not_granted', 404: 'not_found', 409: 'in_progress', 429: 'limit_exceeded', 503: 'unavailable', 504: 'deadline_exceeded' } as Record<number, string>)[status] ?? 'upstream_failed';
  return problem(code, `the gateway answered ${status} without a problem document`, requestId ? { request_id: requestId } : {});
}
