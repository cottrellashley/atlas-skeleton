// A stand-in gateway for the Node SDK's tests: a real HTTP server on a
// loopback port, the seam the client really uses, serving the references'
// conformance fixture (spec/protocol/references/scenarios/fixtures/
// gateway.json): its bootstrap, every profile by exact id, the tokens, and
// the default behaviour of conformance.echo and conformance.rows (README,
// "The conformance capabilities"). A test scripts what else it answers,
// and reads back every request it received.

import { readFileSync } from 'node:fs';
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { join, resolve } from 'node:path';

import { parse, type Json } from '../src/ijson.ts';

export const REPO = resolve(import.meta.dirname, '..', '..', '..');
export const REFS = join(REPO, 'spec', 'protocol', 'references');

/** A file of the references, read strictly. */
export function refs(path: string): any {
  return parse(readFileSync(join(REFS, path), 'utf8'));
}

/** A request the stand-in received. */
export interface Seen {
  at: number;
  method: string;
  path: string;
  /** The address the path ends in (conformance.echo@v1:fetch, atlas.whoami:fetch). */
  address: string;
  headers: Record<string, string>;
  body: string;
  json?: any;
}

/** One scripted answer: when match says so, respond writes the answer. */
export interface Step {
  match: (s: Seen, nth: number) => boolean;
  respond: (s: Seen, res: ServerResponse) => void | Promise<void>;
  times?: number;
}

/** A problem document as the gateway writes one, every required member filled. */
export function problemDoc(code: string, status: number, extra: Record<string, Json> = {}): Record<string, Json> {
  return { type: `https://atlas.jhg.com/problems/${code}`, title: code, status, code, retryable: false, next: 'Do something else.', request_id: 'req-standin', ...extra };
}

export function sendProblem(res: ServerResponse, status: number, code: string, extra: Record<string, Json> = {}, headers: Record<string, string> = {}): void {
  res.writeHead(status, { 'content-type': 'application/problem+json', 'atlas-error-code': code, 'x-request-id': 'req-standin', ...headers });
  res.end(JSON.stringify(problemDoc(code, status, extra)));
}

export function sendJSON(res: ServerResponse, status: number, body: unknown, headers: Record<string, string> = {}): void {
  res.writeHead(status, { 'content-type': 'application/json', 'x-request-id': 'req-standin', ...headers });
  res.end(typeof body === 'string' ? body : JSON.stringify(body));
}

/** An NDJSON answer: the rows, then the end line (none when end is null), written as raw text when a string. */
export function sendNDJSON(res: ServerResponse, rows: unknown[], end: unknown, headers: Record<string, string> = {}): void {
  res.writeHead(200, { 'content-type': 'application/x-ndjson', 'x-request-id': 'req-standin', ...headers });
  for (const r of rows) res.write(JSON.stringify(r) + '\n');
  if (typeof end === 'string') res.write(end);
  else if (end !== null) res.write(JSON.stringify(end) + '\n');
  res.end();
}

/** An end line with every member, as the gateway writes it. */
export function endLine(rows: number, extra: Record<string, Json> = {}): Record<string, Json> {
  return { $atlas: 'end', rows, truncated: false, usage: { rows }, problem: null, ...extra };
}

/** Row i of conformance.rows (README, "The conformance capabilities"). */
export function conformanceRow(i: number): Record<string, Json> {
  const at = new Date(Date.UTC(2026, 0, 1, 0, 0, i)).toISOString().slice(0, 19) + '.' + String(i).padStart(6, '0') + 'Z';
  const day = new Date(Date.UTC(2026, 0, 1 + i)).toISOString().slice(0, 10);
  return { i, label: `row-${i}`, amount: (i * 1.5).toFixed(6), at, day, share: i % 5 === 4 ? null : i / 100 };
}

export interface StandInOptions {
  /** Changes the bootstrap before it is served ({gateway} already filled in). */
  bootstrap?: (b: any, url: string) => void;
  /** The time a request is stamped with; Date.now otherwise. */
  now?: () => number;
}

/** The stand-in conformance gateway. */
export class StandIn {
  url = '';
  readonly seen: Seen[] = [];
  readonly steps: Step[] = [];
  readonly profiles = new Map<string, any>();
  readonly current = new Map<string, string>();
  /** Access tokens it accepts, and refresh tokens it redeems. */
  readonly access = new Set<string>();
  readonly refresh = new Map<string, any>();
  /** The application its token endpoint redeems refresh tokens for, as Entra does: the fixture's client_id unless changed (R3-03). */
  application = '';
  /** How many answers the token endpoint has sent. */
  tokenAnswers = 0;
  bootstrap: any;
  private server!: Server;
  private readonly now: () => number;

  private constructor(opts: StandInOptions) {
    this.now = opts.now ?? Date.now;
  }

  static async start(opts: StandInOptions = {}): Promise<StandIn> {
    const s = new StandIn(opts);
    s.server = createServer((req, res) => { s.handle(req, res).catch((e) => { res.destroy(e); }); });
    await new Promise<void>((ok) => s.server.listen(0, '127.0.0.1', ok));
    s.url = `http://127.0.0.1:${(s.server.address() as AddressInfo).port}`;
    const fixture = refs('scenarios/fixtures/gateway.json');
    s.bootstrap = parse(JSON.stringify(fixture.bootstrap).replaceAll('{gateway}', s.url));
    s.application = String(fixture.bootstrap.sign_in.client_id);
    opts.bootstrap?.(s.bootstrap, s.url);
    for (const [exact, p] of Object.entries(fixture.profiles)) s.profiles.set(exact, p);
    for (const [pinned, exact] of Object.entries(fixture.current)) s.current.set(pinned, exact as string);
    for (const t of fixture.tokens.access) s.access.add(t);
    for (const [rt, answer] of Object.entries(fixture.tokens.refresh)) s.refresh.set(rt, answer);
    return s;
  }

  async close(): Promise<void> {
    this.server.closeAllConnections();
    await new Promise<void>((ok) => this.server.close(() => ok()));
  }

  /** The requests to an address, in order. */
  to(address: string): Seen[] {
    return this.seen.filter((s) => s.address === address);
  }

  /** Holds every answer of the token endpoint until the function it gives back is called. */
  holdTokens(): () => void {
    let release!: () => void;
    this.tokenGate = new Promise<void>((ok) => { release = ok; });
    return () => { this.tokenGate = undefined; release(); };
  }

  private tokenGate?: Promise<void>;

  /** Scripts the next answers to an address. */
  on(address: string, respond: Step['respond'], times = 1): void {
    this.steps.push({ match: (s) => s.address === address, respond, times });
  }

  private async handle(req: IncomingMessage, res: ServerResponse): Promise<void> {
    const chunks: Buffer[] = [];
    for await (const c of req) chunks.push(c as Buffer);
    const body = Buffer.concat(chunks).toString('utf8');
    const headers: Record<string, string> = {};
    for (const [k, v] of Object.entries(req.headers)) headers[k] = Array.isArray(v) ? v.join(', ') : String(v);
    const path = (req.url ?? '/').split('?')[0];
    const seen: Seen = { at: this.now(), method: req.method ?? 'GET', path, address: decodeURIComponent(path.slice(path.lastIndexOf('/') + 1)), headers, body };
    try { seen.json = body ? JSON.parse(body) : undefined; } catch { seen.json = undefined; }
    this.seen.push(seen);
    if (seen.method === 'GET' && path === '/.well-known/atlas') return sendJSON(res, 200, this.bootstrap, { etag: '"b1"' });
    if (path === '/oauth/token') {
      if (this.tokenGate) await this.tokenGate;
      this.tokenAnswers++;
      const form = new URLSearchParams(body);
      if ((form.get('client_id') ?? '') === '' || form.get('client_id') !== this.application) return sendJSON(res, 400, { error: 'invalid_client' });
      const answer = this.refresh.get(form.get('refresh_token') ?? '');
      if (form.get('grant_type') !== 'refresh_token' || !answer) return sendJSON(res, 400, { error: 'invalid_grant' });
      this.access.add(answer.access_token);
      return sendJSON(res, 200, answer);
    }
    const nth = this.seen.filter((s) => s.address === seen.address).length;
    const step = this.steps.find((s) => (s.times ?? 1) > 0 && s.match(seen, nth));
    const token = /^Bearer (.+)$/.exec(headers.authorization ?? '')?.[1];
    if (!token || !this.access.has(token)) {
      if (!step) return sendProblem(res, 401, 'unauthenticated', {}, { 'www-authenticate': 'Bearer realm="atlas", error="invalid_token"' });
    }
    if (step) {
      step.times = (step.times ?? 1) - 1;
      return step.respond(seen, res);
    }
    return this.serve(seen, res);
  }

  /** What the fixture's capabilities answer when nothing is scripted. */
  private serve(s: Seen, res: ServerResponse): void {
    const req = s.json ?? {};
    switch (s.address) {
      case 'atlas.describe:fetch': {
        const exact = req.by === 'exact' ? req.exact : this.current.get(String(req.id).includes('@') ? req.id : `${req.id}@v1`);
        const profile = exact && this.profiles.get(exact);
        if (!profile) return sendProblem(res, 404, 'not_found');
        const pinned = String(exact).slice(0, String(exact).indexOf('#'));
        const governance = { capability: pinned, governance_revision: 'g-1', for_caller: { interactions: {}, limits: [], approval: null } };
        return sendJSON(res, 200, { profile, governance, availability: 'available', eligibility: [] });
      }
      case 'atlas.whoami:fetch':
        return sendJSON(res, 200, { principal: '0b1e0000-0000-4000-8000-000000000001', name: 'Conformance Person', kind: 'person', roles: [], environment: this.bootstrap.environment, available_domains: ['conformance'], house_rules: { revision: 'h-1', rules: [] } });
      case 'conformance.echo@v1:fetch':
        return sendJSON(res, 200, { count: 1, kind: 'plain', ratio: null, when: null, tags: [], ...req }, { 'atlas-profile': `"${this.current.get('conformance.echo@v1')}"` });
      case 'conformance.rows@v1:fetch': {
        const rows = Array.from({ length: Number(req.n ?? 0) }, (_, i) => conformanceRow(i));
        const profile = { 'atlas-profile': `"${this.current.get('conformance.rows@v1')}"` };
        if ((s.headers.accept ?? '').startsWith('application/x-ndjson')) return sendNDJSON(res, rows, endLine(rows.length), profile);
        return sendJSON(res, 200, rows, profile);
      }
    }
    return sendProblem(res, 404, 'not_found');
  }
}
