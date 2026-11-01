// The Node SDK's call path (§9.7.4) against a stand-in gateway on a real
// loopback socket, its time driven by a fake clock. Each test names the
// finding or requirement it proves.

import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test, type TestContext } from 'node:test';

import { formatDeadline, readIdempotencyKey } from '../src/headers.ts';
import { AtlasError, AtlasProblem, atlas, type Client, fingerprint, signIn, type Warning } from '../src/index.ts';
import { FakeClock } from './fakeclock.ts';
import { conformanceRow, endLine, problemDoc, sendJSON, sendNDJSON, sendProblem, StandIn, type StandInOptions } from './standin.ts';

interface Rig { clock: FakeClock; gw: StandIn; home: string; client: Client; warnings: Warning[] }

async function rig(t: TestContext, standin: StandInOptions = {}, connect: Record<string, unknown> = {}): Promise<Rig> {
  const clock = new FakeClock();
  const gw = await StandIn.start({ now: () => clock.now(), ...standin });
  const home = mkdtempSync(join(tmpdir(), 'atlas-node-'));
  t.after(async () => { await gw.close(); rmSync(home, { recursive: true, force: true }); });
  await signIn({ url: gw.url, home, accessToken: 'ct-valid', refreshToken: 'rt-valid', expiresIn: 3600, clock });
  const warnings: Warning[] = [];
  const client = await atlas.connect({ url: gw.url, home, clock, random: () => 0.5, onWarning: (w) => warnings.push(w), ...connect });
  return { clock, gw, home, client, warnings };
}

const problemIs = (code: string, more: (e: AtlasProblem) => boolean = () => true) => (e: unknown) => {
  assert.ok(e instanceof AtlasProblem, `not a problem: ${e}`);
  assert.equal(e.code, code, e.message);
  return more(e);
};

// STREAM-001: every row, then the end line's usage; the answer is complete.
test('TestSTREAM001RowsThenTheEndLine', async (t) => {
  const { client, clock } = await rig(t);
  const r = await client.call('conformance.rows@v1:fetch', { n: 7 }, { accept: ['application/x-ndjson'] });
  const rows = await r.rows();
  assert.deepEqual(rows, Array.from({ length: 7 }, (_, i) => conformanceRow(i)));
  assert.equal(r.complete, true);
  assert.equal(r.truncated, false);
  assert.deepEqual(r.usage, { rows: 7 });
  assert.equal(r.requestId, 'req-standin');
  assert.equal(clock.pending(), 0, 'the exchange released its deadline');
});

// R001 (Node's part), STREAM-004, Q-DOC-15: a truncated table is a visible warning, and is not complete.
test('TestR001TruncationIsAVisibleWarning', async (t) => {
  const { client, gw, warnings } = await rig(t);
  const whole = problemDoc('output_limit_exceeded', 200, { detail: 'the answer was cut at 2 rows, your max_rows' });
  gw.on('conformance.rows@v1:fetch', (_, res) => sendNDJSON(res, [conformanceRow(0), conformanceRow(1)], endLine(2, { truncated: true, problem: whole })));
  const r = await client.call('conformance.rows@v1:fetch', { n: 100 });
  assert.equal((await r.rows()).length, 2);
  assert.deepEqual(r.warnings, [{ code: 'output_limit_exceeded', text: 'the answer was cut at 2 rows, your max_rows' }]);
  assert.deepEqual(warnings, r.warnings, 'the warning reached the warning sink');
  assert.equal(r.truncated, true);
  assert.equal(r.complete, false);
  // An end line that says truncated with no problem is a truncation too.
  gw.on('conformance.rows@v1:fetch', (_, res) => sendNDJSON(res, [conformanceRow(0)], endLine(1, { truncated: true })));
  const r2 = await client.call('conformance.rows@v1:fetch', { n: 100 });
  await r2.rows();
  assert.equal(r2.warnings[0].code, 'output_limit_exceeded');
  assert.equal(r2.complete, false);
});

// R001, §9.7.4 step 11: with no sink given, a warning is process.emitWarning's, a deprecation as DeprecationWarning.
test('TestR001WarningsAreEmittedByDefault', async (t) => {
  const { gw, home, clock } = await rig(t);
  const client = await atlas.connect({ url: gw.url, home, clock });
  gw.on('conformance.echo@v1:fetch', (s, res) => sendJSON(res, 200, { count: 1, kind: 'plain', ratio: null, when: null, tags: [], ...s.json }, { deprecation: '@1795046400', sunset: 'Sat, 01 May 2027 00:00:00 GMT' }));
  const emitted = new Promise<Error>((ok) => process.once('warning', ok));
  const r = await client.call('conformance.echo@v1:fetch', { text: 'hello' });
  const w = await emitted;
  assert.equal(w.name, 'DeprecationWarning');
  assert.equal((w as any).code, 'deprecated');
  assert.equal(r.warnings[0].text, 'conformance.echo@v1:fetch is deprecated since 2026-11-19, sunset 2027-05-01');
});

// STREAM-002, AP-HTTP-002: no end line is upstream_failed, after the rows that came.
test('TestSTREAM002NoEndLineIsUpstreamFailed', async (t) => {
  const { client, gw, clock } = await rig(t);
  gw.on('conformance.rows@v1:fetch', (_, res) => sendNDJSON(res, [conformanceRow(0), conformanceRow(1), conformanceRow(2)], null));
  const r = await client.call('conformance.rows@v1:fetch', { n: 3 });
  const seen: unknown[] = [];
  await assert.rejects((async () => { for await (const row of r) seen.push(row); })(), problemIs('upstream_failed'));
  assert.equal(seen.length, 3);
  assert.equal(r.complete, false);
  assert.equal(clock.pending(), 0);
});

// STREAM-003: a problem in the end line is raised after the rows.
test('TestSTREAM003EndLineProblemIsRaisedAfterTheRows', async (t) => {
  const { client, gw } = await rig(t);
  gw.on('conformance.rows@v1:fetch', (_, res) => sendNDJSON(res, [conformanceRow(0), conformanceRow(1)], endLine(2, { problem: problemDoc('internal', 500) })));
  const r = await client.call('conformance.rows@v1:fetch', { n: 5 });
  const seen: unknown[] = [];
  await assert.rejects((async () => { for await (const row of r) seen.push(row); })(), problemIs('internal'));
  assert.deepEqual(seen, [conformanceRow(0), conformanceRow(1)]);
  await assert.rejects(r.rows(), problemIs('internal'), 'a failed result reports its failure again');
});

// PR09 through the call: an end line that says start is not completion, and trailing bytes are refused.
test('TestPR09CompletionThroughTheCall', async (t) => {
  const { client, gw } = await rig(t);
  gw.on('conformance.rows@v1:fetch', (_, res) => sendNDJSON(res, [conformanceRow(0)], '{"$atlas":"start","rows":1,"truncated":false,"usage":{},"problem":null}\n'));
  await assert.rejects((await client.call('conformance.rows@v1:fetch', { n: 1 })).rows(), problemIs('invalid_document', (e) => /\$atlas "start"/.test(e.message)));
  gw.on('conformance.rows@v1:fetch', (_, res) => sendNDJSON(res, [conformanceRow(0)], JSON.stringify(endLine(1)) + '\n{"i":1}'));
  await assert.rejects((await client.call('conformance.rows@v1:fetch', { n: 1 })).rows(), problemIs('invalid_document', (e) => /data after the end line/.test(e.message)));
});

// R023 (R11): reading a result again never silently returns nothing and never calls again.
test('TestR023SecondReadOfAResult', async (t) => {
  const { client, gw } = await rig(t);
  const r = await client.call('conformance.rows@v1:fetch', { n: 3 }, { accept: ['application/x-ndjson'] });
  const first = await r.rows();
  assert.equal(first.length, 3);
  assert.deepEqual(await r.rows(), first, 'rows() keeps what it read');
  const again: unknown[] = [];
  for await (const row of r) again.push(row);
  assert.deepEqual(again, first, 'an iteration after rows() reads the kept rows');
  const streamed = await client.call('conformance.rows@v1:fetch', { n: 3 }, { accept: ['application/x-ndjson'] });
  let n = 0;
  for await (const _ of streamed) n++;
  assert.equal(n, 3);
  await assert.rejects((async () => { for await (const _ of streamed) n++; })(), problemIs('invalid_request', (e) => /read once/.test(e.message)));
  await assert.rejects(streamed.rows(), problemIs('invalid_request'));
  const json = await client.call('conformance.rows@v1:fetch', { n: 2 }, { accept: ['application/json'] });
  assert.deepEqual(await json.rows(), await json.rows(), 'a JSON table is held in memory');
  assert.equal(gw.to('conformance.rows@v1:fetch').length, 3, 'one request per call, however often it is read');
});

// R024, CORE-TOL-001: an answer written under a newer profile is fetched by exact once and resolved:
// an unknown enum value reads UNKNOWN, unknown members are dropped.
test('TestR024NewerWriterProfileIsResolved', async (t) => {
  const { client, gw } = await rig(t);
  const newer = 'conformance.echo@v1#4fe7738a97223902';
  const answer = { text: 'hello', count: 1, ratio: null, when: null, kind: 'whisper', tags: [], echoed_at: '2026-10-31T00:00:00Z', zzz: { nested: true } };
  gw.on('conformance.echo@v1:fetch', (_, res) => sendJSON(res, 200, answer, { 'atlas-profile': `"${newer}";source=x`, 'atlas-future': '?1', 'x-something-new': '1' }), 2);
  const r = await client.call('conformance.echo@v1:fetch', { text: 'hello' });
  assert.deepEqual(r.record, { text: 'hello', count: 1, ratio: null, when: null, kind: 'UNKNOWN', tags: [] });
  assert.equal(r.profile, newer);
  await client.call('conformance.echo@v1:fetch', { text: 'hello' });
  assert.equal(gw.to('atlas.describe:fetch').filter((s) => s.json.exact === newer).length, 1, 'the writer is fetched once by exact (CORE-DISC-002)');
});

/** conformance.rows@v1 one revision on: label renamed title (its alias), and a colour with a default. */
function newerRows(gw: StandIn): string {
  const older = gw.current.get('conformance.rows@v1')!;
  const p = structuredClone(gw.profiles.get(older));
  const items = p.schemas.result.items;
  const props: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(items.properties)) props[k === 'label' ? 'title' : k] = k === 'label' ? { ...(v as object), 'x-atlas-aliases': ['label'] } : v;
  props.colour = { type: 'string', description: 'A colour.', enum: ['red', 'blue', 'UNKNOWN'], 'x-atlas-enum': [{ value: 'red' }, { value: 'blue', aliases: ['azure'] }, { value: 'UNKNOWN', description: 'A value this reader does not know.' }], default: 'red' };
  items.properties = props;
  items.required = items.required.map((k: string) => (k === 'label' ? 'title' : k));
  p.evolution = { previous_profile: older, lens: [] };
  p.fingerprint = fingerprint(p);
  const exact = `conformance.rows@v1#${p.fingerprint}`;
  gw.profiles.set(exact, p);
  gw.current.set('conformance.rows@v1', exact);
  return older;
}

// R024 (REVIEW.md: aliases, defaults, UNKNOWN and projection): rows written under another profile of the
// chain are matched against the reader's schema; an alias gives a renamed member, a default an absent one,
// an enum alias its value, and projection only the columns asked for.
test('TestR024RowsResolveAliasesDefaultsUnknownAndProjection', async (t) => {
  const { client, gw } = await rig(t);
  const older = newerRows(gw);
  gw.on('conformance.rows@v1:fetch', (s, res) => {
    const rows = [{ ...conformanceRow(0), colour: 'azure' }, { ...conformanceRow(1), colour: 'green' }, conformanceRow(2)];
    const cols: string[] | undefined = s.json.columns ?? undefined;
    const projected = rows.map((r) => (cols ? Object.fromEntries(Object.entries(r).filter(([k]) => cols.includes(k === 'label' ? 'title' : k))) : r));
    sendNDJSON(res, projected, endLine(rows.length), { 'atlas-profile': `"${older}"` });
  }, 2);
  const rows = await (await client.call('conformance.rows@v1:fetch', { n: 3 })).rows();
  assert.deepEqual(rows.map((r) => [r.title, r.colour]), [['row-0', 'blue'], ['row-1', 'UNKNOWN'], ['row-2', 'red']]);
  assert.equal('label' in rows[0], false, 'members no reader property took are dropped');
  const projected = await (await client.call('conformance.rows@v1:fetch', { n: 3, columns: ['i', 'title'] })).rows();
  assert.deepEqual(projected, [{ i: 0, title: 'row-0' }, { i: 1, title: 'row-1' }, { i: 2, title: 'row-2' }]);
});

// R024, §9.5.2 step 2 and §9.5.6: another version never resolves, and a chain that needs a lens is the gateway's to resolve.
test('TestR024UnresolvableWritersAreRefused', async (t) => {
  const { client, gw } = await rig(t);
  gw.on('conformance.echo@v1:fetch', (_, res) => sendJSON(res, 200, { text: 'x', count: 1, kind: 'plain' }, { 'atlas-profile': '"conformance.evolving@v2#eb84229aaeb7ca6b"' }));
  await assert.rejects(client.call('conformance.echo@v1:fetch', { text: 'x' }), problemIs('resolution_failed', (e) => /another version/.test(e.message)));
  // conformance.evolving@v1 #A, #B, #C: each lens holds lens operations, which this client does not implement.
  const reader = gw.current.get('conformance.evolving@v1')!;
  const older = gw.profiles.get(reader).evolution.previous_profile;
  gw.on('conformance.evolving@v1:fetch', (_, res) => sendNDJSON(res, [], endLine(0), { 'atlas-profile': `"${older}"` }));
  await assert.rejects(client.call('conformance.evolving@v1:fetch', { ids: ['a'] }), problemIs('resolution_failed', (e) => /lens/.test(e.message)));
});

// R086, CORE-CALL-004: Idempotency-Key is the String §9.6.3 requires, the same on every retry of a write.
test('TestR086IdempotencyKeyIsAStringKeptForEveryRetry', async (t) => {
  const { client, gw, clock } = await rig(t);
  const resource = { name: 'conformance/things/t1', metadata: { uid: 'u1', etag: '"e1"', generation: 1, created: '2026-11-01T09:00:00.000000Z', updated: '2026-11-01T09:00:00.000000Z', owner: '0b1e0000-0000-4000-8000-000000000001', labels: {}, deleting: null }, colour: 'red', size: 3 };
  gw.on('conformance.thing@v1:create', (_, res) => sendProblem(res, 503, 'unavailable', { retryable: true, retry_after: 1 }, { 'retry-after': '1' }));
  gw.on('conformance.thing@v1:create', (_, res) => sendJSON(res, 200, resource), 5);
  const r = await client.call('conformance.thing@v1:create', { name: 'conformance/things/t1', colour: 'red', size: 3 });
  assert.equal(r.record!.name, 'conformance/things/t1');
  const [a, b] = gw.to('conformance.thing@v1:create');
  assert.match(a.headers['idempotency-key'], /^"[0-9a-f-]{36}"$/);
  assert.equal(readIdempotencyKey(a.headers['idempotency-key']), a.headers['idempotency-key'].slice(1, -1));
  assert.equal(b.headers['idempotency-key'], a.headers['idempotency-key']);
  assert.deepEqual(clock.sleeps, [1000], 'the retry waited its retry_after');
  await client.call('conformance.thing@v1:create', { name: 'conformance/things/t1', colour: 'red', size: 3 }, { idempotencyKey: 'run_2026.10.31' });
  assert.equal(gw.to('conformance.thing@v1:create').at(-1)!.headers['idempotency-key'], '"run_2026.10.31"');
  const before = gw.seen.length;
  await assert.rejects(client.call('conformance.thing@v1:create', { name: 'conformance/things/t1', colour: 'red', size: 3 }, { idempotencyKey: 'a b' }), problemIs('invalid_request', (e) => e.target === 'idempotencyKey'));
  assert.equal(gw.seen.length, before, 'nothing is sent with a key that breaks its grammar');
  assert.equal(gw.to('conformance.echo@v1:fetch').length, 0);
});

// R087, R029, CORE-RETRY-001, CORE-PROB-002: a retryable problem is repeated at most three times, after its
// retry_after, never past the deadline; one that is not retryable is not repeated, whatever its status.
test('TestR087RetriesGoByRetryableAndRetryAfter', async (t) => {
  const { client, gw, clock } = await rig(t);
  const echo = 'conformance.echo@v1:fetch';
  const text = (s: { json?: any }) => s.json?.text;
  gw.steps.push({ match: (s) => s.address === echo && text(s) === 'x' && gw.to(echo).filter((y) => text(y) === 'x').length === 1, respond: (_, res) => sendProblem(res, 429, 'limit_exceeded', { retryable: true, retry_after: 1 }, { 'retry-after': '1' }) });
  gw.steps.push({ match: (s) => s.address === echo && text(s) === 'x' && gw.to(echo).filter((y) => text(y) === 'x').length === 2, respond: (_, res) => sendProblem(res, 503, 'unavailable', { retryable: true, retry_after: 2 }, { 'retry-after': '2' }) });
  gw.steps.push({ match: (s) => s.address === echo && text(s) === 'y', respond: (_, res) => sendProblem(res, 503, 'unavailable', { retryable: true, retry_after: 0 }, { 'retry-after': '0' }), times: 10 });
  gw.steps.push({ match: (s) => s.address === echo && text(s) === 'z', respond: (_, res) => sendProblem(res, 503, 'unavailable', { retryable: true, retry_after: 2 }, { 'retry-after': '2' }), times: 10 });
  gw.steps.push({ match: (s) => s.address === echo && text(s) === 'w', respond: (_, res) => sendProblem(res, 403, 'denied'), times: 10 });
  gw.steps.push({ match: (s) => s.address === echo && text(s) === 'a', respond: (_, res) => sendProblem(res, 503, 'conformance_strange', { retryable: true, retry_after: 0 }), times: 10 });
  gw.steps.push({ match: (s) => s.address === echo && text(s) === 'b', respond: (_, res) => sendProblem(res, 422, 'conformance_odd'), times: 10 });
  gw.steps.push({ match: (s) => s.address === echo && text(s) === 's', respond: (_, res) => sendProblem(res, 503, 'warehouse_suspended', { retryable: false }), times: 10 });
  const count = (v: string) => gw.to(echo).filter((s) => text(s) === v).length;
  assert.equal((await client.call(echo, { text: 'x' })).record!.text, 'x');
  assert.equal(count('x'), 3);
  assert.deepEqual(clock.sleeps, [1000, 2000], 'each retry no sooner than its retry_after');
  const at = gw.to(echo).filter((s) => text(s) === 'x').map((s) => s.at);
  assert.ok(at[1] - at[0] >= 1000 && at[2] - at[1] >= 2000);
  await assert.rejects(client.call(echo, { text: 'y' }), problemIs('unavailable'));
  assert.equal(count('y'), 4, 'at most three retries');
  await assert.rejects(client.call(echo, { text: 'z' }, { deadlineMs: 3000 }), problemIs('unavailable'));
  assert.equal(count('z'), 2, 'never past the deadline');
  await assert.rejects(client.call(echo, { text: 'w' }), problemIs('denied'));
  assert.equal(count('w'), 1);
  await assert.rejects(client.call(echo, { text: 'a' }), problemIs('conformance_strange', (e) => e.status === 503 && e.retryable === true));
  assert.equal(count('a'), 4, 'a code no client knows is handled by its retryable');
  await assert.rejects(client.call(echo, { text: 'b' }), problemIs('conformance_odd', (e) => e.status === 422 && !e.retryable));
  assert.equal(count('b'), 1);
  await assert.rejects(client.call(echo, { text: 's' }), problemIs('warehouse_suspended'));
  assert.equal(count('s'), 1, 'a 503 that is not retryable is not retried');
  assert.equal(clock.pending(), 0);
});

// R087, R029: upstream_failed (502) and in_progress (409) are retryable, and without retry_after the wait
// is min(30, 0.5 x 2^attempt) seconds plus up to 25% (the first retry's attempt is 0, A-162).
test('TestR087BackoffWithJitterWithoutRetryAfter', async (t) => {
  const { client, gw, clock } = await rig(t);
  gw.on('conformance.echo@v1:fetch', (_, res) => sendProblem(res, 502, 'upstream_failed', { retryable: true }), 2);
  gw.on('conformance.echo@v1:fetch', (_, res) => sendProblem(res, 409, 'in_progress', { retryable: true }), 1);
  assert.equal((await client.call('conformance.echo@v1:fetch', { text: 'x' })).record!.text, 'x');
  assert.equal(gw.to('conformance.echo@v1:fetch').length, 4);
  assert.deepEqual(clock.sleeps, [562.5, 1125, 2250], 'random() = 0.5 gives 12.5% jitter on 0.5 s, 1 s and 2 s');
});

// R088: a failure without a problem document gets the Go client's code for its status, and the client's own
// failures the same codes as Go's (not_found for a missing interaction and a missing binding).
test('TestR088ProblemCodesAreGos', async (t) => {
  const { client, gw } = await rig(t);
  const html = (status: number) => (_: unknown, res: any) => { res.writeHead(status, { 'content-type': 'text/html' }); res.end('<html>ingress</html>'); };
  gw.on('conformance.echo@v1:fetch', html(503), 4);
  await assert.rejects(client.call('conformance.echo@v1:fetch', { text: 'x' }), problemIs('unavailable', (e) => e.status === 503 && /without a problem document/.test(e.message)));
  gw.on('conformance.echo@v1:fetch', html(504), 1);
  await assert.rejects(client.call('conformance.echo@v1:fetch', { text: 'x' }), problemIs('deadline_exceeded', (e) => e.status === 504));
  gw.on('conformance.echo@v1:fetch', html(404), 1);
  await assert.rejects(client.call('conformance.echo@v1:fetch', { text: 'x' }), problemIs('not_found'));
  gw.on('conformance.echo@v1:fetch', html(500), 4);
  await assert.rejects(client.call('conformance.echo@v1:fetch', { text: 'x' }), problemIs('upstream_failed', (e) => e.status === 502));
  const before = gw.to('conformance.echo@v1:explain').length;
  await assert.rejects(client.call('conformance.echo@v1:explain', {}), problemIs('not_found', (e) => /has no interaction explain/.test(e.message)));
  assert.equal(gw.to('conformance.echo@v1:explain').length, before);
  await assert.rejects(client.call('conformance.rows@v1:export', { n: 1 }), problemIs('not_found', (e) => /no binding this client speaks/.test(e.message)));
  await assert.rejects(client.call('atlas.nothing@v1:fetch', {}), problemIs('not_found'));
  await assert.rejects(client.call('not an address', {}), problemIs('invalid_request', (e) => e.target === 'address'));
  await assert.rejects(client.call('conformance.echo@v1:fetch', { text: 'x', n: NaN }), problemIs('invalid_document', (e) => /NaN/.test(e.message)));
  await assert.rejects(client.call('conformance.echo@v1:fetch', { text: 'x' }, { deadlineMs: NaN }), problemIs('invalid_request', (e) => e.target === 'deadlineMs'));
});

// R088, CORE-PROB-001: a problem document becomes an AtlasProblem with every member as a property.
test('TestR088ProblemMembersAreProperties', async (t) => {
  const { client, gw } = await rig(t);
  gw.on('conformance.echo@v1:fetch', (_, res) => sendProblem(res, 403, 'not_granted', { target: '/text', detail: 'no', hop: 'warehouse', x_future: 7 }));
  const e = await client.call('conformance.echo@v1:fetch', { text: 'x' }).catch((x) => x);
  assert.ok(e instanceof AtlasProblem && e instanceof AtlasError && e instanceof Error);
  assert.deepEqual([e.code, e.status, e.retryable, e.target, e.next, e.request_id, (e as any).hop, (e as any).x_future], ['not_granted', 403, false, '/text', 'Do something else.', 'req-standin', 'warehouse', 7]);
  assert.equal(e.problem.type, 'https://atlas.jhg.com/problems/not_granted');
});

// CORE-CRIT-001, CORE-CALL-002: a critical trait this client does not implement, or a request that breaks its
// schema, fails before anything is sent; invalid_request names its keyword in every detail (Q-DOC-9).
test('TestClientRefusesBeforeSending', async (t) => {
  const { client, gw } = await rig(t);
  await assert.rejects(client.call('conformance.critical@v1:fetch', { text: 'x' }), problemIs('critical_trait_unsupported'));
  assert.equal(gw.to('conformance.critical@v1:fetch').length, 0);
  await assert.rejects(client.call('conformance.echo@v1:fetch', { text: '', extra: 1 }), problemIs('invalid_request', (e) => e.target === '/extra'
    && JSON.stringify(e.details) === JSON.stringify([
      { code: 'invalid_request', target: '/extra', keyword: 'additionalProperties', detail: '"extra" is not a member this object takes' },
      { code: 'invalid_request', target: '/text', keyword: 'minLength', detail: '0 characters, fewer than 1' }])));
  assert.equal(gw.to('conformance.echo@v1:fetch').length, 0);
});

// CORE-DEADLINE-001: Atlas-Deadline is the interaction's default, capped at its maximum, rounded down to the second.
test('TestDeadlineHeaderIsTheDefaultCappedAtTheMaximum', async (t) => {
  const { client, gw, clock } = await rig(t);
  await client.call('conformance.echo@v1:fetch', { text: 'a' });
  await client.call('conformance.echo@v1:fetch', { text: 'b' }, { deadlineMs: 600_000 });
  const [a, b] = gw.to('conformance.echo@v1:fetch');
  assert.equal(a.headers['atlas-deadline'], `@${Math.floor((clock.now() + 30_000) / 1000)}`);
  assert.equal(b.headers['atlas-deadline'], `@${Math.floor((clock.now() + 120_000) / 1000)}`);
  assert.equal(a.headers['atlas-profile-expect'], '"conformance.echo@v1#f17339273f0617c2"');
  assert.equal(a.headers['atlas-client'], 'name="@atlas/sdk", version="0.3.0", protocol="atlas/1.0"');
  assert.equal(a.headers['atlas-client-features'], 'levels=(core streaming), deliveries=(single chunked), encodings=(json ndjson)');
  assert.match(a.headers.traceparent, /^00-[0-9a-f]{32}-[0-9a-f]{16}-01$/);
});

/** Resolves when the stand-in has received a request to the address, and leaves it unanswered until close. */
function stall(gw: StandIn, address: string, rows: unknown[] = []): Promise<void> {
  return new Promise((ok) => {
    gw.on(address, (_, res) => {
      if (rows.length) {
        res.writeHead(200, { 'content-type': 'application/x-ndjson' });
        for (const r of rows) res.write(JSON.stringify(r) + '\n');
      }
      ok();
    });
  });
}

// CORE-DEADLINE-001, R088, PR06's Node part: a call still running at its deadline ends deadline_exceeded,
// whether it waits for the answer's head or for its body; the deadline bounds the whole exchange.
test('TestDeadlineBoundsTheWholeExchange', async (t) => {
  const { client, gw, clock } = await rig(t);
  const arrived = stall(gw, 'conformance.echo@v1:fetch');
  const call = client.call('conformance.echo@v1:fetch', { text: 'slow' }, { deadlineMs: 1000 });
  await arrived;
  clock.advance(1001);
  await assert.rejects(call, problemIs('deadline_exceeded'));
  const streaming = stall(gw, 'conformance.rows@v1:fetch', [conformanceRow(0), conformanceRow(1)]);
  const r = await client.call('conformance.rows@v1:fetch', { n: 5 }, { deadlineMs: 5000 });
  await streaming;
  const it = r[Symbol.asyncIterator]();
  assert.deepEqual((await it.next()).value, conformanceRow(0));
  assert.deepEqual((await it.next()).value, conformanceRow(1));
  const third = it.next();
  clock.advance(5001);
  await assert.rejects(third, problemIs('deadline_exceeded'), 'a deadline that passes mid-stream is deadline_exceeded, not a raw TimeoutError');
  assert.equal(clock.pending(), 0);
});

// Every outbound call takes the caller's signal: aborting it ends the call with the caller's reason.
test('TestCallerSignalEndsTheCall', async (t) => {
  const { client, gw } = await rig(t);
  const arrived = stall(gw, 'conformance.echo@v1:fetch');
  const stop = new AbortController();
  const call = client.call('conformance.echo@v1:fetch', { text: 'slow' }, { signal: stop.signal });
  await arrived;
  const reason = new Error('the caller stopped');
  stop.abort(reason);
  await assert.rejects(call, (e) => e === reason);
});

// R002, PR01, R26: a gateway that only claims the environment of a stored sign-in receives neither the access
// token nor the refresh token, and the store is not rewritten.
test('TestR002CredentialsGoOnlyToTheGatewayTheyAreBoundTo', async (t) => {
  const { home, clock } = await rig(t);
  const store = join(home, 'state', 'token-conformance.json');
  const bytes = readFileSync(store);
  const rogue = await StandIn.start();
  t.after(() => rogue.close());
  rogue.on('atlas.whoami:fetch', (_, res) => sendProblem(res, 401, 'unauthenticated', {}, { 'www-authenticate': 'Bearer realm="atlas", error="invalid_token"' }), 5);
  const c = await atlas.connect({ url: rogue.url, home, clock });
  await assert.rejects(c.whoami(), problemIs('unauthenticated', (e) => /was made at http:\/\/127\.0\.0\.1:\d+, not at/.test(e.message)));
  assert.equal(rogue.seen.filter((s) => s.headers.authorization !== undefined).length, 0, 'no Authorization reached the rogue');
  assert.equal(rogue.seen.filter((s) => s.path === '/oauth/token').length, 0, 'no refresh token reached the rogue');
  assert.ok(readFileSync(store).equals(bytes), 'the store is unchanged');
});

// R002, R26: the bound gateway's bootstrap cannot redirect the credential either: not to another server,
// and not to another token endpoint.
test('TestR002ABootstrapCannotRedirectACredential', async (t) => {
  const elsewhere = await StandIn.start();
  t.after(() => elsewhere.close());
  for (const change of [
    (b: any) => { b.servers.gateway.url = elsewhere.url; },
    (b: any) => { b.sign_in.token_endpoint = `${elsewhere.url}/oauth/token`; },
    (b: any) => { b.sign_in.scopes = ['api://other/.default']; },
  ]) {
    const { gw, home, clock } = await rig(t);
    change(gw.bootstrap);
    const c = await atlas.connect({ url: gw.url, home, clock });
    await assert.rejects(c.whoami(), problemIs('unauthenticated'));
  }
  assert.equal(elsewhere.seen.length, 0, 'nothing reached the other server');
});

// R002, R26, as the Go client reads the store: a sign-in that records no binding (an older client's, a test's,
// the conformance suite's) is used only by a local or conformance gateway on this machine, never by another.
test('TestR002AnUnboundSignInOnlyForLocalGateways', async (t) => {
  const { gw, home, clock } = await rig(t);
  const { writeFileSync } = await import('node:fs');
  const unbound = JSON.stringify({ access_token: 'ct-valid', expires_at: 0 });
  writeFileSync(join(home, 'state', 'token-conformance.json'), unbound, { mode: 0o600 });
  await (await atlas.connect({ url: gw.url, home, clock })).whoami();
  const dv = await StandIn.start({ bootstrap: (b) => { b.environment = 'dv'; } });
  t.after(() => dv.close());
  writeFileSync(join(home, 'state', 'token-dv.json'), unbound, { mode: 0o600 });
  await assert.rejects((await atlas.connect({ url: dv.url, home, clock })).whoami(), problemIs('unauthenticated', (e) => /not bound to a gateway/.test(e.message)));
  assert.equal(dv.seen.filter((s) => s.headers.authorization !== undefined).length, 0);
});

// R26 "refresh is serialised", with the Go client's lock: a lock another program holds is waited for, within
// the call's deadline; one older than a minute is a crashed holder's and is broken.
test('TestR002RefreshWaitsForTheLock', async (t) => {
  const { gw, home, clock } = await rig(t);
  const { writeFileSync, utimesSync, existsSync } = await import('node:fs');
  await signIn({ url: gw.url, home, accessToken: 'ct-old', refreshToken: 'rt-valid', expiresIn: 30, clock });
  const lock = join(home, 'state', 'token-conformance.json.lock');
  writeFileSync(lock, '4242\n', { mode: 0o600 });
  const c = await atlas.connect({ url: gw.url, home, clock });
  await assert.rejects(c.call('atlas.whoami@v1:fetch', {}, { deadlineMs: 1000 }), problemIs('deadline_exceeded'));
  assert.equal(gw.seen.filter((s) => s.path === '/oauth/token').length, 0, 'nothing redeemed while another program holds the lock');
  const old = (Date.now() - 120_000) / 1000;
  utimesSync(lock, old, old);
  await c.whoami();
  assert.equal(gw.seen.filter((s) => s.path === '/oauth/token').length, 1);
  assert.equal(existsSync(lock), false, 'the lock is released');
});

// §9.7.3 as the Go client writes it: a token whose life is not known has expires_at 0, and is never refreshed early.
test('TestSigninUnknownLifetime', async (t) => {
  const { gw, home, clock } = await rig(t);
  await signIn({ url: gw.url, home, accessToken: 'ct-valid', refreshToken: 'rt-valid', clock });
  assert.equal(JSON.parse(readFileSync(join(home, 'state', 'token-conformance.json'), 'utf8')).expires_at, 0);
  await (await atlas.connect({ url: gw.url, home, clock })).whoami();
  assert.equal(gw.seen.filter((s) => s.path === '/oauth/token').length, 0);
});

// R046: a bootstrap whose environment is not a name is refused at connect, before any file is read.
test('TestR046BootstrapEnvironmentIsValidated', async (t) => {
  const { home, clock } = await rig(t);
  const { writeFileSync } = await import('node:fs');
  writeFileSync(join(home, 'other-tool.json'), JSON.stringify({ access_token: 'ANOTHER-TOOLS-TOKEN', expires_at: 9999999999 }), { mode: 0o600 });
  const evil = await StandIn.start({ bootstrap: (b) => { b.environment = 'x/../../other-tool'; } });
  t.after(() => evil.close());
  await assert.rejects(atlas.connect({ url: evil.url, home, clock }), problemIs('invalid_document', (e) => e.target === '/environment'));
  assert.equal(evil.seen.filter((s) => s.headers.authorization !== undefined).length, 0);
});

// CORE-SIGNIN-002, R26: a token within a minute of expires_at is refreshed first, at the bound token endpoint;
// the store is rewritten atomically, its binding kept, mode 0600.
test('TestSigninRefreshKeepsTheBinding', async (t) => {
  const { gw, home, clock } = await rig(t);
  await signIn({ url: gw.url, home, accessToken: 'ct-old', refreshToken: 'rt-valid', expiresIn: 30, clock });
  const c = await atlas.connect({ url: gw.url, home, clock });
  await c.whoami();
  const refreshes = gw.seen.filter((s) => s.path === '/oauth/token');
  assert.equal(refreshes.length, 1);
  assert.equal(new URLSearchParams(refreshes[0].body).get('refresh_token'), 'rt-valid');
  assert.equal(gw.to('atlas.whoami:fetch').at(-1)!.headers.authorization, 'Bearer ct-refreshed');
  const { statSync } = await import('node:fs');
  const path = join(home, 'state', 'token-conformance.json');
  const doc = JSON.parse(readFileSync(path, 'utf8'));
  assert.equal(doc.access_token, 'ct-refreshed');
  assert.equal(doc.refresh_token, 'rt-next');
  assert.equal(doc.binding.gateway, gw.url);
  assert.equal(statSync(path).mode & 0o777, 0o600);
  assert.equal(statSync(join(home, 'state')).mode & 0o777, 0o700);
});

// CORE-SIGNIN-003 and -004: invalid_token is renewed once and the call repeated; a claims challenge is not
// renewed, and the problem carries the challenge.
test('TestSigninChallenges', async (t) => {
  const { gw, client } = await rig(t);
  gw.on('atlas.whoami:fetch', (_, res) => sendProblem(res, 401, 'unauthenticated', {}, { 'www-authenticate': 'Bearer realm="atlas", error="invalid_token"' }));
  await client.whoami();
  assert.equal(gw.seen.filter((s) => s.path === '/oauth/token').length, 1);
  assert.equal(gw.to('atlas.whoami:fetch').length, 2);
  const claims = 'Bearer realm="atlas", claims="eyJhY2Nlc3NfdG9rZW4iOnt9fQ"';
  gw.on('atlas.whoami:fetch', (_, res) => sendProblem(res, 401, 'unauthenticated', {}, { 'www-authenticate': claims }));
  await assert.rejects(client.whoami(), problemIs('unauthenticated', (e) => e.details.some((d) => d.challenge === claims)));
  assert.equal(gw.seen.filter((s) => s.path === '/oauth/token').length, 1, 'no refresh for a claims challenge');
});

// R26 "refresh is serialised": two clients sharing the store, each its own process's view, refresh once.
test('TestR002RefreshIsSerialisedAcrossClients', async (t) => {
  const gw = await StandIn.start();
  const home = mkdtempSync(join(tmpdir(), 'atlas-node-'));
  t.after(async () => { await gw.close(); rmSync(home, { recursive: true, force: true }); });
  await signIn({ url: gw.url, home, accessToken: 'ct-old', refreshToken: 'rt-valid', expiresIn: 30 });
  const [a, b] = await Promise.all([atlas.connect({ url: gw.url, home }), atlas.connect({ url: gw.url, home })]);
  await Promise.all([a.whoami(), b.whoami()]);
  assert.equal(gw.seen.filter((s) => s.path === '/oauth/token').length, 1, 'the refresh token is redeemed once');
  assert.deepEqual(gw.to('atlas.whoami:fetch').map((s) => s.headers.authorization), ['Bearer ct-refreshed', 'Bearer ct-refreshed']);
});

// R26 (the review's regression): calls that share one refresh each wait no longer than their own signal, and one
// call's abort never fails another: the refresh runs under its own bound, not the first caller's.
test('TestR002OneCallsAbortNeverFailsAnotherSharingTheRefresh', async (t) => {
  const gw = await StandIn.start();
  const home = mkdtempSync(join(tmpdir(), 'atlas-node-'));
  t.after(async () => { await gw.close(); rmSync(home, { recursive: true, force: true }); });
  await signIn({ url: gw.url, home, accessToken: 'ct-old', refreshToken: 'rt-valid', expiresIn: 30 });
  const client = await atlas.connect({ url: gw.url, home });
  const release = gw.holdTokens();
  const first = new AbortController();
  const a = client.call('atlas.whoami@v1:fetch', {}, { signal: first.signal });
  const b = client.call('atlas.whoami@v1:fetch', {});
  while (gw.seen.filter((s) => s.path === '/oauth/token').length === 0) await new Promise((ok) => setTimeout(ok, 5));
  first.abort(new Error('the first caller gave up'));
  await assert.rejects(a, /the first caller gave up/);
  release();
  const answer = await b;
  assert.ok(answer, 'the second call, sharing the refresh, still gets its answer');
  assert.equal(gw.seen.filter((s) => s.path === '/oauth/token').length, 1, 'the refresh token is redeemed once');
});

// A workload's token goes only to the gateway it was given for (R26), never to another server its bootstrap names.
test('TestExplicitTokenStaysWithItsGateway', async (t) => {
  const elsewhere = await StandIn.start();
  t.after(() => elsewhere.close());
  const gw = await StandIn.start({ bootstrap: (b) => { b.servers.gateway.url = elsewhere.url; } });
  t.after(() => gw.close());
  const c = await atlas.connect({ url: gw.url, token: 'ct-valid', home: mkdtempSync(join(tmpdir(), 'atlas-node-')) });
  await assert.rejects(c.whoami(), problemIs('unauthenticated'));
  assert.equal(elsewhere.seen.length, 0);
});

// R3-03: a refresh names the application the sign-in was bound to (client_id, which Entra requires) and its scopes.
// A token endpoint registered for that application renews the sign-in; one registered for another refuses it.
test('TestR3_03RefreshNamesTheBoundApplication', async (t) => {
  for (const [registered, renewed] of [['atlas-conformance', true], ['someone-else', false]] as const) {
    const gw = await StandIn.start();
    const home = mkdtempSync(join(tmpdir(), 'atlas-node-'));
    t.after(async () => { await gw.close(); rmSync(home, { recursive: true, force: true }); });
    gw.application = registered;
    await signIn({ url: gw.url, home, accessToken: 'ct-old', refreshToken: 'rt-valid', expiresIn: 30 });
    const client = await atlas.connect({ url: gw.url, home });
    if (!renewed) {
      await assert.rejects(client.whoami(), problemIs('unauthenticated'));
      continue;
    }
    await client.whoami();
    const sent = gw.seen.filter((s) => s.path === '/oauth/token');
    assert.equal(sent.length, 1);
    const form = new URLSearchParams(sent[0].body);
    assert.equal(form.get('client_id'), 'atlas-conformance');
    assert.equal(form.get('scope'), `${gw.url}/.default`);
    assert.equal(form.get('refresh_token'), 'rt-valid');
  }
});

// R3-05: a caller that gives up never leaves a refresh's rejection unhandled, which would end the process. A caller
// whose signal is already aborted starts no refresh at all; one that gives up as it starts, while its credentials are
// read, starts none either or leaves it observed; a sole caller that gives up while the refresh runs, which then fails,
// leaves its rejection observed; and another caller sharing a refresh still gets its answer.
test('TestR3_05ACancelledCallerLeavesNoUnhandledRejection', async (t) => {
  const unhandled: unknown[] = [];
  const onUnhandled = (e: unknown) => { unhandled.push(e); };
  process.on('unhandledRejection', onUnhandled);
  t.after(() => { process.off('unhandledRejection', onUnhandled); });
  // until waits for cond a bounded number of event-loop turns: what it waits for is this process's own I/O.
  const until = async (cond: () => boolean, what: string) => {
    for (let i = 0; i < 2000 && !cond(); i++) await new Promise((ok) => setTimeout(ok, 1));
    assert.ok(cond(), `timed out waiting for ${what}`);
  };
  const settle = async () => { for (let i = 0; i < 10; i++) await new Promise((ok) => setImmediate(ok)); };
  const start = async (refreshToken: string) => {
    const gw = await StandIn.start();
    const home = mkdtempSync(join(tmpdir(), 'atlas-node-'));
    t.after(async () => { await gw.close(); rmSync(home, { recursive: true, force: true }); });
    await signIn({ url: gw.url, home, accessToken: 'ct-old', refreshToken, expiresIn: 30 });
    return { gw, client: await atlas.connect({ url: gw.url, home }) };
  };
  const refreshes = (gw: StandIn) => gw.seen.filter((s) => s.path === '/oauth/token').length;

  { // already aborted
    const { gw, client } = await start('rt-unknown');
    const gone = new AbortController();
    gone.abort(new Error('the caller had already given up'));
    await assert.rejects(client.call('atlas.whoami@v1:fetch', {}, { signal: gone.signal }), /already given up/);
    await settle();
    assert.equal(refreshes(gw), 0, 'a caller that had given up started no refresh');
  }
  { // aborted as it starts, while its credentials are read
    const { gw, client } = await start('rt-unknown');
    const gone = new AbortController();
    const a = client.call('atlas.whoami@v1:fetch', {}, { signal: gone.signal });
    gone.abort(new Error('the caller gave up at once'));
    await assert.rejects(a, /gave up at once/);
    await until(() => gw.tokenAnswers === refreshes(gw), 'any refresh to be answered');
    await settle();
  }
  { // gives up during a refresh that then fails
    const { gw, client } = await start('rt-unknown');
    const release = gw.holdTokens();
    const gone = new AbortController();
    const a = client.call('atlas.whoami@v1:fetch', {}, { signal: gone.signal });
    await until(() => refreshes(gw) === 1, 'the refresh to start');
    gone.abort(new Error('the caller gave up'));
    await assert.rejects(a, /the caller gave up/);
    release();
    await until(() => gw.tokenAnswers === 1, 'the refresh to be answered');
    await settle();
  }
  { // another caller sharing the refresh continues
    const { gw, client } = await start('rt-valid');
    const release = gw.holdTokens();
    const gone = new AbortController();
    const a = client.call('atlas.whoami@v1:fetch', {}, { signal: gone.signal });
    const b = client.call('atlas.whoami@v1:fetch', {});
    await until(() => refreshes(gw) === 1, 'the refresh to start');
    gone.abort(new Error('the first caller gave up'));
    await assert.rejects(a, /the first caller gave up/);
    release();
    assert.ok(await b, 'the caller that stayed gets its answer');
    assert.equal(refreshes(gw), 1);
  }
  assert.deepEqual(unhandled, [], 'no refresh rejection was left unhandled');
});

// R3-06, HANDOFF.md C2: one absolute deadline per public call. The time discovery takes comes out of the call's own
// deadline, so a slow describe never earns the request a fresh budget: the request's Atlas-Deadline is the call's start
// plus its deadline. A discovery that uses the whole deadline sends no request at all.
test('TestR3_06DiscoveryComesOutOfTheCallsDeadline', async (t) => {
  const r = await rig(t);
  const start = r.clock.now();
  r.gw.on('atlas.describe:fetch', (seen, res) => { r.clock.advance(900); (r.gw as any).serve(seen, res); });
  await r.client.call('conformance.echo@v1:fetch', { text: 'hi' }, { deadlineMs: 1500 });
  const sent = r.gw.to('conformance.echo@v1:fetch');
  assert.equal(sent.length, 1);
  assert.equal(sent[0].headers['atlas-deadline'], formatDeadline(start + 1500), 'the request carries the call\'s own deadline');

  const late = await rig(t);
  late.gw.on('atlas.describe:fetch', (seen, res) => { late.clock.advance(1600); (late.gw as any).serve(seen, res); });
  await assert.rejects(late.client.call('conformance.echo@v1:fetch', { text: 'hi' }, { deadlineMs: 1500 }), problemIs('deadline_exceeded'));
  assert.equal(late.gw.to('conformance.echo@v1:fetch').length, 0, 'nothing is sent once the deadline has passed');
});
