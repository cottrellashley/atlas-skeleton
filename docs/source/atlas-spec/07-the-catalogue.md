# §7 The catalogue

`spec/catalogue.md` · proposed

> Part 7 of the Atlas spec. Every capability the Atlas Gateway offers, named under the naming rules, by domain: 138 capabilities in 27 domains, each with its description, what it returns, its properties and its id today. Proposed for review.

## Purpose

To name every capability Atlas offers, and to say in one place, for each, what it is, what it returns and what its profile declares, so that people, agents and the code agree on one list.

## Concepts

### The domains

| Domain | Kind | What it covers | Capabilities |
|---|---|---|---:|
| `atlas` | Built-in | Atlas itself | 20 |
| `portfolio` | Subject | Portfolios | 17 |
| `index` | Subject | Indices | 3 |
| `security` | Subject | Securities | 6 |
| `curve` | Subject | Curves | 1 |
| `signal` | Subject | Signals | 11 |
| `model-portfolio` | Subject | Model portfolios | 2 |
| `universe` | Subject | Universes | 2 |
| `securitized` | Subject | Securitized | 5 |
| `research` | Subject | Research | 4 |
| `product` | Subject | Products | 2 |
| `event` | Subject | Events | 3 |
| `data` | Subject | The platform's data | 3 |
| `globalrv` | Subject | Global RV | 3 |
| `kb` | Subject | Knowledge bases | 8 |
| `nautilus` | Subject | Nautilus | 5 |
| `bloomberg` | Provider | Bloomberg | 4 |
| `yieldbook` | Provider | Yield Book | 5 |
| `intex` | Provider | Intex | 8 |
| `dataquery` | Provider | J.P. Morgan DataQuery | 5 |
| `spglobal` | Provider | S&P Global (Capital IQ Xpressfeed) | 5 |
| `creditflow` | Provider | CreditFlow | 4 |
| `citi` | Provider | Citi Velocity | 3 |
| `snowflake` | Provider | Snowflake | 2 |
| `sharepoint` | Provider | SharePoint | 1 |
| `azure` | Provider | Azure (Resource Graph only) | 1 |
| `astronomer` | Provider | Astronomer | 5 |

138 capabilities in all, every one served by the Atlas Gateway. They come from: the 171 ids the gateway registers today under current names (its 91 deprecated aliases are left out: each resolves to the capability its current id maps to), merged where one subject capability now has several sources (†), and resources gathering several of today's verbs (`nautilus.*`, `kb.*`); Azure, through Resource Graph only, and Astronomer; and new ones: `atlas.access`, `atlas.compare`, `atlas.switch`, `atlas.catalog.changed`, the first event, `data.landed`, the entities events travel through, `event.topic`, `event.queue` and `event.subscription`, and the first action, `astronomer.deployment` (preview).

What a client does on the caller's own machine (reading files and git checkouts, GitHub with the person's own sign-in, the platform checkouts `qfi` reads) is not a capability and is not catalogued: it is a client extra, on its client's page in §5.

### How to read a domain's table

| Column | Holds |
|---|---|
| Capability | Its id and version: the concept |
| Surface | `data`, `resource`, `action` or `event` (the profile's family, §2.1) |
| Description | What it is for |
| Returns | What one answer holds: for a table, its grain and its main columns |
| Interactions | Its verbs, each read unless marked (`write`, `external`, `irreversible`) |
| Result · delivery | Its result shape and how it is delivered |
| Execution | `gateway` (the gateway answers), or `gateway · handoff` where the result may also be handed off (§2.2) |
| Stability | `official`, `community` or `upstream`; and `confidential` where it is |
| Today | The id or route it is served under today |

Each capability's full contract is its profile, served by the gateway; a generated snapshot of each profile, with its owner's words, is to sit beside these tables.

### Sources behind a subject

A subject capability may draw on several sources, chosen by a parameter (`source`, `history`, `engine`, `view`), marked †: `index.constituents` from Harbour, IDS, ICE, LSTA, Quantum or QuantumLeap, for example. ICE, Quantum, Quantum Leap and IDS therefore have no domain of their own: they are sources of subject capabilities.

### Events

`data.landed` is the first event. Every resource also emits its change events (`<id>.created`, `<id>.updated`, `<id>.deleted`, and one per custom verb in the past tense, such as `nautilus.dashboard.published`) from the gateway's state, and every operation emits `atlas.operation.<state>`. These are not capabilities of their own: they are part of their resource's profile (its `watch`), so they are listed there, not here.

### Actions

`astronomer.deployment@v1` (`plan`, `approve`, `restart`) is the first action, in preview: it is the spec's worked example (§2.4, §3.1) and the one exemplar of the action surface the build needs. It is served to `Gateway.Admin` holders only until its owner widens it.

### House rules (proposed)

One route per question (D38). Where the catalogue holds capabilities whose questions overlap, a house rule says which answers what. They are the first content of `atlas.context` and reach callers through `atlas.whoami` and catalogue pages (§2.6). Each is proposed for its domain owner's review (O28, O30).

| Rule | Text |
|---|---|
| curated-first | Never use `snowflake.query` for data a curated capability serves; use free SQL only for what no capability answers, and say so. |
| benchmark | A fund against its own benchmark, by bucket: `portfolio.benchmark-comparison`. A portfolio against another benchmark: `portfolio.breakdown`. Fund-level contributions at the latest snapshot, without buckets: `portfolio.analytics`. |
| positions | What named portfolios hold: `portfolio.holdings`. Who holds one security: `security.holders`. Positions matching a security filter across portfolios: `portfolio.exposure`. (Proposed merge: `security.holders` into `portfolio.exposure`, filtered by one security.) |
| security-data | Security reference data and analytics: `security.reference` and `security.analytics` first. `bloomberg.cache` for Bloomberg fields Harbour stores. `bloomberg.reference` and `bloomberg.history` only for fields neither holds (metered, with a daily quota). `yieldbook.*` only for analytics Harbour does not compute (scenarios, model prices). |
| portfolio-codes | Turn loose words into portfolio codes with `portfolio.search`; list portfolios with `portfolio.list`. |
| documents | Research, credit memos and offering documents: `research.documents`. A knowledge base the person names: `kb.search` and `kb.context`. |
| freshness | Whether a source's data for a date has landed: `data.status`. To be told when it lands: `data.landed`. |
| provider-search | CreditFlow new issues for the desk: `creditflow.deals`; their metadata alone: `creditflow.new-issues`. DataQuery from plain words: `dataquery.search`; by dataset: `dataquery.datasets`, then `dataquery.expressions`. Citi Velocity tags: `citi.tags` to browse, `citi.tag-details` to explain named tags. |
| confirm-cost | A metered call whose estimate passes your threshold needs the person's confirmation; ask them, never retry around it. |

## Rules

1. Every capability with a profile of its own, served by the gateway, is in exactly one domain's table, under one name. A client extra is never in it.
2. A new capability is added to its domain's table in the same change that adds it to the code.
3. One route per question. No two listed capabilities answer the same question. Where several sources hold the same thing, one capability takes them by a parameter (marked †), or one is authoritative and the others are not listed. Free SQL (`snowflake.query`) is never the route to data a capability serves, and the house rules say so (§2.6).

## Units

The domain pages, linked in the table above.

## Requirements

- **ATLAS-CATALOGUE-001.** The set of capabilities in these tables equals the set the gateway serves at the same commit; the gate fails otherwise.
- **ATLAS-CATALOGUE-002.** No two listed capabilities answer the same question (checked by review when a capability is added, and by the role journeys: ATLAS-VISION-003).

## Decisions

Decided (2026-10-28): only built-ins are `atlas.*`; Harbour's subjects are generic, never `harbour.*`; what the team does not own is named for its provider; no prefix for where a capability runs; a table per domain with each capability's description, what it returns and its properties.

Open:

- Every name in these tables: proposed for review.
- † Whether each merged capability's sources return one result schema. Recommended: keep a merged capability only where they do; otherwise each source's becomes its own capability (`index.constituents` from ICE as `ice.constituents`, in a provider domain `ice`).
- The owner of each domain marked “to name”.
- Whether Mondrian, Quantum and QuantumLeap are the team's own (subject domains) or the firm's (sources).

Recommended: sources, as above.

---

## Astronomer · `astronomer`

`spec/catalogue/astronomer.md` · proposed

> The astronomer domain: 5 capabilities (action, data), with each one's description, what it returns, its properties and its id today.

### Purpose

Astronomer's deployments, DAGs, runs and logs, read by the gateway with a workload identity per environment, the caller recorded in the audit; what each caller may see is Atlas's grant.

| Property | Value |
|---|---|
| Kind of domain | Provider |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `astronomer.deployments@v1` | data | Deployments in the platform's Astronomer workspaces. | Per deployment: kind, runtime, health, Airflow link | fetch | table · single | gateway | upstream | new |
| `astronomer.dags@v1` | data | DAGs in one deployment. | Per DAG: schedule, state, last run | fetch | table · single | gateway | upstream | new |
| `astronomer.runs@v1` | data | DAG runs in one deployment. | Per run: state, failed tasks | fetch | table · single | gateway | upstream | new |
| `astronomer.log@v1` | data | One task try's log. | The log text | fetch | content · chunked | gateway | upstream | new |
| `astronomer.deployment@v1` | action | Restart a deployment's scheduler and workers, planned first, approved where its policy says. | A plan (what restarting will do); a receipt (when, which restart) | plan · approve (write) · restart (external) | record · single; operation for restart | gateway | official (preview) | new |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-ASTRONOMER-001.** Every capability in this table is served with a profile whose properties match it.

---

## Atlas itself · `atlas`

`spec/catalogue/atlas.md` · proposed

> The atlas domain: 20 capabilities (data, event, resource), with each one's description, what it returns, its properties and its id today.

### Purpose

Discovery, access, operations and settings: the only capabilities a client knows without reading a profile (§2.6). They change only with the protocol.

| Property | Value |
|---|---|
| Kind of domain | Built-in |
| Owner | Atlas maintainers |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `atlas.whoami@v1` | data | Who the caller is to the gateway. | Identity, roles, environment, and what is available by domain | fetch | record · single | gateway | official | `/v1/me`; `atlas whoami` |
| `atlas.catalog@v1` | data | The working view: what the caller can use here, now. | One row per available capability: id, version, summary, family, interactions the caller may use, fingerprint | fetch | collection · single | gateway | official | `/v1/catalog` |
| `atlas.catalog.changed@v1` | event | The caller's working view may have changed. | Payload: the new catalogue ETag | subscribe | record · subscription | gateway | official | new |
| `atlas.search@v1` | data | Available capabilities ranked against words. | As `atlas.catalog`, ranked, with why each matched | fetch | collection · single | gateway | official | `atlas search` |
| `atlas.describe@v1` | data | One available capability's full profile. | The profile, plus the caller's own limits and use for it | fetch | record · single | gateway | official | `atlas describe` |
| `atlas.guide@v1` | data | How to use one available capability, written for an agent. | A guide in Markdown: when to use it, parameters, columns, examples in the caller's client, problems and next steps, limits | fetch | record · single | gateway | official | new |
| `atlas.access@v1` | data | The access view: every listed capability with the caller's status. | Per capability: status, reason, how to obtain it, the caller's limits and use | fetch | collection · single | gateway | official | new |
| `atlas.usage@v1` | data | What the caller has used, and what is left. | Per capability and limit: used, remaining, period, the pool it draws on | fetch | collection · single | gateway | official | `/v1/me/usage`; `atlas usage` |
| `atlas.setup@v1` | data | How to set this machine and harness up for Atlas. | The setup plan: environment, sign-in, skill, verification steps | fetch | record · single | gateway | official | `/v1/setup`; `atlas setup` |
| `atlas.clients@v1` | data | The client catalogue. | Per client: the language and roles it is built for, its extras, version, how to install | fetch | collection · single | gateway | official | `/v1/clients` |
| `atlas.compare@v1` | data | Two versions of one data capability, side by side. | The two results joined on their keys, with the lens applied | fetch | table · chunked | gateway | official | new |
| `atlas.operation@v1` | resource | The operations the caller started. | One operation: address, state, progress, result or problem | get · list · cancel (write) · watch | record · single; collection for list; subscription for watch | gateway | official | jobs, runs, publications, permits |
| `atlas.settings@v1` | resource | The caller's own settings. | The caller's settings document | get · update (write) | record · single | gateway | official | `/v1/me/config` |
| `atlas.directory@v1` | data | People and groups, to share with. | Per match: principal, name, kind (person, group) | fetch | table · single | gateway | official | `/v1/directory` |
| `atlas.grant@v1` | resource | Individual grants and denies (`Gateway.Admin`). | One grant: subject, address or family, allow or deny, set by | get · list · create (write) · delete (write) | record · single | gateway | official, confidential | `/v1/admin`; `atlas admin access` |
| `atlas.role@v1` | resource | Roles: named sets of grants with limits (`Gateway.Admin`). | One role: members, grants, limits | get · list · update (write) | record · single | gateway | official, confidential | `/v1/admin` |
| `atlas.limit@v1` | resource | Allowances, pools and ceilings (`Gateway.Admin`). | One limit: subject or pool, interaction, amount, period, set by | get · list · update (write) | record · single | gateway | official, confidential | `/v1/admin`; `atlas admin limits` |
| `atlas.context@v1` | resource | Managed context: skills and system prompts every client receives (`Gateway.Admin`). | One scope: its published revision and drafts | get · list · update (write) · publish (write) · rollback (write) | record · single | gateway | official, confidential | `atlas admin context` |
| `atlas.switch@v1` | resource | Kill switches: turn a capability, domain, service or gateway behaviour off without a deploy (`Gateway.Admin`). | One switch: target, state, reason, until | get · list · update (write) | record · single | gateway | official, confidential | new |
| `atlas.audit@v1` | data | The audit trail, a day at a time (`Gateway.Admin`). | One row per call: address, caller, decision, profiles, usage, outcome | fetch | table · single | gateway | official, confidential | `/v1/admin audit` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-ATLAS-001.** Every capability in this table is served with a profile whose properties match it.

---

## Azure · `azure`

`spec/catalogue/azure.md` · proposed

> The azure domain: 1 capability (data), with each one's description, what it returns, its properties and its id today.

### Purpose

Azure's own inventory, through Azure Resource Graph only: any resource the caller may read, queried in KQL; now as the gateway's own identity, granted by name only; in the target as the caller (On-Behalf-Of), so a manager or an engineer sees whatever their own Azure roles allow (§6.3). Read-only by construction.

| Property | Value |
|---|---|
| Kind of domain | Provider |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `azure.resource-graph@v1` | data | A KQL query over every Azure resource the caller may read, across the subscriptions or management groups named. | The query's rows; its columns are the query's (a dynamic schema) | fetch | table · chunked | gateway | upstream | new |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-AZURE-001.** Every capability in this table is served with a profile whose properties match it.

---

## Bloomberg · `bloomberg`

`spec/catalogue/bloomberg.md` · proposed

> The bloomberg domain: 4 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

Bloomberg's data through its Server API, and the Bloomberg analytics Harbour stores. Metered and licensed.

| Property | Value |
|---|---|
| Kind of domain | Provider |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `bloomberg.reference@v1` | data | Current or point-in-time values for securities and fields (Excel's BDP). | Per security and field: value | fetch | table · single | gateway | upstream | `sapi.reference` |
| `bloomberg.history@v1` | data | A time series per security and field between two dates (Excel's BDH). | Per security, field and date: value | fetch | table · single | gateway | upstream | `sapi.historical` |
| `bloomberg.bulk@v1` | data | Field sets per security (Excel's BDS). | Per security, field and record: values | fetch | table · single | gateway | upstream | `sapi.bulk` |
| `bloomberg.cache@v1` | data | The Bloomberg analytics Harbour stores each business day. | Per security and date: the stored fields | fetch · explain · export | table · chunked | gateway · handoff | official | `harbour.bloomberg-cache` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-BLOOMBERG-001.** Every capability in this table is served with a profile whose properties match it.

---

## Citi Velocity · `citi`

`spec/catalogue/citi.md` · proposed

> The citi domain: 3 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

Citi Velocity time series.

| Property | Value |
|---|---|
| Kind of domain | Provider |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `citi.series@v1` | data | Historical time series by tag. | Per tag and date: value | fetch | table · single | gateway | upstream | `citi.series` |
| `citi.tags@v1` | data | Tags under a prefix, with what each means. | Per tag: meaning, dates covered | fetch | table · single | gateway | upstream | `citi.tags` |
| `citi.tag-details@v1` | data | What each tag means, without its history. | Per tag: known, meaning, dates | fetch | table · single | gateway | upstream | `citi.describe` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-CITI-001.** Every capability in this table is served with a profile whose properties match it.

---

## CreditFlow · `creditflow`

`spec/catalogue/creditflow.md` · proposed

> The creditflow domain: 4 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

CreditFlow primary-market data.

| Property | Value |
|---|---|
| Kind of domain | Provider |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `creditflow.deals@v1` | data | New issues as the desk asks for them. | Per deal and tranche: terms, timing, status | fetch | table · single | gateway | upstream | `creditflow.deals` |
| `creditflow.new-issues@v1` | data | New-issue metadata. | Per deal and tranche: terms, timing | fetch | table · single | gateway | upstream | `creditflow.new-issues` |
| `creditflow.issuers@v1` | data | The CreditFlow issuer master. | Per issuer: id, ticker, name, country, industry | fetch | table · single | gateway | upstream | `creditflow.issuers` |
| `creditflow.sectors@v1` | data | The CreditFlow classification tree. | Per sector and subsector: id, name | fetch | table · single | gateway | upstream | `creditflow.sectors` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-CREDITFLOW-001.** Every capability in this table is served with a profile whose properties match it.

---

## Curves · `curve`

`spec/catalogue/curve.md` · proposed

> The curve domain: 1 capability (data), with each one's description, what it returns, its properties and its id today.

### Purpose

Yield and spread curves.

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `curve.government-zero@v1` | data | Government zero curves. | Per country, currency, tenor and date: zero rate | fetch · explain · export | table · single | gateway · handoff | official | `rates.govt-zero-curves` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-CURVE-001.** Every capability in this table is served with a profile whose properties match it.

---

## The platform's data · `data`

`spec/catalogue/data.md` · proposed

> The data domain: 3 capabilities (data, event), with each one's description, what it returns, its properties and its id today.

### Purpose

What the curated data behind capabilities is, and how fresh it is.

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `data.sources@v1` | data | Every curated source behind a capability. | Per source: title, service, dates, the capabilities that read it | fetch | table · single | gateway | official | `snowflake.sources` |
| `data.status@v1` | data | Each dated source's latest date, live. | Per source: latest date, expected date, status | fetch | table · single | gateway | official | `snowflake.freshness` |
| `data.landed@v1` | event | A source's data for a date is complete. | Payload: source, as of, rows | subscribe · publish (external) | record · subscription | gateway | official | new |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-DATA-001.** Every capability in this table is served with a profile whose properties match it.

---

## J.P. Morgan DataQuery · `dataquery`

`spec/catalogue/dataquery.md` · proposed

> The dataquery domain: 5 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

J.P. Morgan DataQuery time series.

| Property | Value |
|---|---|
| Kind of domain | Provider |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `dataquery.search@v1` | data | Series from plain words. | Candidate datasets and series, each with its expression | fetch | table · single | gateway | upstream | `jpmdq.search` |
| `dataquery.datasets@v1` | data | Datasets by keyword. | Per dataset: id, name, description, provider | fetch | table · single | gateway | upstream | `jpmdq.datasets` |
| `dataquery.expressions@v1` | data | A dataset's series with their exact expressions. | Per series: instrument, attribute, expression | fetch | table · single | gateway | upstream | `jpmdq.expressions` |
| `dataquery.expression-details@v1` | data | What expressions mean, without their history. | Per expression: known, meaning | fetch | table · single | gateway | upstream | `jpmdq.describe` |
| `dataquery.time-series@v1` | data | Historical time series for expressions. | Per expression and date: value | fetch | table · single | gateway | upstream | `jpmdq.time-series` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-DATAQUERY-001.** Every capability in this table is served with a profile whose properties match it.

---

## Events · `event`

`spec/catalogue/event.md` · proposed

> The event domain: 3 capabilities (resource), with each one's description, what it returns, its properties and its id today.

### Purpose

The entities events travel through: topics, queues and subscriptions, created by the gateway on Service Bus for those granted them; publishing and listening are brokered (§2.5).

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | Atlas maintainers |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `event.topic@v1` | resource | Topics: one per event type or family; creating one registers its owner as publisher. | One topic: name, event types, owner, retention | get · list · create (write) · delete (irreversible) | record · single | gateway | official | new |
| `event.queue@v1` | resource | Queues: point-to-point work for one consumer. | One queue: name, owner, retention | get · list · create (write) · delete (irreversible) | record · single | gateway | official | new |
| `event.subscription@v1` | resource | A durable listener on a topic, with its filter, owned by a person or a workload. | One subscription: topic, filter, owner | get · list · create (write) · delete (write) | record · single | gateway | official | new |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-EVENT-001.** Every capability in this table is served with a profile whose properties match it.

---

## Global RV · `globalrv`

`spec/catalogue/globalrv.md` · proposed

> The globalrv domain: 3 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

What the Global RV application reads that is particular to it. Its curves and series are `signal.fvcm.curves` and `universe.group-analytics`.

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `globalrv.navigation@v1` | data | The application's navigation tree. | Per node: name, path, source, children | fetch · explain · export | table · single | gateway · handoff | official | `globalrv.nav-tree` |
| `globalrv.navigation-search@v1` | data | Type-ahead over the navigation tree. | Per match: name, path | fetch · explain · export | table · single | gateway · handoff | official | `globalrv.nav-search` |
| `globalrv.saved-queries@v1` | data | Users' saved queries and folders. | Per folder or query: name, rows | fetch · explain · export | table · single | gateway · handoff | official | `globalrv.saved-queries` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-GLOBALRV-001.** Every capability in this table is served with a profile whose properties match it.

---

## Indices · `index`

`spec/catalogue/index.md` · proposed

> The index domain: 3 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

Indices and their constituents and analytics, from Harbour's index model, with other sources by `source`.

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `index.list@v1` | data | The index catalogue. `source`: `harbour` (default), `ice`, `quantumleap`.† | Per index: vendor, code, name, coverage dates | fetch · explain · export | table · single | gateway · handoff | official | `index.list`, `ice.indices`, `quantumleap.entities` |
| `index.constituents@v1` | data | Constituents of indices on a date or across a range. `source`: `harbour` (default), `ids`, `ice`, `lsta`, `quantum`, `quantumleap`.† | Grain index × security × date: weight, price, analytics | fetch · explain · export | table · chunked | gateway · handoff | official | `index.constituents`, `benchmark.holdings`, `ice.constituents`, `loans.constituents`, `quantum.constituents`, `quantumleap.constituents` |
| `index.analytics@v1` | data | Index-level analytics per date. `source`: `harbour` (default), `ice`, `lsta`.† | Per index and date: OAD, OAS, OASD, DTS, yields, price, returns | fetch · explain · export | table · single | gateway · handoff | official | `index.analytics`, `ice.index`, `loans.index` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2). † A capability that merges several of today's capabilities by a parameter: whether each value returns the same result schema is to be confirmed (§7).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-INDEX-001.** Every capability in this table is served with a profile whose properties match it.

---

## Intex · `intex`

`spec/catalogue/intex.md` · proposed

> The intex domain: 8 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

Intex structured-finance data and analytics. Licensed.

| Property | Value |
|---|---|
| Kind of domain | Provider |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `intex.data@v1` | data | Static and reported data for structured-finance positions. | Per position and field: value | fetch | table · single | gateway | upstream | `intex.data` |
| `intex.fields@v1` | data | The data fields `intex.data` accepts. | Per field: keyword, description, type | fetch | table · single | gateway | upstream | `intex.fields` |
| `intex.cashflows@v1` | data | Projected cashflow vectors for one deal and tranche under a scenario. | Per period: the vectors asked for | fetch | table · operation | gateway | upstream | `intex.cashflow` |
| `intex.cashflow-vectors@v1` | data | The vectors `intex.cashflows` can project. | Per vector: name, description | fetch | table · single | gateway | upstream | `intex.cashflow-vectors` |
| `intex.price-yield@v1` | data | Price/yield analytics for one deal and tranche under a scenario. | The solved fields | fetch | table · operation | gateway | upstream | `intex.priceyield` |
| `intex.price-yield-fields@v1` | data | The fields `intex.price-yield` can solve for. | Per field: name, description | fetch | table · single | gateway | upstream | `intex.priceyield-fields` |
| `intex.curves@v1` | data | Discount curves a price/yield run can use. | Per curve: name | fetch | table · single | gateway | upstream | `intex.curves` |
| `intex.validation@v1` | data | Whether Intex recognises identifiers, and whether the licence covers them. | Per identifier: recognised, licensed | fetch | table · single | gateway | upstream | `intex.validate` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-INTEX-001.** Every capability in this table is served with a profile whose properties match it.

---

## Knowledge bases · `kb`

`spec/catalogue/kb.md` · proposed

> The kb domain: 8 capabilities (data, resource), with each one's description, what it returns, its properties and its id today.

### Purpose

Knowledge bases (OKF bundles) people publish and share, and their memory of facts.

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `kb.base@v1` | resource | One knowledge base: its bundle, sharing and validity. | A base: id, title, version, pages, owner, visibility, validity | get · list · publish (write) · share (write) · delete (irreversible) · download · validate · batch (write) | record · single | gateway | official | `kb.get`, `kb.list`, `kb.publish`, `kb.share`, `kb.delete`, `kb.download`, `kb.validate`, `kb.batch` |
| `kb.page@v1` | resource | A page of a base. | Its Markdown, metadata and revision | get · create (write) · update (write) · outline | record · single | gateway | official | `kb.read`, `kb.page.write`, `kb.outline` |
| `kb.concept@v1` | resource | A concept in a base's memory. | Id, title, summary, aliases, pages | get · list · create (write) · history | record · single | gateway | official | `kb.concept`, `kb.concepts`, `kb.concept.create`, `kb.history` |
| `kb.fact@v1` | resource | An assertion in a base's memory, with its temporal status. | The assertion, its revision, evidence, validity | get · list · create (write) · revise (write) | record · single | gateway | official | `kb.fact`, `kb.facts`, `kb.remember`, `kb.revise` |
| `kb.source@v1` | resource | A source a base's facts cite. | Id, title, type | create (write) | record · single | gateway | official | `kb.source.add` |
| `kb.search@v1` | data | Lexical search in a base. | Per match: page, section, text | fetch | table · single | gateway | official | `kb.search` |
| `kb.context@v1` | data | The pages and assertions that match, packed for an agent. | Matched pages and assertions within a byte budget | fetch | record · single | gateway | official | `kb.context` |
| `kb.reference@v1` | data | One `kb://` or `kb-memory://` reference, resolved. | The page section or assertion it names | fetch | record · single | gateway | official | `kb.ref` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-KB-001.** Every capability in this table is served with a profile whose properties match it.

---

## Model portfolios · `model-portfolio`

`spec/catalogue/model-portfolio.md` · proposed

> The model-portfolio domain: 2 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

The quant team's model (paper) portfolios.

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `model-portfolio.list@v1` | data | Model portfolios. | Per portfolio: name, type, manager, benchmark, currency, status | fetch · explain · export | table · single | gateway · handoff | official | `signal.paper-portfolios` |
| `model-portfolio.positions@v1` | data | Positions of model portfolios. | Per portfolio, security and date: portfolio and optimiser weights | fetch · explain · export | table · chunked | gateway · handoff | official | `signal.paper-positions` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-MODEL-PORTFOLIO-001.** Every capability in this table is served with a profile whose properties match it.

---

## Nautilus · `nautilus`

`spec/catalogue/nautilus.md` · proposed

> The nautilus domain: 5 capabilities (resource), with each one's description, what it returns, its properties and its id today.

### Purpose

The resources Nautilus keeps for a person: dashboards, scheduled jobs, skills and their workspace. Their contracts are the gateway's; Nautilus V3 is built on them.

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | Nautilus maintainers |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `nautilus.dashboard@v1` | resource | A published dashboard. | Id, path, title, owner, visibility, version; its files as a ZIP | get · list · publish (write) · share (write) · restore (write) · delete (irreversible) · watch | record · single; content for its files | gateway | official | `nautilus.dashboards.get`, `.list`, `.publish`, `.share`, `.restore`, `.delete` |
| `nautilus.dashboard-version@v1` | resource | A dashboard's stored versions. | Per version: number, when, by whom, note, files, whether live | list | collection · single | gateway | official | `nautilus.dashboards.versions` |
| `nautilus.job@v1` | resource | A job the person's agent runs on a schedule. | Definition, schedule, state, latest runs; a run's log | get · list · create (write) · update (write) · delete (irreversible) · run (write) · log | record · single; operation for run | gateway | official | `nautilus.jobs.get`, `.list`, `.create`, `.enable`, `.delete`, `.run`, `.log` |
| `nautilus.skill@v1` | resource | A skill in the person's library or shared one. | Files, content, versions, visibility, installs, usage | get · list · save (write) · delete (irreversible) · install (write) · uninstall (write) · publish (write) · unpublish (write) · share (write) · curate (write) · download · usage | record · single; content for download | gateway | official | `nautilus.skills.*` (12) |
| `nautilus.workspace-file@v1` | resource | A file or folder in the person's Workspace. | Path, name, kind, size, modified; a file's bytes | get · list · create (write) · update (write) · move (write) · delete (irreversible) · search | record · single; content for get | gateway | official | `nautilus.workspace.*` (8) |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-NAUTILUS-001.** Every capability in this table is served with a profile whose properties match it.

---

## Portfolios · `portfolio`

`spec/catalogue/portfolio.md` · proposed

> The portfolio domain: 17 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

The firm's portfolios: their master data, holdings, trades, returns, attribution and comparison with benchmarks. Sourced from Harbour and the firm's portfolio data (IDS, performance).

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `portfolio.list@v1` | data | Current portfolios. | Per portfolio: code, name, region, asset class | fetch · explain · export | table · single | gateway · handoff | official | `portfolio.list` |
| `portfolio.search@v1` | data | Loose text resolved to portfolios, ranked. | Per match: code, name, why it matched | fetch | table · single | gateway | official | `portfolio.search` |
| `portfolio.reference@v1` | data | The portfolio master row for each portfolio. `source`: `ids` (default) or `harbour`.† | Per portfolio: base currency, type, country, manager, fund accounting and master's other fields | fetch · explain · export | table · single | gateway · handoff | official | `portfolio.details`, `portfolio.reference` |
| `portfolio.reference-values@v1` | data | The distinct current values of one master field, to build filters that match. | Per value: the value and how many portfolios have it | fetch · explain · export | table · single | gateway · handoff | official | `portfolio.field-values` |
| `portfolio.managers@v1` | data | Current manager assignments. | Per assignment: portfolio code, name, manager | fetch · explain · export | table · single | gateway · handoff | official | `portfolio.managers` |
| `portfolio.holdings@v1` | data | Positions in portfolios on a date; `history` for every end-of-day snapshot in a range.† | Grain portfolio × security × date: market value, weight, analytics | fetch · explain · export | table · chunked | gateway · handoff | official | `portfolio.holdings`, `portfolio.holdings-history` |
| `portfolio.holdings-dates@v1` | data | Valuation dates in a window. | Per portfolio and date: position count, base-currency market value | fetch · explain · export | table · single | gateway · handoff | official | `portfolio.holdings-dates` |
| `portfolio.analytics@v1` | data | Fund-level contributions at latest holdings snapshot. | Per fund (and group): contributions to OAD, OASD, OAC, DTS, yield, and their benchmark's | fetch · explain · export | table · single | gateway · handoff | official | `portfolio.analytics` |
| `portfolio.breakdown@v1` | data | A portfolio against any benchmark: over- and underweights. | Per bucket (sector, rating, maturity, duration): portfolio, benchmark, active | fetch · explain · export | table · single | gateway · handoff | official | `portfolio.breakdown` |
| `portfolio.benchmark-comparison@v1` | data | Each fund beside its own benchmark. | Per fund and bucket: fund, benchmark, active weight and contributions | fetch · explain · export | table · single | gateway · handoff | official | `portfolio.benchmark-comparison` |
| `portfolio.benchmarks@v1` | data | Benchmarks the firm holds constituents for. | Per benchmark: code, name, latest date, constituent count | fetch · explain · export | table · single | gateway · handoff | official | `benchmark.list` |
| `portfolio.exposure@v1` | data | Positions matching a security filter, across portfolios. | Per position: portfolio, security, market value, weight | fetch · explain · export | table · single | gateway · handoff | official | `portfolio.exposure` |
| `portfolio.trades@v1` | data | Executed trades, newest first; `history` for every trade in a range.† | Per trade: portfolio, security, side, quantity, price, dates | fetch · explain · export | table · chunked | gateway · handoff | official | `portfolio.trades`, `portfolio.trades-history` |
| `portfolio.returns@v1` | data | Investment, benchmark and relative returns. | Per portfolio and valuation date: returns over each period | fetch · explain · export | table · single | gateway · handoff | official | `portfolio.returns` |
| `portfolio.attribution@v1` | data | Fixed income attribution: outperformance by effect. | Per portfolio, benchmark and date: allocation, selection, curve and spread effects | fetch · explain · export | table · single | gateway · handoff | official | `attribution.summary` |
| `portfolio.attribution-issuers@v1` | data | Outperformance per issuer. | Per issuer: allocation and selection | fetch · explain · export | table · single | gateway · handoff | official | `attribution.issuers` |
| `portfolio.attribution-securities@v1` | data | Outperformance per security. | Per security: spread change, carry, OASD, contributions, selection | fetch · explain · export | table · single | gateway · handoff | official | `attribution.securities` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2). † A capability that merges several of today's capabilities by a parameter: whether each value returns the same result schema is to be confirmed (§7).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-PORTFOLIO-001.** Every capability in this table is served with a profile whose properties match it.

---

## Products · `product`

`spec/catalogue/product.md` · proposed

> The product domain: 2 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

The firm's fund products.

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `product.aum@v1` | data | Official assets under management. | Per product (and group) and month: AUM and flows | fetch · explain · export | table · single | gateway · handoff | official | `product.aum` |
| `product.share-classes@v1` | data | Open share classes of funds. | Per share class: codes, ISIN, currency, status | fetch · explain · export | table · single | gateway · handoff | official | `product.share-classes` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-PRODUCT-001.** Every capability in this table is served with a profile whose properties match it.

---

## Research · `research`

`spec/catalogue/research.md` · proposed

> The research domain: 4 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

The fixed income research team's coverage and documents.

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `research.issuers@v1` | data | Issuer metadata and whether research covers them. | Per issuer ticker: issuer, sector, industry, country, coverage | fetch · explain · export | table · single | gateway · handoff | official | `research.tickers` |
| `research.coverage@v1` | data | Research coverage per issuer. | Per issuer: analyst, recommendation and date, risk and ESG views | fetch · explain · export | table · single | gateway · handoff | official | `research.coverage` |
| `research.recommendations@v1` | data | Every recorded change of recommendation and ESG rating. | Per issuer and change: rating, date | fetch · explain · export | table · single | gateway · handoff | official | `research.analyst-recs` |
| `research.documents@v1` | data | Semantic search over research, credit memos and offering documents. | Per passage: document, score, text | fetch | table · single | gateway | official | `knowledge.search` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-RESEARCH-001.** Every capability in this table is served with a profile whose properties match it.

---

## Securitized · `securitized`

`spec/catalogue/securitized.md` · proposed

> The securitized domain: 5 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

Securitized holdings and analytics (from Mondrian and Quantum).

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `securitized.holdings@v1` | data | Security-level securitized holdings. `engine`: `mondrian` (default) or `quantum`.† | Per portfolio, security, date: classification and analytics | fetch · explain · export | table · chunked | gateway · handoff | official | `mondrian.holdings`, `mondrian.quantum-holdings` |
| `securitized.contributions@v1` | data | Portfolio-level contributions. | Per portfolio, group, date: OAD, OASD, OAS, DTS contributions | fetch · explain · export | table · single | gateway · handoff | official | `mondrian.aggregates` |
| `securitized.classifications@v1` | data | Securitized classifications of securities. | Per security: class levels 1 to 4 | fetch · explain · export | table · single | gateway · handoff | official | `mondrian.classifications` |
| `securitized.clo-managers@v1` | data | CLO positions with their collateral managers. | Per CLO position: manager, rating, benchmark, deal status | fetch · explain · export | table · single | gateway · handoff | official | `mondrian.clo-platform` |
| `securitized.empirical-durations@v1` | data | Empirical durations from rolling regressions on Treasury yields. | Per portfolio, tenor, date: empirical duration | fetch · explain · export | table · single | gateway · handoff | official | `mondrian.empirical-durations` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2). † A capability that merges several of today's capabilities by a parameter: whether each value returns the same result schema is to be confirmed (§7).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-SECURITIZED-001.** Every capability in this table is served with a profile whose properties match it.

---

## Securities · `security`

`spec/catalogue/security.md` · proposed

> The security domain: 6 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

Individual securities: reference data, analytics, returns, who holds them, and the research universe.

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `security.reference@v1` | data | Reference data by ISIN, CUSIP, SEDOL or ticker. `source`: `ids` (default) or `harbour`.† | Per security: name, issuer hierarchy and LEI, type, coupon, maturity, classification | fetch · explain · export | table · single | gateway · handoff | official | `security.reference`, `index.security-descriptive` |
| `security.analytics@v1` | data | Analytics per security, index and date. | Price, yields, OAD, OAS, OASD, DTS, Z-spread | fetch · explain · export | table · chunked | gateway · handoff | official | `index.security-analytics` |
| `security.returns@v1` | data | Returns per security, index and date. | Total, price, excess, coupon, currency and paydown returns | fetch · explain · export | table · chunked | gateway · handoff | official | `index.security-returns` |
| `security.holders@v1` | data | Positions in one security across portfolios; `history` for every snapshot.† | Per portfolio: position, market value, weight, date | fetch · explain · export | table · single | gateway · handoff | official | `security.holders`, `security.holders-history` |
| `security.universe@v1` | data | Securities matching a research filter, on a date or across a range. | Grain security × date: descriptive and analytic fields | fetch · explain · export | table · chunked | gateway · handoff | official | `security.universe` |
| `security.universe-aggregates@v1` | data | Unweighted aggregates over the research universe, grouped by any field. | Per group: count and averages | fetch · explain · export | table · single | gateway · handoff | official | `security.universe-aggregates` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2). † A capability that merges several of today's capabilities by a parameter: whether each value returns the same result schema is to be confirmed (§7).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-SECURITY-001.** Every capability in this table is served with a profile whose properties match it.

---

## SharePoint · `sharepoint`

`spec/catalogue/sharepoint.md` · proposed

> The sharepoint domain: 1 capability (resource), with each one's description, what it returns, its properties and its id today.

### Purpose

The fixed income SharePoint folders.

| Property | Value |
|---|---|
| Kind of domain | Provider |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `sharepoint.file@v1` | resource | A file in the fixed income SharePoint folders. | Path, name, size, modified; its bytes | get · list | record · single; content for get | gateway | upstream | `sharepoint.get`, `sharepoint.list` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-SHAREPOINT-001.** Every capability in this table is served with a profile whose properties match it.

---

## Signals · `signal`

`spec/catalogue/signal.md` · proposed

> The signal domain: 11 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

The quant team's models' signals.

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `signal.composite-score@v1` | data | Composite score with its FVCM, EMC and DRSK components. | Per bond and date: percentiles, scores, z-scores, identifiers | fetch · explain · export | table · chunked | gateway · handoff | official | `signal.composite-scores` |
| `signal.composite-score.trades@v1` | data | The composite score for a portfolio's or an index's constituents. | Per constituent: composite score and component percentiles | fetch · explain · export | table · single | gateway · handoff | official | `signal.trades` |
| `signal.drsk@v1` | data | DRSK residuals: equity-implied default risk against spread. | Per bond and date: distance to default, predicted OAS, residual, percentile | fetch · explain · export | table · chunked | gateway · handoff | official | `signal.drsk` |
| `signal.emc@v1` | data | EMC momentum signals. | Per bond and date: 1- and 6-month and blended momentum, percentiles | fetch · explain · export | table · chunked | gateway · handoff | official | `signal.emc` |
| `signal.fvcm.curves@v1` | data | FVCM curves. `view`: `curves` (default), `globalrv` (one model's curve on a date), `latest-date`.† | Per model, group and date: Nelson-Siegel parameters, bonds used, errors | fetch · explain · export | table · single | gateway · handoff | official | `signal.fvcm-curves`, `globalrv.curve`, `globalrv.fvcm-latest-date` |
| `signal.fvcm.models@v1` | data | FVCM models on a date. | Per model: its mathematical model and bond count | fetch · explain · export | table · single | gateway · handoff | official | `signal.fvcm-models` |
| `signal.fvcm.residuals@v1` | data | FVCM fitted values and residuals. | Per bond, model and date: fitted value, residual (Bp; positive is cheap) | fetch · explain · export | table · chunked | gateway · handoff | official | `signal.fvcm-residuals` |
| `signal.fvcm.universe@v1` | data | The FVCM bond universe. | Per bond: identifiers, issuer, currency, universe, ratings, industry, coupon, maturity | fetch · explain · export | table · chunked | gateway · handoff | official | `signal.fvcm-universe` |
| `signal.macro-credit@v1` | data | The macro credit model's daily signal. | Per market and date: signal, smoothed and binary signals, scores | fetch · explain · export | table · single | gateway · handoff | official | `signal.macro-credit` |
| `signal.rates-fair-value@v1` | data | The rates fair-value model's daily signal. | Per market and date: signal, RSI z-score, yield changes | fetch · explain · export | table · single | gateway · handoff | official | `signal.rates-fair-value` |
| `signal.asset-allocation@v1` | data | The asset allocation model's states. | Per asset and date: excess return, spread, state and forward probabilities | fetch · explain · export | table · single | gateway · handoff | official | `signal.asset-allocation` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2). † A capability that merges several of today's capabilities by a parameter: whether each value returns the same result schema is to be confirmed (§7).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-SIGNAL-001.** Every capability in this table is served with a profile whose properties match it.

---

## Snowflake · `snowflake`

`spec/catalogue/snowflake.md` · proposed

> The snowflake domain: 2 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

Snowflake itself: free-form read-only SQL and the schemas it reads. Curated data is never named `snowflake.*`; it is in the subject domains.

| Property | Value |
|---|---|
| Kind of domain | Provider |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `snowflake.query@v1` | data | Your own read-only SQL over the registered schemas. | The query's rows | fetch · export | table · chunked | gateway · handoff | upstream (named grant) | `snowflake.query`, `snowflake.session` (`sql`) |
| `snowflake.schema@v1` | data | The objects `snowflake.query` may read. | Per object: schema, name, kind, columns | fetch | table · single | gateway | upstream | `snowflake.schema` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-SNOWFLAKE-001.** Every capability in this table is served with a profile whose properties match it.

---

## S&P Global (Capital IQ Xpressfeed) · `spglobal`

`spec/catalogue/spglobal.md` · proposed

> The spglobal domain: 5 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

S&P Capital IQ company data and transcripts, from Xpressfeed in Snowflake. Its leveraged loan indices are `index.*` with `source` `lsta`.

| Property | Value |
|---|---|
| Kind of domain | Provider |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `spglobal.companies@v1` | data | Companies by ticker or name. | Per company: Capital IQ id, name, industry, country | fetch · explain · export | table · single | gateway · handoff | upstream | `xpressfeed.companies` |
| `spglobal.capital-structure@v1` | data | One company's debt components. | Per component: type, seniority, secured, maturity, amount | fetch · explain · export | table · single | gateway · handoff | upstream | `xpressfeed.capital-structure` |
| `spglobal.financials@v1` | data | One company's financial data items. | Per period and item: value | fetch · explain · export | table · single | gateway · handoff | upstream | `xpressfeed.financials` |
| `spglobal.transcripts@v1` | data | One company's earnings call transcripts. | Per call: id, date, event type, version | fetch · explain · export | table · single | gateway · handoff | upstream | `xpressfeed.transcripts` |
| `spglobal.transcript@v1` | data | The text of one transcript. | Per component, in spoken order: speaker, text | fetch · explain · export | table · single | gateway · handoff | upstream | `xpressfeed.transcript` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-SPGLOBAL-001.** Every capability in this table is served with a profile whose properties match it.

---

## Universes · `universe`

`spec/catalogue/universe.md` · proposed

> The universe domain: 2 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

Groupings of the investable universe and their performance.

| Property | Value |
|---|---|
| Kind of domain | Subject |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `universe.group-performance@v1` | data | Group performance for Global HY and Global Agg corporates. | Per group and period: level and change | fetch · explain · export | table · single | gateway · handoff | official | `globalrv.group-performance` |
| `universe.group-analytics@v1` | data | Grouped analytics. `view`: `quantum` (default), `globalrv` (one series).† | Per entity, grouping and series: analytics over time | fetch · explain · export | table · chunked | gateway · handoff | official | `quantum.grouped-analytics`, `globalrv.series` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2). † A capability that merges several of today's capabilities by a parameter: whether each value returns the same result schema is to be confirmed (§7).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-UNIVERSE-001.** Every capability in this table is served with a profile whose properties match it.

---

## Yield Book · `yieldbook`

`spec/catalogue/yieldbook.md` · proposed

> The yieldbook domain: 5 capabilities (data), with each one's description, what it returns, its properties and its id today.

### Purpose

LSEG Yield Book analytics. Metered.

| Property | Value |
|---|---|
| Kind of domain | Provider |
| Owner | QFI (to name) |

| Capability | Surface | Description | Returns | Interactions | Result · delivery | Execution | Stability | Today |
|---|---|---|---|---|---|---|---|---|
| `yieldbook.reference@v1` | data | Indicative and reference attributes for securities. | Per security: coupon, maturity, ratings, index membership | fetch | table · operation | gateway | upstream | `yieldbook.reference` |
| `yieldbook.price-yield@v1` | data | Price, yield, duration, convexity, OAS and spreads. | Per security: the analytic block | fetch | table · operation | gateway | upstream | `yieldbook.price-yield` |
| `yieldbook.scenarios@v1` | data | A scenario grid for up to 25 securities. | Per security and scenario: the analytic block | fetch | table · operation | gateway | upstream | `yieldbook.price.scenarios` |
| `yieldbook.history@v1` | data | A time series for one security. | Per date: the fields asked for | fetch | table · single | gateway | upstream | `yieldbook.history` |
| `yieldbook.tba-ids@v1` | data | TBA identifiers Yield Book recognises. | Per TBA: programme, coupon, identifier | fetch | table · single | gateway | upstream | `yieldbook.tba-ids` |

Every interaction is `read` unless it says otherwise. Execution `gateway · handoff` means the result is streamed by the gateway or handed off (§2.2).

1. Each capability here has its own profile, served by the gateway; this table is its summary.

- **ATLAS-CAT-YIELDBOOK-001.** Every capability in this table is served with a profile whose properties match it.
