# Unified Tally: using TallyPrime and Tally ERP 9 together

> Answer to: *"combine the MCP with the other, or create a new better way for using TallyPrime and ERP9 at the same time"*

---

## 1. The decision up front

**Do not merge the two codebases. Compose them behind one MCP surface with an explicit router.**

Merging would mean re-implementing 209 tools against a 25%-complete binary format — a downgrade. The two engines answer *different questions*:

| Question | Best engine |
|---|---|
| "What is the closing balance of X right now?" | **live XML** (Tally already computed it correctly) |
| "Give me every voucher of both companies for FY22–26 for analysis" | **file engine** (no Tally load, no 30 s per report) |
| "Create a sales invoice" | **live XML** (Tally validates, allocates bills, posts GST) |
| "Read this backup / this company that isn't loaded" | **file engine** |
| "Why does Tally show Rs. 4.36 Cr when your report shows Rs. 63 L?" | **live XML** for the balance, **file engine** to explain the vouchers |

So the deliverable is a **third package** — `tally-unified` — an MCP server that owns an *instance registry*, two *providers* (XML and File), a *capability prober*, and a *router*. The existing repos are used as-is:

- `tally-mcp-main` → its XML knowledge is reimplemented thinly in the unified XML provider (or, in Phase 2, imported as a library by exporting its `XmlBuilder`/`TDLBuilder`/`XmlParser`).
- `TallyWeb-main/backend` → run unchanged; its REST API *is* the file provider.

```
    MCP clients (Claude / any agent / your own UI)
                     │  stdio  or  StreamableHTTP :4000
                     ▼
        ┌────────────────────────────────────────────┐
        │            tally-unified (MCP)             │
        │  ┌──────────────┐   ┌───────────────────┐  │
        │  │ Instance     │   │ Router            │  │
        │  │ Registry     │──▶│ (policy per op)   │  │
        │  └──────────────┘   └────────┬──────────┘  │
        │        Capability cache      │             │
        └──────────────┬───────────────┴─────────────┘
                       │
        ┌──────────────┴───────────────┐
        ▼                              ▼
  XmlProvider                    FileProvider
  (UTF-16LE XML :9000,           (HTTP → TallyWeb Go :9090
   serial queue, TDL)             → Manager/TranMgr .1800|.900)
        │                              │
        ▼                              ▼
  TallyPrime  A :9000            data dir  A  (020013, *.1800)
  Tally ERP9  B :9001            data dir  B  (100001, *.900)
  TallyPrime  C :9002  …         data dir  C  …
                       │
                       ▼
              IngestStore (Postgres, already in TallyWeb's compose)
              one canonical schema for Prime + ERP9 + history
```

---

## 2. Instance Registry — the piece neither repo has

Both repos assume **one** Tally. "Prime and ERP9 at the same time" needs a first-class multi-instance model. Create `tally-instances.json`:

```json
{
  "defaultInstance": "prime-main",
  "instances": [
    {
      "id": "prime-main",
      "label": "TallyPrime — Prajna Agencies FY25-26",
      "product": "tallyprime",
      "xml":  { "host": "100.84.248.54", "port": 9000,
                "company": "M/S. PRAJNA AGENCIES (2025-26)", "timeoutMs": 30000 },
      "file": { "baseUrl": "http://127.0.0.1:9090", "username": "admin", "password": "admin",
                "companyDir": "020013" },
      "tags": ["production", "pune"]
    },
    {
      "id": "erp9-legacy",
      "label": "Tally ERP 9 — Prajna Agencies FY21-22 (read-only archive)",
      "product": "erp9",
      "file": { "baseUrl": "http://127.0.0.1:9091", "username": "admin", "password": "admin",
                "companyDir": "100001" },
      "tags": ["archive", "readonly"],
      "readOnly": true
    },
    {
      "id": "prime-branch",
      "label": "TallyPrime — Branch",
      "product": "tallyprime",
      "xml":  { "host": "127.0.0.1", "port": 9001 },
      "file": { "baseUrl": "http://127.0.0.1:9092", "companyDir": "030007" }
    }
  ]
}
```

Rules encoded by the registry:
- **`xml` optional** → an ERP9 archive with no XML server is still usable (file mode).
- **`file` optional** → a Tally running on another machine whose disk you cannot see is still usable (XML mode).
- **`readOnly: true`** → the router refuses every mutating tool for that instance.
- **`product`** is a *hint*; the prober may correct it (see §3).

### Running two Tally installs on one machine
- Each installation gets its **own data folder** and its **own XML port** (Tally: *F1 → Settings → Connectivity → TallyPrime acting as server*, set 9000 for one install, 9001 for the other). Two installs cannot share a port or a data directory.
- ERP 9's data root is typically `…` plus `TallyERP9` plus `Data` with folders like `100001` holding `Manager.900`; TallyPrime uses `TallyPrime` plus `data` plus a 6-digit folder with `Manager.1800`.
- Only the **file provider** cares about the difference; the XML provider is version-agnostic.

---

## 3. Capability probing — how the system learns "is this Prime or ERP 9?"

Product and feature differences matter because several tools simply error on ERP 9 (IMS, e-invoice, e-way bill, GSTR-2B, some payroll/TDS collections). `state.ts` in the MCP already proves this: it had to stop requesting payroll fields because they raise TDL errors when the module is off.

**Probe, don't guess. Cache the result with a TTL.**

### XML probe (5–8 serialised calls, only on first use / TTL expiry)
1. `List of Companies` → is the XML server alive, which companies exist.
2. `SVCURRENTCOMPANY` resolution → the active company name.
3. Version: request a TDL function returning `$$Version:Company:##SVCurrentCompany` (plus `$$ReleaseVersion`) as a custom collection expression.
4. Feature probes — one tiny collection each, **failure = unsupported**, never fatal:

| Capability | Probe |
|---|---|
| `inlineTdl` | any inline TDL collection |
| `masterImport` / `voucherImport` | a no-op import with `IGNOREERRORS=1` |
| `eInvoice` | e-invoice / IRN collection |
| `eWayBill` | e-way bill collection |
| `gstr2b` | GSTR-2B collection |
| `ims` | IMS collection (**Prime-only**) |
| `payroll` | `Employee` with a payroll field |
| `tds`, `tcs`, `budget`, `costCentre`, `batchWise`, `multipleGodowns` | matching collection |

### File probe (free — just read the folder)
1. `Manager.1800` present → **Prime**; `Manager.900` present → **ERP 9**. The existing Go code already does exactly this in `hasManagerFile`/`ResolveFile`.
2. Company display name + GSTIN + FY range from `Company.1800`/`.900` → matches the registry against Tally's own view.
3. Presence of `ExtMngr.1800` / decoded GST fields → `gst: true`.
4. Which optional readers succeeded → `aggregates`, `openingBalances`, `linkMgr` booleans. **Report them honestly** — the router must know that the file provider cannot produce opening balances.

Output of a probe (cached, also returned by the `tally_probe` tool):

```json
{
  "instance": "erp9-legacy",
  "product": "erp9",
  "productConfirmed": true,
  "xml":  { "ok": false, "reason": "ECONNREFUSED" },
  "file": { "ok": true, "ext": ".900", "company": "M/S. PRAJNA AGENCIES (2021-22)", "fy": "2021-04-01..2022-03-31" },
  "capabilities": { "masters": true, "vouchers": true, "openingBalances": false,
                    "aggregates": false, "gst": true, "eInvoice": false, "ims": false, "payroll": false },
  "probedAt": "2026-01-01T10:00:00Z", "ttlMs": 300000
}
```

---

## 4. Providers

### 4.1 XmlProvider (live Tally — Prime or ERP 9)
Non-negotiable behaviours, all already proven in `tally-mcp-main/src/tally/client.ts` and `state.ts`:
- **UTF-16LE** request and response bodies, `Content-Type: text/xml;charset=utf-16`.
- **One serial queue per instance** (Tally's XML server is single-threaded). The queue must be **per-instance**, not global, so instance A and instance B can still work in parallel.
- Detect `<EXCEPTION>`, `<LINEERROR>` and `<STATUS>` in responses.
- `SVCURRENTCOMPANY` from the registry, falling back to the auto-detected first company.
- Master-name cache with TTL (the 8 sets from `state.ts`), invalidated per entity after writes.

### 4.2 FileProvider (offline — `.1800` **and** `.900`)
Thin HTTP client over the existing TallyWeb Go API:

| Unified intent | TallyWeb call |
|---|---|
| health / instances | `GET /api/health`, `GET /api/companies` |
| company info | `GET /api/company/{name}?company=` |
| ledgers / groups / stock items / units / godowns / employees | `GET /api/ledgers`, `/api/groups`, `/api/stock-items`, `/api/units`, `/api/godowns`, `/api/employees` |
| vouchers | `GET /api/vouchers?type=&from=&to=&party=` |
| reports | `GET /api/reports/{trial-balance,profit-loss,outstanding,aging,stock-balance,day-book,cash-bank-book,ledger-balances,gst-returns,gstr-1,payables,cash-flow,funds-flow,ratio-analysis,statistics}` |
| create master | `POST /api/ledgers` / `/api/groups` / `/api/stock-items` with `{name, template}` |
| auth | `POST /api/auth/login` with `{username,password}` → `{token}`, then `Authorization: Bearer …` |

Two upgrades the unified layer should add on top of it:
1. **`GET /api/health` is public** — use it as the probe; log in lazily and re-login on 401.
2. **Derive opening balances** (see §6) — the single biggest fix available.

---

## 5. Router policy — the heart of the design

Every read declares an *intent*, and the intent maps to an engine. This keeps correctness (Tally's own numbers) at the top while still getting the file engine's speed and offline reach.

| Operation | Primary | Fallback | Why |
|---|---|---|---|
| `probe` / `instances` | both | — | cheap |
| `read:companies` | XML | File | XML wins if alive |
| `read:ledgers`, `groups`, `units`, `godowns` | XML | File | XML is complete |
| `read:stock_items` | XML | File | XML is complete |
| `read:vouchers` (filtered, small) | XML | File | XML has line items/amounts |
| `read:vouchers` (bulk / export / analytics) | **File** | XML | avoids loading Tally for thousands of rows |
| `read:trial_balance`, `profit_loss`, `balance_sheet` | **XML** | — | includes **opening balances** the file engine lacks; never serve these from files alone |
| `read:outstanding`, `bills`, `ageing` | XML | File (plus warning) | needs bill allocations in `LinkMgr.1800` (only partially decoded) |
| `read:stock_summary`, `valuation` | XML | File | needs `Aggr.1800`, not decoded |
| `read:gst_*`, `tds_*`, `payroll_*` | XML | — | file engine has none |
| `read:day_book` | XML | File | — |
| `create/alter/delete master` | **XML** (import) | File, only if `allowOfflineWrite` **and** Tally verified closed | Tally validates |
| `create/alter/cancel voucher` | **XML** | **refuse** | file writer cannot do vouchers; TallyWeb itself says "not yet" |
| `bulk_*` | XML | — | — |
| `raw_xml`, `tdl_collection`, `tdl_function`, `action` | XML | — | inherently live |
| `fanout` across instances | routed **per instance** | — | merges with `_instance`/`_product`/`_source` annotations |

### Freshness contract (the rule that prevents wrong numbers)
Tally writes vouchers into a **write-ahead log in `TUPDATE.TSF`** and only applies them to `.1800` files on the next open (documented in `docs/tally-binary-format.md` and `docs/tstate-tupdate-format.md`). Therefore:

| Instance state | File reads are | Router behaviour |
|---|---|---|
| XML unreachable / Tally closed | **authoritative** | serve from File, `source: "file"` |
| XML reachable (Tally running) | **a possibly-stale snapshot** | prefer XML; if File is used anyway, attach `"stale_risk": true` and `"snapshot_at"` |

Every read result carries `source` and `stale_risk`. Any agent can then decide whether the number is trustworthy. Never silently mix.

### Write safety rules
1. Default: **all writes go through XML.**
2. File writes require all of: registry `allowOfflineWrite: true`, Tally confirmed closed (`Manager.1800`/`.900` lockable), an automatic timestamped backup copy, and an explicit `confirm: true` in the tool call.
3. After any file write, the router invalidates that instance's caches and logs which regenerable files were deleted (`Index.1800`, `TSTATE.TSF`, `TUPDATE.TSF`, `Aggr.1800`).
4. `readOnly` instances reject mutation outright.

---

## 6. The "better way": three improvements neither repo has

### (a) Closing-balance-anchored opening balances — fixes TallyWeb's #1 unsolved bug
`VERIFICATION.md` admits opening balances are **not decoded**, so the file engine's trial balance is wrong (Rs. 63 L vs Tally's Rs. 4.36 Cr). But the file engine *does* have every voucher, and XML *does* have the correct closing balance. So:

```
opening_balance(ledger)  =  closing_balance_from_XML(ledger)  -  sum( file_vouchers(ledger) )
```

No new reverse engineering required — it *derives* the missing value from the two engines that already work. Store the result once per ledger per FY and the file engine becomes able to produce a correct trial balance and balance sheet offline. Add an integrity check: all derived openings must net to zero; if they do not, flag the company as "unreconciled" with the delta.

### (b) Canonical ingest store — one schema for Prime + ERP9, all companies, all years
TallyWeb's `docker-compose.yml` already ships Postgres + Redis but nothing uses them. Use it:

- A worker walks each registered data folder, detects `.1800`/`.900`, and ingests Company, Ledger, Group, Voucher and VoucherLine into **one normalised schema** (Postgres in production, SQLite for a single machine).
- Delta detection by file mtime + page count + max-seq so re-ingest is cheap.
- Only *new pages* are parsed (the reader is page-addressable, so this is genuinely incremental).
- Now cross-version, cross-company, multi-year reporting — "sales of Prime company A vs ERP9 archive B for the last 5 years" — is one SQL query instead of thousands of serialised XML round-trips.

This is what makes "using Prime and ERP9 at the same time" actually pleasant, because ERP 9 is almost always an *archive* and archives are read constantly.

### (c) One curated tool surface + a passthrough escape hatch
209 MCP tools consume enormous context and push the model toward wrong choices. The unified server should expose roughly **14 tools**, and keep full power reachable:

| Tool | Purpose |
|---|---|
| `tally_instances` | list registry entries with health + product + modes |
| `tally_probe` | force/cached capability probe for an instance |
| `tally_read` | routed read: entity, instance, filters, prefer auto/xml/file |
| `tally_report` | routed report: trial balance, P&L, outstanding, daybook, GST |
| `tally_list_vouchers` | routed voucher listing (declares bulk intent) |
| `tally_create_master` | routed master create (XML first) |
| `tally_create_voucher` | XML-only, explicit error if unavailable |
| `tally_alter` / `tally_delete` | routed mutations |
| `tally_query_collection` | inline TDL collection (XML) |
| `tally_evaluate_function` | TDL function (XML) |
| `tally_raw_xml` | raw passthrough |
| `tally_fanout` | run a routed read across N instances and merge |
| `tally_ingest` | rebuild/refresh the canonical store for an instance |
| `tally_sql` | read-only query over the canonical store (cross-version reporting) |

The 209-tool surface remains available by mounting `tally-mcp-main` **as a second MCP server** for the one instance that needs deep ERP control — but it is no longer the default, so the common path stays cheap.

---

## 7. Implementation phases

**Phase 0 — make the parts runnable (half a day).**
Delete `TallyWeb-main/backend/server` (macOS Mach-O). Set one port (9090) everywhere in `config.yaml`, `config.go`, `Makefile`. Start Postgres via `docker compose up -d`. Either stand up the Go backend against a real data folder, or accept XML-only mode.

**Phase 1 — `tally-unified` skeleton (this workspace already contains it).**
Registry loader, XmlProvider (UTF-16LE + per-instance serial queue), FileProvider (TallyWeb REST + JWT), capability prober, router with the §5 policy, and the curated tools above. stdio + HTTP transports. Run with `--selftest` to print the probe and routing decisions without touching Tally.

**Phase 2 — correctness and scale.**
Opening-balance derivation (§6a); `tally_fanout`; per-instance TTL caches; `stale_risk` annotations; the offline-write guard with backup + `confirm`.

**Phase 3 — canonical ingest store (§6b).**
Postgres schema, incremental page-delta parser (reuse `tallydb` via a small Go CLI or a new `/api/ingest` endpoint), `tally_sql`, and reconciliation reports between the live XML view and the ingested snapshot (this doubles as regression testing for the RE work).

**Phase 4 — harden TallyWeb itself (optional, high value).**
1. Add `POST /api/vouchers` (the file writer for vouchers does not exist — until it does, voucher writes must stay XML-only).
2. Replace single-round SHA-256 with bcrypt/argon2 for passwords.
3. Add `/api/company/{name}/opening-balances` returning the derived values.
4. Implement the advertised analytics (currently the dashboard is 0 bytes).
5. Parse `Aggr.1800` / `ExtMngr.1800` fully — these unlock offline outstanding, stock valuation and GST.

---

## 8. What "at the same time" can mean — pick the right one

| Goal | Mechanism |
|---|---|
| Query a live Prime and a live ERP 9 in one session | two registry entries with separate ports; `tally_fanout` for combined reads |
| Keep an ERP 9 archive readable forever without the old install | file provider on a copied data folder (`.900` is already understood) |
| Migrate ERP 9 → Prime and verify nothing was lost | read both through the same unified tools, diff ledgers/vouchers/balances; use derived openings (§6a) as the control total |
| Consolidated reporting across years/companies | canonical ingest store + `tally_sql` |
| Write from a script while Tally is closed | `allowOfflineWrite` path with backup + `confirm` — masters only, never vouchers |

---

## 9. Honest risk register

| Risk | Severity | Mitigation |
|---|---|---|
| File snapshot misses WAL-pending vouchers | **High** | freshness contract (§5): XML first when live; `stale_risk` on every file read |
| Clone writer pads/truncates names and inherits the template's parent group | **High** | prefer XML for writes; treat file writes as admin-only with backup |
| Concurrency across instances | Medium | serial queue **per instance**, not global |
| Two Tally installs on one port / one data dir | Medium | registry validation at load: reject duplicate host:port and duplicate data path |
| ERP 9 lacks Prime collections (IMS, e-invoice, GSTR-2B, payroll fields) | Medium | capability prober; tools return `unsupported_capability` instead of a TDL crash |
| 209-tool context bloat | Medium | curated tool surface + passthrough |
| Postgres is a hard dependency of the file provider | Low | registry allows XML-only instances; SQLite option for ingest-only mode |
| Committed macOS binary / stale README / empty frontend mislead newcomers | Low | documented in `TALLY-ANALYSIS.md` |

---

## 10. TL;DR

Build `tally-unified`: **one MCP server, an instance registry, two providers (live XML + offline binary files), and a router.** Send reads that need Tally's own arithmetic (balances, opening balances, GST, payroll, outstanding) to the XML provider; send bulk/offline/archive reads to the file provider; send every write to XML, and only allow file writes for masters, offline, with a backup. Tag every answer with `source` and `stale_risk`. Then close TallyWeb's opening-balance gap by derivation (closing XML minus sum of file vouchers) and feed both Prime and ERP 9 data files into one Postgres schema so cross-version and multi-year reporting becomes a SQL query. Expose around 14 tools instead of 209, keeping `raw_xml`/`tdl` passthrough so nothing is lost.
