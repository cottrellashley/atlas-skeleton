// Structured Field Values for HTTP (RFC 9651): every Atlas header is one
// (§9.6.3). parse reads a field value as an Item, a List or a Dictionary,
// exactly as RFC 9651 §4.2 says, or throws; serialize writes one as §4.1
// says. Base64 padding that is missing is synthesised, and an "=" beyond
// what base64 needs fails the parse (Q-SF-1, A-018).

/** A Token (RFC 9651 §3.3.4). */
export class Token {
  readonly value: string;
  constructor(value: string) { this.value = value; }
}

/** A Decimal (§3.3.2), kept as its digits so that it is exact: at most 12 integer and 3 fraction digits. */
export class SfDecimal {
  readonly negative: boolean;
  readonly int: string;
  readonly frac: string;
  constructor(negative: boolean, int: string, frac: string) {
    this.negative = negative;
    this.int = int.replace(/^0+(?=[0-9])/, '');
    this.frac = frac.replace(/0+$/, '');
  }
  /** Its shortest exact text: at least one fraction digit, no sign on zero. */
  get text(): string {
    const zero = /^0*$/.test(this.int) && this.frac === '';
    return (this.negative && !zero ? '-' : '') + (this.int === '' ? '0' : this.int) + '.' + (this.frac === '' ? '0' : this.frac);
  }
}

/** A Byte Sequence (§3.3.5). */
export class ByteSequence {
  readonly bytes: Uint8Array;
  constructor(bytes: Uint8Array) { this.bytes = bytes; }
}

/** A Date (§3.3.7): seconds since the epoch. */
export class SfDate {
  readonly seconds: number;
  constructor(seconds: number) { this.seconds = seconds; }
}

/** A Display String (§3.3.8). */
export class DisplayString {
  readonly value: string;
  constructor(value: string) { this.value = value; }
}

export type BareItem = number | string | boolean | Token | SfDecimal | ByteSequence | SfDate | DisplayString;
export type Parameters = Map<string, BareItem>;
export interface Item { value: BareItem; params: Parameters }
export interface InnerList { items: Item[]; params: Parameters }
export type Member = Item | InnerList;
export type List = Member[];
export type Dictionary = Map<string, Member>;

export function isInnerList(m: Member): m is InnerList {
  return Array.isArray((m as InnerList).items);
}

/** Why a field value does not parse. */
export class NotStructured extends Error {
  constructor(why: string) {
    super(`not a Structured Field: ${why}`);
    this.name = 'NotStructured';
  }
}

const TCHAR = "!#$%&'*+-.^_`|~";

function digit(c: string): boolean { return c >= '0' && c <= '9'; }
function alpha(c: string): boolean { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'); }
function lcalpha(c: string): boolean { return c >= 'a' && c <= 'z'; }
function tchar(c: string): boolean { return alpha(c) || digit(c) || TCHAR.includes(c); }

class Parser {
  readonly s: string;
  i = 0;
  constructor(s: string) { this.s = s; }

  fail(why: string): never { throw new NotStructured(`${why} at character ${this.i}`); }
  empty(): boolean { return this.i >= this.s.length; }
  peek(): string { return this.s[this.i] ?? ''; }

  sp(): void { while (this.peek() === ' ') this.i++; }
  ows(): void { while (this.peek() === ' ' || this.peek() === '\t') this.i++; }

  list(): List {
    const out: List = [];
    while (!this.empty()) {
      out.push(this.member());
      this.ows();
      if (this.empty()) return out;
      if (this.peek() !== ',') this.fail('members are separated by ","');
      this.i++;
      this.ows();
      if (this.empty()) this.fail('a trailing comma');
    }
    return out;
  }

  dictionary(): Dictionary {
    const out: Dictionary = new Map();
    while (!this.empty()) {
      const key = this.key();
      let member: Member;
      if (this.peek() === '=') {
        this.i++;
        member = this.member();
      } else {
        member = { value: true, params: this.params() };
      }
      out.set(key, member);
      this.ows();
      if (this.empty()) return out;
      if (this.peek() !== ',') this.fail('members are separated by ","');
      this.i++;
      this.ows();
      if (this.empty()) this.fail('a trailing comma');
    }
    return out;
  }

  member(): Member {
    return this.peek() === '(' ? this.innerList() : this.item();
  }

  innerList(): InnerList {
    this.i++; // (
    const items: Item[] = [];
    while (!this.empty()) {
      this.sp();
      if (this.peek() === ')') {
        this.i++;
        return { items, params: this.params() };
      }
      items.push(this.item());
      const c = this.peek();
      if (c !== ' ' && c !== ')') this.fail('inner list items are separated by a space');
    }
    return this.fail('an inner list without its ")"');
  }

  item(): Item {
    const value = this.bareItem();
    return { value, params: this.params() };
  }

  params(): Parameters {
    const out: Parameters = new Map();
    while (this.peek() === ';') {
      this.i++;
      this.sp();
      const key = this.key();
      let value: BareItem = true;
      if (this.peek() === '=') {
        this.i++;
        value = this.bareItem();
      }
      out.set(key, value);
    }
    return out;
  }

  key(): string {
    const c = this.peek();
    if (!(lcalpha(c) || c === '*')) this.fail('a key starts with a-z or *');
    const start = this.i;
    while (!this.empty()) {
      const d = this.peek();
      if (!(lcalpha(d) || digit(d) || d === '_' || d === '-' || d === '.' || d === '*')) break;
      this.i++;
    }
    return this.s.slice(start, this.i);
  }

  bareItem(): BareItem {
    const c = this.peek();
    if (c === '-' || digit(c)) return this.number();
    if (c === '"') return this.string();
    if (c === '*' || alpha(c)) return this.token();
    if (c === ':') return this.bytes();
    if (c === '?') return this.boolean();
    if (c === '@') return this.date();
    if (c === '%') return this.displayString();
    return this.fail('not an item');
  }

  number(): number | SfDecimal {
    let decimal = false;
    let negative = false;
    let num = '';
    if (this.peek() === '-') { negative = true; this.i++; }
    if (this.empty()) this.fail('an empty number');
    if (!digit(this.peek())) this.fail('a number needs a digit');
    while (!this.empty()) {
      const c = this.peek();
      if (digit(c)) {
        num += c;
      } else if (!decimal && c === '.') {
        if (num.length > 12) this.fail('a decimal with more than 12 integer digits');
        num += c;
        decimal = true;
      } else {
        break;
      }
      this.i++;
      if (!decimal && num.length > 15) this.fail('an integer of more than 15 digits');
      if (decimal && num.length > 16) this.fail('a decimal of more than 16 characters');
    }
    if (!decimal) {
      const n = Number(num);
      return negative ? (n === 0 ? 0 : -n) : n;
    }
    if (num.endsWith('.')) this.fail('a decimal ending with "."');
    const [int, frac] = num.split('.');
    if (frac.length > 3) this.fail('a decimal with more than 3 fraction digits');
    return new SfDecimal(negative, int, frac);
  }

  string(): string {
    this.i++; // "
    let out = '';
    while (!this.empty()) {
      const c = this.s[this.i++];
      if (c === '\\') {
        if (this.empty()) this.fail('a string ending in a backslash');
        const n = this.s[this.i++];
        if (n !== '"' && n !== '\\') this.fail('a backslash escapes only " and itself');
        out += n;
      } else if (c === '"') {
        return out;
      } else {
        const code = c.charCodeAt(0);
        if (code < 0x20 || code > 0x7e) this.fail('a string holds only printable ASCII');
        out += c;
      }
    }
    return this.fail('an unterminated string');
  }

  token(): Token {
    const start = this.i;
    this.i++;
    while (!this.empty()) {
      const c = this.peek();
      if (!(tchar(c) || c === ':' || c === '/')) break;
      this.i++;
    }
    return new Token(this.s.slice(start, this.i));
  }

  bytes(): ByteSequence {
    this.i++; // :
    const end = this.s.indexOf(':', this.i);
    if (end < 0) this.fail('a byte sequence without its closing ":"');
    const b64 = this.s.slice(this.i, end);
    this.i = end + 1;
    if (!/^[A-Za-z0-9+/=]*$/.test(b64)) this.fail('a byte sequence holds only base64');
    const pad = b64.indexOf('=');
    const data = pad >= 0 ? b64.slice(0, pad) : b64;
    const padding = pad >= 0 ? b64.slice(pad) : '';
    if (!/^=*$/.test(padding)) this.fail('base64 padding only at the end');
    const r = data.length % 4;
    if (r === 1) this.fail('base64 of a length no bytes have');
    const needed = r === 0 ? 0 : 4 - r;
    if (padding.length > needed) this.fail('base64 padding beyond what it needs');
    return new ByteSequence(new Uint8Array(Buffer.from(data, 'base64')));
  }

  boolean(): boolean {
    this.i++; // ?
    const c = this.peek();
    if (c === '1') { this.i++; return true; }
    if (c === '0') { this.i++; return false; }
    return this.fail('a boolean is ?0 or ?1');
  }

  date(): SfDate {
    this.i++; // @
    const n = this.number();
    if (typeof n !== 'number') this.fail('a date is an integer');
    return new SfDate(n);
  }

  displayString(): DisplayString {
    this.i++; // %
    if (this.peek() !== '"') this.fail('a display string starts with %"');
    this.i++;
    const bytes: number[] = [];
    while (!this.empty()) {
      const c = this.s[this.i++];
      const code = c.charCodeAt(0);
      if (code < 0x20 || code > 0x7e) this.fail('a display string holds only printable ASCII');
      if (c === '%') {
        const hex = this.s.slice(this.i, this.i + 2);
        if (!/^[0-9a-f]{2}$/.test(hex)) this.fail('a display string escapes with two lowercase hexadecimal digits');
        this.i += 2;
        bytes.push(parseInt(hex, 16));
      } else if (c === '"') {
        try {
          return new DisplayString(new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(new Uint8Array(bytes)));
        } catch {
          return this.fail('a display string that is not UTF-8');
        }
      } else {
        bytes.push(code);
      }
    }
    return this.fail('an unterminated display string');
  }
}

export type FieldType = 'item' | 'list' | 'dictionary';

/** A field value (several field lines joined with ", ") parsed as its type (§4.2). */
export function parse(field: string, type: 'item'): Item;
export function parse(field: string, type: 'list'): List;
export function parse(field: string, type: 'dictionary'): Dictionary;
export function parse(field: string, type: FieldType): Item | List | Dictionary {
  for (let i = 0; i < field.length; i++) {
    if (field.charCodeAt(i) > 0x7f) throw new NotStructured('a field value is ASCII');
  }
  const p = new Parser(field);
  p.sp();
  const out = type === 'item' ? p.item() : type === 'list' ? p.list() : p.dictionary();
  p.sp();
  if (!p.empty()) p.fail('trailing characters');
  return out;
}

function serializeKey(k: string): string {
  if (!/^[a-z*][a-z0-9_\-.*]*$/.test(k)) throw new NotStructured(`the key ${JSON.stringify(k)}`);
  return k;
}

function serializeParams(p: Parameters): string {
  let out = '';
  for (const [k, v] of p) out += ';' + serializeKey(k) + (v === true ? '' : '=' + serializeBare(v));
  return out;
}

export function serializeBare(v: BareItem): string {
  if (typeof v === 'number') {
    if (!Number.isInteger(v) || Math.abs(v) > 999_999_999_999_999) throw new NotStructured(`the integer ${v}`);
    return String(v === 0 ? 0 : v);
  }
  if (typeof v === 'boolean') return v ? '?1' : '?0';
  if (typeof v === 'string') {
    if (!/^[\x20-\x7e]*$/.test(v)) throw new NotStructured('a string holds only printable ASCII');
    return '"' + v.replaceAll('\\', '\\\\').replaceAll('"', '\\"') + '"';
  }
  if (v instanceof Token) {
    if (!/^[A-Za-z*][A-Za-z0-9!#$%&'*+\-.^_`|~:/]*$/.test(v.value)) throw new NotStructured(`the token ${JSON.stringify(v.value)}`);
    return v.value;
  }
  if (v instanceof SfDecimal) {
    if (v.int.length > 12 || v.frac.length > 3) throw new NotStructured(`the decimal ${v.text}`);
    return v.text;
  }
  if (v instanceof ByteSequence) return ':' + Buffer.from(v.bytes).toString('base64') + ':';
  if (v instanceof SfDate) return '@' + serializeBare(v.seconds);
  if (v instanceof DisplayString) {
    let out = '%"';
    for (const b of new TextEncoder().encode(v.value)) {
      out += b === 0x25 || b === 0x22 || b < 0x20 || b > 0x7e ? '%' + b.toString(16).padStart(2, '0') : String.fromCharCode(b);
    }
    return out + '"';
  }
  throw new NotStructured('not a bare item');
}

function serializeMember(m: Member): string {
  if (isInnerList(m)) return '(' + m.items.map((i) => serializeBare(i.value) + serializeParams(i.params)).join(' ') + ')' + serializeParams(m.params);
  return serializeBare(m.value) + serializeParams(m.params);
}

/** A field value written as RFC 9651 §4.1 writes it; an empty List or Dictionary is "" (the field is not sent). */
export function serialize(v: Item | List | Dictionary): string {
  if (Array.isArray(v)) return v.map(serializeMember).join(', ');
  if (v instanceof Map) {
    return [...v].map(([k, m]) => serializeKey(k) + (!isInnerList(m) && m.value === true ? serializeParams(m.params) : '=' + serializeMember(m))).join(', ');
  }
  return serializeMember(v);
}
