// I-Regexp (RFC 9485), as §9.2.4 uses it: every pattern of a profile is
// ^R$ with R an I-Regexp, matched against the whole value's code points.
// translate parses R by RFC 9485's grammar and writes it for ECMAScript as
// its section 5.3 says: an unescaped "." outside a class becomes [^\n\r],
// the whole is wrapped in ^(?: and )$, and it runs as a Unicode pattern.
// Every literal is written so that nothing in it is special to ECMAScript
// ("^" and "$" are ordinary characters in an I-Regexp). Two refusals go
// beyond RFC 9485, as the references take them (Q-RE-1, A-013): \p{C} and
// \p{Cn}, and a quantifier count above 1000.

/** The largest count a quantifier may give (Q-RE-1). */
export const MAX_REPEAT = 1000;

const CATEGORIES = new Set([
  'L', 'Ll', 'Lm', 'Lo', 'Lt', 'Lu', 'M', 'Mc', 'Me', 'Mn', 'N', 'Nd', 'Nl', 'No',
  'P', 'Pc', 'Pd', 'Pe', 'Pf', 'Pi', 'Po', 'Ps', 'Z', 'Zl', 'Zp', 'Zs', 'S', 'Sc', 'Sk', 'Sm', 'So',
  'Cc', 'Cf', 'Co', // C and Cn are refused (Q-RE-1); Cs is not in RFC 9485
]);

const BS = '\\';

class NotIRegexp extends Error {}

class Parser {
  readonly cps: number[];
  i = 0;
  out = '';
  constructor(r: string) {
    this.cps = Array.from(r, (c) => c.codePointAt(0)!);
  }

  fail(why: string): never {
    throw new NotIRegexp(`not an I-Regexp at character ${this.i}: ${why}`);
  }

  more(): boolean { return this.i < this.cps.length; }
  peek(k = 0): number { return this.cps[this.i + k] ?? -1; }

  regexp(): void {
    this.branch();
    while (this.peek() === 0x7c) { // |
      this.i++;
      this.out += '|';
      this.branch();
    }
  }

  branch(): void {
    while (this.more() && this.peek() !== 0x7c && this.peek() !== 0x29) this.piece();
  }

  piece(): void {
    this.atom();
    const c = this.peek();
    if (c === 0x2a || c === 0x2b || c === 0x3f) { // * + ?
      this.i++;
      this.out += String.fromCodePoint(c);
    } else if (c === 0x7b) { // {
      this.range();
    }
  }

  count(): number | undefined {
    const start = this.i;
    while (this.peek() >= 0x30 && this.peek() <= 0x39) this.i++;
    if (this.i === start) return undefined;
    const n = Number(String.fromCodePoint(...this.cps.slice(start, this.i)));
    if (n > MAX_REPEAT) this.fail(`a count above ${MAX_REPEAT}`);
    return n;
  }

  range(): void {
    this.i++; // {
    const lo = this.count();
    if (lo === undefined) this.fail('a quantifier needs a count');
    let text = String(lo);
    if (this.peek() === 0x2c) { // ,
      this.i++;
      text += ',';
      const hi = this.count();
      if (hi !== undefined) {
        if (hi < lo) this.fail('a quantifier whose counts are out of order');
        text += String(hi);
      }
    }
    if (this.peek() !== 0x7d) this.fail('a quantifier ends with }');
    this.i++;
    this.out += `{${text}}`;
  }

  atom(): void {
    const c = this.peek();
    if (c === 0x28) { // (
      this.i++;
      this.out += '(?:';
      this.regexp();
      if (this.peek() !== 0x29) this.fail('an unclosed (');
      this.i++;
      this.out += ')';
      return;
    }
    if (c === 0x2e) { // .
      this.i++;
      this.out += `[^${BS}n${BS}r]`;
      return;
    }
    if (c === 0x5b) { this.classExpr(); return; } // [
    if (c === 0x5c) { // backslash
      const esc = this.escape(false);
      this.out += esc;
      return;
    }
    if (!normalChar(c)) this.fail(`${c === -1 ? 'the end' : JSON.stringify(String.fromCodePoint(c))} cannot stand here`);
    this.i++;
    this.out += literal(c);
  }

  /** An escape at a backslash: a single character's, or a category's. In a class, "-" escapes as itself. */
  escape(inClass: boolean): string {
    this.i++; // backslash
    const c = this.peek();
    if (c === 0x70 || c === 0x50) { // p P
      this.i++;
      if (this.peek() !== 0x7b) this.fail('a category escape is \\p{...}');
      this.i++;
      let name = '';
      while (this.more() && this.peek() !== 0x7d) name += String.fromCodePoint(this.cps[this.i++]);
      if (this.peek() !== 0x7d) this.fail('an unclosed category');
      this.i++;
      if (!CATEGORIES.has(name)) this.fail(`the category ${JSON.stringify(name)} is not one an I-Regexp may name`);
      return `${BS}${String.fromCodePoint(c)}{${name}}`;
    }
    this.i++;
    switch (c) {
      case 0x6e: return `${BS}n`;
      case 0x72: return `${BS}r`;
      case 0x74: return `${BS}t`;
      case 0x2d: return inClass ? `${BS}-` : '-';
    }
    if ((c >= 0x28 && c <= 0x2b) || c === 0x2e || c === 0x3f || (c >= 0x5b && c <= 0x5e) || (c >= 0x7b && c <= 0x7d)) {
      return BS + String.fromCodePoint(c);
    }
    return this.fail(`the escape of ${c === -1 ? 'nothing' : JSON.stringify(String.fromCodePoint(c))} is not an I-Regexp's`);
  }

  /** A class character, or an escape; its code point when it is one character, for a range. */
  classChar(): { text: string; cp?: number } {
    const c = this.peek();
    if (c === 0x5c) {
      const at = this.i;
      const text = this.escape(true);
      const single = this.cps.slice(at, this.i);
      if (single[1] === 0x70 || single[1] === 0x50) return { text };
      const cp = single[1] === 0x6e ? 0x0a : single[1] === 0x72 ? 0x0d : single[1] === 0x74 ? 0x09 : single[1];
      return { text, cp };
    }
    if (c === -1 || c === 0x2d || (c >= 0x5b && c <= 0x5d) || (c >= 0xd800 && c <= 0xdfff)) this.fail('not a class character');
    this.i++;
    return { text: classLiteral(c), cp: c };
  }

  classExpr(): void {
    this.i++; // [
    let out = '[';
    if (this.peek() === 0x5e) { this.i++; out += '^'; }
    let first = true;
    for (;;) {
      const c = this.peek();
      if (c === 0x5d && !first) { this.i++; break; } // ]
      if (c === 0x2d && (first || this.peek(1) === 0x5d)) { // a literal "-" first or last
        this.i++;
        out += `${BS}-`;
        if (!first) {
          if (this.peek() !== 0x5d) this.fail('a class ends with ]');
          this.i++;
          break;
        }
        first = false;
        continue;
      }
      const lo = this.classChar();
      if (this.peek() === 0x2d && this.peek(1) !== 0x5d && this.peek(1) !== -1) {
        this.i++;
        const hi = this.classChar();
        if (lo.cp === undefined || hi.cp === undefined) this.fail('a range of a category');
        if (hi.cp < lo.cp) this.fail('a range out of order');
        out += `${lo.text}-${hi.text}`;
      } else {
        out += lo.text;
      }
      first = false;
    }
    this.out += out + ']';
  }
}

/** NormalChar of RFC 9485: every code point but ( ) * + . ? [ \ ] { | } and the surrogates. */
function normalChar(c: number): boolean {
  if (c < 0) return false;
  if ((c >= 0x28 && c <= 0x2b) || c === 0x2e || c === 0x3f || (c >= 0x5b && c <= 0x5d) || (c >= 0x7b && c <= 0x7d)) return false;
  return !(c >= 0xd800 && c <= 0xdfff);
}

/** A literal outside a class, escaped where ECMAScript would read it as syntax. */
function literal(c: number): string {
  const s = String.fromCodePoint(c);
  return c === 0x5e || c === 0x24 || c === 0x2f ? BS + s : s;
}

/** A literal inside a class. */
function classLiteral(c: number): string {
  const s = String.fromCodePoint(c);
  return c === 0x5e ? BS + s : s;
}

/** ^R$ with R an I-Regexp, as an ECMAScript expression that matches a whole value; or why it is not one. */
export function translate(pattern: string): RegExp | Error {
  if (!pattern.startsWith('^') || !pattern.endsWith('$') || pattern.length < 2) return new Error('a pattern has the form ^R$');
  const p = new Parser(pattern.slice(1, -1));
  try {
    p.regexp();
    if (p.more()) p.fail('an unmatched )');
    return new RegExp(`^(?:${p.out})$`, 'u');
  } catch (e) {
    return e instanceof Error ? e : new Error(String(e));
  }
}
