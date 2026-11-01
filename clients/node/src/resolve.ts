// Resolution (§9.5), as far as a client is obliged to it: aliases,
// defaults, UNKNOWN and projection (docs/pass-2/REVIEW.md, R03, R04). A
// record written under the writer's profile is matched against the
// reader's schema by §9.5.2 steps 5 to 15, and each value converted by
// §9.5.5. This client implements no lens operation, says so in
// Atlas-Client-Features (lens is empty), and refuses a chain that needs one:
// the gateway resolves such an answer itself (§9.5.6). It never guesses.

import { type Json } from './ijson.ts';
import * as grammar from './grammar.ts';
import { type Obj } from './problem.ts';
import { Validator } from './schema.ts';

type Schema = { [k: string]: any } | boolean;

/** Why a record does not resolve, and the top-level member it failed at (Q-RES-1). */
export class ResolutionFailure extends Error {
  readonly target: string;
  constructor(target: string, why: string) {
    super(why);
    this.name = 'ResolutionFailure';
    this.target = target;
  }
}

function isObject(v: unknown): v is { [k: string]: any } {
  return v !== null && typeof v === 'object' && !Array.isArray(v);
}

/** A schema with its local $ref chain followed to its end (A-012). */
export function definition(s: Schema, root: Schema): Schema {
  for (let n = 0; n < 32 && isObject(s) && typeof s.$ref === 'string' && s.$ref.startsWith('#/$defs/') && isObject(root); n++) {
    const target = root.$defs?.[s.$ref.slice(8)];
    if (target === undefined) break;
    s = target;
  }
  return s;
}

function types(s: Schema): string[] {
  if (!isObject(s) || s.type === undefined) return [];
  return Array.isArray(s.type) ? s.type : [s.type];
}

function allowsNull(s: Schema): boolean {
  return s === true || types(s).includes('null');
}

/** The reader's row schema projected to the columns a request named (§9.5.2 step 5): only those, in its own order. */
export function project(row: Schema, columns: string[] | undefined): Schema {
  if (columns === undefined || !isObject(row)) return row;
  const props = isObject(row.properties) ? row.properties : {};
  return {
    ...row,
    properties: Object.fromEntries(Object.entries(props).filter(([k]) => columns.includes(k))),
    required: (Array.isArray(row.required) ? row.required : []).filter((k: string) => columns.includes(k)),
  };
}

/** Resolves records for one reader schema, whose $defs its properties may name. */
export class Resolver {
  private readonly root: Schema;
  private readonly validator: Validator;

  constructor(root: Schema) {
    this.root = root;
    this.validator = new Validator(root);
  }

  private valid(s: Schema, v: Json): boolean {
    return this.validator.validAt(s, v);
  }

  /** §9.5.2 steps 5 to 15: the record y matched against the schema S. */
  match(y: Obj, S: Schema, at = ''): Obj {
    const s = definition(S, this.root);
    const props: { [k: string]: Schema } = isObject(s) && isObject(s.properties) ? s.properties : {};
    const required: string[] = isObject(s) && Array.isArray(s.required) ? s.required : [];
    const z: Obj = {};
    for (const [p, ps] of Object.entries(props)) {
      const prop = definition(ps, this.root);
      const where = at === '' ? `/${p}` : at;
      let value: Json | undefined;
      if (Object.hasOwn(y, p)) value = y[p];
      else {
        const aliases: unknown = isObject(ps) ? ps['x-atlas-aliases'] ?? (isObject(prop) ? prop['x-atlas-aliases'] : undefined) : undefined;
        if (Array.isArray(aliases)) for (const a of aliases) if (typeof a === 'string' && Object.hasOwn(y, a)) value = y[a];
      }
      if (value === undefined) {
        if (isObject(ps) && 'default' in ps) value = ps.default as Json;
        else if (isObject(prop) && 'default' in prop) value = prop.default as Json;
        else if (allowsNull(prop) && required.includes(p)) value = null;
        else if (!required.includes(p)) continue;
        else throw new ResolutionFailure(where, `the record has no ${p}, which the reader's schema requires`);
      }
      z[p] = this.convert(value as Json, ps, where);
    }
    return z;
  }

  /** §9.5.5: a value converted to the reader's property schema, or a failure at the top-level member. */
  convert(v: Json, ps: Schema, at: string): Json {
    if (this.valid(ps, v)) return v;
    const s = definition(ps, this.root);
    const t = types(s);
    if (v === null) {
      if (allowsNull(s)) return null;
      throw new ResolutionFailure(at, `${at.slice(1)} is null, which the reader's schema does not allow`);
    }
    if (typeof v === 'number' && Number.isSafeInteger(v) && t.includes('number')) return v;
    if (typeof v === 'string' && isObject(s) && s.format === 'date-time' && grammar.date(v)) {
      const promoted = `${v}T00:00:00Z`;
      if (this.valid(ps, promoted)) return promoted;
    }
    if (typeof v === 'string' && isObject(s) && Array.isArray(s.enum) && !s.enum.includes(v)) {
      const entries: unknown[] = Array.isArray(s['x-atlas-enum']) ? s['x-atlas-enum'] : [];
      for (const e of entries) {
        if (isObject(e) && Array.isArray(e.aliases) && e.aliases.includes(v) && typeof e.value === 'string') return e.value;
      }
      if (s.enum.includes('UNKNOWN')) return 'UNKNOWN';
      throw new ResolutionFailure(at, `${JSON.stringify(v)} is not a value the reader knows, and its enum has no UNKNOWN`);
    }
    if (isObject(v) && t.includes('object') && isObject(s) && isObject(s.properties)) {
      const z = this.match(v as Obj, s, at);
      if (this.valid(ps, z)) return z;
    }
    if (Array.isArray(v) && t.includes('array') && isObject(s) && s.items !== undefined) {
      const out = v.map((e) => this.convert(e, s.items, at));
      if (this.valid(ps, out)) return out;
    }
    throw new ResolutionFailure(at, `${at.slice(1)} cannot be converted to the reader's schema`);
  }
}

/** A profile in a chain: its exact id, its schema for the record, and its lens from the profile before it. */
export interface ChainProfile { exact: string; schema: Schema; lens?: unknown[] }

/**
 * §9.5.2 for one record, written under writer for reader, over the chain
 * (oldest first). Step 1: one profile, or a DynamicSchema capability, is
 * unchanged. Step 2: two versions never resolve. A chain whose lenses
 * between the two are not all empty needs lens operations this client does
 * not implement, and is refused. Then steps 5 to 15, with the projection.
 */
export function resolve(chain: ChainProfile[], writer: string, reader: string, record: Obj, columns?: string[], dynamic = false): Obj {
  if (writer === reader || dynamic) return record;
  const id = (exact: string) => exact.slice(0, exact.indexOf('#'));
  if (id(writer) !== id(reader)) throw new ResolutionFailure('', `${writer} and ${reader} are profiles of two versions, and a client never resolves across versions`);
  const w = chain.findIndex((p) => p.exact === writer);
  const r = chain.findIndex((p) => p.exact === reader);
  if (w < 0 || r < 0) throw new ResolutionFailure('', `${writer} and ${reader} are not in one chain`);
  const [lo, hi] = w < r ? [w, r] : [r, w];
  for (let k = lo + 1; k <= hi; k++) {
    if (Array.isArray(chain[k].lens) && chain[k].lens!.length > 0) {
      throw new ResolutionFailure('', `${chain[k].exact} declares a lens, and this client implements no lens operation: the gateway resolves it (§9.5.6)`);
    }
  }
  const root = chain[r].schema;
  return new Resolver(root).match(record, project(definition(root, root), columns));
}
