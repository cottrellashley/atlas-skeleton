// node bin/call.ts ADDRESS [REQUEST-JSON]: one call through the Node SDK,
// its rows (or record) printed one canonical JSON line each, each warning
// on standard error after them (`warning: <code>: <text>`, as the CLI
// prints one), or its problem on standard error with §9.7.6's exit code.
// The acceptance suite's way to drive Node exactly as it drives the CLI.
// A composition root: connect reads ATLAS_URL, ATLAS_HOME and ATLAS_TOKEN.
import { NotIJson, parse } from '../src/ijson.ts';
import { AtlasProblem, atlas, canonical, exitCode, problem } from '../src/index.ts';

const [address, request = '{}'] = process.argv.slice(2);
try {
  let body;
  try {
    body = parse(request);
  } catch (e) {
    throw e instanceof NotIJson ? problem('invalid_document', `the request is not I-JSON: ${e.message}`) : e;
  }
  const client = await atlas.connect({ onWarning: () => {} });
  const result = await client.call(address, body);
  for await (const row of result) process.stdout.write(canonical(row) + '\n');
  for (const w of result.warnings) process.stderr.write(`warning: ${w.code}: ${w.text}\n`);
} catch (e) {
  const p = e instanceof AtlasProblem ? e.problem : { code: 'internal', detail: String(e) };
  process.stderr.write(JSON.stringify(p) + '\n');
  process.exit(e instanceof AtlasProblem ? exitCode(e.code) : 1);
}
