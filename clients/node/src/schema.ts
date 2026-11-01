// Validation (§9.2.5): JSON Schema 2020-12's semantics over the keywords a
// schema uses, the formats of §9.2.2 as assertions, patterns as I-Regexps
// (§9.2.4), and x-atlas-logical's constraint as if written as its own
// keywords (§9.2.3). The same validator holds a request to its contract
// before it is sent, an answer before it is used (§9.3 rule 1), and every
// protocol document against its schema (§9.7.4 step 10).
//
// A failure is the instance's JSON Pointer and the keyword that refused
// it; failures are sorted by pointer, then keyword, comparing code points,
// each pair once, so the first is invalid_request's target (Q-VAL-1,
// A-009). Numbers are binary64 values; an integer lies within I-JSON's
// range.

import { canonical, equal, type Json } from './ijson.ts';
import * as grammar from './grammar.ts';
import { translate } from './iregexp.ts';

export { canonical };
export type { Json };

export interface Failure { pointer: string; keyword: string; message: string }

type Schema = { [k: string]: any } | boolean;

/** Schemas a $ref may name by their $id: the protocol's documents. */
export class Registry {
  private readonly docs = new Map<string, Schema>();

  add(doc: Schema): void {
    if (typeof doc === 'object' && typeof doc.$id === 'string') this.docs.set(doc.$id, doc);
  }

  get(uri: string): Schema | undefined { return this.docs.get(uri); }
}

export interface ValidatorOptions {
  /** The documents a $ref to another document reaches. */
  registry?: Registry;
  /**
   * A document the gateway sent: an enum that includes UNKNOWN takes any
   * string, as if the value it does not know had been mapped to UNKNOWN
   * before validating (Q-DOC-1, §9.7.10).
   */
  tolerant?: boolean;
  /**
   * The protocol's own documents, which are 2020-12 schemas: a pattern that
   * is not ^R$ with R an I-Regexp is an ECMAScript expression, searched.
   */
  documents?: boolean;
}

const LOGICAL: Record<string, { pattern?: string; minLength?: number; format?: string }> = {
  Bp: {}, Percent: {}, Fraction: {}, Years: {}, Money: {},
  Currency: { pattern: '^[A-Z]{3}$' },
  Country: { pattern: '^[A-Z]{2}$' },
  Isin: { pattern: '^[A-Z]{2}[A-Z0-9]{9}[0-9]$' },
  Cusip: { pattern: '^[A-Z0-9]{8}[0-9]$' },
  Sedol: { pattern: '^[B-DF-HJ-NP-TV-Z0-9]{6}[0-9]$' },
  Figi: { pattern: '^BBG[B-DF-HJ-NP-TV-Z0-9]{8}[0-9]$' },
  Lei: { pattern: '^[A-Z0-9]{18}[0-9]{2}$' },
  Tenor: { pattern: '^[1-9][0-9]*[DWMY]$' },
  PortfolioCode: { minLength: 1 },
  IndexCode: { minLength: 1 },
  Path: { minLength: 1 },
  Principal: { format: 'uuid' },
  Uri: { format: 'uri' },
};

/** Decimal(p,s), p 1 to 38 and s 0 to p, written without spaces or leading zeros. */
export function decimalType(logical: unknown): { p: number; s: number } | undefined {
  if (typeof logical !== 'string') return undefined;
  const m = /^Decimal\(([1-9][0-9]?),(0|[1-9][0-9]?)\)$/.exec(logical);
  if (!m) return undefined;
  const p = +m[1];
  const s = +m[2];
  return p <= 38 && s <= p ? { p, s } : undefined;
}

const MONEY_AMOUNT: Schema = { type: 'string', format: 'decimal', 'x-atlas-logical': 'Decimal(38,6)' };
const MONEY_CURRENCY: Schema = { type: 'string', 'x-atlas-logical': 'Currency' };

const FORMATS: Record<string, (s: string) => boolean> = {
  date: grammar.date,
  'date-time': grammar.dateTime,
  duration: grammar.duration,
  decimal: grammar.decimal,
  int64: grammar.int64,
  uri,
  uuid: (s) => /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(s),
  'capability-id': grammar.capabilityId,
  'pinned-id': grammar.pinnedId,
  'exact-id': grammar.exactId,
  address: grammar.address,
};

const UNRESERVED_SUB = "-._~!$&'()*+,;=";

/** Unreserved, sub-delims, percent-encodings and the extra characters; none of except. ASCII only (A-010). */
function uriChars(s: string, extra: string, except = ''): boolean {
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (except.includes(c)) return false;
    if (c === '%') {
      if (!/^[0-9A-Fa-f]{2}$/.test(s.slice(i + 1, i + 3))) return false;
      i += 2;
      continue;
    }
    if (/^[A-Za-z0-9]$/.test(c) || UNRESERVED_SUB.includes(c) || extra.includes(c)) continue;
    return false;
  }
  return true;
}

function ipv6(s: string): boolean {
  // RFC 3986's IPv6address: up to eight groups of 1-4 hex digits, one "::", an optional trailing IPv4.
  const v4 = /^(?:25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])(?:\.(?:25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])){3}$/;
  let groups = s;
  let extra = 0;
  const last = s.lastIndexOf(':');
  if (last >= 0 && s.slice(last + 1).includes('.')) {
    if (!v4.test(s.slice(last + 1))) return false;
    groups = s.slice(0, last + 1) + '0';
    extra = 1;
  }
  const halves = groups.split('::');
  if (halves.length > 2) return false;
  const part = (h: string) => (h === '' ? [] : h.split(':'));
  const head = part(halves[0]);
  const tail = halves.length === 2 ? part(halves[1]) : [];
  if (![...head, ...tail].every((g) => /^[0-9A-Fa-f]{1,4}$/.test(g))) return false;
  const n = head.length + tail.length + extra;
  return halves.length === 2 ? n <= 7 : n === 8;
}

/** format uri (§9.2.2, A-010): an RFC 3986 URI with a scheme, which may have a query and a fragment; ASCII only. */
export function uri(s: string): boolean {
  const colon = s.indexOf(':');
  if (colon < 1 || !/^[A-Za-z][A-Za-z0-9+.-]*$/.test(s.slice(0, colon))) return false;
  let rest = s.slice(colon + 1);
  const hash = rest.indexOf('#');
  if (hash >= 0) {
    if (!uriChars(rest.slice(hash + 1), '/?:@')) return false;
    rest = rest.slice(0, hash);
  }
  const q = rest.indexOf('?');
  if (q >= 0) {
    if (!uriChars(rest.slice(q + 1), '/?:@')) return false;
    rest = rest.slice(0, q);
  }
  if (rest.startsWith('//')) {
    const after = rest.slice(2);
    const slash = after.indexOf('/');
    let authority = slash >= 0 ? after.slice(0, slash) : after;
    rest = slash >= 0 ? after.slice(slash) : '';
    const at = authority.lastIndexOf('@');
    if (at >= 0) {
      if (!uriChars(authority.slice(0, at), ':', '@')) return false;
      authority = authority.slice(at + 1);
    }
    let port = '';
    if (authority.startsWith('[')) {
      const end = authority.indexOf(']');
      if (end < 0) return false;
      const lit = authority.slice(1, end);
      if (/^[vV]/.test(lit)) {
        if (!/^[vV][0-9A-Fa-f]+\.[A-Za-z0-9\-._~!$&'()*+,;=:]+$/.test(lit)) return false;
      } else if (!ipv6(lit)) {
        return false;
      }
      const after = authority.slice(end + 1);
      if (after !== '') {
        if (!after.startsWith(':')) return false;
        port = after.slice(1);
      }
    } else {
      const c = authority.indexOf(':');
      const host = c >= 0 ? authority.slice(0, c) : authority;
      if (c >= 0) port = authority.slice(c + 1);
      if (!uriChars(host, '', ':@')) return false;
    }
    if (!/^[0-9]*$/.test(port)) return false;
  }
  return uriChars(rest, '/:@');
}

/** A JSON value's type as JSON Schema names it; an integer only within I-JSON's range. */
function jsonType(v: Json): string {
  if (v === null) return 'null';
  if (Array.isArray(v)) return 'array';
  if (typeof v === 'number') return Number.isSafeInteger(v) ? 'integer' : 'number';
  return typeof v;
}

function escapePointer(name: string): string {
  return name.replaceAll('~', '~0').replaceAll('/', '~1');
}

/** Code-point order (Q-VAL-1): UTF-16 order differs between astral characters and U+E000 to U+FFFF. */
export function compareCodePoints(a: string, b: string): number {
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    const x = a.codePointAt(i)!;
    const y = b.codePointAt(j)!;
    if (x !== y) return x < y ? -1 : 1;
    i += x > 0xffff ? 2 : 1;
    j += y > 0xffff ? 2 : 1;
  }
  return i < a.length ? 1 : j < b.length ? -1 : 0;
}

function describe(v: Json): string {
  if (typeof v === 'string') return JSON.stringify(v.length > 40 ? v.slice(0, 40) + '...' : v);
  if (Array.isArray(v)) return 'an array';
  if (v !== null && typeof v === 'object') return 'an object';
  return String(v);
}

const MAX_SCHEMA_DEPTH = 256;
const PATTERNS = new Map<string, RegExp | null>();
const MAX_PATTERNS = 4096;

/** A pattern as a whole-value I-Regexp; for a document, an ECMAScript expression searched if it is not one. Null when it is neither. */
export function compilePattern(pattern: string, documents: boolean): RegExp | null {
  const key = (documents ? 'd' : 'p') + pattern;
  const hit = PATTERNS.get(key);
  if (hit !== undefined) return hit;
  let re: RegExp | null = null;
  const t = translate(pattern);
  if (t instanceof RegExp) re = t;
  else if (documents) {
    try { re = new RegExp(pattern, 'u'); } catch { re = null; }
  }
  if (PATTERNS.size >= MAX_PATTERNS) PATTERNS.clear();
  PATTERNS.set(key, re);
  return re;
}

/** Every pattern of a schema that is not an I-Regexp, by its pointer in the schema: a profile with one cannot be used (§9.2.4). */
export function badPatterns(schema: Schema, at = ''): string[] {
  const out: string[] = [];
  const walk = (s: unknown, ptr: string, depth: number) => {
    if (depth > MAX_SCHEMA_DEPTH || s === null || typeof s !== 'object' || Array.isArray(s)) return;
    const o = s as { [k: string]: any };
    if (typeof o.pattern === 'string' && compilePattern(o.pattern, false) === null) out.push(`${ptr}/pattern`);
    for (const k of ['properties', '$defs']) {
      if (o[k] && typeof o[k] === 'object') for (const [n, sub] of Object.entries(o[k])) walk(sub, `${ptr}/${k}/${escapePointer(n)}`, depth + 1);
    }
    for (const k of ['items', 'additionalProperties', 'propertyNames', 'if', 'then', 'else', 'not']) walk(o[k], `${ptr}/${k}`, depth + 1);
    for (const k of ['prefixItems', 'anyOf', 'oneOf', 'allOf']) {
      if (Array.isArray(o[k])) o[k].forEach((sub: unknown, i: number) => walk(sub, `${ptr}/${k}/${i}`, depth + 1));
    }
  };
  walk(schema, at, 0);
  return out;
}

/** The value a JSON Pointer names in a document, or undefined. */
function pointerGet(doc: unknown, fragment: string): unknown {
  if (fragment === '') return doc;
  if (!fragment.startsWith('/')) return undefined;
  let v: any = doc;
  for (const raw of fragment.slice(1).split('/')) {
    const k = decodeURIComponent(raw).replaceAll('~1', '/').replaceAll('~0', '~');
    if (v === null || typeof v !== 'object' || !Object.hasOwn(v, k)) return undefined;
    v = v[k];
  }
  return v;
}

/** A schema, held ready to validate instances. */
export class Validator {
  private readonly root: Schema;
  private readonly opts: ValidatorOptions;

  constructor(root: Schema, opts: ValidatorOptions = {}) {
    this.root = root;
    this.opts = opts;
  }

  /** Every failure, sorted and each (pointer, keyword) once; none means valid. */
  validate(v: Json): Failure[] {
    const out: Failure[] = [];
    this.walk(this.root, this.root, v, '', out, 0);
    out.sort((a, b) => compareCodePoints(a.pointer, b.pointer) || compareCodePoints(a.keyword, b.keyword));
    return out.filter((f, i) => i === 0 || f.pointer !== out[i - 1].pointer || f.keyword !== out[i - 1].keyword);
  }

  valid(v: Json): boolean {
    const out: Failure[] = [];
    this.walk(this.root, this.root, v, '', out, 0);
    return out.length === 0;
  }

  /** Whether v is valid against a schema inside this one: its $refs reach this schema's $defs. */
  validAt(sub: Schema, v: Json): boolean {
    return this.ok(sub, this.root, v, 0);
  }

  private ok(schema: Schema, doc: Schema, v: Json, depth: number): boolean {
    const out: Failure[] = [];
    this.walk(schema, doc, v, '', out, depth);
    return out.length === 0;
  }

  /** A $ref's target and the document it is in. */
  private target(ref: string, doc: Schema): [Schema, Schema] | undefined {
    const hash = ref.indexOf('#');
    const uri = hash >= 0 ? ref.slice(0, hash) : ref;
    const fragment = hash >= 0 ? ref.slice(hash + 1) : '';
    let base = doc;
    if (uri !== '') {
      const other = this.opts.registry?.get(uri) ?? (typeof doc === 'object' && doc.$id === uri ? doc : undefined);
      if (other === undefined) return undefined;
      base = other;
    }
    const t = pointerGet(base, fragment);
    if (t === true || t === false || (t !== null && typeof t === 'object' && !Array.isArray(t))) return [t as Schema, base];
    return undefined;
  }

  private walk(schema: Schema, doc: Schema, v: Json, ptr: string, out: Failure[], depth: number): void {
    const fail = (keyword: string, message: string, at = ptr) => { out.push({ pointer: at, keyword, message }); };
    if (schema === true) return;
    if (schema === false) { fail('false', 'no value is allowed here'); return; }
    if (schema === null || typeof schema !== 'object' || Array.isArray(schema)) return;
    if (depth > MAX_SCHEMA_DEPTH) { fail('$ref', 'the schema nests too deeply'); return; }
    const s = schema as { [k: string]: any };
    if (typeof s.$ref === 'string') {
      const t = this.target(s.$ref, doc);
      if (t === undefined) fail('$ref', `${s.$ref} names no schema this client holds`);
      else this.walk(t[0], t[1], v, ptr, out, depth + 1);
    }
    const t = jsonType(v);
    if (s.type !== undefined) {
      const types: string[] = Array.isArray(s.type) ? s.type : [s.type];
      if (!types.includes(t) && !(t === 'integer' && types.includes('number'))) {
        fail('type', `${describe(v)} is not ${types.join(' or ')}`);
      }
    }
    if (Array.isArray(s.enum) && !s.enum.some((e: Json) => equal(e, v))) {
      if (!(this.opts.tolerant && typeof v === 'string' && s.enum.includes('UNKNOWN'))) fail('enum', `${describe(v)} is not one of the enum`);
    }
    if ('const' in s && !equal(s.const, v)) fail('const', `${describe(v)} is not the const`);
    const logical = s['x-atlas-logical'];
    if (typeof v === 'string') this.str(s, logical, v, fail);
    if (typeof v === 'number') this.number(s, v, fail);
    if (Array.isArray(v)) this.array(s, doc, v, ptr, out, depth, fail);
    if (t === 'object') this.object(s, doc, logical, v as { [k: string]: Json }, ptr, out, depth, fail);
    if (Array.isArray(s.allOf)) for (const b of s.allOf) this.walk(b, doc, v, ptr, out, depth + 1);
    if (s.if !== undefined) {
      const branch = this.ok(s.if, doc, v, depth + 1) ? s.then : s.else;
      if (branch !== undefined) this.walk(branch, doc, v, ptr, out, depth + 1);
    }
    if (s.not !== undefined && this.ok(s.not, doc, v, depth + 1)) fail('not', 'the value matches what not refuses');
    if (Array.isArray(s.anyOf) && !s.anyOf.some((b: Schema) => this.ok(b, doc, v, depth + 1))) fail('anyOf', 'the value matches none of anyOf');
    if (Array.isArray(s.oneOf)) {
      const n = s.oneOf.filter((b: Schema) => this.ok(b, doc, v, depth + 1)).length;
      if (n !== 1) fail('oneOf', `the value matches ${n} of oneOf, not exactly one`);
    }
  }

  private str(s: { [k: string]: any }, logical: unknown, v: string, fail: (k: string, m: string) => void): void {
    const lengthOf = () => { let n = 0; for (const _ of v) n++; return n; };
    const implied = typeof logical === 'string' ? LOGICAL[logical] : undefined;
    const minLength = Math.max(typeof s.minLength === 'number' ? s.minLength : -1, implied?.minLength ?? -1);
    if (minLength >= 0 || typeof s.maxLength === 'number') {
      const len = lengthOf();
      if (minLength >= 0 && len < minLength) fail('minLength', `${len} characters, fewer than ${minLength}`);
      if (typeof s.maxLength === 'number' && len > s.maxLength) fail('maxLength', `${len} characters, more than ${s.maxLength}`);
    }
    for (const pattern of [s.pattern, implied?.pattern]) {
      if (typeof pattern !== 'string') continue;
      const re = compilePattern(pattern, this.opts.documents === true);
      if (re === null || !re.test(v)) fail('pattern', `${describe(v)} does not match its pattern`);
    }
    for (const format of [s.format, implied?.format]) {
      if (typeof format === 'string' && FORMATS[format] && !FORMATS[format](v)) fail('format', `${describe(v)} is not a ${format}`);
    }
    const dec = decimalType(logical);
    if (dec && (!grammar.decimal(v) || !grammar.decimalFits(v, dec.p, dec.s))) fail('format', `${describe(v)} is not a Decimal(${dec.p},${dec.s})`);
  }

  private number(s: { [k: string]: any }, v: number, fail: (k: string, m: string) => void): void {
    if (typeof s.minimum === 'number' && v < s.minimum) fail('minimum', `${v} is below ${s.minimum}`);
    if (typeof s.maximum === 'number' && v > s.maximum) fail('maximum', `${v} is above ${s.maximum}`);
    if (typeof s.exclusiveMinimum === 'number' && v <= s.exclusiveMinimum) fail('exclusiveMinimum', `${v} is not above ${s.exclusiveMinimum}`);
    if (typeof s.exclusiveMaximum === 'number' && v >= s.exclusiveMaximum) fail('exclusiveMaximum', `${v} is not below ${s.exclusiveMaximum}`);
    if (typeof s.multipleOf === 'number' && s.multipleOf > 0 && v % s.multipleOf !== 0) fail('multipleOf', `${v} is not a multiple of ${s.multipleOf}`);
  }

  private array(s: { [k: string]: any }, doc: Schema, v: Json[], ptr: string, out: Failure[], depth: number, fail: (k: string, m: string, at?: string) => void): void {
    const prefix: Schema[] = Array.isArray(s.prefixItems) ? s.prefixItems : [];
    v.forEach((e, i) => {
      const at = `${ptr}/${i}`;
      if (i < prefix.length) this.walk(prefix[i], doc, e, at, out, depth + 1);
      else if (s.items === false) fail('items', 'no item is allowed here', at);
      else if (s.items !== undefined) this.walk(s.items, doc, e, at, out, depth + 1);
    });
    if (typeof s.minItems === 'number' && v.length < s.minItems) fail('minItems', `${v.length} items, fewer than ${s.minItems}`);
    if (typeof s.maxItems === 'number' && v.length > s.maxItems) fail('maxItems', `${v.length} items, more than ${s.maxItems}`);
    if (s.uniqueItems === true && new Set(v.map(canonical)).size !== v.length) fail('uniqueItems', 'items repeat');
  }

  private object(s: { [k: string]: any }, doc: Schema, logical: unknown, o: { [k: string]: Json }, ptr: string, out: Failure[], depth: number, fail: (k: string, m: string, at?: string) => void): void {
    const props: { [k: string]: Schema } = s.properties && typeof s.properties === 'object' ? s.properties : {};
    const required: string[] = Array.isArray(s.required) ? [...s.required] : [];
    const implied: { [k: string]: Schema } = {};
    if (logical === 'Money') {
      implied.amount = MONEY_AMOUNT;
      implied.currency = MONEY_CURRENCY;
      for (const r of ['amount', 'currency']) if (!required.includes(r)) required.push(r);
    }
    for (const [name, value] of Object.entries(o)) {
      const at = `${ptr}/${escapePointer(name)}`;
      if (s.propertyNames !== undefined && !this.ok(s.propertyNames, doc, name, depth + 1)) fail('propertyNames', `the member name ${JSON.stringify(name)} is not one this object takes`, at);
      const own = Object.hasOwn(props, name) ? props[name] : undefined;
      if (Object.hasOwn(implied, name)) this.walk(implied[name], doc, value, at, out, depth + 1);
      if (own !== undefined) { this.walk(own, doc, value, at, out, depth + 1); continue; }
      if (Object.hasOwn(implied, name)) continue;
      if (s.additionalProperties === false) fail('additionalProperties', `${JSON.stringify(name)} is not a member this object takes`, at);
      else if (s.additionalProperties !== undefined) this.walk(s.additionalProperties, doc, value, at, out, depth + 1);
    }
    for (const name of required) if (typeof name === 'string' && !Object.hasOwn(o, name)) fail('required', `${JSON.stringify(name)} is required`, `${ptr}/${escapePointer(name)}`);
    const n = Object.keys(o).length;
    if (typeof s.minProperties === 'number' && n < s.minProperties) fail('minProperties', `${n} members, fewer than ${s.minProperties}`);
    if (typeof s.maxProperties === 'number' && n > s.maxProperties) fail('maxProperties', `${n} members, more than ${s.maxProperties}`);
  }
}
