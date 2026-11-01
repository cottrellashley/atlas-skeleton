// Deterministic fuzzing for the parsers (AGENTS.md: fuzz every parser):
// a seeded generator mutates a corpus of valid inputs, so a failure names
// its seed and iteration and replays exactly. ATLAS_FUZZ_ITERATIONS runs
// longer, and ATLAS_FUZZ_SEED another sequence.

/** mulberry32: a small, fast, seeded generator. */
export function rng(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export function iterations(n: number): number {
  const v = Number(process.env.ATLAS_FUZZ_ITERATIONS);
  return Number.isSafeInteger(v) && v > 0 ? v : n;
}

export function seed(): number {
  const v = Number(process.env.ATLAS_FUZZ_SEED);
  return Number.isSafeInteger(v) ? v : 20261101;
}

/** One mutation of bytes: a byte inserted, removed, replaced or flipped, or a piece of the input or of a dictionary spliced in. */
export function mutate(input: Uint8Array, r: () => number, dictionary: Uint8Array[]): Uint8Array {
  const b = [...input];
  const at = Math.floor(r() * (b.length + 1));
  switch (Math.floor(r() * 7)) {
    case 0: b.splice(at, 0, Math.floor(r() * 256)); break;
    case 1: if (b.length) b.splice(Math.min(at, b.length - 1), 1); break;
    case 2: if (b.length) b[Math.min(at, b.length - 1)] = Math.floor(r() * 256); break;
    case 3: if (b.length) b[Math.min(at, b.length - 1)] ^= 1 << Math.floor(r() * 8); break;
    case 4: { const from = Math.floor(r() * b.length); const len = Math.floor(r() * 8); b.splice(at, 0, ...b.slice(from, from + len)); break; }
    case 5: { const d = dictionary[Math.floor(r() * dictionary.length)]; b.splice(at, 0, ...d); break; }
    default: { const n = Math.floor(r() * 4); b.splice(at, n); break; }
  }
  return new Uint8Array(b);
}
