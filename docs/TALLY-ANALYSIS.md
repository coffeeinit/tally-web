# Tally Workspace — Folder-by-Folder Analysis

Workspace root: `D:\github\tally\`
Analysed: read-only inspection of all 136 source files, excluding `node_modules`, `dist`, `.next`

---

## 0. What is actually in the root

| Entry | Type | What it is |
|---|---|---|
| `TallyWeb-main/` | folder (100 files) | A Go backend + Next.js frontend that reads/writes **Tally's binary data files on disk** (`.1800` for Prime, `.900` for ERP 9). Also a large reverse-engineering research archive. |
| `tally-mcp-main/` | folder (36 files) | A TypeScript **MCP server** exposing **209 tools** that drive a *running* Tally (Prime or ERP 9) over its XML HTTP port (9000). |

There is **no `.git`** anywhere and no `TallyData/` folder. Both folders are GitHub ZIP downloads (`-main` suffix). The `TallyData/` tree that `docs/HOW-TO-CONTINUE.md` refers to (memory dumps, Ghidra project, live company data) exists only on the original author's machine and is **not** in this workspace.

**The two projects do not overlap — they are two different ways to reach the same ERP:**

| | `tally-mcp-main` | `TallyWeb-main` |
|---|---|---|
| Reaches Tally via | XML/HTTP API on port 9000 | direct 512-byte page parsing of data files |
| Tally must be running | **Yes** | **No** (reads on-disk files) |
| Write capability | Full CRUD for ~every object (via Tally's own engine) | Masters only (Ledger/Group/StockItem), by *cloning* a template |
| Correctness | Tally validates everything | hand-rolled; ~25–32% reverse-engineered |
| Maturity | 209 tools, complete | reader good, writer fragile, frontend empty |
| Product awareness | assumes "TallyPrime" | **explicitly supports both `.1800` and `.900`** |

---

## 1. `TallyWeb-main/` — offline binary data engine + RE research archive

### 1.1 The important framing caveat

The root `README.md` describes an **architecture that no longer exists** in the code. It advertises `backend/internal/model/`, `backend/internal/tally/` (a "Tally XML API client"), a Go REST proxy to port 9000, and a Next.js dashboard in `frontend/src/`.

The actual code has:
- `backend/internal/tallydb/` (binary file engine) — no `model/`, no `tally/` XML client
- `frontend/app/` + `frontend/components/` + `frontend/lib/` — **every file is 0 bytes**
- `backend/config.yaml` defines `tally.host`/`tally.port` and `config.go` parses them into a `TallyConfig` struct, but **nothing ever uses that struct**. The XML client was removed.
- `backend/config.yaml` says port **9090**, `Makefile` says **8080**, `config.go` defaults to **8080**.

Treat the README as aspirational history, and the code as the truth.

### 1.2 `backend/` — Go REST API (the real product)

Module: `github.com/rudra/tallyweb-backend`, Go 1.25, deps = `gopkg.in/yaml.v3` + `jackc/pgx/v5` (Postgres).

#### `backend/cmd/server/main.go` (159 lines) — application entry point
Wires everything together:
1. Loads `config.yaml` (`internal/config`).
2. Connects to **PostgreSQL** via `pgxpool`; hard-fails if unreachable.
3. Initialises JWT auth + user store; seeds a default `admin/admin` owner.
4. Resolves the Tally data folder: `-data` flag → `config.yaml data_path` → `tallydb.FindDataPath()` auto-discovery.
5. `tallydb.Open(dp)` scans for company sub-folders containing `Manager.1800`/`Manager.900`.
6. Registers 30+ routes on `http.ServeMux` using Go 1.22+ method patterns (`GET /api/...`).
7. Splits public routes (`POST /api/auth/login`, `GET /api/health`) from a `protected` mux mounted under `middleware.Auth`, where **every route is wrapped in `middleware.RBAC(resource, handler)`**.

Note: it expects **one** data path and auto-picks `db.Companies[0]` as default. Multi-instance/multi-product is not modelled.

#### `backend/cmd/fieldscan/main.go` (97 lines) — reverse-engineering helper CLI
`fieldscan <company_data_dir>` globs `*.1800` **and** `*.900`, dumps every TLV field ID with type/count/sample values as JSON. This is the tool used to discover field IDs. Not part of the server; it is the RE workbench.

#### `backend/internal/config/config.go` (41 lines)
YAML loader for `Config{Tally, Server, DefaultCompany, DataPath}`. `Tally` is dead config (see above).

#### `backend/internal/auth/auth.go` (213 lines) — users, roles, JWT
- Roles: `owner, manager, accountant, salesperson, store_manager, auditor` with a `Permissions` map (owner = `*`).
- `Store` on top of Postgres; auto-migrates a `users` table; seeds `admin/admin`.
- Password hashing = **single SHA-256 of `password+salt`** (not bcrypt/argon2 — weak, worth flagging).
- Hand-rolled HS256 JWT (`GenerateToken`/`ValidateToken`, 24 h expiry).

#### `backend/internal/middleware/`
- `auth.go` — `Auth` validates the `Bearer` token and injects `*auth.Claims` into the request context; `RBAC(resource, next)` checks `HasPermission(role, resource)`.
- `cors.go` — permissive CORS from `config.yaml`.

#### `backend/internal/handler/` (14 files) — thin HTTP layer over `tallydb`
| File | Routes / role |
|---|---|
| `base.go` | shared `Base{DB, DefaultCompany}`; `CompanyFolder(r)` resolves `?company=` by folder **or** display name; `WriteJSON`/`WriteError` |
| `auth.go` | `POST /api/auth/login`, `GET/POST /api/auth/users`, `DELETE /api/auth/users/{username}` (owner-only for mutations) |
| `health.go` | `GET /api/health` → `{status, mode:"file", companies}` |
| `companies.go` | `GET /api/companies`, `GET /api/company/{name}` |
| `dashboard.go` | `GET /api/dashboard/overview` |
| `ledgers.go` | `GET /api/ledgers`; `POST /api/ledgers` **requires a `template` ledger name** |
| `groups.go` | `GET /api/groups` (filtered to groups actually used as parents); `POST /api/groups` (template required) |
| `stock.go` | `GET/POST /api/stock-items` (template required), `GET /api/price-lists` |
| `units.go`, `godowns.go`, `employees.go` | read-only list endpoints |
| `vouchers.go` | `GET /api/vouchers` (type/from/to/party filters, `DD-MM-YYYY`), `GET /api/vouchers/{id}`, `GET /api/ledgers/{name}/vouchers` (computes OB + closing) |
| `reports.go` (8.5 KB, the largest) | trial-balance, profit-loss, gstr-1, gst-returns, ledger-balances, outstanding, stock-balance, day-book, aging, cash-bank-book, payables, cash-flow, funds-flow, ratio-analysis, statistics, search |
| `banking.go` | `GET /api/banking/transactions` |

All handlers are 10–50 lines: they call a `DB` method and serialise JSON. No business logic lives here.

#### `backend/internal/tallydb/` — **the core: a reverse-engineered Tally file database**
15 files. This is the intellectual heart of the repo.

| File | Lines | What it does |
|---|---|---|
| `reader.go` | 210 | Reads a `.1800`/`.900` file, slices it into **512-byte pages**, decodes the page header (checksum, SeqNum, PageIdx, flags, ObjType, ParentSeq at bytes 28–31) and walks the **TLV field stream**: `02 10 [fid:u16] 00 0F [len:u16] [UTF-16LE]` for strings, `... 00 08 / 00 09 [i64]` for amounts, `... 00 06 [u32]` for ints, `... 00 0D [days]` for dates, `... 00 07` for the amount+date compound used in LinkMgr. |
| `fields.go` | 6 KB | Field-ID constants (0x0002 name, 0x001B company name, 0x001D address, 0x0066 email, 0x01F9 parent group, 0x01FB GUID, …). |
| `checksum.go` | 1.6 KB | The cracked **dual CRC-16/CCITT** checksum with the page number as seed. Verified against 53,489 pages. |
| `db.go` | **633 lines** | Public `DB` facade: `Open`/`FindDataPath` (finds company dirs by `Manager.1800|.900`), `CompanyDir`, `GetCompanyInfo`, `ListCompanies`, `GetMasters`, `CreateLedger/StockItem/Group`, `GetVouchers(+/ByType/ByParty)`, `GetBankEntries`, `GetPriceLists`, `GetGSTReturns`, `GetUnits`, `GetTrialBalance`, `GetGSTR1Summary`. |
| `masters.go` | 17 KB | Parses `Manager.1800` into Ledgers/Groups/StockItems/Units/Godowns. Builds a `groupBySeq` map from `header[28:32]` on `pidx=4 type=0x000B` pages to attach each ledger's parent group. |
| `vouchers.go` | 10.7 KB | Parses `TranMgr.1800` vouchers; infers voucher **type** from the party's ledger group (no hardcoded prefixes). |
| `computed.go` | 5.9 KB | Derived values Tally would compute: `ComputeLedgerBalances`, `ComputeOutstanding`, `ComputeStockBalance`, `ComputeAging`, `GetOverview`. |
| `aggr.go`, `banking.go`, `extended.go` | 2.5/2.2/2.5 KB | Partial readers for `Aggr.1800`, bank data, and `ExtMngr.1800` (GST/TDS/bank details). |
| `writer.go` | 5.4 KB | Core writer: `findTemplatePages`, `findAnyLedger`, `findChainTail`, `incrementCounters`, `replaceName`, `replaceSeqRefs`, `Save`, plus cache/index invalidation. |
| `writer_ledger.go` | 91 | `WriteLedger`/`WriteStockItem`/`WriteGroup` → one generic `writeMaster`: find template → clone its `pidx=2`+`pidx=0` pages → rename → relink the sibling chain at page offsets **58/62/66** → bump the 0x0042 counter pages → rewrite header → recompute checksums. |
| `writer_genesis.go` | 1.3 KB | `CreateFirstLedger`: applies an embedded **genesis patch** (4 appended pages + 7 modified pages) so a brand-new company can get its first ledger. |
| `writer_voucher.go` | 8.8 KB | Clone-and-modify voucher writer — **partial**, date replacement only. |
| `*_test.go` | 3 tests | `checksum_test.go`, `writer_test.go`, `writer_voucher_test.go` (all described as passing). |

#### `backend/server` (9 MB, committed binary)
Magic bytes `CF FA ED FE` = a **macOS Mach-O 64-bit** executable. It will not run on this Windows machine. It is a stray build artefact, not a deliverable.

#### `backend/Makefile` / `go.mod`
`Makefile` is 155 bytes (`run`/`build`/`clean`). `go.mod` requires `pgx/v5` — so **the backend cannot start without PostgreSQL running** even though the README claims "zero external dependencies".

### 1.3 `frontend/` — an empty Next.js scaffold

`package.json` + configs + `public/` SVGs are real. **Every source file is 0 bytes:**

- `app/(auth)/layout.tsx`, `app/(auth)/login/page.tsx`, `app/(auth)/signup/page.tsx`
- `app/(dashboard)/layout.tsx`
- `components/ui/button.tsx`, `card.tsx`, `input.tsx`
- `hooks/use-auth.ts`, `lib/api.ts`, `lib/auth.ts`, `types/index.ts`

Only `app/layout.tsx` (705 B), `app/page.tsx` (102 B), `app/globals.css` (488 B) and `next.config.ts` have content — i.e. the `create-next-app` default shell. `README.md` is the untouched Next.js boilerplate.

**The "Modern web dashboard for TallyPrime ERP" advertised in the README does not exist yet.** The API is complete; the UI is not started.

### 1.4 `docs/` — the reverse-engineering archive (19 files)

This is the most valuable non-code asset. It documents a genuine RE effort against `tally.exe` (VMProtect-virtualised, dumped from memory with PE-sieve, decompiled with Ghidra).

| Doc | Content |
|---|---|
| `HOW-TO-CONTINUE.md` | Handoff doc: repo map, the Windows box accessed over Tailscale, current progress **32%**, the next 10 tasks, how to re-dump/decompile, and 10 key findings (512-byte pages, TLV layout, Excel-serial dates, paise int64 amounts, 6-byte quantities, which files are safe to delete). |
| `reverse-engineering-progress.md` | The honest scorecard: 52 object types across 11 data + 10 aux files; per-file read/write status; binary-encoding table; engine-internals table; weighted total **~25–32%**. |
| `tally-binary-format.md` | The format spec: page layout, the dual CRC-16/CCITT algorithm, string-field byte layout, sibling chain at offsets 58/62/66, 0x0042 counter pages, the clone+chain write recipe, the genesis patch, and the limitations list (**Tally must be closed during writes**; vouchers use a WAL in `TUPDATE.TSF`). |
| `voucher-format.md` | 355 lines on vouchers: they span **TranMgr.1800 + LinkMgr.1800**, ~90 pages/voucher, ~38 LinkMgr entries/voucher, page types `otype 0x0003/0x0005/0x000A/0x000B/0x0042`. |
| `ledger-creation.md` | Step-by-step of both write paths (genesis vs clone). |
| `index-1800-format.md` | Proves `Index.1800` is a regenerable **search index**; safe to delete; not needed for master writes. |
| `tstate-tupdate-format.md` | `TSTATE.TSF` (object counts) and `TUPDATE.TSF` (audit/WAL) layouts; both recomputed by Tally on startup. |
| `tally-engine-reversing.md` | Decompiled-engine findings: 48.9 MB PE / 95,529 functions; page-ID = `[3-bit file_index][29-bit page_number]`; Tally's in-memory index is a plain **BST, not a B-tree**; write pipeline, page allocator. |
| `all-data-files.md` | The file inventory + which files may be deleted, `otype`/`pidx` meanings, and the writer strategy (delete `Index.1800`, `TSTATE.TSF`, `TUPDATE.TSF`, `Aggr.1800` after a master write). |
| `field-id-catalog.md` | 155 mapped field IDs with real sample values from a live company. |
| `apicollection/*.json` (7 files) | **Orphaned** Postman collections ("TallyPrime via ODBC/XML") from the era when the repo was an XML proxy. Not referenced by any code. |
| `xml-reference/sampledata_masters.xml` (625 KB), `sampledata_vouchers.xml` (94 KB) | Orphaned Tally XML export samples, same era. |

### 1.5 `VERIFICATION.md` — the most important honesty document

Validated the binary parser against TallyPrime Gold UI screenshots for company `M/S. SAHOO SANITARY (2026-27)`:

- Voucher amounts: 123/126 exact; May sales 35/36 exact; total SS sales **Rs. 32,59,949 exact**.
- Voucher-type detection works without hardcoding (84 Sales + 39 Purchase).
- Ledger→group mapping: 286/286 ledgers.
- **Known unsolved:** opening balances are **not decoded at all** → its trial balance shows Rs. 63 L current-year vs Tally's Rs. 4.36 Cr (missing Rs. 3.72 Cr of opening balance). One voucher is Rs. 20,000 short. 15 voucher parties are missing from the ledger list.

### 1.6 Root files
`docker-compose.yml` (Postgres 17 + Redis 7 only — **no app services**), `Makefile` (`dev`/`backend`/`frontend`/`install`/`build`/`clean`), `LICENSE` (MIT), `CONTRIBUTING.md`, `.editorconfig`, `.gitignore`.

---

## 2. `tally-mcp-main/` — a 209-tool MCP server for a live Tally

### 2.1 What it is

A production-shaped **Model Context Protocol** server. An AI client speaks MCP; the server translates tool calls into Tally's XML API on port 9000 and returns parsed JSON. It works against **any** Tally that exposes the XML server (TallyPrime or Tally ERP 9) — the code never branches on product version, it simply assumes the XML endpoints exist.

### 2.2 Entry point and transport — `src/index.ts` (152 lines)

- Reads env: `TALLY_HOST`, `TALLY_PORT`, `TALLY_TIMEOUT_MS`, `TALLY_COMPANY`, `MCP_SERVER_PORT` (4000), `MASTER_CACHE_TTL_MS`, `LOG_LEVEL`.
- Transport chosen by `MCP_TRANSPORT=stdio` **or** the `--stdio` argv flag; otherwise HTTP.
- **stdio mode** (Claude Desktop / CLI): connects `StdioServerTransport`; warm-up (connection test + cache load) runs in `setImmediate` so it never blocks the protocol; fatal errors are written to **stderr only** (stdout is the MCP channel).
- **HTTP mode**: dynamically imports `express` + `StreamableHTTPServerTransport`, exposes `POST/GET/DELETE /mcp` and `GET /health` (which reports `tally_connected`, `active_company`, `cache_stats`), then connects and eagerly loads the master cache.
- Startup never fails just because Tally is down — tools report the connection error individually.

### 2.3 `src/server.ts` (56 lines) — tool registration
Creates one `McpServer` (`name`/`version` from env) and calls 16 `register*Tools(server, client, state)` factories, summing a tool count. **Measured actual total: 209 tools** (the README's "169+" is stale).

### 2.4 `src/tally/` — the Tally protocol layer (5 files, ~52 KB)

**`client.ts` (268 lines)** — the wire layer, and the file that makes the ERP9/Prime distinction mostly moot:
- Sends `POST /` with `Content-Type: text/xml;charset=utf-16` and body encoded **UTF-16LE** (`Buffer.from(xml, 'utf16le')`), decoding responses as `utf16le`. Correct for Indian-language data.
- **Serial request queue**: `enqueue()` chains every request on a single promise chain, because Tally's XML server is single-threaded and concurrent requests freeze/crash it. `postXml` always goes through it.
- Error surface: `TallyConnectionError` (ECONNREFUSED/ETIMEDOUT with an explanatory message) and `TallyResponseError` (carries `rawXml`); `checkForErrors` scans for `<EXCEPTION>` and `<LINEERROR>`.
- Exactly **five** communication methods, matching the README:

| Method | Client entry point | Built by |
|---|---|---|
| A. Standard XML export (report/collection) | `exportReport`, `exportCollection` | `XmlBuilder` |
| B. Inline TDL collection query | `queryCollection`, `queryRawTdlCollection` | `TDLBuilder` / `XmlBuilder` |
| C. XML import (masters/vouchers) | `importMasters`, `importVouchers` | `XmlBuilder` |
| D. TDL action | `executeAction` | `XmlBuilder` |
| E. Object export | `exportObject` | `XmlBuilder` |

Plus `evaluateFunction` (TDL functions) and `getCompanyList()`.

**`xml-builder.ts` (40.6 KB)** — the largest file; builds every request envelope: export requests with `STATICVARIABLES` (including `SVCURRENTCOMPANY`), inline-TDL collections, master XML for Ledger/Group/StockItem/Unit/Godown/CostCentre/Employee/Budget/Currency/VoucherType, **voucher XML for all 16 voucher types**, delete-master XML, action requests.

**`xml-parser.ts` (11.5 KB)** — `parse()` → `TallyParseResult{success, data, error, raw}`, plus `extractNameList`, `extractObjectData`, `extractImportResult`, and **Tally value parsers** for dates, amounts, quantities, rates and booleans.

**`tdl-builder.ts` (11.5 KB)** — programmatic inline-TDL `CollectionConfig`/`FunctionConfig` builders (`buildLedgerListQuery`, `buildStockItemQuery`, …). This is what lets the AI ask things the standard API cannot express.

**`state.ts` (274 lines)** — the master-name cache: 8 `Set<string>` (ledgers, stockItems, voucherTypes, groups, godowns, costCentres, units, employees) + a batch map, `activeCompany`, `lastRefreshed`.
- `refresh()` resolves the active company **first** (everything else needs `SVCURRENTCOMPANY`), then refreshes the 8 caches *sequentially* to avoid queueing a backlog on Tally.
- `ensureFresh()` honours the TTL (default 5 min); `refreshAfterMasterChange(entity)` refreshes just the affected cache after a write; `getCacheStats()` feeds `/health`.
- Note the code comment: employee refresh requests **only** `Name` because payroll fields raise TDL errors when the Payroll module is disabled — evidence that Tally feature-gating is a real problem this code already hits.

### 2.5 `src/tools/` — 16 modules, 209 tools (measured)

| Module | Tools | Coverage |
|---|---:|---|
| `masters.ts` | 28 | ledger/group/stock item/stock group/category/unit/compound unit/price level/price list/godown/cost centre/cost category/voucher type/currency |
| `vouchers.ts` | 16 | sales, purchase, receipt, payment, journal, contra, credit/debit note, stock journal, physical stock, reversing, memorandum, alter, cancel, delete, list |
| `orders.ts` | 10 | sales/purchase orders, delivery/receipt notes, rejections in/out, order book, outstanding |
| `reports-financial.ts` | 15 | trial balance, P&L, balance sheet, cash/funds flow, ledger vouchers, group summary, daybook, cash/bank book, ratio analysis, budget variance, cost/profit centre, statistics |
| `reports-inventory.ts` | 10 | stock summary/item detail/godown summary/movement/batch/expiry/reorder/valuation/category |
| `reports-outstanding.ts` | 8 | receivables, payables, bills, overdue, party outstanding, ageing, interest |
| `gst.ts` | 18 | GSTR-1 (+B2B/B2C/CDNR), 2A, 2B, 3B, HSN, liability, ITC, reconciliation, e-invoice, e-way bill, **IMS**, notices, GSTIN verify, return status, exceptions |
| `tds-tcs.ts` | 12 | TDS/TCS computation+outstanding, forms 26Q/27Q/16, PAN verify, challans, LDC, nature summary |
| `payroll.ts` | 20 | employees, groups, pay heads, salary structures, processing, payslips, registers, PF/ESI/gratuity/CTC/statutory |
| `manufacturing.ts` | 14 | BOM CRUD, manufacturing journal, production/consumption, job costing, work orders, wastage, yield |
| `banking.ts` | 10 | reconciliation, unreconciled, cheque register, PDC, deposit slips, balances, payment performance, cash position |
| `budgets.ts` | 8 | budget CRUD, vs actual, utilization, scenario |
| `security.ts` | 8 | user CRUD, audit log, altered/deleted voucher logs, exception reports |
| `company.ts` | 8 | list/info/set active/alter company, test connection, refresh cache, license info, feature status |
| `bulk.ts` | 12 | batch create/alter/delete masters & vouchers, CSV/flat export |
| `tdl-query.ts` | 12 | the escape hatch: `tally_query_collection`, `tally_execute_tdl_inline`, `tally_evaluate_function`, `tally_export_object`, `tally_export_report`, `tally_execute_action`, `tally_search_masters`, `tally_get_master_names`, `tally_import_raw_xml`, `tally_send_raw_xml`, `tally_get_collection_types`, `tally_get_tdl_syntax_help` |

Every tool name is prefixed `tally_` — good for namespacing, but note the risk of a tool-name collision if a second Tally MCP server is mounted side by side.

### 2.6 `src/utils/`
`logger.ts` (Winston, stderr-safe), `validators.ts` (GSTIN/PAN/date checks), `date-utils.ts`, `helpers.ts` (`result()`/`handleError()` uniform MCP responses).

### 2.7 `tdl/tally-mcp.tdl` (4.8 KB)
A persistent TDL file to drop into Tally's TDL folder. Declares pre-built collections (`MCP Ledgers`, `MCP StockItems`, `MCP Groups`, `MCP Vouchers`, `MCP Debtors`, `MCP Creditors`, `MCP BankAccounts`, `MCP CostCentres`, `MCP Employees`, `MCP CashBankSummary`) and the `MCP FinancialSnapshot` function.

### 2.8 Other
`dist/` = committed compiled JS + `.d.ts` (so it runs without building). `Dockerfile` + `docker-compose.yml` (port 4000, `host.docker.internal`). `.env.example`. `package.json` v**3.1.0** (README/index say 3.0.0). SDK `@modelcontextprotocol/sdk 1.25.2`, `zod`, `fast-xml-parser`, `express`, `winston`, `dotenv`. `node_modules/` is present.

---

## 3. Side-by-side capability matrix (as built today)

| Capability | tally-mcp-main | TallyWeb-main |
|---|---|---|
| Needs Tally running | Yes | No |
| Needs port 9000 reachable | Yes | No |
| Needs Postgres | No | **Yes** |
| Read masters | ✅ all | ✅ 5 of 16 types |
| Read vouchers | ✅ all 24 types | 🔶 Sales/Purchase, no line-item amounts |
| **Opening balances** | ✅ (Tally computes) | ❌ not decoded |
| Read computed reports | ✅ 33 report tools | 🔶 ~16 endpoints, less accurate |
| Create/alter/delete any master | ✅ | 🔶 Ledger/Group/StockItem, clone-only, name length constrained |
| Create any voucher | ✅ 16 types | ❌ |
| GST/e-invoice/e-way/TDS/payroll | ✅ 68 tools | ❌ |
| Bulk operations | ✅ 12 tools | ❌ |
| Works offline / on a backup copy | ❌ | ✅ |
| Multi-company in one session | per-call `SVCURRENTCOMPANY` | `?company=` per request |
| Product awareness | none (assumes XML available) | **file extension `.1800` vs `.900`** |
| Auth/RBAC | none (transport-level) | JWT + 6 roles |
| UI | none | **empty scaffold** |

---

## 4. Gotchas worth knowing before building anything

1. **`TallyWeb-main`'s README describes a program that no longer exists.** Read `VERIFICATION.md` + `docs/` instead.
2. **The committed `backend/server` is a macOS binary** — delete it; it will not run here.
3. **The TallyWeb backend hard-requires PostgreSQL** and will `log.Fatalf` without it, despite "zero external dependencies" in the README.
4. **Password hashing is a single-round SHA-256** — do not expose that API to the internet as-is.
5. **Binary writes require Tally to be closed** (file locking) and delete `Index.1800`/`TSTATE.TSF`/`TUPDATE.TSF`/`Aggr.1800`; the clone writer also **pads/truncates the new name to the template's length** and inherits the template's parent group. Never point it at a live company.
6. **Vouchers go through a WAL in `TUPDATE.TSF`** — a file-snapshot reader can therefore miss recent transactions while Tally is running. This is the single most important reason the two engines must be *router-selected*, not blindly mixed.
7. **The MCP server serialises all requests** — do not "parallelise" it; fan-out across instances must be per-instance serialised.
8. **Feature gating is real**: employee payroll fields, IMS and e-invoice collections raise TDL errors when the feature/module is absent (mostly ERP 9). Any unified layer must probe capabilities instead of assuming.
9. **209 tools is a lot of context.** Mounting the full MCP surface plus TallyWeb tools at once will bloat every prompt. The unified layer should expose a curated set plus a passthrough.

---

## 5. One-paragraph summary

`tally-mcp-main` is a finished, broad **live-API bridge** (209 tools) that can read and write practically everything in a running Tally through Tally's own validated engine — but is blind when Tally is closed and has no concept of multiple installations or products. `TallyWeb-main` is an unfinished but genuinely impressive **offline binary engine** that already reads both `.1800` (Prime) and `.900` (ERP 9) files without Tally running, plus an RE archive documenting how Tally's file format and write engine work — but its writer is narrow and risky, its opening balances are unsolved, and its advertised frontend is entirely empty. They are complements with almost zero overlap, which is exactly why combining them (not merging their code) is the right move.
