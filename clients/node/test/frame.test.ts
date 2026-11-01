// NDJSON completion (§9.6.5), validated exactly as the Go reader
// (sdk/go/protocol/frame) validates it. PR09: $atlas must be "end", every
// end-line member has its type, the end line's problem has every member
// problem.schema.json requires, and nothing follows the end line.
// AP-HTTP-002: an answer without its end line is upstream_failed.

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { MAX_LINE, NDJSONReader } from '../src/frame.ts';
import { AtlasProblem } from '../src/problem.ts';
import { parse as parseSF, serialize } from '../src/sf.ts';
import { iterations, mutate, rng, seed } from './fuzz.ts';

const enc = (s: string) => new TextEncoder().encode(s);

async function* chunks(...parts: (string | Uint8Array)[]): AsyncIterable<Uint8Array> {
  for (const p of parts) yield typeof p === 'string' ? enc(p) : p;
}

/** Every row, then the end; or the problem the reader raised, with the rows read before it. */
async function read(...parts: (string | Uint8Array)[]): Promise<{ rows: unknown[]; end?: unknown; problem?: AtlasProblem }> {
  const r = new NDJSONReader(chunks(...parts), (e) => e);
  const rows: unknown[] = [];
  try {
    for (;;) {
      const row = await r.next();
      if (row === undefined) return { rows, end: r.end };
      rows.push(row);
    }
  } catch (e) {
    assert.ok(e instanceof AtlasProblem, `threw ${e}`);
    return { rows, problem: e };
  }
}

const END = '{"$atlas":"end","rows":1,"truncated":false,"usage":{},"problem":null}\n';
const ROW = '{"a":1}\n';

async function refused(code: string, why: RegExp, ...parts: string[]): Promise<void> {
  const got = await read(...parts);
  assert.equal(got.problem?.code, code, `${JSON.stringify(parts)}: ${got.problem?.message ?? 'accepted'}`);
  assert.match(got.problem!.message, why);
}

// PR09: the control cases the refuters ran.
test('TestPR09AValidEndLineIsComplete', async () => {
  const got = await read(ROW, END);
  assert.deepEqual(got.rows, [{ a: 1 }]);
  assert.deepEqual(got.end, { rows: 1, truncated: false, usage: {}, problem: null });
  assert.deepEqual((await read('{"a":1}\n{"$atl', 'as":"end","rows":1,"truncated":false,"usage":{"rows":1},"problem":null}', '\n')).rows, [{ a: 1 }]);
});

// PR09: an end line whose $atlas is not "end" is never completion.
test('TestPR09EndLineMustSayEnd', async () => {
  await refused('invalid_document', /the end line has \$atlas "start"/, ROW, '{"$atlas":"start","rows":1,"truncated":false,"usage":{},"problem":null}\n');
});

// PR09: data after the end line, ended by an LF or not, is refused; nothing is dropped silently.
test('TestPR09DataAfterTheEndLineIsRefused', async () => {
  await refused('invalid_document', /data after the end line/, ROW, END, '{"a":2}');
  await refused('invalid_document', /data after the end line/, ROW, END + '{"a":2}\n');
  await refused('invalid_document', /data after the end line/, ROW, END, 'garbage');
  await refused('invalid_document', /data after the end line/, ROW, END, '\n');
});

// PR09: each end-line member has the type chunk-end.schema.json gives it.
test('TestPR09EndLineMembersAreTyped', async () => {
  const end = (m: Record<string, unknown>) => JSON.stringify({ $atlas: 'end', rows: 0, truncated: false, usage: {}, problem: null, ...m }) + '\n';
  await refused('invalid_document', /truncated is not a boolean/, end({ truncated: 'no' }));
  await refused('invalid_document', /usage is not an object/, end({ usage: 5 }));
  await refused('invalid_document', /rows is not a count/, end({ rows: -1 }));
  await refused('invalid_document', /rows is not a count/, end({ rows: 0.5 }));
  assert.ok(Object.is(((await read('{"$atlas":"end","rows":-0,"truncated":false,"usage":{},"problem":null}\n')).end as any).rows, 0), 'rows -0 is no rows, as in the Go reader (found by FuzzNDJSONReader)');
  await refused('invalid_document', /neither null nor a problem/, end({ problem: 'x' }));
  for (const m of ['rows', 'truncated', 'usage', 'problem']) {
    const o: Record<string, unknown> = { $atlas: 'end', rows: 0, truncated: false, usage: {}, problem: null };
    delete o[m];
    await refused('invalid_document', new RegExp(`the end line has no ${m}`), JSON.stringify(o) + '\n');
  }
  // A line without $atlas is a row, as in the Go reader: the answer then ends without its end line.
  await refused('upstream_failed', /ends without an end line/, '{"rows":0,"truncated":false,"usage":{},"problem":null}\n');
});

// The end line's problem is a whole problem (problem.schema.json's required members), as the Go reader holds it.
test('TestPR09EndLineProblemIsWhole', async () => {
  const whole = { type: 'https://atlas.jhg.com/problems/internal', status: 500, code: 'internal', retryable: false, next: 'Retry.', request_id: 'r1' };
  const got = await read(ROW, JSON.stringify({ $atlas: 'end', rows: 1, truncated: false, usage: {}, problem: whole }) + '\n');
  assert.deepEqual((got.end as any).problem, whole);
  await refused('invalid_document', /problem has no request_id/, JSON.stringify({ $atlas: 'end', rows: 0, truncated: false, usage: {}, problem: { ...whole, request_id: undefined } }) + '\n');
  await refused('invalid_document', /problem has a status that is not a integer/, JSON.stringify({ $atlas: 'end', rows: 0, truncated: false, usage: {}, problem: { ...whole, status: '500' } }) + '\n');
});

// AP-HTTP-002, STREAM-002: EOF alone is not success.
test('TestPR09WithoutItsEndLineIsUpstreamFailed', async () => {
  await refused('upstream_failed', /ends without an end line/, ROW, '{"a":2}\n');
  await refused('upstream_failed', /ends without an end line/);
  await refused('upstream_failed', /its last line is cut short/, ROW, '{"$atlas":"end","rows":1,"truncated":false,"usage":{},"problem":null}');
  await refused('upstream_failed', /its last line is cut short/, ROW, '{"a":');
});

// The rest of the framing: a blank line, a count that is wrong, a line that is not a record, not I-JSON, or too long.
test('TestPR09FramingRules', async () => {
  await refused('invalid_document', /a blank line/, ROW, '\n', END);
  await refused('invalid_document', /a blank line/, ROW, ' \t\r\n', END);
  await refused('invalid_document', /counts 2 rows, and 1 arrived/, ROW, '{"$atlas":"end","rows":2,"truncated":false,"usage":{},"problem":null}\n');
  await refused('invalid_document', /line 1 is not a record/, '[1]\n');
  await refused('invalid_document', /line 2: .*duplicate member/, ROW, '{"a":1,"a":2}\n');
  await refused('invalid_document', /line 1: .*invalid UTF-8/, enc('{"a":"') as any, new Uint8Array([0xc3, 0x28]) as any, '"}\n');
  await refused('invalid_document', /line 1: .*byte-order mark/, new Uint8Array([0xef, 0xbb, 0xbf]) as any, ROW);
  const long = '{"a":"' + 'x'.repeat(MAX_LINE) + '"}\n';
  await refused('invalid_document', /a line longer than/, long);
});

// A transport failure mid-body is the reader's broken(): the Exchange makes it deadline_exceeded or upstream_failed.
test('TestPR09TransportFailureIsReported', async () => {
  async function* failing(): AsyncIterable<Uint8Array> {
    yield enc(ROW);
    throw new Error('socket hang up');
  }
  const r = new NDJSONReader(failing(), () => new AtlasProblem({ code: 'upstream_failed', detail: 'cut' }));
  assert.deepEqual(await r.next(), { a: 1 });
  await assert.rejects(r.next(), (e: AtlasProblem) => e.code === 'upstream_failed');
  await assert.rejects(r.next(), (e: AtlasProblem) => e.code === 'upstream_failed', 'a failed reader stays failed');
});

// AGENTS.md: fuzz every parser. The NDJSON reader, over mutated answers split at random: it raises only
// problems, never takes an answer without a valid end line for complete, and never yields more rows than it counts.
test('FuzzNDJSONReader', async () => {
  const corpus = [ROW + END, '{"a":1}\n{"b":[2]}\n{"$atlas":"end","rows":2,"truncated":true,"usage":{"rows":2},"problem":null}\n', END.replace('"rows":1', '"rows":0')].map(enc);
  const dictionary = ['\n', '{"$atlas":"end"', '"rows":', '"problem":', 'null', '"end"', '"start"', '}', '{', ',', '"truncated":true'].map(enc);
  const r = rng(seed() + 1);
  for (let i = 0; i < iterations(3000); i++) {
    const input = mutate(corpus[i % corpus.length], r, dictionary);
    const cut = Math.floor(r() * (input.length + 1));
    const got = await read(input.subarray(0, cut), input.subarray(cut));
    if (got.problem) {
      assert.ok(['invalid_document', 'upstream_failed'].includes(got.problem.code), `iteration ${i}: ${got.problem.code}`);
      continue;
    }
    const end = got.end as any;
    assert.equal(end.rows, got.rows.length, `iteration ${i}: the end line counts what arrived`);
    const text = new TextDecoder().decode(input);
    assert.ok(text.endsWith('\n'), `iteration ${i}: complete without its last LF`);
  }
});

// AGENTS.md: fuzz every parser. RFC 9651: every field value either fails to parse or serialises to a
// value that parses back to the same serialisation (a fixed point), as Item, List and Dictionary.
test('FuzzStructuredFields', () => {
  const corpus = ['name="fiatlas", version="2.3.0", protocol="atlas/1.0"', 'levels=(core streaming), lens=()', '"portfolio.holdings@v1#bcaa5bbad97cc612"', '@1793318400', '1250.5', ':aGk=:', '?1', 'a;b=1;c, d=%"caf%c3%a9"'].map(enc);
  const dictionary = ['"', '\\', '(', ')', ';', '=', ',', ' ', '\t', '@', '?', ':', '%', '*', '-', '.', '9'].map(enc);
  const r = rng(seed() + 2);
  for (let i = 0; i < iterations(20000); i++) {
    const field = new TextDecoder().decode(mutate(corpus[i % corpus.length], r, dictionary));
    for (const t of ['item', 'list', 'dictionary'] as const) {
      let once: string;
      try { once = serialize(parseSF(field, t as any)); } catch (e) { assert.equal((e as Error).name, 'NotStructured', `iteration ${i}: ${e}`); continue; }
      assert.equal(serialize(parseSF(once, t as any)), once, `iteration ${i}: ${JSON.stringify(field)} as ${t}`);
    }
  }
});
