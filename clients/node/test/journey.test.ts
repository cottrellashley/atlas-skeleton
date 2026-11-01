// The journey through the Node SDK, against the real development gateway
// (DuckDB and all): the same requests as the CLI and Python, the same
// logical results, checked against the fixture independently.
//
// It builds gateway-local and the CLI from this checkout, or takes them
// from ATLAS_NODE_TEST_BIN (a directory holding gateway-local and atlas,
// built elsewhere: a checkout other work is changing need not build).

import assert from 'node:assert/strict';
import { execFileSync, spawn, spawnSync, type ChildProcess } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { after, before, test } from 'node:test';

import { AtlasProblem, atlas, canonical, type Client, signIn, type Warning } from '../src/index.ts';

const repo = resolve(import.meta.dirname, '..', '..', '..');
const prebuilt = process.env.ATLAS_NODE_TEST_BIN;
const bin = prebuilt ?? mkdtempSync(join(tmpdir(), 'atlas-node-bin-'));
const env = { ...process.env, GOTOOLCHAIN: 'local', GOPRIVATE: 'github.com/jhgonline', GOVCS: 'github.com/jhgonline:off' };
const gateways: ChildProcess[] = [];
let url = '';
let capped = '';

function build(module: string, pkg: string, name: string): string {
  const out = join(bin, name);
  if (!prebuilt) execFileSync('go', ['-C', join(repo, module), 'build', '-o', out, pkg], { env, stdio: 'pipe' });
  return out;
}

const cli = () => join(bin, 'atlas');

/** A development gateway on a free port, with its arguments; its URL once it listens. */
async function start(args: string[]): Promise<string> {
  const addr = join(mkdtempSync(join(tmpdir(), 'atlas-node-addr-')), 'addr');
  const gw = spawn(join(bin, 'gateway-local'), ['-listen', '127.0.0.1:0', '-addr-file', addr, '-quiet', ...args], { stdio: 'ignore' });
  gateways.push(gw);
  const exited = new Promise((_, fail) => gw.once('exit', (code) => fail(new Error(`gateway-local exited ${code}`))));
  for (;;) {
    if (existsSync(addr) && readFileSync(addr, 'utf8').trim()) return readFileSync(addr, 'utf8').trim();
    await Promise.race([new Promise((ok) => setImmediate(ok)), exited]);
  }
}

before(async () => {
  build('gateway/local', './cmd/gateway-local', 'gateway-local');
  build('clients/cli', './cmd/atlas', 'atlas');
  url = await start([]);
  // R001: a policy whose output cap is two rows, so that holdings come back truncated.
  const policy = JSON.parse(readFileSync(join(repo, 'gateway', 'fixtures', 'policy.json'), 'utf8'));
  for (const l of policy.limits) l.max_rows = 2;
  const path = join(mkdtempSync(join(tmpdir(), 'atlas-node-policy-')), 'policy.json');
  writeFileSync(path, JSON.stringify(policy));
  capped = await start(['-policy', path]);
});

after(() => { for (const g of gateways) g.kill(); });

async function home(token: string, at = url): Promise<string> {
  const h = mkdtempSync(join(tmpdir(), `atlas-node-${token}-`));
  await signIn({ url: at, home: h, accessToken: token, refreshToken: `refresh-${token}` });
  return h;
}

async function as(token: string, at = url, warnings?: Warning[]): Promise<Client> {
  return atlas.connect({ url: at, home: await home(token, at), onWarning: warnings ? (w) => warnings.push(w) : undefined });
}

const REQUEST = { portfolios: ['147HYC', 'GLOBAL-CREDIT'], as_of: '2026-10-29', columns: ['portfolio', 'security_id', 'as_of', 'quantity', 'market_value', 'weight'] };

test('orient and find', async () => {
  const alice = await as('dev-alice');
  const who = await alice.whoami();
  assert.equal(who.name, 'Alice Analyst');
  const ids = (await alice.catalog()).map((c) => c.id);
  assert.deepEqual(ids, ['data.status', 'portfolio.holdings', 'portfolio.holdings-dates', 'portfolio.list', 'portfolio.search']);
  const guide = await alice.guide('portfolio.holdings');
  assert.match(guide, /await client\.call\('portfolio\.holdings@v1:fetch'/);
});

test('holdings: exact strings, the fixture checked independently', async () => {
  const alice = await as('dev-alice');
  const r = await alice.call('portfolio.holdings@v1:fetch', REQUEST);
  const rows = await r.rows();
  assert.equal(r.complete, true);
  assert.deepEqual(r.warnings, []);
  const fixture = JSON.parse(readFileSync(join(repo, 'gateway', 'fixtures', 'harbour.json'), 'utf8'));
  const table = fixture.tables.find((t: any) => t.name === 'HARBOUR.HOLDINGS.POSITIONS_V');
  const cols: string[] = table.columns.map((c: string[]) => c[0]);
  const want = table.rows.map((r: any[]) => Object.fromEntries(cols.map((c, i) => [c, r[i]])))
    .filter((r: any) => REQUEST.portfolios.includes(r.PORTFOLIO_CODE) && r.AS_OF_DATE === '2026-10-29')
    .sort((a: any, b: any) => (a.PORTFOLIO_CODE + a.SECURITY_ID < b.PORTFOLIO_CODE + b.SECURITY_ID ? -1 : 1));
  assert.equal(rows.length, want.length);
  rows.forEach((row, i) => {
    assert.equal(row.security_id, want[i].SECURITY_ID);
    // The amount is the exact decimal, at Money's scale: never a float.
    const amount = (row.market_value as any).amount as string;
    assert.equal(typeof amount, 'string');
    const [int, frac = ''] = String(want[i].MARKET_VALUE).split('.');
    assert.equal(amount, `${int}.${frac.padEnd(6, '0')}`);
  });
});

// The acceptance result's second and fifth points: the same person, the
// same request record, the same answer through the CLI and Node.
test('the same request gives the same rows in the CLI and in Node', async () => {
  const h = await home('dev-alice');
  const out = execFileSync(cli(), ['--gateway', url, 'call', 'portfolio.holdings@v1:fetch', '--request', JSON.stringify(REQUEST)], { env: { ...env, ATLAS_HOME: h } }).toString();
  // The CLI prints the protocol's documents unchanged, so an NDJSON answer may end with its end line.
  const lines = out.trim().split('\n').map((l) => JSON.parse(l));
  const fromCli = lines.filter((v) => !('$atlas' in v)).map(canonical);
  const r = await (await atlas.connect({ url, home: h })).call('portfolio.holdings@v1:fetch', REQUEST);
  const fromNode = (await r.rows()).map(canonical);
  assert.deepEqual(fromNode, fromCli);
  const end = lines.find((v) => '$atlas' in v);
  if (end) assert.deepEqual([end.rows, end.truncated, canonical(end.usage)], [r.end!.rows, r.end!.truncated, canonical(r.end!.usage)], 'the same completion in both');
});

test('two identities, two scopes', async () => {
  const bob = await as('dev-bob');
  await assert.rejects(bob.call('portfolio.holdings@v1:fetch', { portfolios: ['147HYC', 'GLOBAL-CREDIT'] }), (e: AtlasProblem) => e.code === 'not_granted' && e.target === '/portfolios/1' && e.status === 403);
  const list = await (await bob.call('portfolio.list@v1:fetch')).rows();
  assert.deepEqual(list.map((r) => r.code), ['147HYC']);
});

test('an invalid request is refused before it is sent', async () => {
  let posts = 0;
  const counting: typeof fetch = (input, init) => { if (init?.method === 'POST') posts++; return fetch(input, init); };
  const alice = await atlas.connect({ url, home: await home('dev-alice'), fetch: counting });
  await alice.whoami();
  const before = posts;
  await assert.rejects(alice.call('portfolio.holdings@v1:fetch', { portfolios: [], as_of: 'yesterday' }), (e: AtlasProblem) => e.code === 'invalid_request' && e.target === '/as_of' && e.details.length === 2 && e.details.every((d) => typeof d.keyword === 'string'));
  assert.equal(posts - before, 1, 'only the describe that fetched the contract');
});

// R001 (Node's part), STREAM-004: the real gateway caps the answer; Node returns the rows, says it is
// incomplete, and warns; bin/call.ts prints the warning on standard error as the CLI does.
test('TestR001TruncatedHoldingsAreMarkedIncomplete', async () => {
  const warnings: Warning[] = [];
  const alice = await as('dev-alice', capped, warnings);
  const r = await alice.call('portfolio.holdings@v1:fetch', REQUEST);
  const rows = await r.rows();
  assert.equal(rows.length, 2);
  assert.equal(r.truncated, true);
  assert.equal(r.complete, false);
  assert.equal(r.warnings[0]?.code, 'output_limit_exceeded');
  assert.deepEqual(warnings, r.warnings);
  const h = await home('dev-alice', capped);
  const run = spawnSync('node', [join(repo, 'clients', 'node', 'bin', 'call.ts'), 'portfolio.holdings@v1:fetch', JSON.stringify(REQUEST)], { env: { ...env, ATLAS_HOME: h, ATLAS_URL: capped }, encoding: 'utf8' });
  assert.equal(run.status, 0, run.stderr);
  assert.equal(run.stdout.trim().split('\n').length, 2);
  assert.match(run.stderr, /^warning: output_limit_exceeded: /m);
});

// bin/call.ts reports a problem as the CLI does: the document on standard error, §9.7.6's exit code.
test('bin/call.ts exits with the problem code', async () => {
  const h = await home('dev-bob');
  const run = spawnSync('node', [join(repo, 'clients', 'node', 'bin', 'call.ts'), 'portfolio.holdings@v1:fetch', JSON.stringify({ portfolios: ['GLOBAL-CREDIT'] })], { env: { ...env, ATLAS_HOME: h, ATLAS_URL: url }, encoding: 'utf8' });
  assert.equal(run.status, 4);
  assert.equal(JSON.parse(run.stderr.trim().split('\n').at(-1)!).code, 'not_granted');
});
