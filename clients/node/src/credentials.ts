// The person's sign-in (§9.7.3) under R26 (docs/pass-2/REVIEW.md), in the
// shape the Go client keeps it (sdk/go/client.go), since every client on a
// machine shares the store: a credential is bound to the gateway it was
// issued at, with that gateway's authority, client, audience, scopes and
// token endpoint. An unauthenticated bootstrap cannot redirect it: the
// token is sent only to the gateway it is bound to, while that gateway's
// sign_in is the one it was issued under, and its refresh token is
// redeemed only at the bound token endpoint. Otherwise nothing is sent and
// the call is unauthenticated (A-160).
//
// The store is <home>/state/token-<environment>.json (token-store.schema.json),
// mode 0600, written atomically, refreshed within a minute of expires_at (0
// when its life is not known). Refreshes are serialised in this process,
// and across processes by an exclusive lock file beside the store, the one
// the Go client takes (A-161).

import { randomBytes } from 'node:crypto';
import { chmod, mkdir, open, rename, rm, stat } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';

import type { Clock } from './clock.ts';
import { validateDocument } from './documents.ts';
import { type Json, NotIJson, parseBytes } from './ijson.ts';
import { type AtlasProblem, type Obj, problem } from './problem.ts';

/** What a stored sign-in was issued for (R26): the members the Go client writes. */
export interface Binding {
  /** The origin of the gateway signed in at. */
  gateway: string;
  environment: string;
  authority: string;
  client_id: string;
  /** The resource the scopes are for; absent when they name none. */
  audience?: string;
  scopes: string[];
  token_endpoint: string;
}

/** A URL's origin, the identity a sign-in is bound to: http or https, a host, no user. */
export function originOf(url: string): string | undefined {
  try {
    const u = new URL(url.trim());
    if ((u.protocol !== 'https:' && u.protocol !== 'http:') || u.hostname === '' || u.username !== '' || u.password !== '') return undefined;
    return u.origin;
  } catch {
    return undefined;
  }
}

/** Whether an origin is this machine. */
export function loopback(origin: string): boolean {
  let h: string;
  try { h = new URL(origin).hostname; } catch { return false; }
  return h === 'localhost' || h === '[::1]' || /^127\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$/.test(h);
}

/** Whether a credential may travel to an origin: HTTPS (§9.6.1), or HTTP to this machine (RFC 8252 §8.3's loopback exception). */
export function secureOrigin(origin: string): boolean {
  return origin.startsWith('https://') || (origin.startsWith('http://') && loopback(origin));
}

/** The resource the scopes are for ("api://atlas-dev" for "api://atlas-dev/.default"), or "" when they name none or differ: the Go client's reading. */
export function audience(scopes: string[]): string {
  let out = '';
  for (let i = 0; i < scopes.length; i++) {
    const s = scopes[i];
    const cut = s.indexOf('://');
    if (cut <= 0) return '';
    const rest = s.slice(cut + 3);
    const j = rest.lastIndexOf('/');
    if (j <= 0) return '';
    const a = s.slice(0, cut) + '://' + rest.slice(0, j);
    if (i > 0 && a !== out) return '';
    out = a;
  }
  return out;
}

/** The binding the gateway at origin gives a sign-in made there now. */
export function bindingOf(origin: string, bootstrap: Obj): Binding {
  const signIn = bootstrap.sign_in as Obj;
  const scopes = (signIn.scopes as Json[]).map(String);
  // The members in the order the Go client writes them.
  const a = audience(scopes);
  return {
    gateway: origin, environment: String(bootstrap.environment), authority: String(signIn.authority), client_id: String(signIn.client_id),
    ...(a !== '' ? { audience: a } : {}), scopes, token_endpoint: String(signIn.token_endpoint),
  };
}

/** The first member in which a stored binding differs from the gateway's now, or undefined when it does not. */
export function bindingDiffers(stored: Binding, now: Binding): string | undefined {
  const sorted = (s: string[]) => [...s].sort().join(' ');
  if (stored.gateway !== now.gateway) return 'gateway';
  for (const k of ['environment', 'authority', 'client_id', 'token_endpoint'] as const) if (stored[k] !== now[k]) return k;
  if ((stored.audience ?? '') !== (now.audience ?? '')) return 'audience';
  if (sorted(stored.scopes) !== sorted(now.scopes)) return 'scopes';
  return undefined;
}

function asBinding(v: Json | undefined): Binding | undefined {
  if (v === null || typeof v !== 'object' || Array.isArray(v)) return undefined;
  if (!['gateway', 'environment', 'authority', 'client_id', 'token_endpoint'].every((k) => typeof v[k] === 'string')) return undefined;
  if (v.audience !== undefined && typeof v.audience !== 'string') return undefined;
  if (!Array.isArray(v.scopes) || !v.scopes.every((s) => typeof s === 'string')) return undefined;
  const b: Binding = { gateway: String(v.gateway), environment: String(v.environment), authority: String(v.authority), client_id: String(v.client_id), scopes: v.scopes as string[], token_endpoint: String(v.token_endpoint) };
  if (typeof v.audience === 'string' && v.audience !== '') b.audience = v.audience;
  return b;
}

/** Where a call's token comes from. The caller has checked the destination is the gateway connected to. */
export interface Credentials {
  /** The token to send, refreshed first when it is due. */
  token(signal: AbortSignal): Promise<string>;
  /** The token to send again after the gateway refused rejected as invalid_token: renewed, unless another program already did. */
  renew(signal: AbortSignal, rejected: string): Promise<string>;
}

/** A workload's own token (connect's token, or ATLAS_TOKEN): never renewed, and sent only to the gateway connected to. */
export class ExplicitToken implements Credentials {
  private readonly value: string;
  constructor(value: string) { this.value = value; }
  async token(): Promise<string> { return this.value; }
  async renew(): Promise<string> { return this.value; }
}

/** A refresh lock older than this is a crashed holder's, and is broken: the Go client's staleLock. */
export const LOCK_STALE_MS = 60_000;
/** How often a waiting refresh looks at the lock again. */
export const LOCK_POLL_MS = 25;
/** A token within this of its expires_at is refreshed first (§9.7.3). */
export const REFRESH_EARLY_MS = 60_000;
const MAX_STORE_BYTES = 64 << 10;
const MAX_TOKEN_ANSWER = 1 << 20;

interface StoredToken { access_token: string; expires_at: number; refresh_token?: string; binding?: Binding }

/** The path of an environment's store: one file directly under state/, or a refusal (R046). */
export function storePath(home: string, environment: string): string {
  if (!/^[a-z](-?[a-z0-9])*$/.test(environment)) throw problem('invalid_document', `the environment ${JSON.stringify(environment)} is not a name a sign-in file can have`, { target: '/environment' });
  const dir = resolve(home, 'state');
  const path = join(dir, `token-${environment}.json`);
  if (dirname(path) !== dir) throw problem('invalid_document', 'the environment names a sign-in file outside the store', { target: '/environment' });
  return path;
}

async function writeAtomic(path: string, text: string): Promise<void> {
  const dir = dirname(path);
  await mkdir(dir, { recursive: true, mode: 0o700 });
  await chmod(dir, 0o700);
  const tmp = join(dir, `.${randomBytes(8).toString('hex')}.tmp`);
  const h = await open(tmp, 'wx', 0o600);
  try {
    await h.writeFile(text);
    await h.sync();
  } finally {
    await h.close();
  }
  try {
    await rename(tmp, path);
  } catch (e) {
    await rm(tmp, { force: true });
    throw e;
  }
}

/** Writes a sign-in, bound when a binding is given: what `atlas login` does with a token it was given (R26). */
export async function writeStore(home: string, environment: string, token: StoredToken): Promise<string> {
  const path = storePath(home, environment);
  const doc: Obj = { access_token: token.access_token, expires_at: token.expires_at };
  if (token.refresh_token) doc.refresh_token = token.refresh_token;
  if (token.binding) doc.binding = token.binding as unknown as Obj;
  await writeAtomic(path, JSON.stringify(doc));
  return path;
}

function refused(why: string, next = 'Sign in to this gateway: atlas login --token <token>.'): AtlasProblem {
  return problem('unauthenticated', why, { next });
}

/** The shared store, for the gateway this client connected to. */
export class TokenStore implements Credentials {
  private readonly path: string;
  private readonly origin: string;
  private readonly now: Binding;
  private readonly clock: Clock;
  private readonly http: typeof fetch;
  private refreshing?: Promise<string>;

  constructor(home: string, origin: string, bootstrap: Obj, clock: Clock, http: typeof fetch) {
    this.now = bindingOf(origin, bootstrap);
    this.path = storePath(home, this.now.environment);
    this.origin = origin;
    this.clock = clock;
    this.http = http;
  }

  /** The stored sign-in, held to its permissions and its schema. */
  private async read(): Promise<StoredToken> {
    let st;
    try {
      st = await stat(this.path);
    } catch {
      throw refused(`you are not signed in to ${this.origin}`);
    }
    if ((st.mode & 0o077) !== 0) throw refused(`${this.path} can be read by others, so its token is not used`, 'chmod 600 the file, or sign in again.');
    let doc: Json;
    try {
      const h = await open(this.path, 'r');
      let bytes: Uint8Array;
      try {
        const buf = new Uint8Array(MAX_STORE_BYTES + 1);
        const { bytesRead } = await h.read(buf, 0, buf.length, 0);
        if (bytesRead > MAX_STORE_BYTES) throw new NotIJson(0, `larger than ${MAX_STORE_BYTES} bytes`);
        bytes = buf.subarray(0, bytesRead);
      } finally {
        await h.close();
      }
      doc = parseBytes(bytes);
    } catch (e) {
      throw refused(`${this.path} holds no token: ${e instanceof NotIJson ? e.message : 'it cannot be read'}`);
    }
    const o = (doc !== null && typeof doc === 'object' && !Array.isArray(doc) ? doc : {}) as Obj;
    // An expires_at left out reads as 0, its life not known (A-168): older clients wrote none.
    const fs = validateDocument('token-store', { expires_at: 0, ...o });
    if (fs.length > 0) throw refused(`${this.path} holds no token: it breaks token-store.schema.json at ${fs[0].pointer} (${fs[0].keyword})`);
    const binding = o.binding === undefined ? undefined : asBinding(o.binding);
    if (o.binding !== undefined && !binding) throw refused(`${this.path} holds no token: its binding cannot be read`);
    return { access_token: String(o.access_token), expires_at: typeof o.expires_at === 'number' ? o.expires_at : 0, refresh_token: typeof o.refresh_token === 'string' ? o.refresh_token : undefined, binding };
  }

  /**
   * Whether a stored sign-in may be sent to the gateway connected to (R26).
   * A bound one only at the gateway it was made at, while that gateway's
   * sign_in is the one it was issued under. An unbound one (an older client,
   * a test, the conformance suite) only to a local or conformance gateway on
   * this machine. Neither is ever sent in the clear off this machine.
   */
  private trusted(f: StoredToken): void {
    if (!secureOrigin(this.origin)) throw refused(`${this.origin} is plain HTTP and not on this machine, so no sign-in is sent to it`, "Use the gateway's https URL.");
    const b = f.binding;
    if (!b) {
      if (loopback(this.origin) && (this.now.environment === 'local' || this.now.environment === 'conformance')) return;
      throw refused(`the saved sign-in for ${this.now.environment} is not bound to a gateway, so it is not sent to ${this.origin}`);
    }
    if (b.gateway !== this.origin) throw refused(`the saved sign-in for ${this.now.environment} was made at ${b.gateway}, not at ${this.origin}, so it is not sent there`);
    const differs = bindingDiffers(b, this.now);
    if (differs) throw refused(`${this.origin}'s sign-in has changed since you signed in (its ${differs}), so the saved sign-in is not used`, 'Sign in again: atlas login --token <token>.');
  }

  private async usable(): Promise<StoredToken> {
    const f = await this.read();
    this.trusted(f);
    return f;
  }

  /** Whether a stored token is refreshed before it is sent: it is the one the gateway refused, or it ends within a minute. */
  private due(f: StoredToken, failed: string | undefined): boolean {
    if (!f.refresh_token) return false;
    return (failed !== undefined && f.access_token === failed) || (f.expires_at !== 0 && this.clock.now() + REFRESH_EARLY_MS >= f.expires_at * 1000);
  }

  async token(signal: AbortSignal): Promise<string> {
    return this.get(signal, undefined);
  }

  async renew(signal: AbortSignal, rejected: string): Promise<string> {
    return this.get(signal, rejected);
  }

  private async get(signal: AbortSignal, failed: string | undefined): Promise<string> {
    // A caller that has given up starts nothing (R3-05).
    if (signal.aborted) throw signal.reason;
    if (!this.refreshing) {
      const f = await raceSignal(this.usable(), signal);
      if (!this.due(f, failed)) return f.access_token;
      // The refresh is shared by every call that needs it, so no one call's
      // signal bounds it: it runs under its own bound, and each call waits no
      // longer than its own signal allows. One call's abort or deadline never
      // fails the others (R26, "every outbound call honours its deadline").
      // Its rejection is always observed here, so a refresh whose every
      // caller gave up never ends the process (R3-05).
      if (!this.refreshing) {
        const shared = this.locked(AbortSignal.timeout(LOCK_STALE_MS), failed).finally(() => { this.refreshing = undefined; });
        shared.catch(() => {});
        this.refreshing = shared;
      }
    }
    return raceSignal(this.refreshing, signal);
  }

  private async locked(signal: AbortSignal, failed: string | undefined): Promise<string> {
    const release = await this.lock(signal);
    try {
      const f = await this.usable(); // another program may have refreshed it while this one waited
      if (!this.due(f, failed)) return f.access_token;
      return await this.refresh(f, signal);
    } finally {
      await release();
    }
  }

  /** The lock file beside the store, made exclusively and removed when done; one older than LOCK_STALE_MS is broken. */
  private async lock(signal: AbortSignal): Promise<() => Promise<void>> {
    const path = this.path + '.lock';
    for (;;) {
      if (signal.aborted) throw signal.reason;
      try {
        const h = await open(path, 'wx', 0o600);
        try { await h.writeFile(`${process.pid}\n`); } finally { await h.close(); }
        return async () => { await rm(path, { force: true }); };
      } catch (e) {
        if ((e as NodeJS.ErrnoException).code !== 'EEXIST') throw problem('unavailable', `the sign-in cannot be locked: ${(e as Error).message}`, { retry_after: 1 });
      }
      // A file's age is the file system's: it is measured by the wall clock, which wrote its mtime.
      const st = await stat(path).catch(() => undefined);
      if (st && Date.now() - st.mtimeMs > LOCK_STALE_MS) {
        await rm(path, { force: true });
        continue;
      }
      await this.clock.sleep(LOCK_POLL_MS, signal);
    }
  }

  /** Redeems the refresh token at the token endpoint it was bound to (RFC 6749 §6), and rewrites the store whole, binding kept. */
  private async refresh(f: StoredToken, signal: AbortSignal): Promise<string> {
    const endpoint = f.binding?.token_endpoint ?? this.now.token_endpoint;
    const eo = originOf(endpoint);
    if (eo === undefined || !secureOrigin(eo) || (!f.binding && eo !== this.origin)) {
      throw refused(`the sign-in cannot be renewed at ${endpoint}: a refresh token goes only to the token endpoint it was issued for`, 'Sign in again: atlas login --token <token>.');
    }
    // The application and scopes are the binding's, as the endpoint is
    // (R3-03): Entra refuses a refresh that names no client_id.
    const app = f.binding ?? this.now;
    const form = new URLSearchParams({ grant_type: 'refresh_token', refresh_token: f.refresh_token! });
    if (app.client_id !== '') form.set('client_id', app.client_id);
    if (app.scopes.length > 0) form.set('scope', app.scopes.join(' '));
    let res: Response;
    try {
      res = await this.http(endpoint, {
        method: 'POST', headers: { 'content-type': 'application/x-www-form-urlencoded' },
        body: form.toString(),
        signal, redirect: 'error',
      });
    } catch (e) {
      if (signal.aborted) throw signal.reason;
      throw problem('unavailable', `the token endpoint cannot be reached: ${(e as Error).message}`, { retry_after: 5 });
    }
    let got: Json = null;
    try {
      const body = await readLimited(res, MAX_TOKEN_ANSWER, signal);
      got = res.status === 200 ? parseBytes(body) : null;
    } catch {
      if (signal.aborted) throw signal.reason;
      got = null;
    }
    const g = got !== null && typeof got === 'object' && !Array.isArray(got) ? got : {};
    if (res.status !== 200 || typeof g.access_token !== 'string' || g.access_token === '') {
      throw refused(`the sign-in could not be renewed (the token endpoint answered ${res.status})`, 'Sign in again: atlas login --token <token>.');
    }
    const next: StoredToken = {
      access_token: g.access_token,
      expires_at: typeof g.expires_in === 'number' && Number.isSafeInteger(g.expires_in) && g.expires_in > 0 ? Math.floor(this.clock.now() / 1000) + g.expires_in : 0,
      refresh_token: typeof g.refresh_token === 'string' && g.refresh_token !== '' ? g.refresh_token : f.refresh_token,
      binding: f.binding,
    };
    try {
      await writeStore(dirname(dirname(this.path)), this.now.environment, next);
    } catch (e) {
      throw problem('unavailable', `the renewed sign-in cannot be saved: ${(e as Error).message}`, { retry_after: 1 });
    }
    return next.access_token;
  }
}

/** A body read whole, refused past max bytes; the signal ends the read. */
export async function readLimited(res: Response, max: number, signal?: AbortSignal): Promise<Uint8Array> {
  const parts: Uint8Array[] = [];
  let n = 0;
  if (!res.body) return new Uint8Array();
  for await (const chunk of res.body as unknown as AsyncIterable<Uint8Array>) {
    if (signal?.aborted) throw signal.reason;
    n += chunk.length;
    if (n > max) throw new NotIJson(0, `larger than ${max} bytes`);
    parts.push(chunk);
  }
  const out = new Uint8Array(n);
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
}

/** p, or signal's reason if it aborts first; the listener is removed either way. */
function raceSignal<T>(p: Promise<T>, signal: AbortSignal): Promise<T> {
  if (signal.aborted) {
    p.catch(() => {}); // abandoned, its rejection observed (R3-05)
    return Promise.reject(signal.reason);
  }
  return new Promise<T>((resolve, reject) => {
    const onAbort = () => reject(signal.reason);
    signal.addEventListener('abort', onAbort, { once: true });
    p.then(
      (v) => { signal.removeEventListener('abort', onAbort); resolve(v); },
      (e) => { signal.removeEventListener('abort', onAbort); reject(e); },
    );
  });
}
