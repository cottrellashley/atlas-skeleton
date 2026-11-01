// The grammars of the Atlas Protocol (§9.1.3 to §9.1.5, and the strings §9
// defines in prose, as references/grammar.abnf collects them). Every one is
// case-sensitive, and a receiver rejects a value that does not match
// (§9.1 rule 1). Each rule is a predicate; parseAddress also takes an
// address apart.

const SEGMENT = '[a-z](?:-?[a-z0-9])*'; // a-z first; never ends with -, never holds --
const CAPABILITY = `${SEGMENT}(?:\\.${SEGMENT})+`;
const VERSION = 'v[1-9][0-9]*';
const FINGERPRINT = '[0-9a-f]{16}';

function whole(source: string): RegExp {
  return new RegExp(`^(?:${source})$`);
}

const re = {
  segment: whole(SEGMENT),
  capability: whole(CAPABILITY),
  version: whole(VERSION),
  nzDigits: /^[1-9][0-9]*$/,
  fingerprint: whole(FINGERPRINT),
  pinned: whole(`(${CAPABILITY})@${VERSION}`),
  exact: whole(`(${CAPABILITY})@${VERSION}#${FINGERPRINT}`),
  address: new RegExp(`^(${CAPABILITY})(?:@v([1-9][0-9]*))?:(${SEGMENT})$`),
  protocol: /^atlas\/[0-9]+\.[0-9]+$/,
  name: /^[a-z][a-z0-9_]*$/,
  date: /^([0-9]{4})-([0-9]{2})-([0-9]{2})$/,
  dateTime: /^([0-9]{4}-[0-9]{2}-[0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:\.[0-9]{1,9})?(?:Z|[+-]([0-9]{2}):([0-9]{2}))$/,
  duration: /^P(?:[0-9]+D(?:T(?:[0-9]+H)?(?:[0-9]+M)?(?:[0-9]+(?:\.[0-9]{1,9})?S)?)?|T(?:[0-9]+H(?:[0-9]+M)?(?:[0-9]+(?:\.[0-9]{1,9})?S)?|[0-9]+M(?:[0-9]+(?:\.[0-9]{1,9})?S)?|[0-9]+(?:\.[0-9]{1,9})?S))$/,
  decimal: /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$/,
  integerStr: /^-?(?:0|[1-9][0-9]*)$/,
  idempotencyKey: /^[A-Za-z0-9._-]{1,255}$/,
  problemType: /^https:\/\/atlas\.jhg\.com\/problems\/[a-z][a-z0-9_]*$/,
  dataschema: whole(`urn:atlas:profile:(${CAPABILITY})@${VERSION}:${FINGERPRINT}`),
  eventType: whole(`com\\.jhg\\.fiquant\\.(${CAPABILITY})\\.v[1-9][0-9]*`),
  resourceId: /^[a-z0-9][a-z0-9-]*$/,
  resourceName: whole(`${SEGMENT}(?:/${SEGMENT}/[a-z0-9][a-z0-9-]*)+`),
  operationName: /^operations\/[a-z0-9][a-z0-9-]*$/,
  entityTag: /^(?:W\/)?"[\x21\x23-\x7e]*"$/,
};

/** The longest capability id (§9.1.3). */
export const MAX_CAPABILITY_ID = 128;
/** The longest address (§9.1.3). */
export const MAX_ADDRESS = 160;

function leap(y: number): boolean {
  return (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0;
}

/** Whether y-m-d is a real date of the proleptic Gregorian calendar, year 0000 included (Q-GRAM-4). */
function realDate(y: number, m: number, d: number): boolean {
  if (m < 1 || m > 12 || d < 1) return false;
  const days = [31, leap(y) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][m - 1];
  return d <= days;
}

export function segment(s: string): boolean { return re.segment.test(s); }
export function capabilityId(s: string): boolean { return s.length <= MAX_CAPABILITY_ID && re.capability.test(s); }
export function domain(s: string): boolean { return segment(s); }
export function version(s: string): boolean { return re.version.test(s); }
export function nzDigits(s: string): boolean { return re.nzDigits.test(s); }
export function interaction(s: string): boolean { return segment(s); }
export function fingerprint(s: string): boolean { return re.fingerprint.test(s); }

export function pinnedId(s: string): boolean {
  const m = re.pinned.exec(s);
  return m !== null && m[1].length <= MAX_CAPABILITY_ID;
}

export function exactId(s: string): boolean {
  const m = re.exact.exec(s);
  return m !== null && m[1].length <= MAX_CAPABILITY_ID;
}

/** An address taken apart: its capability, its version when it is pinned, and its interaction. */
export interface Address { id: string; version?: number; interaction: string }

/** An address (§9.1.3): a capability id or a pinned id, then an interaction; at most 160 characters. */
export function parseAddress(s: string): Address | undefined {
  if (s.length > MAX_ADDRESS) return undefined;
  const m = re.address.exec(s);
  if (!m || m[1].length > MAX_CAPABILITY_ID) return undefined;
  const v = m[2] === undefined ? undefined : Number(m[2]);
  if (v !== undefined && !Number.isSafeInteger(v)) return undefined;
  return v === undefined ? { id: m[1], interaction: m[3] } : { id: m[1], version: v, interaction: m[3] };
}

export function address(s: string): boolean { return parseAddress(s) !== undefined; }
export function protocolVer(s: string): boolean { return re.protocol.test(s); }
export function propertyName(s: string): boolean { return re.name.test(s); }
export function problemCode(s: string): boolean { return re.name.test(s); }

/** A date (§9.1.4): RFC 3339's full-date, a real calendar date. */
export function date(s: string): boolean {
  const m = re.date.exec(s);
  return m !== null && realDate(+m[1], +m[2], +m[3]);
}

/** A date-time (§9.1.4): an offset always, hour 00-23, minute and second 00-59 (never the leap second 60), offset 00-23:00-59. */
export function dateTime(s: string): boolean {
  const m = re.dateTime.exec(s);
  if (!m || !date(m[1])) return false;
  if (+m[2] > 23 || +m[3] > 59 || +m[4] > 59) return false;
  return m[5] === undefined || (+m[5] <= 23 && +m[6] <= 59);
}

/** A duration (§9.1.4): days, hours, minutes and seconds; P alone is not one, P1DT is (A-003). */
export function duration(s: string): boolean { return re.duration.test(s); }

/** A duration's length in milliseconds (one day is 86400 seconds), or undefined when it is not a duration. */
export function durationMs(s: string): number | undefined {
  if (!duration(s)) return undefined;
  const m = /^P(?:([0-9]+)D)?(?:T(?:([0-9]+)H)?(?:([0-9]+)M)?(?:([0-9]+(?:\.[0-9]+)?)S)?)?$/.exec(s);
  if (!m) return undefined;
  const ms = ((+(m[1] ?? 0) * 24 + +(m[2] ?? 0)) * 60 + +(m[3] ?? 0)) * 60000 + Math.floor(+(m[4] ?? 0) * 1000);
  return Number.isFinite(ms) ? ms : undefined;
}

/** An exact decimal (§9.1.5); negative zero is not one (Q-DEC-2). */
export function decimal(s: string): boolean {
  return re.decimal.test(s) && !(s.startsWith('-') && /^-0(?:\.0+)?$/.test(s));
}

/** Whether a decimal fits Decimal(p,s): at most s fraction digits, and at most p-s integer digits without leading zeros (Q-DEC-1). */
export function decimalFits(s: string, p: number, sc: number): boolean {
  const [int, frac = ''] = s.replace(/^-/, '').split('.');
  return frac.length <= sc && int.replace(/^0+/, '').length <= p - sc;
}

/** An integer outside I-JSON's range, as a string (§9.1.5); -0 is one (Q-DEC-3). */
export function integerStr(s: string): boolean { return re.integerStr.test(s); }

const INT64_MIN = -(2n ** 63n);
const INT64_MAX = 2n ** 63n - 1n;

/** format int64 (§9.2.2): an integer-str within a signed 64-bit integer. */
export function int64(s: string): boolean {
  if (!integerStr(s)) return false;
  const n = BigInt(s);
  return n >= INT64_MIN && n <= INT64_MAX;
}

/** An Idempotency-Key (§9.6.3): 1 to 255 characters of A-Z a-z 0-9 - _ . */
export function idempotencyKey(s: string): boolean { return re.idempotencyKey.test(s); }
export function problemType(s: string): boolean { return re.problemType.test(s); }

export function dataschema(s: string): boolean {
  const m = re.dataschema.exec(s);
  return m !== null && m[1].length <= MAX_CAPABILITY_ID;
}

export function eventType(s: string): boolean {
  const m = re.eventType.exec(s);
  return m !== null && m[1].length <= MAX_CAPABILITY_ID;
}

export function resourceName(s: string): boolean { return re.resourceName.test(s); }
export function resourceId(s: string): boolean { return re.resourceId.test(s); }
export function operationName(s: string): boolean { return re.operationName.test(s); }
export function entityTag(s: string): boolean { return re.entityTag.test(s); }

/** Every rule by its name in grammar.abnf. */
export const RULES: Record<string, (s: string) => boolean> = {
  segment, 'capability-id': capabilityId, domain, version, 'nz-digits': nzDigits, interaction, fingerprint,
  'pinned-id': pinnedId, 'exact-id': exactId, address, 'protocol-ver': protocolVer, 'property-name': propertyName,
  'problem-code': problemCode, date, 'date-time': dateTime, duration, decimal, 'integer-str': integerStr,
  'idempotency-key': idempotencyKey, 'problem-type': problemType, dataschema, 'event-type': eventType,
  'resource-name': resourceName, 'resource-id': resourceId, 'operation-name': operationName, 'entity-tag': entityTag,
};
