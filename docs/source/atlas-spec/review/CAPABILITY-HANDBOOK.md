# Atlas capability handbook — proposal for review

**Status: a proposed human reference, not a deployment inventory or a replacement for the source specification.** This handbook accounts for all **138 distinct capability IDs in 27 domains** in the [current catalogue](../07-the-catalogue.md): 114 data capabilities, 21 resources, two events and one action. Every catalogue entry is version 1. Later sections account separately for examples, test fixtures, old names and an uncatalogued upload example.

The catalogue gives useful summaries. It does **not** supply 138 complete, executable profiles: most exact request schemas, field definitions, defaults, freshness promises and error contracts still need their owner's words. The four profiles in [§3.1](../03-contracts.md) are explicitly abbreviated examples. “Official” below is the catalogue's proposed stability classification; it is not evidence that a capability is deployed, healthy, accessible or production ready.

The shared gateway kit and local SQL library provide common mechanisms behind capability providers. This handbook describes the desired public experience without inventing implementation boundaries. Backtesting research is deferred; it is not designed here.

Start with the [complete inventory](#complete-catalogue-inventory) to browse a capability, the [shared call examples](#one-journey-then-one-call-shape) to see the interface, or the [holdings page](#worked-human-page-portfolio-holdings) to see the proposed depth of a finished guide. The [name and overlap decisions](#names-and-overlaps-that-need-an-explicit-decision) identify unresolved catalogue choices.

## The experience Atlas should provide

An agent starts with the person's task, not a list of databases and APIs. Atlas tells it who it represents, which environment it is using, which capabilities it can invoke, which ones answer the question, and how to invoke them. After the agent has explored through the CLI, its Python or Node program uses the same capability address, request fields and behavior.

The agent should still understand the person's intent. Atlas should not silently decide which of several portfolios the phrase “my fund” means, which benchmark is appropriate, or whether stale data is good enough. Atlas supplies candidates, documented defaults and evidence so the agent can make that choice explicitly.

The useful division is:

| Atlas owns | The agent or application owns |
|---|---|
| Sign-in context, caller and workload identity, grants, limits, source access, routing and audit | The task, the intended audience and the decision the result supports |
| Finding relevant capabilities and explaining their meaning, parameters and limitations | Choosing among genuinely different business questions |
| Resolving business identifiers through documented capabilities | Confirming an ambiguous business choice with the person |
| Executing a capability through the right source and returning a typed result | Analysis, presentation and local files |
| Explaining a refusal or failure with a useful next step | Following that next step without inventing a different access route |

This follows [capabilities](../02-1-capabilities.md), [discovery](../02-6-discovery.md) and [clients](../05-clients.md). The explicit caller-context and relevance rules below are proposals to finish that experience.

## Establish the context once

The CLI, Python SDK and Node SDK should resolve the same connection context and use the same sign-in for a person's session. A scheduled workload uses its own identity. Moving a request from the CLI into a pipeline does not transfer the person's grants to the workload.

**Proposed context rule:** one configured organization/tenant and environment are the default for a connection. The gateway derives the authenticated tenant and principal from trusted authentication, verifies the selected organization, and returns the effective context in `whoami`. A request payload cannot impersonate a principal or choose an arbitrary tenant. Switching organization, account or environment is explicit. Caches and saved connection state are isolated by that effective context. The current [client connection rules](../09-7-clients.md) define gateway/environment precedence and default to Production; they do not yet define a complete organization/tenant switching contract. That omission needs a decision before company-wide rollout.

**Proposed relevance rule:** “available to this caller” is the permission boundary; “recommended for this task” is a ranking within that boundary. A role can inform recommendations, but it does not identify a person's current portfolio or silently narrow the result. If Atlas applies a default portfolio, date, currency, source or scope, the guide describes it and the answer reports the effective value. An explicit request overrides a default only within authorization.

Keep these three facts separate when presenting a capability:

| Fact | What it proves | What it does not prove |
|---|---|---|
| The caller holds an Atlas interaction | Atlas permits an attempt in this environment | The upstream provider accepts the identity, the licence covers an instrument, or a requested row exists |
| The source and service are currently available | A current health check is satisfactory | The next request cannot fail, the date has landed, or the caller has the relevant data entitlement |
| This client supports an interaction's binding | This client can speak the required protocol | The caller may invoke it or its result fits in memory |

The source's controls remain authoritative. Where the gateway calls as a shared service identity, the profile and guide need to say how Atlas enforces the caller's permitted scope; recording a caller in an audit does not itself provide row-level restriction. See [access](../06-2-access-limits-and-audit.md) and [source identities](../06-3-reaching-systems.md).

## One journey, then one call shape

The proposed default sequence is short:

1. `atlas whoami` establishes context and applicable house rules.
2. `atlas search "the business question"` finds relevant capabilities the caller can use.
3. `atlas guide <id>` explains the selected capability. Use `describe` for the exact machine contract.
4. Invoke the interaction. Use `explain` first when the capability offers it and a cost, scope or expensive result needs checking.
5. Inspect completion, effective dates, warnings and any partial/truncated indicator before interpreting the result.

`atlas access` is the explicit route to discover why an expected listed capability is absent. A confidential capability remains hidden to a non-holder. Guides and recommendations must not disclose capabilities that caller cannot use. These rules are already central to [§2.6](../02-6-discovery.md).

**The examples below are target interfaces from the proposed spec, not instructions verified against an installed SDK or a live gateway.** Package names and constructors are those documented by [§5](../05-clients.md) and [§9.7](../09-7-clients.md). The generic `call(address, request, options)` is the shared foundation. Language-specific convenience methods can sit above it, but must not introduce different business parameters or hidden execution behavior.

The same request, member for member:

```json
{
  "portfolios": ["147HYC"],
  "as_of": "2026-10-28",
  "columns": ["portfolio", "isin", "as_of", "weight"]
}
```

```console
atlas whoami
atlas search "holdings and weights for a portfolio"
atlas guide portfolio.holdings@v1
atlas call portfolio.holdings@v1:fetch --request '{"portfolios":["147HYC"],"as_of":"2026-10-28","columns":["portfolio","isin","as_of","weight"]}'
```

```python
from fiatlas import FIAtlas

atlas = FIAtlas()
request = {
    "portfolios": ["147HYC"],
    "as_of": "2026-10-28",
    "columns": ["portfolio", "isin", "as_of", "weight"],
}
result = atlas.call("portfolio.holdings@v1:fetch", request)
```

```typescript
import { atlas } from "@atlas/sdk";

const client = await atlas.connect();
const request = {
  portfolios: ["147HYC"],
  as_of: "2026-10-28",
  columns: ["portfolio", "isin", "as_of", "weight"],
};
const result = await client.call("portfolio.holdings@v1:fetch", request);
```

`columns`, business filters and dates belong in the request. Deadlines, idempotency keys, transport preferences and local output destinations are call/client options. The same address with `:explain` plans this request without fetching it, if that interaction is granted. `fetch` convenience methods and Python's `read_as` can provide native tables; converting to a DataFrame is a local presentation choice. Exact result-handle methods for the generic call should be settled consistently across clients before examples are published as runnable instructions.

For a durable application, pin the capability version and reviewed contract through its lock. An unversioned convenience command is suitable for exploration. The agent's saved program should retain the chosen address, parameters and business meaning, not a private Snowflake table name or provider endpoint discovered behind the scenes.

## What every human capability page must answer

The same authored meaning should feed the human page, the CLI guide and the SDK examples. There should be one profile beside the implementation, not an independently maintained manual and a second schema language. Generate the mechanical parameter, field and interaction tables; have the owner write the purpose, exclusions, business definitions and examples.

| Part of the page | Required content |
|---|---|
| Promise | One sentence explaining the question this capability answers; owner; canonical ID and version |
| Use / do not use | Concrete examples, authoritative route and relevant alternatives |
| Behavior | Data/resource/action/event; each offered interaction and its effect; stability and lifecycle separately |
| What to supply | Exact field names, types, requiredness, defaults, allowed values, units and how to obtain valid identifiers |
| What to expect | Result shape; table grain and keys or record fields; ordering; null/empty behavior; business meaning and units |
| Time | Meaning of each date; source freshness and expected publication; handling of weekends, missing snapshots, corrections and stale answers |
| Authority | Identity used downstream; scope/entitlement checks; visibility and licensing obligations |
| Cost and size | Metering unit; whether an estimate exists; caller-specific limits; output caps, paging and truncation behavior |
| Execution | Whether it may run as an operation, stream or export; what completion means; where results expire |
| Change behavior | Whether it changes anything; ETag and idempotency requirements; approval policy and retry behavior |
| Examples | A minimal valid request, a realistic request and the same request in CLI, Python and Node |
| Failure and recovery | Known business problems, retryability, precise next steps and an owner/support route |

The [agent-readiness gate in §3.1.4a](../03-contracts.md) already requires much of this. Most catalogue rows do not yet contain enough information to fill the table honestly. An owner-complete profile is therefore the acceptance criterion, not the presence of a row in this inventory.

## Common behavior a reader learns once

| Interaction or result | Expected behavior in the current proposal |
|---|---|
| Data `fetch` | Read the answer described by the profile. A read can be licensed, metered, slow or large. It is not automatically free. |
| Data `explain` | Validate and plan a particular request, with estimates where supported. It is offered and granted separately; it is not an obligatory extra call before every read. |
| Data `export` | Produce a downloadable result for bulk use. It has its own result/delivery contract; a catalogue table's “table, single” usually describes `fetch`, not export. |
| Resource `get` / `list` | Read an object / a paged collection of objects. Standard methods have one meaning across domains. |
| Resource mutation | Require the appropriate grant and idempotency behavior. Updates and deletes use the ETag read by the caller; a conflict requires re-reading and deciding, not unconditional overwrite. |
| Action `plan` / `approve` / named verb | Show the intended external change, obtain approval where required, then run and follow the operation to a receipt. Reuse the idempotency key for the same logical attempt. |
| Event `subscribe` / `publish` | Listen / emit under distinct grants. The spec brokers a direct Service Bus connection; an event is not proof that every consumer has processed it. |
| Chunked table | Consume batches. Do not equate received rows with a successfully completed answer until the completion marker is checked. |
| Operation | An accepted task, followed by progress and a final result or problem. A `202` is not success. |
| Transfer/export | An upload/download with completion and expiry. A short-lived URL is a delivery mechanism, not permanent storage. |

The source defines `read`, `write`, `external` and `irreversible` effects, with increasingly strict confirmation/approval defaults. Exact policy still comes from current governance. Reads with a cost above the caller's confirmation threshold require confirmation. A generic retry must never evade a budget, approval, deny or confirmation. For the same mutation attempt, retain the same idempotency key; a fresh key denotes a fresh attempt, and the retention window must be respected. See [surfaces](../02-1-capabilities.md), [resources](../02-3-resources.md), [actions](../02-4-actions.md) and [request/result lifecycle](../04-plugging-in.md).

The source spec keeps an operation at least 24 hours after completion and specifies a 24-hour idempotency window. The [proposed core](ATLAS-CORE.md) requires retention to cover active work and its promised recovery window, including operations that last longer than a day. Export links/files expire according to served metadata; do not infer a permanent URL. A large table should be streamed or exported; materializing every row is an explicit client choice. “Latest” must be reported as an effective source date, not mistaken for the current wall-clock time.

## Worked human page: Portfolio holdings

**Illustrative completed-page structure, using facts actually present in the abbreviated holdings profile.** The unknowns below are acceptance work, not guarantees invented for this handbook.

| Item | Description |
|---|---|
| Capability | `portfolio.holdings@v1` — positions in one or more portfolios on a valuation date |
| Use it when | You need what named portfolios hold, including weights, market values or holding analytics |
| Use another question when | You need a bucketed benchmark comparison, historical executed trades or positions selected by a security filter across portfolios; the catalogue provides separate questions for these |
| Family and stability | Data; official in the proposed catalogue. The sample profile says stable; deployment readiness has not been verified |
| Interactions | `fetch`, `explain`, `export`, each a read; grant and limits are per interaction |
| Inputs shown in the sample profile | Required `portfolios`: array of portfolio codes; optional `as_of`: date/null; optional `asset_class`: `credit`, `rates` or null; optional `columns`: string array/null |
| Identifier lookup | Resolve loose words with `portfolio.search`; use `portfolio.list` to obtain recognized codes, if granted |
| Time meaning in the sample | The portfolio's last valuation on or before `as_of`; the meaning of null and multi-portfolio date alignment still need explicit prose |
| Grain and sample keys | Portfolio × security × date; sample keys are `portfolio`, `isin`, `as_of` |
| Fields shown | `portfolio`, nullable `isin`, `as_of`, `market_value` as Money, `weight` as Fraction, nullable `oas` as basis points |
| Delivery | `fetch` may stream through the gateway or use handoff; transport choice must preserve the same business answer. Export is a separate bulk result |
| Changes and cost | Reads do not change holdings. Warehouse costs, quotas and licensing may still apply; read current governance and estimates |
| Problems shown | `portfolio_not_found` and `warehouse_suspended`; the abbreviated error definitions still need complete descriptions and next steps |

The request in the earlier CLI/Python/Node example asks for the same columns and the same date in all three clients. A reader interprets `weight` as a fraction, not a percentage number. If the returned valuation date precedes the requested date, that follows the stated “on or before” rule; the page must say how the caller detects and rejects an excessively old snapshot.

Before publishing this as a complete profile, the owner must settle: the history/range request described by §7 but omitted from the sample schema; what null `as_of` means; whether every portfolio uses the same snapshot date; keys for cash, derivatives and securities without ISINs; duplicate-lot aggregation; market-value currency and signs; whether weights include cash and sum to one; partial authorization or missing portfolios; ordering and row limits; source-correction/freshness behavior; and the `Licensed` sample trait's persistence rules. In particular, a nullable `isin` cannot by itself distinguish every security within one portfolio/date.

Sources: [catalogue, Portfolios](../07-the-catalogue.md), [abbreviated profile, §3.1.3](../03-contracts.md), [data surface](../02-2-data-surface.md).

## Worked human page: Restart an Astronomer deployment

**Illustrative operational page; this is not a recommendation to run a restart.**

| Item | Description |
|---|---|
| Capability | `astronomer.deployment@v1` |
| Promise | Restart the selected deployment's scheduler and workers after a plan and the applicable approval |
| Availability stated in catalogue | Official, preview; initially restricted to `Gateway.Admin` |
| What to supply | The deployment identifier shown in the abbreviated request; discover deployments through the read capability if granted |
| Read first | `plan` returns what restarting will do; the page must spell out expected interruption, dependencies and what is not restarted |
| Approval | Separate from requesting the action; current policy determines which environments require another principal |
| Production restriction | The current action surface requires a person to run a Production action; availability in the catalogue does not authorize an autonomous Production restart |
| Change | `restart` is marked external and returns an operation; completion returns the receipt |
| Replay | Reuse the original idempotency key for a retry of the same restart; never substitute a new key merely because an answer was lost |
| Expiry and completion | Plans expire; an expired plan needs a fresh plan and any applicable approval. Follow the operation to a terminal state and inspect its receipt |
| Receipt sample | The abbreviated profile shows `restarted_at` and `scheduler_pods`; it does not yet fully establish worker completion or application health |

The caller uses the same generic call interface with `astronomer.deployment@v1:plan`, `:approve` where authorized, and `:restart`. The exact linkage of the plan ID, its revision, the approved request and the run must be defined in the final request schemas; this handbook does not invent those fields. A complete guide should distinguish “restart request accepted,” “pods restarted,” and “deployment healthy.” Cancelling an operation cannot promise reversal of a restart already performed.

There is a source contradiction to resolve: [§2.4](../02-4-actions.md) says no action is catalogued yet; [§7](../07-the-catalogue.md) explicitly includes this preview action. The sample profile says `reversible: false` while the effect is `external`; the policy must clarify the distinction between a non-reversible execution and the stricter `irreversible` effect category.

## Complete catalogue inventory

These tables answer **which question each named capability serves and what the catalogue says comes back**. Request clues are plain-language orientation, not authoritative field names or required/optional declarations. Obtain the actual request schema from its profile. No concrete schema is invented where §7 supplies none.

For the many curated data domains, the standard set is **`fetch`, `explain`, `export`**, all read; the fetched answer is a table; execution is gateway or handoff; stability is official. Each domain states whether it follows this convention. “Single” and “chunked” below describe the catalogue's fetched answer. Exact profiles must separate the result/delivery of `fetch`, `explain` and `export`.

Every section below is sourced from its corresponding domain table in [§7 The catalogue](../07-the-catalogue.md). “Merged” means the source explicitly leaves schema equivalence between alternatives unresolved.

The catalogue assigns `atlas` and `event` to Atlas maintainers, `nautilus` to Nautilus maintainers, and leaves QFI ownership “to name” for the other domains. Named operational ownership remains unfinished for those 24 domains. Lifecycle, visibility, deadlines, exact schemas, freshness targets and business errors are generally missing at row level; do not infer “stable,” public visibility or a latency promise from “official.” The restart's preview/administrator restriction and the confidential administration entries are explicit exceptions already stated by the source.

### Atlas — 20 capabilities

Built-ins, official; gateway execution. Administrative capabilities are confidential. The selection parameters mentioned below are conceptual unless already defined by the client protocol.

| Capability | Use it for → what comes back | Behavior and request clues |
|---|---|---|
| `atlas.whoami@v1` | Orient the session → identity, roles, environment and available domains | Data, `fetch`, one record; no business input; context defaults still need the proposal above |
| `atlas.catalog@v1` | Browse usable capabilities → IDs, summaries, granted interactions and fingerprints | Data, `fetch`, collection; optional domain/family/effect |
| `atlas.catalog.changed@v1` | Learn the working view changed → new catalogue ETag | Event, `subscribe`; refetch the caller's catalogue |
| `atlas.search@v1` | Find capabilities from words → ranked matches with reasons | Data, `fetch`, collection; task words |
| `atlas.describe@v1` | Inspect an exact capability → profile plus own governance and live availability | Data, `fetch`, record; capability/version/fingerprint |
| `atlas.guide@v1` | Learn how to use a capability → human/agent Markdown guide | Data, `fetch`, record; capability and client syntax/token budget |
| `atlas.access@v1` | Understand access gaps → own status, reason, next step and limits | Data, `fetch`, collection; optional capability/domain |
| `atlas.usage@v1` | Check consumption → used, remaining, period and pool | Data, `fetch`, collection; exact filters to be authored |
| `atlas.setup@v1` | Set up a machine/harness → sign-in, environment, skill and verification plan | Data, `fetch`, record; local context inputs to be defined |
| `atlas.clients@v1` | Choose a client → languages, roles, extras, versions and install instructions | Data, `fetch`, collection |
| `atlas.compare@v1` | Compare capability versions → key-joined results after a lens | Data, `fetch`, chunked table; two versions and a data request; business comparison semantics need care |
| `atlas.operation@v1` | Follow started work → state, progress, result or problem | Resource: `get`, `list`, `watch`; `cancel` write; operation name |
| `atlas.settings@v1` | Read/change own settings → settings document | Resource: `get`; `update` write; updates need current ETag |
| `atlas.directory@v1` | Find a person/group to share with → principal, name and kind | Data, `fetch`, single table; search terms |
| `atlas.grant@v1` | Administer explicit grants/denies → grant and who set it | Confidential resource: `get`, `list`; `create`, `delete` write |
| `atlas.role@v1` | Administer named grant/limit sets → role details | Confidential resource: `get`, `list`; `update` write; membership authority is Entra |
| `atlas.limit@v1` | Administer allowances and pools → subject, interaction, amount and period | Confidential resource: `get`, `list`; `update` write |
| `atlas.context@v1` | Maintain distributed guidance → scope, published revision and drafts | Confidential resource: `get`, `list`; `update`, `publish`, `rollback` write |
| `atlas.switch@v1` | Suspend a capability/domain/service/behavior → switch state, reason and expiry | Confidential resource: `get`, `list`; `update` write |
| `atlas.audit@v1` | Inspect call decisions and outcomes → one row per call | Confidential data, `fetch`, single table; a day and documented filters |

### Portfolio — 17 capabilities

Curated data convention. All fetched tables are single except holdings and trades, which are chunked. `portfolio.search` has `fetch` only and gateway execution only.

| Capability | Use it for → what comes back | Request clues / distinctions |
|---|---|---|
| `portfolio.list@v1` | Enumerate current portfolios → code, name, region, asset class | Current catalogue; exact filters unspecified |
| `portfolio.search@v1` | Resolve loose portfolio names → ranked codes and reasons | Search words; use returned codes in subsequent requests |
| `portfolio.reference@v1` | Read portfolio master data → currency, type, country, manager and accounting fields | Portfolio codes; merged `source`: `ids` default or `harbour` |
| `portfolio.reference-values@v1` | Build a valid master-data filter → distinct values and portfolio counts | A master field |
| `portfolio.managers@v1` | Find manager assignments → portfolio code, name, manager | Scope/filter to be defined |
| `portfolio.holdings@v1` | Read positions → portfolio × security × date, value, weight, analytics | Portfolios and date; merged history/range semantics; chunked |
| `portfolio.holdings-dates@v1` | Find valuation dates → portfolio/date, position count and total market value | Portfolio/date window |
| `portfolio.analytics@v1` | Read fund contributions at latest snapshot → OAD, OASD, OAC, DTS, yield and benchmark contributions | Funds; latest snapshot, not arbitrary benchmark bucketing |
| `portfolio.breakdown@v1` | Compare a portfolio with a chosen benchmark → bucket weights and active differences | Portfolio, any benchmark, bucket dimension |
| `portfolio.benchmark-comparison@v1` | Compare funds with their own benchmarks → bucket weights and contributions | Funds and bucket choices; owns the “own benchmark” question |
| `portfolio.benchmarks@v1` | Find benchmarks with stored constituents → code, name, latest date, count | Listing/filter parameters to be authored |
| `portfolio.exposure@v1` | Find matching positions across portfolios → portfolio/security, value and weight | Security filter; overlap with `security.holders` is unresolved |
| `portfolio.trades@v1` | Read executed trades → side, quantity, price and dates | Portfolios; newest first; merged history/range; chunked |
| `portfolio.returns@v1` | Read portfolio and benchmark returns → investment, benchmark and relative returns by date/period | Portfolios, valuation date and periods |
| `portfolio.attribution@v1` | Explain outperformance by effect → allocation, selection, curve and spread effects | Portfolio, benchmark and dates |
| `portfolio.attribution-issuers@v1` | Explain issuer outperformance → issuer allocation and selection | Attribution scope and issuer filters |
| `portfolio.attribution-securities@v1` | Explain security outperformance → spread change, carry, OASD, contributions and selection | Attribution scope and security filters |

### Index — 3 capabilities

Curated data convention; all three merge multiple sources, with schema equivalence still open.

| Capability | Use it for → what comes back | Request clues / distinctions |
|---|---|---|
| `index.list@v1` | Find indices → vendor, code, name and coverage dates | `source`: `harbour` default, `ice`, `quantumleap`; single table |
| `index.constituents@v1` | Read index membership/weights → index × security × date, price and analytics | Index/date or range; `source`: `harbour` default, `ids`, `ice`, `lsta`, `quantum`, `quantumleap`; chunked |
| `index.analytics@v1` | Read index-level metrics → OAD, OAS, OASD, DTS, yields, price and returns | Index/date; `source`: `harbour` default, `ice`, `lsta`; single table |

### Security — 6 capabilities

Curated data convention. Analytics, returns and universe are chunked; the rest are single.

| Capability | Use it for → what comes back | Request clues / distinctions |
|---|---|---|
| `security.reference@v1` | Identify securities → issuer hierarchy, LEI, type, coupon, maturity and classification | ISIN/CUSIP/SEDOL/ticker; merged `source`: `ids` default or `harbour` |
| `security.analytics@v1` | Read security metrics → price, yields, OAD, OAS, OASD, DTS and Z-spread | Security, index context and date |
| `security.returns@v1` | Read return components → total, price, excess, coupon, currency and paydown | Security, index context and date |
| `security.holders@v1` | Find who holds one security → portfolio positions, value, weight and date | Security; merged history option; proposed overlap/merge with `portfolio.exposure` |
| `security.universe@v1` | Screen research securities → security × date descriptive/analytic fields | Research filter and date or range |
| `security.universe-aggregates@v1` | Summarize a research screen → group count and unweighted averages | Research filter and grouping; not portfolio-weighted analytics |

### Curve — 1 capability

Curated data convention; single table.

| Capability | Use it for → what comes back | Request clues |
|---|---|---|
| `curve.government-zero@v1` | Read government zero curves → country/currency/tenor/date, zero rate | Countries, currencies, tenors and dates |

### Signal — 11 capabilities

Curated data convention. Units and model definitions still require authored profiles; names alone do not define a trading strategy.

| Capability | Use it for → what comes back | Request clues / delivery |
|---|---|---|
| `signal.composite-score@v1` | Read composite and FVCM/EMC/DRSK components → bond/date scores, percentiles and z-scores | Bond/date selection; chunked |
| `signal.composite-score.trades@v1` | Score a portfolio/index's constituents → composite score and components | Portfolio or index; single; despite “trades,” description is constituent scoring |
| `signal.drsk@v1` | Read equity-implied default-risk residuals → default distance, predicted OAS, residual and percentile | Bond/date; chunked |
| `signal.emc@v1` | Read momentum signals → 1/6-month/blended momentum and percentiles | Bond/date; chunked |
| `signal.fvcm.curves@v1` | Read fitted curves → model/group/date, Nelson–Siegel parameters, bonds and errors | Merged `view`: `curves` default, `globalrv`, `latest-date`; single; last variant needs a grain decision |
| `signal.fvcm.models@v1` | List FVCM models → mathematical model and bond count | Date; single |
| `signal.fvcm.residuals@v1` | Read fitted values/residuals → bond/model/date, residual in Bp, positive means cheap | Bond/model/date; chunked |
| `signal.fvcm.universe@v1` | Inspect the model universe → identifiers, issuer, currency, universe, ratings, industry, coupon and maturity | Model/universe scope to be defined; chunked |
| `signal.macro-credit@v1` | Read daily macro-credit signal → market/date, raw, smoothed and binary signals/scores | Market/date; single |
| `signal.rates-fair-value@v1` | Read daily rates model → market/date, signal, RSI z-score and yield changes | Market/date; single |
| `signal.asset-allocation@v1` | Read asset-allocation model states → asset/date, excess return, spread, state and probabilities | Asset/date; single |

### Model portfolio — 2 capabilities

Curated data convention. These are model/paper portfolios, not the live holdings question.

| Capability | Use it for → what comes back | Request clues / delivery |
|---|---|---|
| `model-portfolio.list@v1` | Find model portfolios → name, type, manager, benchmark, currency and status | Current model catalogue; single |
| `model-portfolio.positions@v1` | Read paper positions → portfolio/security/date, portfolio and optimizer weights | Model portfolio, securities and dates; chunked |

### Universe — 2 capabilities

Curated data convention.

| Capability | Use it for → what comes back | Request clues / delivery |
|---|---|---|
| `universe.group-performance@v1` | Read Global HY/Global Agg corporate group performance → group/period level and change | Universe, group and period; single |
| `universe.group-analytics@v1` | Read grouped analytics through time → entity/grouping/series analytics | Merged `view`: `quantum` default or `globalrv`; chunked; schema equivalence open |

### Securitized — 5 capabilities

Curated data convention. Holdings are chunked; the rest are single.

| Capability | Use it for → what comes back | Request clues / distinctions |
|---|---|---|
| `securitized.holdings@v1` | Read securitized positions → portfolio/security/date classification and analytics | Portfolios/dates; merged `engine`: `mondrian` default or `quantum` |
| `securitized.contributions@v1` | Read portfolio contributions → portfolio/group/date OAD, OASD, OAS and DTS | Portfolio, groups and dates |
| `securitized.classifications@v1` | Classify securities → class levels 1–4 | Securities |
| `securitized.clo-managers@v1` | Link CLO positions to managers → manager, rating, benchmark and deal status | Position/universe filters to be defined |
| `securitized.empirical-durations@v1` | Read regression-based duration → portfolio/tenor/date empirical duration | Portfolio, Treasury tenor and dates; regression window needs owner definition |

### Research — 4 capabilities

The first three follow the curated data convention with single tables. Documents is `fetch` only, gateway only, official, single table. This domain is the existing catalogue's fixed-income research data, not a design for the deferred repository.

| Capability | Use it for → what comes back | Request clues / distinctions |
|---|---|---|
| `research.issuers@v1` | Find issuer metadata and coverage → ticker, sector, industry, country and coverage | Issuer/ticker filters |
| `research.coverage@v1` | Read current research assessment → analyst, recommendation/date, risk and ESG views | Issuers |
| `research.recommendations@v1` | Read recorded rating changes → issuer/change/date and rating | Issuers and time range |
| `research.documents@v1` | Search research, credit memos and offering documents → scored passages with document references | Semantic query and permitted filters; distinguish from a named KB's lexical search |

### Product — 2 capabilities

Curated data convention; single tables.

| Capability | Use it for → what comes back | Request clues |
|---|---|---|
| `product.aum@v1` | Read official fund-product AUM → product/group/month AUM and flows | Product/group/month; currency and flow definitions need profile |
| `product.share-classes@v1` | Find open fund share classes → codes, ISIN, currency and status | Fund/share-class filters |

### Data — 3 capabilities

Official, gateway execution. This is the source-status domain; a source is not itself a license to invoke every capability reading it.

| Capability | Use it for → what comes back | Behavior / request clues |
|---|---|---|
| `data.sources@v1` | Understand curated source coverage → source title, service, dates and reading capabilities | Data, `fetch`, single table; references must be filtered to caller visibility |
| `data.status@v1` | Check freshness → source latest date, expected date and status | Data, `fetch`, single table; source/date behavior requires clarification |
| `data.landed@v1` | React when a source/date is complete → source, as-of date, row count | Event; `subscribe` read, `publish` external; profile declares source ordering/history |

### Event infrastructure — 3 capabilities

Official resources, gateway execution. These manage durable messaging entities; the event type and publish/listen grants remain separate.

| Capability | Use it for → what comes back | Interactions / request clues |
|---|---|---|
| `event.topic@v1` | Own a topic for an event type/family → name, types, owner, retention | `get`, `list`; `create` write; `delete` irreversible; creating registers publisher ownership |
| `event.queue@v1` | Own point-to-point work delivery → name, owner and retention | `get`, `list`; `create` write; `delete` irreversible |
| `event.subscription@v1` | Own a durable filtered listener → topic, filter and owner | `get`, `list`; `create`, `delete` write |

### Global RV — 3 capabilities

Curated data convention; single tables. Its curves and analytic series are in the signal/universe domains.

| Capability | Use it for → what comes back | Request clues |
|---|---|---|
| `globalrv.navigation@v1` | Browse the application tree → name, path, source, children | Tree scope/filter to be defined |
| `globalrv.navigation-search@v1` | Type ahead in that tree → matching name/path | Search text |
| `globalrv.saved-queries@v1` | Read saved queries and folders → folder/query name and rows | Caller-owned/shared scope must be explicit |

### Knowledge bases — 8 capabilities

Official; gateway execution. Resource result shapes depend on the interaction; `list` is a collection, and download/content behavior needs its own profile entry.

| Capability | Use it for → what comes back | Interactions / request clues |
|---|---|---|
| `kb.base@v1` | Manage an OKF knowledge base → id, title, version, pages, owner, visibility and validity | `get`, `list`, `download`, `validate`; `publish`, `share`, `batch` write; `delete` irreversible; base/bundle/sharing inputs |
| `kb.page@v1` | Read/write a page → Markdown, metadata and revision | `get`, `outline`; `create`, `update` write; base/page identity and content |
| `kb.concept@v1` | Manage a remembered concept → id, title, summary, aliases and pages | `get`, `list`, `history`; `create` write; base/concept identity |
| `kb.fact@v1` | Record or revise an assertion → revision, evidence and temporal validity | `get`, `list`; `create`, `revise` write; base/concept/evidence context |
| `kb.source@v1` | Register evidence a fact cites → source id, title and type | `create` write; source metadata; no read interaction listed |
| `kb.search@v1` | Lexically search a named base → page/section/text matches | Data, `fetch`, single table; base and search words |
| `kb.context@v1` | Gather agent context → matched pages and assertions within a byte budget | Data, `fetch`, record; base, query and budget |
| `kb.reference@v1` | Resolve a citation → named page section or assertion | Data, `fetch`, record; `kb://` or `kb-memory://` reference |

### Nautilus — 5 capabilities

Official resources, gateway execution. These manage hosted state. Placing a downloaded file in a local harness is a client extra, not the hosted install interaction.

| Capability | Use it for → what comes back | Interactions / request clues |
|---|---|---|
| `nautilus.dashboard@v1` | Manage a published dashboard → identity, owner, visibility, version and ZIP files | `get`, `list`, `watch`; `publish`, `share`, `restore` write; `delete` irreversible; dashboard/bundle and sharing |
| `nautilus.dashboard-version@v1` | Browse stored dashboard revisions → number, time, author, note, files and live status | `list`, collection; dashboard identity |
| `nautilus.job@v1` | Manage a scheduled agent job → definition, schedule, state, runs and log | `get`, `list`, `log`; `create`, `update`, `run` write; `delete` irreversible; run returns operation |
| `nautilus.skill@v1` | Manage hosted/shared skills → files, versions, visibility, installs and usage | `get`, `list`, `download`, `usage`; `save`, `install`, `uninstall`, `publish`, `unpublish`, `share`, `curate` write; `delete` irreversible |
| `nautilus.workspace-file@v1` | Manage hosted workspace files/folders → path, name, kind, size, modified and bytes | `get`, `list`, `search`; `create`, `update`, `move` write; `delete` irreversible; remote path/content |

### Bloomberg — 4 capabilities

Provider data is metered/licensed according to the domain description. Live calls are upstream, `fetch` only, gateway only and single tables. Cache follows the curated convention and is chunked/official. Prefer owned security capabilities and the stored cache where they answer the question.

| Capability | Use it for → what comes back | Request clues / distinctions |
|---|---|---|
| `bloomberg.reference@v1` | Read current/point-in-time fields (BDP) → security/field value | Securities, fields, optional point-in-time options from profile |
| `bloomberg.history@v1` | Read historical fields (BDH) → security/field/date value | Securities, fields and date range |
| `bloomberg.bulk@v1` | Read field sets (BDS) → security/field/record values | Securities and bulk fields; nested-record interpretation needs profile |
| `bloomberg.cache@v1` | Read Bloomberg fields already stored daily → security/date stored fields | Securities, fields and business dates; source freshness differs from live API |

### Yield Book — 5 capabilities

Upstream data, gateway only, `fetch` only; metered. First three return operations ending in tables. History and identifiers return single tables.

| Capability | Use it for → what comes back | Request clues / distinctions |
|---|---|---|
| `yieldbook.reference@v1` | Read indicative/reference attributes → coupon, maturity, ratings and index membership | Securities; operation |
| `yieldbook.price-yield@v1` | Calculate analytics → price, yield, duration, convexity, OAS and spreads | Securities and pricing assumptions; operation |
| `yieldbook.scenarios@v1` | Calculate a scenario grid → security/scenario analytic block | Up to 25 securities and scenario assumptions; operation |
| `yieldbook.history@v1` | Read one security's history → requested fields by date | One security, fields and dates |
| `yieldbook.tba-ids@v1` | Find recognized TBA identifiers → programme, coupon and identifier | Identifier catalogue/filter inputs to be authored |

### Intex — 8 capabilities

Upstream data, gateway only, `fetch` only; licensed. Cashflows and price-yield return operations; the remainder return single tables.

| Capability | Use it for → what comes back | Request clues / distinctions |
|---|---|---|
| `intex.data@v1` | Read static/reported structured-finance data → position/field value | Positions and fields |
| `intex.fields@v1` | Learn accepted data fields → keyword, description and type | Field catalogue/filter |
| `intex.cashflows@v1` | Project cashflows → requested vectors by period | One deal/tranche and scenario; operation |
| `intex.cashflow-vectors@v1` | Learn available vectors → name and description | Vector catalogue/filter |
| `intex.price-yield@v1` | Solve price/yield analytics → solved fields | One deal/tranche and scenario; operation |
| `intex.price-yield-fields@v1` | Learn available solved fields → name and description | Field catalogue/filter |
| `intex.curves@v1` | Find usable discount curves → curve names | Curve catalogue/filter |
| `intex.validation@v1` | Check identifiers and licence coverage → recognized/licensed flags | Identifiers; passing recognition does not mean every calculation will succeed |

### DataQuery — 5 capabilities

Upstream data, gateway only, `fetch` only, single tables.

| Capability | Use it for → what comes back | Request clues / distinctions |
|---|---|---|
| `dataquery.search@v1` | Discover series from words → candidate datasets/series with expressions | Plain-language query |
| `dataquery.datasets@v1` | Browse datasets → id, name, description and provider | Keyword |
| `dataquery.expressions@v1` | Browse a dataset's series → instrument, attribute and exact expression | Dataset identifier |
| `dataquery.expression-details@v1` | Explain known expressions → recognition and meaning | Expressions; no historical values |
| `dataquery.time-series@v1` | Read series history → expression/date value | Exact expressions and dates |

### S&P Global — 5 capabilities

Data, upstream; `fetch`, `explain`, `export`; single fetched tables; gateway or handoff. Xpressfeed's storage in Snowflake does not change the provider-owned meaning.

| Capability | Use it for → what comes back | Request clues |
|---|---|---|
| `spglobal.companies@v1` | Find a company → Capital IQ id, name, industry and country | Ticker/name |
| `spglobal.capital-structure@v1` | Read debt structure → component type, seniority, security, maturity and amount | One company |
| `spglobal.financials@v1` | Read company financial items → period/item value | One company, items and periods |
| `spglobal.transcripts@v1` | Find earnings calls → call id, date, event type and version | One company |
| `spglobal.transcript@v1` | Read one call → speaker/text components in spoken order | Transcript identifier |

### CreditFlow — 4 capabilities

Upstream data, gateway only, `fetch` only, single tables.

| Capability | Use it for → what comes back | Request clues / distinctions |
|---|---|---|
| `creditflow.deals@v1` | Read desk-facing new issues → deal/tranche terms, timing and status | Deal filters; overlap with new-issues requires clearer distinction |
| `creditflow.new-issues@v1` | Read new-issue metadata → deal/tranche terms and timing | Metadata filters; difference from deals must be documented |
| `creditflow.issuers@v1` | Read issuer master → id, ticker, name, country and industry | Issuer lookup/filter |
| `creditflow.sectors@v1` | Browse classification tree → sector/subsector id and name | Classification catalogue/filter |

### Citi — 3 capabilities

Upstream data, gateway only, `fetch` only, single tables.

| Capability | Use it for → what comes back | Request clues / distinctions |
|---|---|---|
| `citi.series@v1` | Read historical series → tag/date value | Tags and dates |
| `citi.tags@v1` | Browse tags → meaning and coverage dates | Prefix |
| `citi.tag-details@v1` | Explain specified tags → known flag, meaning and dates | Exact tags; no historical values |

### Snowflake — 2 capabilities

Upstream data. Use curated capabilities first. Raw SQL is an explicitly granted escape hatch for questions not already served by a capability; it must not become the standard agent discovery workflow.

| Capability | Use it for → what comes back | Behavior / request clues |
|---|---|---|
| `snowflake.query@v1` | Run approved read-only SQL over registered schemas → query-defined rows | `fetch`, `export`; chunked; gateway or handoff; named grant; dynamic schema per answer |
| `snowflake.schema@v1` | Inspect objects allowed for free SQL → schema, object name/kind and columns | `fetch`; single table; gateway only; object scope/filter to be authored |

### SharePoint — 1 capability

Upstream resource; gateway execution.

| Capability | Use it for → what comes back | Behavior / request clues |
|---|---|---|
| `sharepoint.file@v1` | Browse/read fixed-income folders → path, name, size, modified time and bytes | `get`, `list`; remote path/identifier; list must have collection semantics and get content semantics |

### Azure — 1 capability

Upstream data; gateway execution. The catalogue distinguishes today's gateway identity from the target delegated caller identity; a guide must state which is effective in the deployed environment.

| Capability | Use it for → what comes back | Behavior / request clues |
|---|---|---|
| `azure.resource-graph@v1` | Query Azure inventory → query-defined rows | `fetch`; chunked; KQL and subscription/management-group scope; dynamic schema; effective source RBAC applies |

### Astronomer — 5 capabilities

Gateway execution. Read capabilities are upstream. The action is official/preview, initially administrator only. The catalogue says a workload identity is used per environment and Atlas grants determine caller visibility.

| Capability | Use it for → what comes back | Behavior / request clues |
|---|---|---|
| `astronomer.deployments@v1` | Browse deployments → kind, runtime, health and Airflow link | Data, `fetch`, single table; workspace scope needs profile |
| `astronomer.dags@v1` | Inspect a deployment's DAGs → schedule, state and last run | Data, `fetch`, single table; deployment |
| `astronomer.runs@v1` | Inspect DAG runs → state and failed tasks | Data, `fetch`, single table; deployment and run filters |
| `astronomer.log@v1` | Read one task attempt → log text | Data, `fetch`, chunked content; deployment/DAG/run/task/attempt identifiers |
| `astronomer.deployment@v1` | Restart scheduler/workers with a plan → plan, operation and receipt | Action: `plan` read, `approve` write, `restart` external; deployment and final plan linkage |

## Names and overlaps that need an explicit decision

The 138 catalogue IDs are unique. Unique strings do not establish unique questions, and merging names does not establish a uniform contract.

| Area | Current source evidence | Proposed disposition |
|---|---|---|
| Holdings versus exposure | [§7 house rules](../07-the-catalogue.md) proposes merging `security.holders` into security-filtered `portfolio.exposure` | Keep both documented during review; choose one authoritative question/shape before adding an alias or retiring either |
| Portfolio comparisons | Analytics, breakdown and benchmark-comparison overlap in vocabulary | Keep their distinct business questions visible: latest fund contributions; arbitrary benchmark buckets; own-benchmark buckets. Require example requests proving the difference |
| CreditFlow deals versus new issues | Both return deal/tranche terms/timing; only deals explicitly adds status | Owner must justify a distinct workflow or consolidate; a projection of the same answer alone is weak reason for another capability |
| Search versus browse versus describe | `portfolio.list/search`, Citi tags/details, DataQuery search/datasets/expressions/details | These can be legitimate distinct discovery tasks. Describe the exact transition from found identifier to retrieval instead of merging merely because outputs overlap |
| Multi-source merges | Index list/constituents/analytics; portfolio reference/holdings/trades; security reference/holders; securitized holdings; FVCM curves; universe group-analytics | Approve a merge only when identifiers, grain, keys, units, freshness, nulls and meaning agree after an owned normalization. Shared column names are insufficient |
| FVCM `latest-date` | `signal.fvcm.curves` lists a latest-date view alongside full fitted-curve rows | Likely distinct shape/grain; verify source behavior. Prefer discovery/status metadata or a separately named question over a shape-changing “view” |
| `signal.composite-score.trades` | Description scores constituents, while its noun suggests executed trades | Rename for the actual concept or document what “trades” means before approving the name |
| `data.status` and event history | §7 describes latest date; [event profile](../03-contracts.md) points `meaning.history` at data.status | A latest snapshot is not a replayable landing history. Define whether fallback means latest-state reconciliation or event replay; do not promise both |
| Resource result summaries | Many §7 rows say record/single while offering list, download or watch | Publish per-interaction shapes/deliveries in profiles; retain a concise human table without claiming one shape for every verb |
| Restart status | [actions](../02-4-actions.md) says no catalogued action; §7 includes the preview | Pick one authoritative status; mark the example clearly until tested |
| Prose/guide freshness | [§3](../03-contracts.md) gives prose a revision separate from fingerprint; [§2.6](../02-6-discovery.md) guide determinism omits that prose revision | Cache/generated guide identity needs prose revision as well as contract/governance/context/client; otherwise corrected instructions can remain stale |
| Event type registration | [§2.5](../02-5-events.md) says every event type is a capability; §7 excludes derived resource/operation events from its capability count | Keep this inventory's count faithful to §7. Before implementing events, choose whether derived events are named payloads under a resource's watch or independent event capabilities, and define discovery/authorization accordingly |

### Older names are migration references, not extra capabilities

The catalogue's “Today” column maps current names/routes to the proposed names. Examples include `sapi.*` to Bloomberg, `jpmdq.*` to DataQuery, `xpressfeed.*` to S&P Global, `knowledge.search` to `research.documents`, `snowflake.freshness` to `data.status`, and `benchmark.holdings` to an index-constituents source. Several old resource verbs become interactions of one object (`kb.*`, `nautilus.*`). The column is the source of this mapping; it is not proof that these aliases already resolve in a deployed Atlas protocol.

The catalogue mentions 171 current IDs and 91 deprecated aliases, but does not enumerate every deprecated alias. A complete migration manifest must therefore come from the existing registry. Do not count those old IDs as new catalogue capabilities, or invent exact compatibility behavior from the prose.

### Illustrations and names outside the 138-entry catalogue

| Name(s) | Classification and source | How to treat them |
|---|---|---|
| `credit-risk.spread-duration@v1` | Future team-onboarding illustration in [Vision](../00-vision.md) | Demonstrates extension without client release; not a currently catalogued business capability |
| `dataset.upload@v1` | Direct publish/upload example in [§6.3](../06-3-reaching-systems.md); no dataset domain in §7 | Unresolved orphan: explicitly label hypothetical or author/catalogue it. Do not silently advertise it as available |
| `portfolio.holdings@v2`, `nautilus.dashboard@v2`, `astronomer.deployment@v2`, `data.landed@v2` | Version-evolution examples in [§3](../03-contracts.md) | Illustrative future versions, not four new live entries |
| `nautilus.dashboard.created`, `.updated`, `.deleted`, `.published` and other `<resource>.<past-tense-verb>` events | Resource change-event convention in [§7](../07-the-catalogue.md) | Events in the resource's profile/watch contract; not independently counted capabilities |
| `atlas.operation.<state>` and `atlas.operation.succeeded` | Operation lifecycle convention in [§4](../04-plugging-in.md) and [§6.1](../06-1-front-door-and-services.md) | Derived lifecycle events; not a separate catalogue entry for every state |
| `atlas.grant.changed` | Cache-invalidation event named in [§6.1](../06-1-front-door-and-services.md) | Clarify whether an internal control event or public resource event and author that contract; “changed” does not automatically specify a new listed capability |
| `atlas.sql`, `atlas.portfolio.holdings`, `atlas.credit_risk.spread_duration`, `atlas.connect`, `atlas.lock` | SDK conveniences, constructors or lock files in [clients](../05-clients.md), [protocol clients](../09-7-clients.md) and [Vision](../00-vision.md) | Not built-in capability IDs; `atlas.sql` is shorthand for the explicitly granted free-SQL capability |
| `atlas.python` | Client hint in [discovery](../02-6-discovery.md) | Treat as a client label, not a capability; align terminology with the actual `fiatlas` package |

The [conformance gateway](../09-8-conformance.md) also defines **15 versioned fixture profiles, representing 14 distinct IDs**. They exercise the protocol and do not belong in an end user's production business catalogue:

| Fixture profile | Purpose |
|---|---|
| `conformance.echo@v1` | Single-record validation, headers and tolerant reading |
| `conformance.rows@v1` | Table framing, encodings, explain, export and truncation |
| `conformance.items@v1` | Collection paging |
| `conformance.slow@v1` | Operations, polling, cancellation and deadlines |
| `conformance.evolving@v1` | Contract fingerprints, lenses and deprecation |
| `conformance.evolving@v2` | Two live versions |
| `conformance.retired@v1` | Retired-version refusal |
| `conformance.critical@v1` | Unsupported critical trait |
| `conformance.costly@v1` | Metering and confirmation |
| `conformance.dynamic@v1` | Dynamic query schema |
| `conformance.handoff@v1` | Handoff download and decoding |
| `conformance.thing@v1` | Resource ETags, masks, custom verb and idempotency |
| `conformance.file@v1` | Upload/download, checksums, commit and expired links |
| `conformance.change@v1` | Planned and approved action with receipt |
| `conformance.tick@v1` | Events, duplicates, older profiles and revoked access |

## Completing the handbook without building a second specification

For each capability, assign a named owner and complete the page contract above from one authored profile. Validate the examples against its request schema, verify the result grain and semantics against fixtures, and generate CLI/Python/Node examples from the same request. An owned business question should be understandable without opening source code or learning its backing tables.

Use the same page in two views: a review snapshot in this knowledge base, containing the full proposed inventory; and the served caller-specific guide, containing only granted interactions, permitted references, effective context and current limits. The static review inventory must never be mistaken for a caller's authorization-filtered working view.

A capability is ready when a human can answer “Is this the right question, what exactly will happen, what will I get, what might fail, and how do I call it?” and an agent can express that call unchanged in the CLI, Python and Node. Additional transports, compatibility machinery or generated wrappers should preserve that simple contract rather than become prerequisites for understanding the capability.
