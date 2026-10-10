# What can be done in the web UI today

Legend: **built** = a screen and/or MCP tool exists · **passthrough** = reachable through
inline TDL, object export or a raw XML/import call, but no first-class screen yet ·
**blocked** = Tally's API cannot do it, or does not expose the data.

Everything below goes through Tally's own API, so Tally validates it and the statutory
audit trail stays intact. Nothing is ever written to `.1800` / `.900` files.

---

## 1. Sales and billing

| Task | Status | How it reaches Tally |
|---|---|---|
| Tax invoice with inventory lines | **built** | Invoice screen → `POST /api/vouchers` → `tally_create_sales_invoice` |
| Party, narration, voucher number, place of supply | **built** | same |
| Line discount, rate, unit, per-line sales ledger | **built** | `ALLINVENTORYENTRIES.LIST` with `ACCOUNTINGALLOCATIONS.LIST` |
| GST on the invoice (CGST/SGST or IGST) | **built** | "Apply on items" computes it into tax ledger lines |
| Sales return (Credit Note) | **built** | same screen, Credit Note tab |
| Sales order, delivery note, rejection out | passthrough | TDL collection or `tally_import_data` |
| E-invoice / e-way bill | passthrough | needs an IRP/GSP integration on top; TDL can reach the ledgers |
| Price lists and price levels | passthrough | `GET /api/price-lists` on the file engine only |

## 2. Purchases

| Task | Status | How |
|---|---|---|
| Purchase invoice with inventory | **built** | Invoice screen, Purchase tab |
| Purchase return (Debit Note) | **built** | Invoice screen, Debit Note tab |
| Purchase order, receipt note, rejection in | passthrough | TDL / import |
| Input tax credit lines | **built** | add Input CGST / SGST / IGST as tax lines |

## 3. Money in and out

| Task | Status | How |
|---|---|---|
| Receipt from a customer | **built** | Compose → ledger entries (party credit, bank debit) |
| Payment to a supplier | **built** | Compose |
| Contra: cash deposited / withdrawn | **built** | Compose |
| Journal, provisions, depreciation | **built** | Compose |
| Outstanding receivables and payables | **built** | Reports |
| Ageing of dues | **built** | Reports |
| Bank reconciliation against a statement | **not yet** | needs statement import plus a matching screen; the read side (`/api/banking/transactions`) exists on the file engine |
| Post-dated cheques, deposit slips | passthrough | TDL collections |

## 4. Accounting and masters

| Task | Status | How |
|---|---|---|
| Create a ledger under any group | **built** | Compose, or `tally_create_ledger` |
| List ledgers with group and balance | **built** | Ledgers screen |
| Create a stock item with unit, opening qty/rate, GST, HSN | **built** | Stock screen, or `tally_create_stock_item` |
| List stock items with quantity, rate, value | **built** | Stock screen |
| Create groups, units, godowns, cost centres, currencies | passthrough | `tally_import_data` with an "All Masters" payload |
| Opening balances | **blocked** | Tally holds them; the file engine cannot decode them, so read them from a live instance |
| Alter or delete an existing master | passthrough | `tally_import_data` with `ACTION="Alter"` / `"Delete"` |

## 5. Inventory operations

| Task | Status | How |
|---|---|---|
| Stock summary with value | **built** | Reports → Stock Summary |
| Stock journal, godown transfer | passthrough | voucher import with `Stock Journal` |
| Batch-wise, expiry, reorder reports | passthrough | TDL collections |
| Physical stock adjustment | passthrough | voucher import with `Physical Stock` |
| BOM, manufacturing, job work | passthrough | TDL / import |

## 6. Reports

All of these run through the same engine call, and the raw Tally XML is always returned
alongside the parsed rows.

| Report | Status |
|---|---|
| Trial Balance | **built** |
| Profit and Loss | **built** |
| Balance Sheet | **built** (needs a live instance: opening balances) |
| Day Book | **built** (also the Day Book screen, with filters) |
| Outstanding Receivables | **built** |
| Outstanding Payables | **built** |
| Ageing Analysis | **built** |
| Stock Summary | **built** |
| Ledger Balances | **built** |
| GST Returns | **built** |
| Statistics | **built** |
| Cash / Bank Book | **built** |
| Sales register, purchase register | **built in effect** — filter the Day Book by voucher type, or `tally_list_vouchers` |
| Ledger statement for one party | **built in effect** — `tally_list_vouchers` with `party`, or the file engine's `/api/ledgers/{name}/vouchers` |
| Ratio analysis, cash flow, funds flow | passthrough (named report) |
| GSTR-1 / 3B detail, HSN summary, ITC reconciliation | passthrough (TDL) |
| Cost centre, profit centre, budget variance | passthrough (TDL) |
| Payroll, PF/ESI/gratuity/CTC | passthrough (TDL) |
| TDS/TCS computation, 26Q, 27Q, 16 | passthrough (TDL) |
| Audit trail, altered and deleted vouchers | passthrough (TDL) |
| Multi-company consolidated reporting | **not yet** — `tally_fanout`-style merging is designed but not built here |

---

## 7. What every screen has in common

- **Same engine.** The screen, the HTTP API, the copilot and MCP all call one `Engine`
  interface, so they can never disagree about the books.
- **Writes are gated.** `--allow-writes` must be set, and the UI shows *"can post to Tally"*
  or *"server is read-only"* before you start typing.
- **Nothing is faked.** Reads come from Tally or from the files; if neither answers, you get
  the real error, not a placeholder.
- **Raw XML is always one click away**, so an unsupported case is never a dead end.

## 8. Honest gaps, ranked by how often a business hits them

1. **Bank reconciliation.** Daily for most businesses. Needs statement import plus a
   matching UI. The read side already exists.
2. **The deep statutory modules** (GST return detail, TDS, payroll). Reachable through TDL,
   but an accountant will want named screens.
3. **Alter and cancel an existing voucher.** Create works; changing a posted voucher is
   passthrough only, and a real product needs it with a confirmation step.
4. **Printing and PDF.** Invoices need a printable format. Not started.
5. **Multi-company consolidation** across connections.
6. **Authentication.** Still none; the server binds `127.0.0.1` only.

---

## 9. Suggested order of work

1. **Print / PDF for invoices** — you cannot run billing without handing the customer a document.
2. **Alter and cancel voucher** with a confirm step, since corrections are constant.
3. **Bank reconciliation** — statement CSV import, auto-match, propose entries.
4. **Sales and purchase registers** as first-class reports, with party drill-down.
5. **GST return detail** (GSTR-1, 3B, HSN) as named screens — the highest-value statutory gap.
6. **Auth, then TLS**, before this touches a network.
7. **Consolidated reporting** across connections, using the multi-instance registry.
