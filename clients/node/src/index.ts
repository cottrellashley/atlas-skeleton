// Atlas from Node (ATLAS-CORE §6, §9.7): connect to a gateway from its
// bootstrap, sign in from the shared store under R26's binding, and call
// any interaction by its address with a request record, the same record the
// CLI and Python send. Every document received is strict I-JSON and is held
// to its schema; the request is held to its contract before it is sent and
// every row to the reader's contract after it is resolved (§9.5). A table
// without its end line is never complete, and a truncated one says so. The
// call's deadline bounds the whole exchange, the body included. An exact
// decimal stays its exact string.

import { createHash, randomBytes, randomUUID } from 'node:crypto';
import { mkdir, readFile, rename, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

import { type Clock, systemClock } from './clock.ts';
import { bindingOf, type Credentials, ExplicitToken, originOf, readLimited, secureOrigin, TokenStore, writeStore } from './credentials.ts';
import { validateDocument } from './documents.ts';
import { type End, NDJSONReader } from './frame.ts';
import * as grammar from './grammar.ts';
import { formatClient, formatDeadline, formatFeatures, formatIdempotencyKey, formatString, readChallenge, readDate, readExact, readHTTPDate, readRetryAfter } from './headers.ts';
import { canonical, fromValue, type Json, NotIJson, parseBytes } from './ijson.ts';
import { AtlasProblem, type Obj, problem, problemForStatus } from './problem.ts';
import { definition, project as projectRow, ResolutionFailure, resolve } from './resolve.ts';
import { badPatterns, type Failure, Validator } from './schema.ts';
import { readSettings } from './settings.ts';

export { canonical, Validator, AtlasProblem, AtlasProblem as AtlasError, problem };
export type { Failure, Json, Obj, Clock };
export { exitCode, REGISTRY } from './problem.ts';

export const VERSION = '0.3.0';
const PROTOCOL = 'atlas/1.0';
const CLIENT = formatClient('@atlas/sdk', VERSION, PROTOCOL);
// What this client implements: no lens operation, so the gateway resolves a chain that needs one (§9.5.6).
const FEATURES = formatFeatures({ levels: ['core', 'streaming'], deliveries: ['single', 'chunked'], encodings: ['json', 'ndjson'] });
const MEDIA: Record<string, string> = { json: 'application/json', ndjson: 'application/x-ndjson' };

/** The largest single-document answer, the bootstrap, a problem and a profile read from the cache. */
const MAX_ANSWER = 256 << 20;
const MAX_BOOTSTRAP = 4 << 20;
const MAX_PROBLEM = 1 << 20;
/** How many retries a retryable problem gets (§9.7.7). */
export const MAX_RETRIES = 3;
/** The deadline of what has no interaction to give one: the bootstrap, the built-ins, a profile. */
export const DEFAULT_DEADLINE_MS = 30_000;
const MAX_PROFILES = 256;

/** The built-ins' requests and results, by capability and interaction (builtins.schema.json). */
const BUILTINS: Record<string, [string, string?]> = {
  'atlas.whoami:fetch': ['builtins#whoami_request', 'builtins#whoami_result'],
  'atlas.catalog:fetch': ['builtins#catalog_request', 'catalog-page'],
  'atlas.search:fetch': ['builtins#search_request', 'catalog-page'],
  'atlas.describe:fetch': ['builtins#describe_request', 'describe'],
  'atlas.guide:fetch': ['builtins#guide_request', 'builtins#guide_result'],
  'atlas.access:fetch': ['builtins#access_request', 'page'],
  'atlas.usage:fetch': ['builtins#usage_request'],
  'atlas.setup:fetch': ['builtins#setup_request', 'builtins#setup_result'],
  'atlas.clients:fetch': ['builtins#clients_request'],
  'atlas.operation:get': ['builtins#operation_get_request', 'operation'],
  'atlas.operation:list': ['builtins#operation_list_request'],
};

/** Something an answer says beside its result: a deprecation, a truncation. */
export interface Warning { code: string; text: string }

/** Options of a connection. */
export interface ConnectOptions {
  /** The gateway; ATLAS_URL otherwise. */
  url?: string;
  /** The client's directory; ATLAS_HOME, else ~/.atlas. */
  home?: string;
  /** A workload's access token, instead of the person's sign-in; ATLAS_TOKEN otherwise. */
  token?: string;
  fetch?: typeof fetch;
  clock?: Clock;
  /** Uniform in [0, 1): the retries' jitter. */
  random?: () => number;
  /** Ends the connect, and with it every call of the client. */
  signal?: AbortSignal;
  /** Bounds reading the bootstrap. */
  deadlineMs?: number;
  /** Where a warning goes: process.emitWarning by default (§9.7.4 step 11). */
  onWarning?: (w: Warning) => void;
}

/** Options of one call: transport concerns, never part of the request record (§9.7.1). */
export interface CallOptions {
  /** The call's deadline from now; the interaction's default otherwise, never beyond its maximum. */
  deadlineMs?: number;
  /** The media types to ask for, in preference order. */
  accept?: string[];
  /** The call's logical execution, for an effect that is not read; a new one otherwise, kept for every retry. */
  idempotencyKey?: string;
  /** Ends the call, its answer's body included. */
  signal?: AbortSignal;
}

interface Profile {
  doc: Obj;
  exact: string;
  pinned: string;
  interactions: Obj;
  schemas: Obj;
  traits: { name: string; critical: boolean }[];
  previous?: string;
  lens: Json[];
  replacedBy?: string;
}

function isObject(v: unknown): v is Obj {
  return v !== null && typeof v === 'object' && !Array.isArray(v);
}

/** A profile's projection (§9.4.1): what its fingerprint is computed from. */
export function project(profile: Obj): Obj {
  const p = structuredClone(profile) as any;
  for (const k of ['fingerprint', 'revision', 'usage']) delete p[k];
  if (isObject(p.identity)) for (const k of ['title', 'summary', 'owner']) delete p.identity[k];
  if (Array.isArray(p.errors)) for (const e of p.errors) if (isObject(e)) { delete (e as any).description; delete (e as any).next; }
  if (isObject(p.evolution)) delete p.evolution.changelog;
  if (isObject(p.schemas)) for (const s of Object.values(p.schemas)) strip(s);
  return p;
}

function strip(s: any): void {
  if (s === null || typeof s !== 'object' || Array.isArray(s)) return;
  for (const k of ['title', 'description', '$comment', 'examples']) delete s[k];
  for (const k of ['properties', '$defs']) if (isObject(s[k])) for (const sub of Object.values(s[k])) strip(sub);
  strip(s.items);
  strip(s.additionalProperties);
  for (const k of ['prefixItems', 'anyOf', 'oneOf']) if (Array.isArray(s[k])) for (const sub of s[k]) strip(sub);
  if (Array.isArray(s['x-atlas-enum'])) for (const e of s['x-atlas-enum']) if (isObject(e)) delete (e as any).description;
  if (isObject(s['x-atlas-deprecated'])) delete (s['x-atlas-deprecated'] as any).reason;
}

/** A profile's fingerprint (§9.4): the first 16 hex digits of SHA-256 of its projection's RFC 8785 form. */
export function fingerprint(profile: Obj): string {
  return createHash('sha256').update(canonical(project(profile)), 'utf8').digest('hex').slice(0, 16);
}

/**
 * One call's exchange: its deadline and the caller's signal joined, so that
 * one abort ends the request, every retry's wait and the answer's body.
 */
class Exchange {
  readonly deadline: number;
  readonly signal: AbortSignal;
  private readonly controller = new AbortController();
  private readonly cancel: () => void;
  private readonly detach: () => void;

  constructor(clock: Clock, ms: number, outer: (AbortSignal | undefined)[]) {
    this.deadline = clock.now() + ms;
    this.signal = this.controller.signal;
    this.cancel = clock.after(ms, () => this.controller.abort(problem('deadline_exceeded', 'the call did not complete before its deadline')));
    const listeners: [AbortSignal, () => void][] = [];
    for (const s of outer) {
      if (!s) continue;
      if (s.aborted) { this.controller.abort(s.reason); break; }
      const on = () => this.controller.abort(s.reason);
      s.addEventListener('abort', on, { once: true });
      listeners.push([s, on]);
    }
    this.detach = () => { for (const [s, on] of listeners) s.removeEventListener('abort', on); };
  }

  /** The exchange is over: its timer and listeners go. */
  done(): void {
    this.cancel();
    this.detach();
  }

  /** What a failure becomes once the exchange is aborted: the deadline's problem, or the caller's reason. */
  reason(): unknown {
    return this.signal.reason;
  }

  /** A transport failure: the abort's reason when the exchange ended, else what why says. */
  broken(e: unknown, why: (e: unknown) => AtlasProblem): unknown {
    if (this.signal.aborted) return this.reason();
    return why(e);
  }
}

/** One call's answer: a record, or a table's rows, each checked as it arrives. */
export class Result implements AsyncIterable<Obj> {
  /** The call's id (X-Request-Id). */
  readonly requestId: string;
  /** The exact id the answer was written under (Atlas-Profile). */
  readonly profile: string;
  /** What the answer says beside its result: deprecations, truncation. */
  readonly warnings: Warning[] = [];
  /** A record answer. */
  record?: Obj;
  /** A streamed table's end line, once its rows are read. */
  end?: End;
  private kept?: Obj[];
  private next?: () => Promise<Obj | undefined>;
  private stop?: () => Promise<void>;
  private state: 'ready' | 'reading' | 'read' = 'ready';
  private failure?: unknown;
  private readonly warn: (w: Warning) => void;

  constructor(requestId: string, profile: string, warn: (w: Warning) => void) {
    this.requestId = requestId;
    this.profile = profile;
    this.warn = warn;
  }

  /** @internal A table held in memory: read as often as the caller likes. */
  hold(rows: Obj[]): void { this.kept = rows; }

  /** @internal A streamed table: read once, as it arrives. */
  stream(next: () => Promise<Obj | undefined>, stop: () => Promise<void>): void {
    this.next = next;
    this.stop = stop;
  }

  /** @internal */
  warning(w: Warning): void {
    this.warnings.push(w);
    this.warn(w);
  }

  /** Whether the answer is a table's rows. */
  get table(): boolean { return this.kept !== undefined || this.next !== undefined; }

  /** Whether the result is whole: a record, or a table whose end line said nothing was cut. */
  get complete(): boolean {
    if (this.next === undefined) return true;
    return this.end !== undefined && !this.truncated && this.end.problem === null;
  }

  /** Whether the table was cut at the caller's output limit (STREAM-004): its rows are only the first ones. */
  get truncated(): boolean {
    return this.end !== undefined && (this.end.truncated || this.end.problem?.code === 'output_limit_exceeded');
  }

  /** What the call used, from the end line. */
  get usage(): Obj | undefined { return this.end?.usage; }

  private replayed(): AtlasProblem {
    return problem('invalid_request', 'this result was streamed and has been read: a streamed table is read once (call rows() to keep its rows)', { target: 'result' });
  }

  async *[Symbol.asyncIterator](): AsyncIterator<Obj> {
    if (this.kept) { yield* this.kept; return; }
    if (!this.next) { if (this.record) yield this.record; return; }
    if (this.failure !== undefined) throw this.failure;
    if (this.state !== 'ready') throw this.replayed();
    this.state = 'reading';
    let finished = false;
    try {
      for (;;) {
        const row = await this.next();
        if (row === undefined) break;
        yield row;
      }
      finished = true;
    } catch (e) {
      this.failure = e;
      throw e;
    } finally {
      this.state = 'read';
      if (!finished) await this.stop!();
    }
  }

  /**
   * Every row, kept: materialising a table is the caller's choice (ATLAS-CORE
   * §6), and a result read this way can be read again, as often as the
   * caller likes, without another call (R11).
   */
  async rows(): Promise<Obj[]> {
    if (this.kept) return [...this.kept];
    if (!this.next) return this.record ? [this.record] : [];
    if (this.failure !== undefined) throw this.failure;
    if (this.state !== 'ready') throw this.replayed();
    const out: Obj[] = [];
    for await (const r of this) out.push(r);
    this.kept = out;
    return [...out];
  }
}

/** Reads the gateway's bootstrap (§9.6.1): unsigned, strict I-JSON, held to bootstrap.schema.json. */
async function readBootstrap(base: string, http: typeof fetch, x: Exchange): Promise<Obj> {
  let res: Response;
  try {
    res = await http(base + '/.well-known/atlas', { headers: { accept: 'application/json' }, signal: x.signal, redirect: 'error' });
  } catch (e) {
    throw x.broken(e, (err) => problem('unavailable', `the gateway cannot be reached: ${(err as Error).message ?? err}`, { next: "Check the gateway's URL and your network." }));
  }
  if (res.status !== 200) {
    await discard(res);
    throw problem('unavailable', `the gateway's bootstrap answered ${res.status}`);
  }
  let b: Json;
  try {
    b = parseBytes(await readLimited(res, MAX_BOOTSTRAP, x.signal));
  } catch (e) {
    if (e instanceof NotIJson) throw problem('invalid_document', `the bootstrap is not I-JSON: ${e.message}`);
    throw x.broken(e, () => problem('upstream_failed', 'the bootstrap was cut short'));
  }
  const fs = validateDocument('bootstrap', b);
  if (fs.length) throw problem('invalid_document', `the bootstrap breaks its schema at ${fs[0].pointer} (${fs[0].keyword})`, { target: fs[0].pointer });
  const o = b as Obj;
  if (!(o.protocols as Json[]).includes(PROTOCOL)) throw problem('upgrade_required', `the gateway does not serve ${PROTOCOL}`);
  return o;
}

function base(url: string): string {
  return url.replace(/\/+$/, '');
}

async function discard(res: Response): Promise<void> {
  try { await res.body?.cancel(); } catch { /* already gone */ }
}

function defaultWarning(w: Warning): void {
  process.emitWarning(w.text, { type: w.code === 'deprecated' ? 'DeprecationWarning' : 'AtlasWarning', code: w.code });
}

function traceparent(): string {
  return `00-${randomBytes(16).toString('hex')}-${randomBytes(8).toString('hex')}-01`;
}

/** A connection to one gateway. */
export class Client {
  readonly environment: string;
  readonly bootstrap: Obj;
  /** The gateway's URL, as connect was given it. */
  readonly url: string;
  /** The gateway's origin: the one place this client sends a credential (R26). */
  readonly origin: string;
  private readonly servers: Record<string, string> = {};
  private readonly credentials: Credentials;
  private readonly profiles = new Map<string, Profile>();
  private readonly current = new Map<string, string>();
  private readonly home: string;
  private readonly http: typeof fetch;
  private readonly clock: Clock;
  private readonly random: () => number;
  private readonly signal?: AbortSignal;
  private readonly onWarning: (w: Warning) => void;

  private constructor(url: string, origin: string, bootstrap: Obj, home: string, credentials: Credentials, opts: ConnectOptions) {
    this.url = url;
    this.origin = origin;
    this.bootstrap = bootstrap;
    this.environment = String(bootstrap.environment);
    for (const [name, s] of Object.entries(bootstrap.servers as Obj)) this.servers[name] = String((s as Obj).url);
    this.home = home;
    this.credentials = credentials;
    this.http = opts.fetch ?? fetch;
    this.clock = opts.clock ?? systemClock;
    this.random = opts.random ?? Math.random;
    this.signal = opts.signal;
    this.onWarning = opts.onWarning ?? defaultWarning;
  }

  /** Connect: read the gateway's bootstrap (§9.7.2). The first call signs in. */
  static async connect(opts: ConnectOptions = {}): Promise<Client> {
    const settings = readSettings();
    const url = base(opts.url ?? settings.url ?? '');
    if (!url) throw problem('invalid_request', 'no gateway: name one, or set ATLAS_URL', { next: 'atlas.connect({ url }), or export ATLAS_URL.' });
    const origin = originOf(url);
    if (origin === undefined) throw problem('invalid_request', `${url} is not a gateway URL: an http or https URL with a host`, { target: 'url' });
    const home = opts.home ?? settings.home;
    const clock = opts.clock ?? systemClock;
    const x = new Exchange(clock, opts.deadlineMs ?? DEFAULT_DEADLINE_MS, [opts.signal]);
    try {
      const bootstrap = await readBootstrap(url, opts.fetch ?? fetch, x);
      const token = opts.token ?? settings.token;
      const credentials = token ? new ExplicitToken(token) : new TokenStore(home, origin, bootstrap, clock, opts.fetch ?? fetch);
      return new Client(url, origin, bootstrap, home, credentials, opts);
    } finally {
      x.done();
    }
  }

  /** Call one interaction once (ATLAS-CORE §6, §9.7.4). */
  async call(address: string, request: unknown = {}, opts: CallOptions = {}): Promise<Result> {
    const addr = grammar.parseAddress(address);
    if (!addr) throw problem('invalid_request', `${address} is not an address: <id>@v<N>:<interaction>`, { target: 'address' });
    let body: Obj;
    try {
      const v = fromValue(request);
      if (!isObject(v)) throw problem('invalid_request', 'a request record is a JSON object', { target: '' });
      body = v;
    } catch (e) {
      if (e instanceof NotIJson) throw problem('invalid_document', `the request is not I-JSON: ${e.message}`);
      throw e;
    }
    if (opts.deadlineMs !== undefined && !(Number.isFinite(opts.deadlineMs) && opts.deadlineMs > 0)) {
      throw problem('invalid_request', `a deadline is a positive number of milliseconds, not ${opts.deadlineMs}`, { target: 'deadlineMs' });
    }
    if (opts.idempotencyKey !== undefined && !grammar.idempotencyKey(opts.idempotencyKey)) {
      throw problem('invalid_request', 'an idempotency key is 1 to 255 characters of A-Z a-z 0-9 - _ . (§9.6.3)', { target: 'idempotencyKey' });
    }
    for (const a of opts.accept ?? []) {
      if (!Object.values(MEDIA).includes(a)) throw problem('invalid_request', `this client reads ${Object.values(MEDIA).join(' and ')}, not ${a}`, { target: 'accept' });
    }
    if (addr.id.startsWith('atlas.')) return this.builtin(addr.id, addr.version ?? 1, addr.interaction, body, opts);
    const p = await this.profile(addr.version === undefined ? addr.id : `${addr.id}@v${addr.version}`, undefined, opts);
    for (const t of p.traits) {
      if (t.critical) throw problem('critical_trait_unsupported', `the capability's trait ${t.name} is critical, and this client does not implement it`);
    }
    const inter = p.interactions[addr.interaction];
    if (!isObject(inter)) throw problem('not_found', `${p.pinned} has no interaction ${addr.interaction}`, { next: `atlas describe ${p.pinned} lists its interactions.` });
    const takes = typeof inter.takes === 'string' ? p.schemas[inter.takes] : undefined;
    if (takes !== undefined) {
      const fs = new Validator(takes as Obj).validate(body);
      if (fs.length) throw invalidRequest(fs);
    }
    const binding = (Array.isArray(inter.bindings) ? inter.bindings : []).find((b) => isObject(b) && b.protocol === 'https'
      && ['single', 'chunked'].includes(String(b.delivery ?? inter.delivery))
      && (Array.isArray(b.encodings) ? b.encodings : ['json']).some((e) => e === 'json' || e === 'ndjson')) as Obj | undefined;
    if (!binding) throw problem('not_found', 'the interaction has no binding this client speaks', { next: 'atlas describe shows its bindings and what each needs.' });
    const deadline = isObject(inter.deadline) ? inter.deadline : {};
    let ms = opts.deadlineMs ?? grammar.durationMs(String(deadline.default)) ?? DEFAULT_DEADLINE_MS;
    const max = grammar.durationMs(String(deadline.max));
    if (max !== undefined && max > 0 && ms > max) ms = max; // CORE-DEADLINE-001: never past the maximum
    const headers: Record<string, string> = { 'atlas-profile-expect': formatString(p.exact) };
    if (inter.effect !== 'read') headers['idempotency-key'] = formatIdempotencyKey(opts.idempotencyKey ?? randomUUID())!;
    const encodings = (Array.isArray(binding.encodings) ? binding.encodings : ['json']).map(String);
    headers.accept = (opts.accept ?? ['ndjson', 'json'].filter((e) => encodings.includes(e)).map((e) => MEDIA[e])).join(', ');
    const traits = p.traits.map((t) => t.name);
    const columns = traits.includes('Projectable') && Array.isArray(body.columns) ? body.columns.map(String) : undefined;
    const x = new Exchange(this.clock, ms, [this.signal, opts.signal]);
    try {
      const res = await this.send(this.server(binding) + String(binding.address), body, headers, x);
      return await this.answer(res, x, { reader: p, name: addr.interaction, inter, columns, address: `${p.pinned}:${addr.interaction}`, dynamic: traits.includes('DynamicSchema') });
    } catch (e) {
      x.done();
      throw e;
    }
  }

  private server(binding: Obj): string {
    const s = this.servers[String(binding.server)];
    if (s === undefined) throw problem('invalid_document', `the binding names the server ${String(binding.server)}, which the bootstrap does not`);
    return s;
  }

  private async builtin(id: string, version: number, name: string, body: Obj, opts: CallOptions): Promise<Result> {
    const docs = BUILTINS[`${id}:${name}`];
    if (docs) {
      const fs = validateDocument(docs[0], body);
      if (fs.length) throw invalidRequest(fs);
    }
    const entry = (this.bootstrap.builtins as Obj)[id];
    const interactions = isObject(entry) && isObject(entry.interactions) ? entry.interactions : {};
    const bindings = isObject(interactions[name]) && Array.isArray((interactions[name] as Obj).bindings) ? (interactions[name] as Obj).bindings as Json[] : [];
    const binding = bindings.find((b) => isObject(b) && b.protocol === 'https') as Obj | undefined;
    if (!binding || (isObject(entry) && entry.version !== version)) throw problem('not_found', `the gateway's bootstrap names no binding for ${id}@v${version}:${name}`);
    const x = new Exchange(this.clock, opts.deadlineMs ?? DEFAULT_DEADLINE_MS, [this.signal, opts.signal]);
    try {
      const res = await this.send(this.server(binding) + String(binding.address), body, { accept: MEDIA.json }, x);
      return await this.answer(res, x, { document: docs?.[1], address: `${id}@v${version}:${name}` });
    } catch (e) {
      x.done();
      throw e;
    }
  }

  private async record(address: string, request: Obj = {}, opts: CallOptions = {}): Promise<Obj> {
    const r = await this.call(address, request, opts);
    if (!r.record) throw problem('invalid_document', `${address} answered no record`);
    return r.record;
  }

  /** The caller's effective context. */
  whoami(): Promise<Obj> { return this.record('atlas.whoami@v1:fetch'); }

  /** Every capability in the caller's working view, every page followed. */
  catalog(filter: Obj = {}): Promise<Obj[]> { return this.pages('atlas.catalog@v1:fetch', filter); }

  /** The capabilities that match words, best first. */
  search(words: string): Promise<Obj[]> { return this.pages('atlas.search@v1:fetch', { words }); }

  /** Why the caller can or cannot use each capability. */
  access(filter: Obj = {}): Promise<Obj[]> { return this.pages('atlas.access@v1:fetch', filter); }

  /** A capability's guide, in Node's syntax. */
  async guide(id: string, client = 'node'): Promise<string> { return String((await this.record('atlas.guide@v1:fetch', { by: 'id', id, client })).markdown); }

  /** A capability's describe answer; its profile is checked by its fingerprint and kept. */
  async describe(id: string): Promise<Obj> {
    const d = await this.record('atlas.describe@v1:fetch', { by: 'id', id });
    this.admit(d.profile as Obj);
    return d;
  }

  private async pages(address: string, request: Obj): Promise<Obj[]> {
    const out: Obj[] = [];
    for (let req = { ...request }, n = 0; n < 1000; n++) {
      const page = await this.record(address, req);
      out.push(...((page.items as Obj[]) ?? []));
      if (page.next_cursor === null || page.next_cursor === undefined) return out;
      req = { ...request, cursor: page.next_cursor };
    }
    throw problem('upstream_failed', 'the listing did not end after 1000 pages');
  }

  /** A profile checked as §9.4.2 says and read: a wrong fingerprint is invalid_document (CORE-DISC-001). */
  private admit(doc: Obj): Profile {
    const fs = validateDocument('profile', doc);
    if (fs.length) throw problem('invalid_document', `the profile breaks profile.schema.json at ${fs[0].pointer} (${fs[0].keyword})`, { target: fs[0].pointer });
    const fp = fingerprint(doc);
    if (fp !== doc.fingerprint) throw problem('invalid_document', `the profile carries fingerprint ${String(doc.fingerprint)}, and its projection's is ${fp}`, { target: '/fingerprint' });
    const identity = doc.identity as Obj;
    const exact = `${String(identity.id)}@v${String(identity.version)}#${fp}`;
    if (!grammar.exactId(exact)) throw problem('invalid_document', `the profile's identity ${exact} is not an exact-id`, { target: '/identity' });
    for (const [name, s] of Object.entries(doc.schemas as Obj)) {
      const bad = badPatterns(s as Obj, `/schemas/${name}`);
      if (bad.length) throw problem('invalid_document', `the profile's pattern at ${bad[0]} is not an I-Regexp (§9.2.4)`, { target: bad[0] });
    }
    const props = doc.properties as Obj;
    const evolution = isObject(doc.evolution) ? doc.evolution : {};
    const p: Profile = {
      doc, exact, pinned: `${String(identity.id)}@v${String(identity.version)}`,
      interactions: doc.interactions as Obj, schemas: doc.schemas as Obj,
      traits: (props.traits as Obj[]).map((t) => ({ name: String(t.name), critical: t.critical === true })),
      previous: typeof evolution.previous_profile === 'string' ? evolution.previous_profile : undefined,
      lens: Array.isArray(evolution.lens) ? evolution.lens : [],
      replacedBy: typeof props.replaced_by === 'string' ? props.replaced_by : undefined,
    };
    if (this.profiles.size >= MAX_PROFILES) this.profiles.delete(this.profiles.keys().next().value!);
    this.profiles.set(p.exact, p);
    return p;
  }

  /** A profile by pinned or bare id (the current one) or by exact id: from memory, the cache on disk, then the gateway, each fetched once. */
  private async profile(pinned: string | undefined, exact?: string, opts: CallOptions = {}): Promise<Profile> {
    exact ??= pinned === undefined ? undefined : this.current.get(pinned);
    if (exact) {
      const held = this.profiles.get(exact);
      if (held) return held;
      if (!grammar.exactId(exact)) throw problem('invalid_document', `${exact} is not an exact-id`);
      const cached = await this.cached(exact);
      if (cached) return cached;
    }
    const d = await this.record('atlas.describe@v1:fetch', exact ? { by: 'exact', exact } : { by: 'id', id: pinned! }, { deadlineMs: opts.deadlineMs, signal: opts.signal });
    const p = this.admit(d.profile as Obj);
    if (exact && p.exact !== exact) throw problem('invalid_document', `asked for ${exact}, the gateway described ${p.exact}`, { target: '/profile/fingerprint' });
    if (!exact && pinned !== undefined) this.current.set(pinned, p.exact);
    await this.keep(p);
    return p;
  }

  /** The profile cache (§9.7.5, R01): only the fingerprinted contract, never the caller's governance. */
  private cachePath(exact: string): string {
    return join(this.home, 'cache', 'profiles', exact + '.json');
  }

  private async cached(exact: string): Promise<Profile | undefined> {
    try {
      const doc = parseBytes(new Uint8Array(await readFile(this.cachePath(exact))), { maxBytes: MAX_ANSWER });
      if (!isObject(doc)) return undefined;
      const p = this.admit(doc);
      return p.exact === exact ? p : undefined;
    } catch {
      return undefined; // a cache is a convenience: a profile that does not read from it is fetched
    }
  }

  private async keep(p: Profile): Promise<void> {
    try {
      await mkdir(join(this.home, 'cache', 'profiles'), { recursive: true, mode: 0o700 });
      const tmp = this.cachePath(p.exact) + '.' + randomBytes(4).toString('hex');
      await writeFile(tmp, JSON.stringify(p.doc), { mode: 0o600, flag: 'wx' });
      await rename(tmp, this.cachePath(p.exact));
    } catch {
      // a cache is a convenience: the profile is held in memory regardless
    }
  }

  /**
   * POSTs a call with every header of §9.6.3: a token refreshed and the call
   * repeated once after invalid_token (§9.6.6); a retryable problem repeated
   * at most three times, after its retry_after or a backoff with jitter, and
   * never past the deadline (§9.7.7). Every request is replayable: its body
   * is a JSON record, never a stream.
   */
  private async send(url: string, body: Obj, extra: Record<string, string>, x: Exchange): Promise<Response> {
    const payload = canonical(body);
    const o = originOf(url);
    if (o === undefined) throw problem('invalid_document', `the bootstrap's binding ${url} is not an http or https URL`);
    if (o !== this.origin) throw problem('unauthenticated', `the gateway's bootstrap sends this call to ${o}, which is not the gateway connected to (${this.origin}), so no sign-in is sent there`, { next: `Connect to ${o} itself, if you trust it.` });
    if (!secureOrigin(o)) throw problem('unauthenticated', `${o} is plain HTTP and not on this machine, so no sign-in is sent to it`, { next: "Use the gateway's https URL." });
    let rejected: string | undefined; // the token the gateway refused as invalid_token
    let renewed = false;
    for (let retries = 0; ; ) {
      const token = rejected === undefined ? await this.credentials.token(x.signal) : await this.credentials.renew(x.signal, rejected);
      rejected = undefined;
      const headers: Record<string, string> = {
        ...extra, 'content-type': 'application/json', authorization: `Bearer ${token}`,
        'atlas-client': CLIENT, 'atlas-client-features': FEATURES, 'atlas-deadline': formatDeadline(x.deadline), traceparent: traceparent(),
      };
      let res: Response | undefined;
      let failure: AtlasProblem;
      try {
        res = await this.http(url, { method: 'POST', headers, body: payload, signal: x.signal, redirect: 'manual' });
      } catch (e) {
        if (x.signal.aborted) throw x.reason();
        res = undefined;
        failure = problem('unavailable', `the gateway cannot be reached: ${(e as Error).message ?? e}`, { retry_after: 1 });
      }
      if (res !== undefined) {
        const challenge = res.status === 401 ? readChallenge(res.headers.get('www-authenticate')) : undefined;
        if (challenge && !renewed && challenge.params.error === 'invalid_token' && challenge.params.claims === undefined) {
          await discard(res);
          renewed = true; // renewed, and the call repeated once (CORE-SIGNIN-003)
          rejected = token;
          continue;
        }
        const type = (res.headers.get('content-type') ?? '').split(';')[0].trim().toLowerCase();
        if (res.status < 300 && type !== 'application/problem+json') return res;
        failure = await this.problemOf(res, x);
        if (challenge?.params.claims !== undefined && !failure.details.some((d) => typeof d.challenge === 'string')) {
          failure = new AtlasProblem({ ...failure.problem, details: [...failure.details, { challenge: res.headers.get('www-authenticate') ?? '' }] });
        }
      }
      const wait = this.backoff(failure!, retries, res);
      if (wait === undefined || this.clock.now() + wait > x.deadline) throw failure!;
      await this.clock.sleep(wait, x.signal).catch(() => { throw x.reason(); });
      retries++;
    }
  }

  /** How long to wait before repeating a failure, or undefined when it is not repeated (§9.7.7, A-162). */
  private backoff(p: AtlasProblem, retries: number, res: Response | undefined): number | undefined {
    if (!p.retryable || retries >= MAX_RETRIES) return undefined;
    const header = readRetryAfter(res?.headers.get('retry-after'));
    if (header !== undefined) return header * 1000;
    if (p.retry_after !== undefined && Number.isSafeInteger(p.retry_after) && p.retry_after > 0) return p.retry_after * 1000;
    return Math.min(30, 0.5 * 2 ** retries) * 1000 * (1 + 0.25 * this.random());
  }

  /** A problem answer: its document, strict I-JSON with a code; else the status's problem, as the Go client reads it (R088). */
  private async problemOf(res: Response, x: Exchange): Promise<AtlasProblem> {
    const requestId = res.headers.get('x-request-id') ?? undefined;
    if (res.status >= 300 && res.status < 400) {
      await discard(res);
      return problem('upstream_failed', `the gateway answered ${res.status}, a redirect, which Atlas never sends`, { retryable: false, ...(requestId ? { request_id: requestId } : {}) });
    }
    let doc: Json = null;
    try {
      doc = parseBytes(await readLimited(res, MAX_PROBLEM, x.signal));
    } catch (e) {
      if (x.signal.aborted) throw x.reason();
      doc = null;
    }
    if (isObject(doc) && typeof doc.code === 'string' && grammar.problemCode(doc.code)) {
      return new AtlasProblem(typeof doc.status === 'number' ? doc : { ...doc, status: res.status });
    }
    return problemForStatus(res.status, requestId);
  }

  /** Reads an answer: a record, a table's rows as they arrive, written under the profile it names and resolved to the reader's. */
  private async answer(res: Response, x: Exchange, how: { reader?: Profile; name?: string; inter?: Obj; columns?: string[]; document?: string; address: string; dynamic?: boolean }): Promise<Result> {
    const requestId = res.headers.get('x-request-id') ?? '';
    const writerExact = readExact(res.headers.get('atlas-profile'));
    const r = new Result(requestId, writerExact ?? how.reader?.exact ?? '', this.onWarning);
    this.deprecation(res, r, how);
    let row: ((record: Obj, n: number) => Obj) | undefined;
    let shape: Obj | undefined;
    if (how.reader && how.inter) {
      const returns = typeof how.inter.returns === 'string' ? how.reader.schemas[how.inter.returns] : undefined;
      const table = isObject(returns) ? returns : undefined;
      const chain = await this.plan(how.reader, writerExact, x, how.dynamic === true);
      const resolved = chain && { writer: writerExact!, chain: chain.map((p) => ({ exact: p.exact, lens: p.lens, schema: schemaOf(p, how.name!, how.inter!.result === 'table') })) };
      if (table && how.inter.result === 'table') {
        const items = definition(table.items as Obj, table) as Obj;
        const rowSchema = projectRow(items, how.columns) as Obj;
        const check = new Validator({ ...rowSchema, ...(table.$defs ? { $defs: table.$defs } : {}) });
        row = (record, n) => {
          let v = record;
          if (resolved) {
            try {
              v = resolve(resolved.chain, resolved.writer, how.reader!.exact, record, how.columns, how.dynamic === true) as Obj;
            } catch (e) {
              if (e instanceof ResolutionFailure) throw problem('resolution_failed', `row ${n}: ${e.message}`, { target: e.target });
              throw e;
            }
          }
          const fs = check.validate(v);
          if (fs.length) throw problem('invalid_document', `row ${n} breaks the capability's contract at ${fs[0].pointer} (${fs[0].keyword})`, { target: fs[0].pointer });
          return v;
        };
      } else if (table && how.inter.result === 'record') {
        shape = table;
        row = (record) => {
          let v = record;
          if (resolved) {
            try {
              v = resolve(resolved.chain, resolved.writer, how.reader!.exact, record) as Obj;
            } catch (e) {
              if (e instanceof ResolutionFailure) throw problem('resolution_failed', e.message, { target: e.target });
              throw e;
            }
          }
          const fs = new Validator(table).validate(v);
          if (fs.length) throw problem('invalid_document', `the answer breaks the capability's contract at ${fs[0].pointer} (${fs[0].keyword})`, { target: fs[0].pointer });
          return v;
        };
      }
    }
    const type = (res.headers.get('content-type') ?? '').split(';')[0].trim().toLowerCase();
    if (type === MEDIA.ndjson) {
      const reader = new NDJSONReader(res.body as unknown as AsyncIterable<Uint8Array>, (e) => x.broken(e, (err) => problem('upstream_failed', `the answer is incomplete: ${(err as Error).message ?? err}`)));
      let n = 0;
      r.stream(async () => {
        try {
          const v = await reader.next();
          if (v !== undefined) return row ? row(v, ++n) : v;
          r.end = reader.end!;
          x.done();
          this.ended(r, n);
          return undefined;
        } catch (e) {
          x.done();
          await reader.close();
          throw e;
        }
      }, async () => { x.done(); await reader.close(); });
      return r;
    }
    let doc: Json;
    try {
      doc = parseBytes(await readLimited(res, MAX_ANSWER, x.signal));
    } catch (e) {
      if (e instanceof NotIJson) throw problem('invalid_document', `the answer is not I-JSON: ${e.message}`);
      throw x.broken(e, () => problem('upstream_failed', 'the answer was cut short'));
    } finally {
      x.done();
    }
    if (Array.isArray(doc)) {
      const rows: Obj[] = [];
      doc.forEach((e, i) => {
        if (!isObject(e)) throw problem('invalid_document', "a table's row is an object");
        rows.push(row && !shape ? row(e, i + 1) : e);
      });
      r.hold(rows);
      return r;
    }
    if (!isObject(doc)) throw problem('invalid_document', 'an answer is a record or a table');
    if (how.document) {
      const fs = validateDocument(how.document, doc);
      if (fs.length) throw problem('invalid_document', `${how.address} answered a document that breaks its schema at ${fs[0].pointer} (${fs[0].keyword})`, { target: fs[0].pointer });
    }
    r.record = shape && row ? row(doc, 1) : doc;
    return r;
  }

  /** The end of a streamed table: its problem raised after the rows (STREAM-003), a truncation a warning (STREAM-004). */
  private ended(r: Result, rows: number): void {
    const end = r.end!;
    const p = end.problem;
    if (p !== null && p.code !== 'output_limit_exceeded') throw new AtlasProblem(p);
    if (end.truncated || p !== null) {
      const text = typeof p?.detail === 'string' && p.detail !== '' ? p.detail : `the answer was cut at the caller's output limit after ${rows} rows: it is incomplete`;
      r.warning({ code: 'output_limit_exceeded', text });
    }
  }

  /** Deprecation and Sunset (§9.7.4 step 11, CORE-DEPR-001): a warning naming what, its sunset and its replacement. */
  private deprecation(res: Response, r: Result, how: { reader?: Profile; address: string }): void {
    const since = readDate(res.headers.get('deprecation'));
    if (since === undefined) return;
    const day = (s: number) => new Date(s * 1000).toISOString().slice(0, 10);
    let text = `${how.address} is deprecated since ${day(since)}`;
    const sunset = readHTTPDate(res.headers.get('sunset'), this.clock.now());
    if (sunset !== undefined) text += `, sunset ${day(sunset)}`;
    if (how.reader?.replacedBy) text += `; replaced by ${how.reader.replacedBy}`;
    r.warning({ code: 'deprecated', text });
  }

  /**
   * The chain from the writer's profile to the reader's (§9.5.1), oldest
   * first, each fetched by its exact id (§9.7.4 step 9); undefined when the
   * answer was written under the reader's own profile. A writer of another
   * version, or out of the reader's chain, is resolution_failed.
   */
  private async plan(reader: Profile, writer: string | undefined, x: Exchange, dynamic: boolean): Promise<Profile[] | undefined> {
    if (writer === undefined || writer === reader.exact || dynamic) return undefined;
    const within = () => ({ signal: x.signal, deadlineMs: Math.max(1, x.deadline - this.clock.now()) });
    const w = await this.profile(undefined, writer, within());
    if (w.pinned !== reader.pinned) throw problem('resolution_failed', `the answer was written under ${w.exact}, another version than ${reader.exact}: a client never resolves across versions`);
    const walk = async (from: Profile, to: string): Promise<Profile[] | undefined> => {
      const out = [from];
      for (let p = from; p.exact !== to; ) {
        if (!p.previous || out.length > 32) return undefined;
        p = await this.profile(undefined, p.previous, within());
        out.unshift(p);
      }
      return out;
    };
    const chain = (await walk(w, reader.exact)) ?? (await walk(reader, w.exact));
    if (!chain) throw problem('resolution_failed', `${w.exact} and ${reader.exact} are not in one chain of profiles`);
    const lensed = chain.slice(1).find((p) => p.lens.length > 0);
    if (lensed) throw problem('resolution_failed', `${lensed.exact} declares a lens, and this client implements no lens operation: the gateway resolves the answer for it (§9.5.6)`);
    return chain;
  }
}

/** A profile's schema for one interaction's result, for resolution: a table's row (with the table's $defs), or the record. */
function schemaOf(p: Profile, name: string, table: boolean): Obj {
  const inter = p.interactions[name];
  const s = isObject(inter) && typeof inter.returns === 'string' ? p.schemas[inter.returns] : undefined;
  if (!isObject(s)) return {};
  if (!table) return s;
  return { ...(definition(s.items as Obj, s) as Obj), ...(s.$defs ? { $defs: s.$defs } : {}) };
}

/** An interaction's request failed its schema: the first failing pointer is the target (§9.2.5, Q-VAL-1, Q-DOC-9). */
function invalidRequest(fs: Failure[]): AtlasProblem {
  return problem('invalid_request', `${fs[0].keyword}: ${fs[0].message}`, {
    target: fs[0].pointer,
    details: fs.map((f) => ({ code: 'invalid_request', target: f.pointer, keyword: f.keyword, detail: f.message })),
  });
}

/** Atlas, as the examples write it: `const client = await atlas.connect();`. */
export const atlas = { connect: Client.connect };

/** Options of a sign-in with a token already in hand. */
export interface SignInOptions {
  /** The gateway the token is for: its bootstrap gives the binding (R26). */
  url: string;
  home?: string;
  accessToken: string;
  refreshToken?: string;
  /** Seconds the access token lasts; expires_at is 0, its life not known, without it. */
  expiresIn?: number;
  fetch?: typeof fetch;
  clock?: Clock;
  signal?: AbortSignal;
}

/**
 * Writes the shared store for a gateway, as `atlas login --token` does: the
 * token bound to the gateway's origin and its sign_in's authority, client,
 * audience, scopes and token endpoint (R26), mode 0600 in a 0700
 * directory, atomically.
 */
export async function signIn(opts: SignInOptions): Promise<{ environment: string; path: string }> {
  const url = base(opts.url);
  const origin = originOf(url);
  if (origin === undefined) throw problem('invalid_request', `${url} is not an http or https URL`, { target: 'url' });
  if (!secureOrigin(origin)) throw problem('invalid_request', `${origin} is not HTTPS: a sign-in is kept only for an HTTPS gateway, or one on this machine`, { target: 'url' });
  const clock = opts.clock ?? systemClock;
  const x = new Exchange(clock, DEFAULT_DEADLINE_MS, [opts.signal]);
  try {
    if (!opts.accessToken) throw problem('invalid_request', 'a sign-in needs an access token', { target: 'accessToken' });
    const bootstrap = await readBootstrap(url, opts.fetch ?? fetch, x);
    const binding = bindingOf(origin, bootstrap);
    const known = opts.expiresIn !== undefined && Number.isSafeInteger(opts.expiresIn) && opts.expiresIn > 0;
    const path = await writeStore(opts.home ?? readSettings().home, binding.environment, {
      access_token: opts.accessToken, refresh_token: opts.refreshToken, expires_at: known ? Math.floor(clock.now() / 1000) + opts.expiresIn! : 0, binding,
    });
    return { environment: binding.environment, path };
  } finally {
    x.done();
  }
}
