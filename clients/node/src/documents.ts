// The protocol's 21 documents (§9.3.1) as JSON Schemas, compiled into one
// registry, so that this client holds every document it receives to its
// schema (§9.3 rule 1, §9.7.4 step 10) and every built-in request before it
// is sent (§9.2 rule 2). The files under schemas/ are the references' own
// (spec/protocol/references/schemas), copied byte for byte; a test fails
// when they drift.

import { readdirSync, readFileSync } from 'node:fs';

import { type Json, parseBytes } from './ijson.ts';
import { type Failure, Registry, Validator } from './schema.ts';

/** The $id every document's URI begins with. */
export const BASE = 'https://atlas.jhg.com/protocol/1.0/';

/** Where the documents are, beside this file. */
export const DIR = new URL('./schemas/', import.meta.url);

let registry: Registry | undefined;
const validators = new Map<string, Validator>();

/** Every document, read once. A document that does not read is a broken installation, and says so. */
function documents(): Registry {
  if (registry) return registry;
  const r = new Registry();
  for (const name of readdirSync(DIR).filter((n) => n.endsWith('.schema.json')).sort()) {
    try {
      r.add(parseBytes(readFileSync(new URL(name, DIR))) as { [k: string]: Json });
    } catch (e) {
      throw new Error(`@atlas/sdk: the protocol document ${name} does not read: ${(e as Error).message}`, { cause: e });
    }
  }
  registry = r;
  return r;
}

/**
 * A document's validator, by its name ("bootstrap", "problem") or one of
 * its definitions ("builtins#whoami_result"). Documents are open and read
 * tolerantly: an enum value this client does not know is UNKNOWN (Q-DOC-1).
 */
export function documentValidator(name: string): Validator {
  let v = validators.get(name);
  if (v) return v;
  const [doc, def] = name.split('#');
  const ref = BASE + doc + '.schema.json' + (def ? '#/$defs/' + def : '');
  const reg = documents();
  if (reg.get(BASE + doc + '.schema.json') === undefined) throw new Error(`@atlas/sdk: no protocol document ${name}`);
  v = new Validator({ $ref: ref }, { registry: reg, tolerant: true, documents: true });
  validators.set(name, v);
  return v;
}

/** A document's failures against its schema; none when it is valid. */
export function validateDocument(name: string, v: Json): Failure[] {
  return documentValidator(name).validate(v);
}
