// The NDJSON framing of a chunked answer (§9.6.5): one record per line,
// each ended by a single LF, no blank lines, and the end line last. The
// reader validates completion exactly as the Go client's reader does
// (sdk/go/protocol/frame): the end line has every member of
// chunk-end.schema.json with its type, $atlas is "end", rows counts the
// rows that arrived, its problem has every member problem.schema.json
// requires, and nothing follows it. An answer that stops without its end
// line is upstream_failed (AP-HTTP-002): EOF alone is not success.

import { decode, type Json, NotIJson, parse } from './ijson.ts';
import { type AtlasProblem, type Obj, problem } from './problem.ts';

/** The longest NDJSON line this reader takes, its LF counted: the Go reader's limit (A-163). */
export const MAX_LINE = 16 << 20;

/** The end line, read. */
export interface End { rows: number; truncated: boolean; usage: Obj; problem: Obj | null }

function incomplete(why: string): AtlasProblem {
  return problem('upstream_failed', 'the answer is incomplete: ' + why);
}

function invalid(why: string): AtlasProblem {
  return problem('invalid_document', 'the answer is not framed as §9.6.5 says: ' + why);
}

const END_MEMBERS = ['$atlas', 'rows', 'truncated', 'usage', 'problem'];
const PROBLEM_MEMBERS: [string, 'string' | 'boolean' | 'integer'][] = [
  ['type', 'string'], ['status', 'integer'], ['code', 'string'], ['retryable', 'boolean'], ['next', 'string'], ['request_id', 'string'],
];

function isObject(v: Json | undefined): v is Obj {
  return v !== null && v !== undefined && typeof v === 'object' && !Array.isArray(v);
}

/** An end line as chunk-end.schema.json defines it: a line that only says {"$atlas":"end"} is never taken for completion. */
export function parseEnd(o: Obj): End {
  for (const m of END_MEMBERS) if (!Object.hasOwn(o, m)) throw invalid(`the end line has no ${m}`);
  if (o.$atlas !== 'end') throw invalid(`the end line has $atlas ${JSON.stringify(o.$atlas)}`);
  const rows = o.rows;
  if (typeof rows !== 'number' || !Number.isInteger(rows) || rows < 0) throw invalid("the end line's rows is not a count");
  if (typeof o.truncated !== 'boolean') throw invalid("the end line's truncated is not a boolean");
  if (!isObject(o.usage)) throw invalid("the end line's usage is not an object");
  const p = o.problem;
  if (p !== null && !isObject(p)) throw invalid("the end line's problem is neither null nor a problem");
  if (p !== null) {
    for (const [name] of PROBLEM_MEMBERS) if (!Object.hasOwn(p, name)) throw invalid(`the end line's problem has no ${name}`);
    for (const [name, want] of PROBLEM_MEMBERS) {
      const v = p[name];
      const ok = want === 'integer' ? typeof v === 'number' && Number.isInteger(v) : typeof v === want;
      if (!ok) throw invalid(`the end line's problem has a ${name} that is not a ${want}`);
    }
  }
  return { rows: rows === 0 ? 0 : rows, truncated: o.truncated, usage: o.usage, problem: p }; // -0 counts no rows, as in the Go reader
}

/**
 * An NDJSON answer's rows, then its end. next() gives each row, then
 * undefined once the end line has been read and nothing follows it; the
 * end line's problem is then end.problem, for the caller to raise after
 * the rows (STREAM-003) or report as a warning (STREAM-004). A transport
 * failure is reported by broken, which knows whether the call's deadline
 * passed.
 */
export class NDJSONReader {
  private readonly it: AsyncIterator<Uint8Array>;
  private readonly broken: (e: unknown) => unknown;
  private chunk?: Uint8Array;
  private at = 0;
  private parts: Uint8Array[] = [];
  private partLen = 0;
  private failed?: unknown;
  rows = 0;
  end?: End;

  constructor(body: AsyncIterable<Uint8Array>, broken: (e: unknown) => unknown) {
    this.it = body[Symbol.asyncIterator]();
    this.broken = broken;
  }

  async next(): Promise<Obj | undefined> {
    if (this.failed !== undefined) throw this.failed;
    try {
      return await this.read();
    } catch (e) {
      this.failed = e;
      await this.close();
      throw e;
    }
  }

  /** Stops reading: the body is released. */
  async close(): Promise<void> {
    try { await this.it.return?.(); } catch { /* the body is already gone */ }
  }

  private async pull(): Promise<IteratorResult<Uint8Array>> {
    try {
      return await this.it.next();
    } catch (e) {
      throw this.broken(e);
    }
  }

  /** One line without its LF, or undefined at the end of the body (cut tells whether bytes were left without one). */
  private async line(): Promise<{ line?: Uint8Array; cut: boolean }> {
    for (;;) {
      if (this.chunk) {
        const lf = this.chunk.indexOf(0x0a, this.at);
        const piece = this.chunk.subarray(this.at, lf >= 0 ? lf : this.chunk.length);
        this.partLen += piece.length + (lf >= 0 ? 1 : 0);
        if (this.partLen > MAX_LINE) throw invalid(`a line longer than ${MAX_LINE} bytes`);
        if (lf < 0) {
          this.parts.push(piece);
          this.chunk = undefined;
        } else {
          this.at = lf + 1;
          if (this.at >= this.chunk.length) this.chunk = undefined;
          const line = concat([...this.parts, piece]);
          this.parts = [];
          this.partLen = 0;
          return { line, cut: false };
        }
      }
      const r = await this.pull();
      if (r.done) return { cut: this.partLen > 0 };
      this.chunk = r.value;
      this.at = 0;
    }
  }

  private async read(): Promise<Obj | undefined> {
    if (this.end) return undefined;
    const { line, cut } = await this.line();
    if (line === undefined) throw incomplete(cut ? 'its last line is cut short' : 'it ends without an end line');
    let text: Json;
    try {
      const s = decode(line);
      if (s.trim() === '') throw invalid('a blank line');
      text = parse(s);
    } catch (e) {
      if (e instanceof NotIJson) throw problem('invalid_document', `line ${this.rows + 1}: ${e.message}`);
      throw e;
    }
    if (!isObject(text)) throw invalid(`line ${this.rows + 1} is not a record`);
    if (Object.hasOwn(text, '$atlas')) {
      const end = parseEnd(text);
      if (end.rows !== this.rows) throw invalid(`the end line counts ${end.rows} rows, and ${this.rows} arrived`);
      if (this.chunk && this.at < this.chunk.length) throw invalid('data after the end line');
      for (;;) {
        const r = await this.pull();
        if (r.done) break;
        if (r.value.length > 0) throw invalid('data after the end line');
      }
      this.end = end;
      return undefined;
    }
    this.rows++;
    return text;
  }
}

function concat(parts: Uint8Array[]): Uint8Array {
  if (parts.length === 1) return parts[0];
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
}
