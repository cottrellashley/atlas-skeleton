// The sign-in store's rules (§9.7.3, R26), unit by unit, as the Go client
// keeps the same store (sdk/go/client.go): the binding's members and how
// they compare, which origins a credential may travel to, and the path of
// an environment's file (R046).

import assert from 'node:assert/strict';
import { chmodSync, mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';

import { audience, bindingDiffers, bindingOf, loopback, originOf, secureOrigin, storePath, TokenStore } from '../src/credentials.ts';
import { AtlasProblem } from '../src/problem.ts';
import { FakeClock } from './fakeclock.ts';

const bootstrap = {
  environment: 'pd',
  servers: { gateway: { url: 'https://atlas.example' } },
  sign_in: { authority: 'https://login.example/tenant-1/v2.0', client_id: 'atlas-cli', scopes: ['api://atlas/.default'], token_endpoint: 'https://login.example/tenant-1/oauth2/v2.0/token' },
};

// R046: an environment that is not a name never becomes a path; every store file is directly under state/.
test('TestR046StorePathStaysInTheStore', () => {
  assert.equal(storePath('/home/a/.atlas', 'local'), '/home/a/.atlas/state/token-local.json');
  for (const env of ['x/../../other-tool', '../x', 'X', 'a b', '', 'a--b', 'a-', 'a/b', '.', 'a' + String.fromCharCode(0) + 'b']) {
    assert.throws(() => storePath('/home/a/.atlas', env), (e: AtlasProblem) => e.code === 'invalid_document' && e.target === '/environment', env);
  }
});

// R26: a credential travels over HTTPS, or plain HTTP only to this machine.
test('TestR002SecureOrigins', () => {
  assert.equal(originOf('HTTPS://Atlas.Example:443/v1/'), 'https://atlas.example');
  assert.equal(originOf('http://127.0.0.1:8740/'), 'http://127.0.0.1:8740');
  assert.equal(originOf('http://user:pw@atlas.example'), undefined, 'a URL with a user is not a gateway');
  assert.equal(originOf('ftp://atlas.example'), undefined);
  for (const o of ['https://atlas.example', 'http://127.0.0.1:8740', 'http://localhost:1', 'http://[::1]:9']) assert.equal(secureOrigin(o), true, o);
  for (const o of ['http://atlas.example', 'http://10.0.0.1', 'http://127.example.com']) assert.equal(secureOrigin(o), false, o);
  assert.equal(loopback('http://127.1.2.3:5'), true);
});

// R26: the binding's members, as the Go client writes them, and the first that differs.
test('TestR002BindingMembers', () => {
  const b = bindingOf('https://atlas.example', bootstrap as any);
  assert.deepEqual(Object.keys(b), ['gateway', 'environment', 'authority', 'client_id', 'audience', 'scopes', 'token_endpoint']);
  assert.equal(b.audience, 'api://atlas');
  assert.equal(audience(['api://a/.default', 'api://b/.default']), '', 'scopes for two audiences name none');
  assert.equal(audience(['openid']), '');
  assert.equal(bindingDiffers(b, { ...b, scopes: [...b.scopes].reverse() }), undefined, 'scopes compare as a set');
  assert.equal(bindingDiffers(b, { ...b, gateway: 'https://evil.example' }), 'gateway');
  assert.equal(bindingDiffers(b, { ...b, token_endpoint: 'https://evil.example/token' }), 'token_endpoint');
  assert.equal(bindingDiffers(b, { ...b, authority: 'https://login.example/tenant-2/v2.0' }), 'authority', 'another tenant');
  assert.equal(bindingDiffers(b, { ...b, audience: undefined }), 'audience');
});

// CORE-SIGNIN-005: a store others can read is refused, and its token never sent.
test('TestSigninStoreWithWiderPermissionsIsRefused', async (t) => {
  const home = mkdtempSync(join(tmpdir(), 'atlas-node-'));
  t.after(() => rmSync(home, { recursive: true, force: true }));
  mkdirSync(join(home, 'state'), { mode: 0o700 });
  writeFileSync(join(home, 'state', 'token-pd.json'), JSON.stringify({ access_token: 't', expires_at: 0 }), { mode: 0o644 });
  let fetched = 0;
  const store = new TokenStore(home, 'https://atlas.example', bootstrap as any, new FakeClock(), (async () => { fetched++; return new Response(); }) as any);
  await assert.rejects(store.token(new AbortController().signal), (e: AtlasProblem) => e.code === 'unauthenticated' && /can be read by others/.test(e.message));
  chmodSync(join(home, 'state', 'token-pd.json'), 0o600);
  writeFileSync(join(home, 'state', 'token-pd.json'), '{"access_token":"t","access_token":"u","expires_at":0}');
  await assert.rejects(store.token(new AbortController().signal), (e: AtlasProblem) => e.code === 'unauthenticated' && /duplicate member/.test(e.message), 'the store is strict I-JSON too');
  assert.equal(fetched, 0);
});
