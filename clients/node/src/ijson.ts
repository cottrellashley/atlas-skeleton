// I-JSON (§9.1.2, RFC 7493 on RFC 8259): the one reader every document
// this client receives goes through, and the one writer of every document
// it sends. JSON.parse cannot do this job: it keeps the last of two
// members with one name, gives Infinity for 1e400, and lets a lone
// surrogate or a noncharacter through; fetch's text() and json() also
// drop a byte-order mark and turn invalid UTF-8 into U+FFFD.
//
// The reader refuses, as the Go client's ijson does: invalid UTF-8; a
// byte-order mark; duplicate member names at any depth; unpaired
// surrogates and noncharacters, escaped or not; an unescaped control
// character; a number beyond binary64 (and an exponent of more than four
// digits, which can only mean zero or infinity); nesting deeper than 128;
// and trailing data. A number is read as its binary64 value, the value
// I-JSON gives it.

/** A JSON value as this client holds it. */
export type Json = null | boolean | number | string | Json[] | { [k: string]: Json };

/** Why a text is not I-JSON, and where (a UTF-16 offset into the text). */
export class NotIJson extends Error {
  readonly offset: number;
  readonly reason: string;
  constructor(offset: number, reason: string) {
    super(`not I-JSON at character ${offset}: ${reason}`);
    this.name = 'NotIJson';
    this.offset = offset;
    this.reason = reason;
  }
}

/** Bounds on a document, so a hostile one cannot exhaust a stack or memory. */
export interface Limits {
  /** Nesting of arrays and objects; 128 when not given. */
  maxDepth?: number;
  /** The largest document, in bytes, that parseBytes reads. */
  maxBytes?: number;
}

/** The nesting a document may have: the Go reader's limit, so both refuse the same documents. */
export const MAX_DEPTH = 128;

const BACKSLASH = 0x5c;
const QUOTE = 0x22;

/** A strict UTF-8 decoder that keeps a byte-order mark, so that it can be refused. */
export function utf8Decoder() {
  return new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });
}

/** The text of UTF-8 bytes; invalid UTF-8 is not I-JSON. */
export function decode(bytes: Uint8Array): string {
  try {
    return utf8Decoder().decode(bytes);
  } catch {
    throw new NotIJson(0, 'invalid UTF-8');
  }
}

/** One whole I-JSON document from its bytes. */
export function parseBytes(bytes: Uint8Array, limits: Limits = {}): Json {
  if (limits.maxBytes !== undefined && bytes.byteLength > limits.maxBytes) throw new NotIJson(0, `larger than ${limits.maxBytes} bytes`);
  if (bytes.byteLength >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf) throw new NotIJson(0, 'a byte-order mark');
  return parse(decode(bytes), limits);
}

/** One whole I-JSON document from its text. */
export function parse(text: string, limits: Limits = {}): Json {
  if (text.charCodeAt(0) === 0xfeff) throw new NotIJson(0, 'a byte-order mark');
  const r = new Reader(text, limits.maxDepth ?? MAX_DEPTH);
  r.space();
  const v = r.value(0);
  r.space();
  if (r.i !== text.length) throw r.fail('data after the document');
  return v;
}

/** Unicode's noncharacters: U+FDD0 to U+FDEF and the last two code points of every plane (RFC 7493 §2.1). */
export function noncharacter(cp: number): boolean {
  return (cp >= 0xfdd0 && cp <= 0xfdef) || (cp & 0xfffe) === 0xfffe;
}

function isDigit(c: number): boolean {
  return c >= 0x30 && c <= 0x39;
}

function hexValue(c: number): number {
  if (c >= 0x30 && c <= 0x39) return c - 0x30;
  if (c >= 0x61 && c <= 0x66) return c - 0x61 + 10;
  if (c >= 0x41 && c <= 0x46) return c - 0x41 + 10;
  return -1;
}

function hex4(cp: number): string {
  return cp.toString(16).toUpperCase().padStart(4, '0');
}

/** Sets a member as its own property: a member named __proto__ never reaches the prototype. */
function setMember(o: { [k: string]: Json }, name: string, v: Json): void {
  if (name === '__proto__') Object.defineProperty(o, name, { value: v, enumerable: true, writable: true, configurable: true });
  else o[name] = v;
}

class Reader {
  readonly s: string;
  readonly max: number;
  i = 0;
  constructor(s: string, max: number) {
    this.s = s;
    this.max = max;
  }

  fail(reason: string, at = this.i): NotIJson {
    return new NotIJson(at, reason);
  }

  space(): void {
    const s = this.s;
    while (this.i < s.length) {
      const c = s.charCodeAt(this.i);
      if (c !== 0x20 && c !== 0x09 && c !== 0x0a && c !== 0x0d) return;
      this.i++;
    }
  }

  value(depth: number): Json {
    const s = this.s;
    if (this.i >= s.length) throw this.fail('unexpected end');
    const c = s.charCodeAt(this.i);
    if (c === 0x7b) return this.object(depth + 1);
    if (c === 0x5b) return this.array(depth + 1);
    if (c === QUOTE) return this.string();
    if (c === 0x2d || isDigit(c)) return this.number();
    if (s.startsWith('true', this.i)) { this.i += 4; return true; }
    if (s.startsWith('false', this.i)) { this.i += 5; return false; }
    if (s.startsWith('null', this.i)) { this.i += 4; return null; }
    throw this.fail(`unexpected ${JSON.stringify(s[this.i])}`);
  }

  object(depth: number): Json {
    if (depth > this.max) throw this.fail(`nested deeper than ${this.max}`);
    const s = this.s;
    this.i++; // {
    const o: { [k: string]: Json } = {};
    this.space();
    if (s.charCodeAt(this.i) === 0x7d) { this.i++; return o; }
    for (;;) {
      this.space();
      if (s.charCodeAt(this.i) !== QUOTE) throw this.fail('a member name must be a string');
      const at = this.i;
      const name = this.string();
      if (Object.hasOwn(o, name)) throw this.fail(`duplicate member ${JSON.stringify(name)}`, at);
      this.space();
      if (s.charCodeAt(this.i) !== 0x3a) throw this.fail("a member name is followed by ':'");
      this.i++;
      this.space();
      setMember(o, name, this.value(depth));
      this.space();
      if (this.i >= s.length) throw this.fail('unexpected end in an object');
      const c = s.charCodeAt(this.i++);
      if (c === 0x7d) return o;
      if (c !== 0x2c) throw this.fail("members are separated by ','", this.i - 1);
    }
  }

  array(depth: number): Json {
    if (depth > this.max) throw this.fail(`nested deeper than ${this.max}`);
    const s = this.s;
    this.i++; // [
    const out: Json[] = [];
    this.space();
    if (s.charCodeAt(this.i) === 0x5d) { this.i++; return out; }
    for (;;) {
      this.space();
      out.push(this.value(depth));
      this.space();
      if (this.i >= s.length) throw this.fail('unexpected end in an array');
      const c = s.charCodeAt(this.i++);
      if (c === 0x5d) return out;
      if (c !== 0x2c) throw this.fail("elements are separated by ','", this.i - 1);
    }
  }

  number(): Json {
    const s = this.s;
    const start = this.i;
    if (s.charCodeAt(this.i) === 0x2d) this.i++;
    const first = s.charCodeAt(this.i);
    if (first === 0x30) this.i++;
    else if (first >= 0x31 && first <= 0x39) while (isDigit(s.charCodeAt(this.i))) this.i++;
    else throw this.fail('a number needs digits');
    if (s.charCodeAt(this.i) === 0x2e) {
      this.i++;
      if (!isDigit(s.charCodeAt(this.i))) throw this.fail('a fraction needs digits');
      while (isDigit(s.charCodeAt(this.i))) this.i++;
    }
    const e = s.charCodeAt(this.i);
    if (e === 0x65 || e === 0x45) {
      this.i++;
      const sign = s.charCodeAt(this.i);
      if (sign === 0x2b || sign === 0x2d) this.i++;
      if (!isDigit(s.charCodeAt(this.i))) throw this.fail('an exponent needs digits');
      const digits = this.i;
      while (isDigit(s.charCodeAt(this.i))) this.i++;
      // binary64 spans about 10^±308: a longer exponent can only mean zero
      // or infinity (the Go reader's rule, so both refuse the same text).
      if (this.i - digits > 4) throw this.fail('an exponent beyond binary64', start);
    }
    const n = Number(s.slice(start, this.i));
    if (!Number.isFinite(n)) throw this.fail('a number beyond binary64', start);
    return n;
  }

  string(): string {
    const s = this.s;
    this.i++; // "
    let out = '';
    let run = this.i;
    for (;;) {
      if (this.i >= s.length) throw this.fail('unterminated string');
      const c = s.charCodeAt(this.i);
      if (c === QUOTE) {
        out += s.slice(run, this.i);
        this.i++;
        return out;
      }
      if (c === BACKSLASH) {
        out += s.slice(run, this.i);
        out += this.escape();
        run = this.i;
        continue;
      }
      if (c < 0x20) throw this.fail('an unescaped control character in a string');
      if (c >= 0xd800 && c <= 0xdbff) {
        const lo = s.charCodeAt(this.i + 1);
        if (!(lo >= 0xdc00 && lo <= 0xdfff)) throw this.fail('an unpaired surrogate');
        const cp = 0x10000 + ((c - 0xd800) << 10) + (lo - 0xdc00);
        if (noncharacter(cp)) throw this.fail(`the noncharacter U+${hex4(cp)}`);
        this.i += 2;
        continue;
      }
      if (c >= 0xdc00 && c <= 0xdfff) throw this.fail('an unpaired surrogate');
      if (noncharacter(c)) throw this.fail(`the noncharacter U+${hex4(c)}`);
      this.i++;
    }
  }

  /** One escape, the reader at its backslash; returns the text it stands for. */
  escape(): string {
    const s = this.s;
    if (this.i + 1 >= s.length) throw this.fail('unterminated escape');
    const c = s.charCodeAt(this.i + 1);
    this.i += 2;
    switch (c) {
      case 0x22: return '"';
      case 0x5c: return '\\';
      case 0x2f: return '/';
      case 0x62: return '\b';
      case 0x66: return '\f';
      case 0x6e: return '\n';
      case 0x72: return '\r';
      case 0x74: return '\t';
      case 0x75: { // u: four hexadecimal digits, a surrogate pair as two escapes
        const hi = this.hex4();
        if (hi >= 0xd800 && hi <= 0xdbff) {
          if (s.charCodeAt(this.i) !== BACKSLASH || s.charCodeAt(this.i + 1) !== 0x75) throw this.fail('an unpaired surrogate');
          this.i += 2;
          const lo = this.hex4();
          if (!(lo >= 0xdc00 && lo <= 0xdfff)) throw this.fail('an unpaired surrogate');
          const cp = 0x10000 + ((hi - 0xd800) << 10) + (lo - 0xdc00);
          if (noncharacter(cp)) throw this.fail(`the noncharacter U+${hex4(cp)}`);
          return String.fromCharCode(hi, lo);
        }
        if (hi >= 0xdc00 && hi <= 0xdfff) throw this.fail('an unpaired surrogate');
        if (noncharacter(hi)) throw this.fail(`the noncharacter U+${hex4(hi)}`);
        return String.fromCharCode(hi);
      }
    }
    throw this.fail(`the unknown escape of ${JSON.stringify(String.fromCharCode(c))}`, this.i - 2);
  }

  hex4(): number {
    const s = this.s;
    let v = 0;
    for (let k = 0; k < 4; k++) {
      const h = hexValue(s.charCodeAt(this.i + k));
      if (h < 0) throw this.fail('a u-escape needs four hexadecimal digits');
      v = v * 16 + h;
    }
    this.i += 4;
    return v;
  }
}

/** Whether a string can travel in I-JSON: well formed, with no noncharacter. */
export function wellFormed(s: string): boolean {
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c >= 0xd800 && c <= 0xdbff) {
      const lo = s.charCodeAt(i + 1);
      if (!(lo >= 0xdc00 && lo <= 0xdfff)) return false;
      if (noncharacter(0x10000 + ((c - 0xd800) << 10) + (lo - 0xdc00))) return false;
      i++;
      continue;
    }
    if ((c >= 0xdc00 && c <= 0xdfff) || noncharacter(c)) return false;
  }
  return true;
}

function plainObject(v: object): boolean {
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

/**
 * A caller's value as an I-JSON value, or why it cannot be one. Nothing is
 * changed silently: NaN, Infinity, a BigInt, a Date, a Map, a class
 * instance, a function, an undefined array element, an ill-formed string
 * and a cycle are refused. A member whose value is undefined is absent, as
 * JavaScript writes absence.
 */
export function fromValue(v: unknown, at = '', depth = 0): Json {
  if (depth > MAX_DEPTH) throw new NotIJson(0, `${at || 'the value'} is nested deeper than ${MAX_DEPTH} (or holds itself)`);
  switch (typeof v) {
    case 'boolean':
      return v;
    case 'number':
      if (!Number.isFinite(v)) throw new NotIJson(0, `${at || 'the value'} is ${v}, which JSON cannot carry`);
      return v;
    case 'string':
      if (!wellFormed(v)) throw new NotIJson(0, `${at || 'the value'} holds an unpaired surrogate or a noncharacter`);
      return v;
    case 'object': {
      if (v === null) return null;
      if (Array.isArray(v)) {
        const out: Json[] = [];
        for (let i = 0; i < v.length; i++) {
          if (v[i] === undefined) throw new NotIJson(0, `${at}/${i} is undefined, which JSON cannot carry`);
          out.push(fromValue(v[i], `${at}/${i}`, depth + 1));
        }
        return out;
      }
      if (!plainObject(v)) throw new NotIJson(0, `${at || 'the value'} is a ${v.constructor?.name ?? 'non-plain object'}, not a JSON object`);
      const out: { [k: string]: Json } = {};
      for (const [k, m] of Object.entries(v)) {
        if (m === undefined) continue;
        if (!wellFormed(k)) throw new NotIJson(0, `a member name under ${at || 'the value'} holds an unpaired surrogate or a noncharacter`);
        setMember(out, k, fromValue(m, `${at}/${k.replaceAll('~', '~0').replaceAll('/', '~1')}`, depth + 1));
      }
      return out;
    }
  }
  throw new NotIJson(0, `${at || 'the value'} is a ${typeof v}, which JSON cannot carry`);
}

/** A JSON value in the JSON Canonicalization Scheme (RFC 8785): members sorted by UTF-16 code units, numbers in ECMAScript's shortest form. */
export function canonical(v: Json): string {
  if (v === null || typeof v !== 'object') {
    if (typeof v === 'number' && !Number.isFinite(v)) throw new NotIJson(0, `${v} is not a JSON number`);
    return JSON.stringify(v);
  }
  if (Array.isArray(v)) return '[' + v.map(canonical).join(',') + ']';
  const keys = Object.keys(v).sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));
  return '{' + keys.map((k) => JSON.stringify(k) + ':' + canonical(v[k])).join(',') + '}';
}

/** Whether two values are equal as JSON values: numbers by binary64 value (so -0 is 0), objects regardless of member order. */
export function equal(a: Json, b: Json): boolean {
  return canonical(a) === canonical(b);
}
