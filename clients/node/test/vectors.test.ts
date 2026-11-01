// The protocol's vectors (spec/protocol/references/vectors) that a client
// passes, read with Node's own I-JSON reader: §9.8 "Each implementation
// reads references/vectors/*.json in its own unit tests and checks every
// case". R055 (Node runs no vector), R012's TypeScript half.

import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { test } from 'node:test';

import { DIR as DOCUMENTS, validateDocument } from '../src/documents.ts';
import { RULES } from '../src/grammar.ts';
import { readHeader } from '../src/headers.ts';
import { canonical, parse } from '../src/ijson.ts';
import { fingerprint } from '../src/index.ts';
import { exitCode, PROBLEM_BASE, REGISTRY } from '../src/problem.ts';
import { resolve, ResolutionFailure } from '../src/resolve.ts';
import { badPatterns, Validator } from '../src/schema.ts';
import { parse as parseSF, serialize } from '../src/sf.ts';
import { REFS, refs } from './standin.ts';

// §9.3.3 and §9.7.6: every code's status, retryable, type and exit code, in the registry's order.
test('TestVectorsProblems', () => {
  const v = refs('vectors/problems.json');
  assert.equal(v.type_base, PROBLEM_BASE);
  const registered = [...REGISTRY.keys()];
  let row = 0;
  for (const c of v.cases) {
    assert.equal(exitCode(c.code), c.expected.cli_exit, `${c.name}: exit code`);
    if (c.expected.status === undefined) {
      assert.equal(REGISTRY.has(c.code), false, `${c.code} is not in the registry`);
      continue;
    }
    assert.equal(registered[row], c.code, `row ${row} of the registry`);
    const r = REGISTRY.get(c.code)!;
    assert.deepEqual([r.status, r.retryable, PROBLEM_BASE + c.code], [c.expected.status, c.expected.retryable, c.expected.type], c.name);
    row++;
  }
  assert.equal(row, registered.length);
});

// AP-CONV-002: every client accepts exactly the strings the grammars accept.
test('TestVectorsGrammar', () => {
  const v = refs('vectors/grammar.json');
  for (const rule of v.rules) assert.ok(RULES[rule], `no rule ${rule}`);
  for (const c of v.cases) assert.equal(RULES[c.rule](c.input), c.expected.valid, c.name);
  assert.equal(v.cases.length, 229);
});

// AP-HTTP-001: every Atlas header in the header vectors reads exactly, and the malformed ones are absent.
test('TestVectorsHeaders', () => {
  const v = refs('vectors/headers.json');
  const now = Date.UTC(2026, 9, 31); // Q-HDR-3's reference year
  for (const c of v.cases) {
    const got = readHeader(c.header, c.lines.join(', '), now);
    if (c.expected.absent) assert.equal(got, undefined, `${c.name}: the vectors say absent (${c.expected.why})`);
    else assert.equal(canonical(got as any), canonical(c.expected.value), c.name);
  }
});

// RFC 9651 under every Atlas header: the same values parse, and each serialises to the same text (Q-SF-1).
test('TestVectorsStructuredFields', () => {
  const v = refs('vectors/structured-fields.json');
  let n = 0;
  for (const c of v.cases) {
    for (const t of ['item', 'list', 'dictionary'] as const) {
      const e = c.expected[t];
      let got: string | undefined;
      try { got = serialize(parseSF(c.field, t as any)); } catch { got = undefined; }
      assert.equal(got !== undefined, e.ok, `${c.name} as ${t}`);
      if (e.ok && e.serialised !== undefined) assert.equal(got, e.serialised, `${c.name} as ${t}: serialised`);
      n++;
    }
  }
  assert.equal(n, 4089 * 3);
});

// AP-SCHEMA-001: the validator gives every validation vector's result, failures sorted by pointer then keyword (Q-VAL-1).
test('TestVectorsSchemasValidate', () => {
  const v = refs('vectors/schemas.json');
  let n = 0;
  for (const c of v.cases) {
    if (c.kind !== 'validate') continue;
    const instance = 'instance_text' in c ? parse(c.instance_text) : c.instance;
    const got = new Validator(c.schema).validate(instance).map((f) => ({ pointer: f.pointer, keyword: f.keyword }));
    assert.deepEqual(got, c.expected.errors, c.name);
    n++;
  }
  assert.equal(n, 119);
});

// §9.2.4 and Q-RE-1: a pattern that is not ^R$ with R an I-Regexp makes a profile unusable; every one the profile vectors refuse for its pattern is refused here, and no accepted one.
test('TestVectorsSchemasPatterns', () => {
  const v = refs('vectors/schemas.json');
  for (const c of v.cases) {
    if (c.kind !== 'profile') continue;
    const bad = badPatterns(c.schema);
    if (c.problem && c.rule === 'pattern') assert.deepEqual(bad, [c.problem.target], c.name);
    else if (!c.problem) assert.deepEqual(bad, [], c.name);
  }
});

// AP-FP-001: every fingerprint vector's canonical form, digest and fingerprint; a client refuses a profile whose fingerprint is wrong.
test('TestVectorsFingerprints', async () => {
  const { createHash } = await import('node:crypto');
  const { project } = await import('../src/index.ts');
  for (const c of refs('vectors/fingerprints.json').cases) {
    const text = canonical(project(c.profile));
    assert.equal(text, c.expected.canonical, `${c.name}: canonical form`);
    assert.equal(createHash('sha256').update(text, 'utf8').digest('hex'), c.expected.sha256, `${c.name}: sha256`);
    assert.equal(fingerprint(c.profile), c.expected.fingerprint, `${c.name}: fingerprint`);
  }
  for (const file of readdirSync(join(REFS, 'vectors', 'profiles'))) {
    for (const c of refs(`vectors/profiles/${file}`).cases) {
      assert.equal(fingerprint(c.profile), c.expected.fingerprint, `${file}: fingerprint`);
      assert.equal(`${c.profile.identity.id}@v${c.profile.identity.version}#${fingerprint(c.profile)}`, c.expected.exact, `${file}: exact id`);
      assert.deepEqual(validateDocument('profile', c.profile), [], `${file}: valid against profile.schema.json`);
    }
  }
});

// AP-RESOLVE-001, narrowed by REVIEW.md (R03, R04): a client is obliged to aliases, defaults, UNKNOWN and projection.
// Every case whose span of the chain declares no lens gives exactly the expected record or problem;
// a case that needs a lens operation is refused, because Node declares none and the gateway resolves it (§9.5.6, A-165).
test('TestVectorsResolution', () => {
  let exact = 0;
  let refusedForLens = 0;
  for (const c of refs('vectors/resolution.json').cases) {
    const chain = c.chain.map((p: any) => ({ exact: p.profile, schema: p.schema, lens: p.lens }));
    let got: { record?: unknown; problem?: { code: string; target: string }; why?: string };
    try {
      got = { record: resolve(chain, c.writer, c.reader, c.record, c.columns, c.dynamic_schema === true) };
    } catch (e) {
      assert.ok(e instanceof ResolutionFailure, `${c.name}: threw ${e}`);
      got = { problem: { code: 'resolution_failed', target: e.target }, why: e.message };
    }
    if (got.why?.includes('declares a lens')) {
      refusedForLens++;
      continue;
    }
    exact++;
    if (c.problem) {
      assert.equal(got.problem?.code, c.problem.code, c.name);
      if (c.problem.target !== undefined) assert.equal(got.problem?.target, c.problem.target, `${c.name}: target`);
    } else {
      assert.equal(canonical(got.record as any), canonical(c.expected), c.name);
    }
  }
  assert.ok(exact >= 12, `${exact} cases resolved without a lens`);
  assert.equal(exact + refusedForLens, 42);
});

// The embedded documents are the references' own, byte for byte (the Go client's TestTheDocumentsAreTheReferences).
test('TestDocumentsAreTheReferences', () => {
  const names = readdirSync(DOCUMENTS).filter((n) => n.endsWith('.schema.json')).sort();
  assert.equal(names.length, 21);
  assert.deepEqual(names, readdirSync(join(REFS, 'schemas')).filter((n) => n.endsWith('.schema.json')).sort());
  for (const n of names) {
    assert.ok(readFileSync(new URL(n, DOCUMENTS)).equals(readFileSync(join(REFS, 'schemas', n))), `${n} has drifted from spec/protocol/references/schemas`);
  }
});

// AP-DOC-001's client half: the fixture's bootstrap is valid, and what the documents refuse is refused, through every keyword they use.
test('TestDocumentsValidate', () => {
  const fixture = refs('scenarios/fixtures/gateway.json');
  const bootstrap = parse(JSON.stringify(fixture.bootstrap).replaceAll('{gateway}', 'https://gateway.example')) as any;
  assert.deepEqual(validateDocument('bootstrap', bootstrap), []);
  const cases: [string, unknown, string, string][] = [
    ['binding', { protocol: 'https', server: 'gateway', address: '/x', auth: 'bearer' }, '/method', 'required'],
    ['bootstrap', { ...bootstrap, servers: { Gateway: bootstrap.servers.gateway } }, '/servers/Gateway', 'propertyNames'],
    ['bootstrap', { ...bootstrap, environment: 'x/../../other-tool' }, '/environment', 'pattern'],
    ['operation', { name: 'operations/a1', address: 'x.y@v1:fetch', state: 'succeeded', created: '2026-10-31T00:00:00Z', updated: '2026-10-31T00:00:00Z' }, '', 'anyOf'],
    ['chunk-end', { $atlas: 'end', rows: 0, truncated: false, usage: {} }, '/problem', 'required'],
    ['problem', { type: 'https://atlas.jhg.com/problems/internal', status: 500, code: 'internal', retryable: false, next: 'Retry.' }, '/request_id', 'required'],
    ['builtins#search_request', { words: 'holdings', page_size: 501 }, '/page_size', 'maximum'],
  ];
  for (const [doc, value, pointer, keyword] of cases) {
    const fs = validateDocument(doc, value as any);
    assert.ok(fs.some((f) => f.pointer === pointer && f.keyword === keyword), `${doc} ${JSON.stringify(value).slice(0, 60)}: ${JSON.stringify(fs)}`);
  }
  // Q-DOC-1: a gateway's document with an enum value this client does not know reads as UNKNOWN, not as invalid.
  const newer = structuredClone(bootstrap);
  newer.builtins['atlas.whoami'].interactions.fetch.bindings[0].protocol = 'grpc';
  assert.deepEqual(validateDocument('bootstrap', newer), []);
});
