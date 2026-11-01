// The headers of §9.6.3: what this client writes on a call, and what it
// reads from every header it may see. A receiver ignores a header it does
// not know, and treats one that does not parse as its type as absent
// (§9.6.3); one that parses but breaks the grammar §9.6.3 gives it is absent
// too (Q-HDR-1, A-026). Each reader returns undefined for absent.

import * as grammar from './grammar.ts';
import { uri } from './schema.ts';
import {
  type BareItem, type Dictionary, type InnerList, type Item, isInnerList, parse, SfDate, SfDecimal, serialize, Token,
} from './sf.ts';

function item(field: string | null | undefined): Item | undefined {
  if (field === null || field === undefined) return undefined;
  try { return parse(field, 'item'); } catch { return undefined; }
}

function dictionary(field: string | null | undefined): Dictionary | undefined {
  if (field === null || field === undefined) return undefined;
  try { return parse(field, 'dictionary'); } catch { return undefined; }
}

function bare(m: Item | InnerList | undefined): BareItem | undefined {
  return m === undefined || isInnerList(m) ? undefined : m.value;
}

/** Atlas-Client: a Dictionary whose name, version and protocol are Strings, protocol a protocol-ver. */
export function readClient(field: string | null | undefined): { name: string; version: string; protocol: string } | undefined {
  const d = dictionary(field);
  if (!d) return undefined;
  const name = bare(d.get('name'));
  const version = bare(d.get('version'));
  const protocol = bare(d.get('protocol'));
  if (typeof name !== 'string' || typeof version !== 'string' || typeof protocol !== 'string' || !grammar.protocolVer(protocol)) return undefined;
  return { name, version, protocol };
}

/** What a client implements (Atlas-Client-Features): each member an Inner List of Tokens. */
export interface Features { levels: string[]; lens: string[]; deliveries: string[]; encodings: string[]; traits: string[]; handoff: string[] }

const FEATURE_MEMBERS = ['levels', 'lens', 'deliveries', 'encodings', 'traits', 'handoff'] as const;

export function readFeatures(field: string | null | undefined): Features | undefined {
  const d = dictionary(field);
  if (!d) return undefined;
  const out: Features = { levels: [], lens: [], deliveries: [], encodings: [], traits: [], handoff: [] };
  for (const name of FEATURE_MEMBERS) {
    const m = d.get(name);
    if (m === undefined) continue;
    if (!isInnerList(m) || !m.items.every((i) => i.value instanceof Token)) return undefined;
    out[name] = m.items.map((i) => (i.value as Token).value);
  }
  return out;
}

export function formatFeatures(f: Partial<Features>): string {
  const d: Dictionary = new Map();
  for (const name of FEATURE_MEMBERS) {
    const tokens = f[name];
    if (tokens && tokens.length > 0) d.set(name, { items: tokens.map((t) => ({ value: new Token(t), params: new Map() })), params: new Map() });
  }
  return serialize(d);
}

/** Atlas-Client written: name, version and protocol as Strings. */
export function formatClient(name: string, version: string, protocol: string): string {
  return serialize(new Map([['name', { value: name, params: new Map() }], ['version', { value: version, params: new Map() }], ['protocol', { value: protocol, params: new Map() }]]));
}

/** A String item's value, parameters ignored. */
function stringItem(field: string | null | undefined): string | undefined {
  const v = item(field)?.value;
  return typeof v === 'string' ? v : undefined;
}

/** Atlas-Profile and Atlas-Profile-Expect: a String that is an exact-id. */
export function readExact(field: string | null | undefined): string | undefined {
  const v = stringItem(field);
  return v !== undefined && grammar.exactId(v) ? v : undefined;
}

/** A String header's value: Atlas-Profile-Expect, Idempotency-Key. */
export function formatString(s: string): string {
  return serialize({ value: s, params: new Map() });
}

/** Atlas-Deadline, Deprecation: a Date, in seconds since the epoch. */
export function readDate(field: string | null | undefined): number | undefined {
  const v = item(field)?.value;
  return v instanceof SfDate ? v.seconds : undefined;
}

/** Atlas-Deadline written: the deadline in whole seconds, rounded down (Q-HDR-2). */
export function formatDeadline(epochMs: number): string {
  return serialize({ value: new SfDate(Math.floor(epochMs / 1000)), params: new Map() });
}

/** Idempotency-Key: a String of 1 to 255 of A-Z a-z 0-9 - _ . */
export function readIdempotencyKey(field: string | null | undefined): string | undefined {
  const v = stringItem(field);
  return v !== undefined && grammar.idempotencyKey(v) ? v : undefined;
}

/** Idempotency-Key written as the String §9.6.3 requires; undefined when the key breaks its grammar. */
export function formatIdempotencyKey(key: string): string | undefined {
  return grammar.idempotencyKey(key) ? formatString(key) : undefined;
}

/** Atlas-Execution: a Token, gateway, handoff or client. */
export function readExecution(field: string | null | undefined): string | undefined {
  const v = item(field)?.value;
  return v instanceof Token && ['gateway', 'handoff', 'client'].includes(v.value) ? v.value : undefined;
}

/** X-Atlas-Interactive: a Boolean. */
export function readInteractive(field: string | null | undefined): boolean | undefined {
  const v = item(field)?.value;
  return typeof v === 'boolean' ? v : undefined;
}

/** Atlas-Confirm: a Decimal (an Integer is absent, Q-HDR-4), as its exact text. */
export function readConfirm(field: string | null | undefined): string | undefined {
  const v = item(field)?.value;
  return v instanceof SfDecimal ? v.text : undefined;
}

/** Atlas-Governance-Revision: a String. */
export function readGovernanceRevision(field: string | null | undefined): string | undefined {
  return stringItem(field);
}

/** Atlas-Limit-Remaining: a Dictionary of Integers, parameters ignored. */
export function readLimits(field: string | null | undefined): Record<string, number> | undefined {
  const d = dictionary(field);
  if (!d) return undefined;
  const out: Record<string, number> = {};
  for (const [k, m] of d) {
    const v = bare(m);
    if (typeof v !== 'number') return undefined;
    out[k] = v;
  }
  return out;
}

/** Atlas-Error-Code: a Token that is a problem-code. */
export function readErrorCode(field: string | null | undefined): string | undefined {
  const v = item(field)?.value;
  return v instanceof Token && grammar.problemCode(v.value) ? v.value : undefined;
}

/** Retry-After: delay-seconds only (§9.6.3); an HTTP-date is not one. */
export function readRetryAfter(field: string | null | undefined): number | undefined {
  if (field === null || field === undefined || !/^[0-9]+$/.test(field.trim())) return undefined;
  const n = Number(field.trim());
  return Number.isSafeInteger(n) ? n : undefined;
}

/** Operation-Location: an absolute URL. */
export function readOperationLocation(field: string | null | undefined): string | undefined {
  if (field === null || field === undefined) return undefined;
  const v = field.trim();
  return uri(v) ? v : undefined;
}

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const LONG_DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

function instant(y: number, mon: number, d: number, h: number, mi: number, s: number, weekday: number): number | undefined {
  if (h > 23 || mi > 59 || s > 59 || d < 1) return undefined;
  const t = Date.UTC(2000, mon, d, h, mi, s);
  const probe = new Date(t);
  probe.setUTCFullYear(y);
  if (probe.getUTCMonth() !== mon || probe.getUTCDate() !== d || probe.getUTCDay() !== weekday) return undefined;
  return Math.floor(probe.getTime() / 1000);
}

/**
 * Sunset: an HTTP-date in any of RFC 9110's three forms, in GMT, with the
 * right day of the week (A-027). An rfc850 two-digit year more than 50
 * years ahead of now is the century before; now is given (Q-HDR-3).
 */
export function readHTTPDate(field: string | null | undefined, nowMs: number): number | undefined {
  if (field === null || field === undefined) return undefined;
  const v = field.trim();
  let m = /^(Mon|Tue|Wed|Thu|Fri|Sat|Sun), ([0-9]{2}) (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) ([0-9]{4}) ([0-9]{2}):([0-9]{2}):([0-9]{2}) GMT$/.exec(v);
  if (m) return instant(+m[4], MONTHS.indexOf(m[3]), +m[2], +m[5], +m[6], +m[7], DAYS.indexOf(m[1]));
  m = /^(Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday), ([0-9]{2})-(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)-([0-9]{2}) ([0-9]{2}):([0-9]{2}):([0-9]{2}) GMT$/.exec(v);
  if (m) {
    const now = new Date(nowMs).getUTCFullYear();
    let y = Math.floor(now / 100) * 100 + +m[4];
    if (y > now + 50) y -= 100;
    return instant(y, MONTHS.indexOf(m[3]), +m[2], +m[5], +m[6], +m[7], LONG_DAYS.indexOf(m[1]));
  }
  m = /^(Mon|Tue|Wed|Thu|Fri|Sat|Sun) (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) ([ 0-9][0-9]) ([0-9]{2}):([0-9]{2}):([0-9]{2}) ([0-9]{4})$/.exec(v);
  if (m) return instant(+m[7], MONTHS.indexOf(m[2]), +m[3].trim(), +m[4], +m[5], +m[6], DAYS.indexOf(m[1]));
  return undefined;
}

/** A challenge (RFC 9110 §11.6.1): its scheme and auth-params. */
export interface Challenge { scheme: string; params: Record<string, string> }

const TOKEN = "[!#$%&'*+.^_`|~0-9A-Za-z-]+";

/** WWW-Authenticate's first challenge, its parameters' names lowercased. */
export function readChallenge(field: string | null | undefined): Challenge | undefined {
  if (field === null || field === undefined) return undefined;
  const v = field.trim();
  const head = new RegExp(`^(${TOKEN})(?: +|$)`).exec(v);
  if (!head) return undefined;
  const params: Record<string, string> = {};
  let rest = v.slice(head[0].length);
  const param = new RegExp(`^(${TOKEN})[ \\t]*=[ \\t]*(?:(${TOKEN})|"((?:[^"\\\\]|\\\\[\\s\\S])*)")[ \\t]*(?:,[ \\t]*|$)`);
  while (rest !== '') {
    const m = param.exec(rest);
    if (!m) break; // a token68, or a second challenge: the first challenge's parameters are read
    params[m[1].toLowerCase()] = m[2] ?? m[3].replace(/\\([\s\S])/g, '$1');
    rest = rest.slice(m[0].length);
  }
  return { scheme: head[1], params };
}

/** Authorization: a Bearer credential (RFC 6750), the scheme case-insensitive. */
export function readBearer(field: string | null | undefined): string | undefined {
  if (field === null || field === undefined) return undefined;
  const m = /^[Bb][Ee][Aa][Rr][Ee][Rr] +([A-Za-z0-9\-._~+/]+=*)$/.exec(field.trim());
  return m ? m[1] : undefined;
}

/** If-Match: "*" or a list of entity-tags. */
export function readIfMatch(field: string | null | undefined): string[] | undefined {
  if (field === null || field === undefined) return undefined;
  const v = field.trim();
  if (v === '*') return ['*'];
  const tags = v.split(/[ \t]*,[ \t]*/);
  return tags.every(grammar.entityTag) ? tags : undefined;
}

/** Cache-Control: its directives. */
export function readCacheControl(field: string | null | undefined): string[] | undefined {
  if (field === null || field === undefined) return undefined;
  const ds = field.trim().split(/[ \t]*,[ \t]*/).filter((d) => d !== '');
  return ds.length > 0 && ds.every((d) => new RegExp(`^${TOKEN}(?:=(?:${TOKEN}|"[^"]*"))?$`).test(d)) ? ds : undefined;
}

/** traceparent (W3C Trace Context): version, trace-id and parent-id in lowercase hexadecimal, neither id all zeros. */
export function readTraceparent(field: string | null | undefined): { version: string; trace_id: string; parent_id: string; flags: string } | undefined {
  if (field === null || field === undefined) return undefined;
  const m = /^([0-9a-f]{2})-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})(-.*)?$/.exec(field.trim());
  if (!m || m[1] === 'ff' || (m[1] === '00' && m[5] !== undefined)) return undefined;
  if (/^0+$/.test(m[2]) || /^0+$/.test(m[3])) return undefined;
  return { version: m[1], trace_id: m[2], parent_id: m[3], flags: m[4] };
}

/** Every header of the vectors, read as Atlas reads it (AP-HTTP-001); now is for Sunset's two-digit years. */
export function readHeader(name: string, field: string | null | undefined, nowMs: number): unknown {
  switch (name.toLowerCase()) {
    case 'atlas-client': return readClient(field);
    case 'atlas-client-features': return readFeatures(field);
    case 'atlas-confirm': return readConfirm(field);
    case 'atlas-deadline': return readDate(field);
    case 'atlas-error-code': return readErrorCode(field);
    case 'atlas-execution': return readExecution(field);
    case 'atlas-governance-revision': return readGovernanceRevision(field);
    case 'atlas-limit-remaining': return readLimits(field);
    case 'atlas-profile': return readExact(field);
    case 'atlas-profile-expect': return readExact(field);
    case 'authorization': return readBearer(field);
    case 'cache-control': return readCacheControl(field);
    case 'deprecation': return readDate(field);
    case 'idempotency-key': return readIdempotencyKey(field);
    case 'if-match': return readIfMatch(field);
    case 'operation-location': return readOperationLocation(field);
    case 'retry-after': return readRetryAfter(field);
    case 'sunset': return readHTTPDate(field, nowMs);
    case 'www-authenticate': return readChallenge(field);
    case 'x-atlas-interactive': return readInteractive(field);
    case 'traceparent': return readTraceparent(field);
  }
  return undefined;
}
