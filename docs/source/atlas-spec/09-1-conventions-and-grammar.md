# §9.1 Conventions and grammar

`spec/protocol/conventions.md` · proposed

> Section 9.1 of the Atlas Protocol: requirement words, how algorithms and grammars are written, the rules every JSON document obeys (I-JSON), and the exact grammar of every identifier, version, address, fingerprint, date, time, decimal and duration that crosses the wire.

## Purpose

To remove every choice an implementer could make differently: the same bytes mean the same thing in every language.

## Concepts

### 9.1.1 Words and notation

- MUST, MUST NOT, REQUIRED, SHALL, SHALL NOT, SHOULD, SHOULD NOT, RECOMMENDED, MAY and OPTIONAL are used as RFC 2119 and RFC 8174 define them, and only when in capitals.
- Grammars are ABNF (RFC 5234), using its core rules (`ALPHA`, `DIGIT`, `HEXDIG`, `DQUOTE`, `SP`). ABNF strings are case-insensitive; **every Atlas grammar is case-sensitive**, so literals are written with `%s"…"` (RFC 7405).
- Algorithms are numbered steps, executed in order; “fail with `code`” means stop and produce the problem `code` (§9.3); “return” ends the algorithm.
- “Implementation-defined” never appears: anything not defined here is a defect of this text.

### 9.1.2 JSON

Every JSON document in the protocol (bodies, profiles, events, test vectors) MUST be an **I-JSON message** (RFC 7493):

1. Encoded as UTF-8, with no byte-order mark.
2. No object has two members with the same name. A receiver MUST reject a document with duplicate names (`invalid_document`).
3. Strings contain no unpaired surrogates and no noncharacters.
4. Numbers are IEEE 754 binary64 values; an integer member's value lies within `[-(2^53)+1, (2^53)-1]`. Integers outside that range, and every exact decimal, are sent as strings (§9.1.5).
5. Object member order carries no meaning, except where a canonical form is computed (§9.4).
6. Strings are compared by their Unicode code points, without normalisation. The protocol never normalises text.
7. Unknown members of any protocol document MUST be ignored by a receiver (the tolerant reader, §9.7), unless the document's schema closes it (`"additionalProperties": false`), which only request bodies do.

### 9.1.3 Identifiers

```abnf
segment       = %x61-7A *( %x61-7A / DIGIT / "-" )       ; a-z first, then a-z 0-9 -
capability-id = segment 1*( "." segment )                 ; portfolio.holdings, atlas.whoami
domain        = segment                                    ; the first segment of a capability-id
version       = %s"v" nz-digits                           ; v1, v12
nz-digits     = %x31-39 *DIGIT
interaction   = segment                                    ; fetch, get, restart
fingerprint   = 16( DIGIT / %x61-66 )                      ; 16 lowercase hexadecimal characters
pinned-id     = capability-id "@" version                  ; portfolio.holdings@v1
exact-id      = pinned-id "#" fingerprint                  ; portfolio.holdings@v1#3f9a1c0b7d2e4a65
address       = ( capability-id / pinned-id ) ":" interaction ; portfolio.holdings@v1:fetch
protocol-ver  = %s"atlas/" 1*DIGIT "." 1*DIGIT             ; atlas/1.0
property-name = %x61-7A *( %x61-7A / DIGIT / "_" )       ; snake_case, in every schema
problem-code  = %x61-7A *( %x61-7A / DIGIT / "_" )       ; not_found, limit_exceeded
```

A capability id is at most 128 characters; an address at most 160. A `capability-id` whose domain is `atlas` names a built-in capability (§9.3). A segment never ends with `-`, and never contains `--`.

### 9.1.4 Dates, times and durations

```abnf
date      = 4DIGIT "-" 2DIGIT "-" 2DIGIT                                                   ; RFC 3339 full-date
date-time = date %s"T" 2DIGIT ":" 2DIGIT ":" 2DIGIT [ "." 1*9DIGIT ] ( %s"Z" / ( "+" / "-" ) 2DIGIT ":" 2DIGIT )
duration  = %s"P" [ 1*DIGIT %s"D" ] [ %s"T" [ 1*DIGIT %s"H" ] [ 1*DIGIT %s"M" ] [ 1*DIGIT [ "." 1*9DIGIT ] %s"S" ] ]
```

- A `date-time` MUST carry an offset; the gateway MUST send `Z` (UTC). Fractions of a second are truncated, never rounded, to the receiver's precision. The leap second `60` is not allowed.
- A `duration` uses days, hours, minutes and seconds only (no years, months or weeks), so it has one length; `P` alone is not allowed; at least one component is present.
- A `date` is a calendar date without a time zone.

### 9.1.5 Decimals and large integers

```abnf
decimal     = [ "-" ] ( "0" / nz-digits ) [ "." 1*DIGIT ]
integer-str = [ "-" ] ( "0" / nz-digits )
```

- An exact decimal (logical type `Decimal(p,s)`, `Money.amount`) is a JSON string matching `decimal`, with at most `p` significant digits and at most `s` digits after the point; a sender writes exactly `s` digits after the point when `s > 0`. `-0` is not allowed.
- An integer outside the I-JSON range is a JSON string matching `integer-str`.
- A receiver converting a decimal to a binary floating-point value rounds to nearest, ties to even.

### 9.1.6 Booleans, nulls and absence

`null` and an absent member are different. In a request, an absent optional parameter takes its default; `null` is sent only where the schema allows `null`. In a result, a nullable field is always present, `null` when it has no value; a field the schema does not require MAY be absent.

### 9.1.7 Media types

| Media type | For |
|---|---|
| `application/json` | JSON documents and single answers |
| `application/problem+json` | Problems |
| `application/x-ndjson` | Chunked answers as JSON lines (§9.6) |
| `application/vnd.apache.arrow.stream` | Chunked answers as an Arrow IPC stream (§9.6) |
| `text/csv` | Tables for people (never parsed by a client) |
| `application/vnd.apache.parquet` | Export files |
| `application/vnd.atlas.profile+json` | A profile |
| `application/cloudevents+json` | An event, alone |
| `application/vnd.atlas.handoff+json` | A handoff document (§9.3) |
| `application/vnd.atlas.connect+json` | A connect handover (§9.3) |

## Rules

1. Every grammar here is used exactly; a sender MUST NOT send, and a receiver MUST reject, a value that does not match.
2. Where a receiver rejects input, it produces the problem named in the section that defines it.

## Units

The ABNF above is collected in `references/grammar.abnf` (§9.3).

## Requirements

- **AP-CONV-001.** Every client and the gateway rejects a JSON document with duplicate member names, or a number outside the I-JSON range where an integer is required.
- **AP-CONV-002.** Every client and the gateway accepts exactly the strings the grammars of 9.1.3 to 9.1.5 accept (the grammar test vectors).
