// The I-JSON reader (§9.1.2) that every document Node receives goes
// through, against the cases R025 measured and the rules of RFC 7493.
// ATLAS-CORE: AP-CONV-001 (duplicate names, a number outside I-JSON), R025,
// R055 (the TypeScript half of R12: decode the same fixtures in Go and
// TypeScript).

import assert from 'node:assert/strict';
import { test } from 'node:test';

import { canonical, decode, fromValue, MAX_DEPTH, NotIJson, parse, parseBytes, wellFormed } from '../src/ijson.ts';
import { iterations, mutate, rng, seed } from './fuzz.ts';

const enc = (s: string) => new TextEncoder().encode(s);
// A JSON u-escape, built at run time so that no source file holds one.
const u = (hex: string) => String.fromCharCode(92) + 'u' + hex;

function refused(text: string | Uint8Array, why: RegExp): void {
  assert.throws(() => (typeof text === 'string' ? parse(text) : parseBytes(text)), (e: unknown) => e instanceof NotIJson && why.test(e.message), `${String(text)} must be refused (${why})`);
}

// R025, AP-CONV-001: a duplicate member is refused at any depth, never the last one kept.
test('TestR025DuplicateMembersAreRefused', () => {
  refused('{"weight":1,"weight":0.999}', /duplicate member "weight"/);
  refused('[{"a":{"b":1,"b":2}}]', /duplicate member "b"/);
  assert.deepEqual(parse('{"a":{"b":1},"c":{"b":2}}'), { a: { b: 1 }, c: { b: 2 } });
});

// R025: a byte-order mark is refused, as bytes and as text; fetch's text() would have dropped it.
test('TestR025ByteOrderMarkIsRefused', () => {
  refused(new Uint8Array([0xef, 0xbb, 0xbf, 0x7b, 0x7d]), /byte-order mark/);
  refused(String.fromCharCode(0xfeff) + '{}', /byte-order mark/);
});

// R025: invalid UTF-8 is refused, never turned into U+FFFD.
test('TestR025InvalidUTF8IsRefused', () => {
  refused(new Uint8Array([0x22, 0xc3, 0x28, 0x22]), /invalid UTF-8/);
  refused(new Uint8Array([0x22, 0xed, 0xa0, 0x80, 0x22]), /invalid UTF-8/); // a surrogate encoded in UTF-8
  assert.throws(() => decode(new Uint8Array([0xff])), NotIJson);
});

// R025, §9.1.2 rule 3: unpaired surrogates, escaped or raw, are refused.
test('TestR025LoneSurrogatesAreRefused', () => {
  refused(`"${u('d800')}"`, /unpaired surrogate/);
  refused(`"${u('dc00')}"`, /unpaired surrogate/);
  refused(`"${u('d800')}${u('0041')}"`, /unpaired surrogate/);
  refused(`"${u('d800')}x"`, /unpaired surrogate/);
  refused('"' + String.fromCharCode(0xd800) + '"', /unpaired surrogate/);
  assert.equal(parse(`"${u('d83d')}${u('de00')}"`), String.fromCodePoint(0x1f600));
});

// §9.1.2 rule 3: noncharacters are refused, escaped or not, in every plane.
test('TestR025NoncharactersAreRefused', () => {
  refused(`"${u('fdd0')}"`, /noncharacter U\+FDD0/);
  refused(`"${u('fffe')}"`, /noncharacter U\+FFFE/);
  refused('"' + String.fromCharCode(0xffff) + '"', /noncharacter U\+FFFF/);
  refused(`"${u('d83f')}${u('dffe')}"`, /noncharacter U\+1FFFE/);
  refused('"' + String.fromCodePoint(0x10ffff) + '"', /noncharacter U\+10FFFF/);
  assert.equal(parse(`"${u('fdcf')}"`), String.fromCharCode(0xfdcf));
});

// R025, §9.1.2 rule 4: a number beyond binary64 is refused, never Infinity; one within is its binary64 value.
test('TestR025NumbersOutOfRangeAreRefused', () => {
  refused('{"weight":1e400}', /beyond binary64/);
  refused('-1e400', /beyond binary64/);
  refused('1e00001', /exponent beyond binary64/); // the Go reader's rule: more than four exponent digits
  assert.equal(parse('1e-400'), 0);
  assert.equal(parse('9007199254740993'), 9007199254740992);
  assert.equal(parse('1E2'), 100);
  assert.ok(Object.is(parse('-0'), -0));
  for (const bad of ['01', '1.', '.5', '+1', '1e', '-', '0x10', 'NaN', 'Infinity']) refused(bad, /./);
});

test('TestR025TrailingDataAndControlCharactersAreRefused', () => {
  refused('{} {}', /data after the document/);
  refused('"a\tb"', /control character/);
  refused('"a', /unterminated/);
  refused(`"${String.fromCharCode(92)}x"`, /unknown escape/);
  assert.deepEqual(parse(' \n\t{"a":[1,true,false,null,"s"]}\r\n '), { a: [1, true, false, null, 's'] });
});

// The Go reader's depth: 128 levels are read, 129 are refused (A-163).
test('TestR025DepthIsBounded', () => {
  assert.doesNotThrow(() => parse('['.repeat(MAX_DEPTH) + ']'.repeat(MAX_DEPTH)));
  refused('['.repeat(MAX_DEPTH + 1) + ']'.repeat(MAX_DEPTH + 1), /nested deeper than 128/);
  refused('{"a":'.repeat(MAX_DEPTH + 1) + '1' + '}'.repeat(MAX_DEPTH + 1), /nested deeper than 128/);
});

test('TestR025ProtoIsAnOrdinaryMember', () => {
  const v = parse('{"__proto__":{"polluted":true},"b":1}') as any;
  assert.equal(({} as any).polluted, undefined);
  assert.deepEqual(Object.keys(v), ['__proto__', 'b']);
  assert.equal(Object.getPrototypeOf(v), Object.prototype);
});

// Q-ARROW-1: member order is kept, a schema's properties order with it.
test('TestR025MemberOrderIsKept', () => {
  assert.deepEqual(Object.keys(parse('{"z":1,"a":2,"m":3}') as object), ['z', 'a', 'm']);
});

// A caller's value becomes I-JSON or is refused: nothing is changed silently (AP-CLIENT-003).
test('TestR025RequestValuesAreHeldToIJSON', () => {
  for (const bad of [NaN, Infinity, -Infinity, 10n, new Date(0), new Map(), () => 1, Symbol('s'), [1, undefined], String.fromCharCode(0xd800)]) {
    assert.throws(() => fromValue({ a: bad }), NotIJson, String(bad));
  }
  const cycle: any = {};
  cycle.self = cycle;
  assert.throws(() => fromValue(cycle), NotIJson);
  assert.deepEqual(fromValue({ a: undefined, b: null, c: [1, 'x'] }), { b: null, c: [1, 'x'] });
  assert.equal(canonical(fromValue({ text: 'naïve é', count: 2, ratio: 0.5, when: '2026-10-31', kind: 'loud', tags: ['b', 'a'] }) as any),
    '{"count":2,"kind":"loud","ratio":0.5,"tags":["b","a"],"text":"naïve é","when":"2026-10-31"}'); // CORE-CALL-005
  assert.equal(wellFormed('a' + String.fromCodePoint(0x1f600)), true);
});

// AGENTS.md: fuzz every parser. Mutations of valid documents: the reader
// throws nothing but NotIJson; what it accepts JSON.parse accepts, with the
// same value, and its canonical form reads back the same; what JSON.parse
// refuses, it refuses.
test('FuzzIJSONReader', () => {
  const corpus = [
    '{"a":1,"b":[true,false,null],"c":{"d":"e"}}', '[1.5e10,-0,0.000001,"x"]', `"${u('d83d')}${u('de00')}"`,
    '{"$atlas":"end","rows":3,"truncated":false,"usage":{},"problem":null}', '{"amount":"125001.500000","currency":"USD"}',
  ].map(enc);
  const dictionary = ['"', '\\', u('d800'), u('fffe'), '1e400', ',', ':', '{', '}', '[', ']', '"a":1', 'null', String.fromCodePoint(0xe9), '\n'].map(enc);
  dictionary.push(new Uint8Array([0xef, 0xbb, 0xbf]), new Uint8Array([0xc3]), new Uint8Array([0xff]));
  const r = rng(seed());
  const n = iterations(20000);
  for (let i = 0; i < n; i++) {
    const input = mutate(corpus[i % corpus.length], r, dictionary);
    let accepted: unknown;
    let ok = false;
    try {
      accepted = parseBytes(input);
      ok = true;
    } catch (e) {
      assert.ok(e instanceof NotIJson, `iteration ${i}: threw ${e}`);
    }
    let lax: unknown;
    let laxOk = false;
    try {
      lax = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(input));
      laxOk = true;
    } catch { /* JSON.parse refuses it too */ }
    if (ok) {
      assert.ok(laxOk, `iteration ${i}: accepted what JSON.parse refuses`);
      assert.equal(canonical(lax as any), canonical(accepted as any), `iteration ${i}: a different value from JSON.parse`);
      assert.equal(canonical(parse(canonical(accepted as any))), canonical(accepted as any), `iteration ${i}: the canonical form does not read back`);
    }
  }
});
