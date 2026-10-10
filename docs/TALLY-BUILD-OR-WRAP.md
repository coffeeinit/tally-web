# Build our own engine, or wrap Tally? — decision analysis

> Follow-up question: instead of integrating, make a **full clone of TallyPrime** as a
> new Go + Vue.js web product.

---

## Verdict

**Owning an engine is right. Starting with a full clone is the wrong first move.**

The value of TallyPrime is roughly **20% interface and 80% statutory depth, accumulated edge
cases and trust**. Cloning the 20% is a few months. Cloning the 80% is a company, and it comes
with a permanent maintenance obligation that never ends.

Build the UI against an **engine-agnostic interface** with two adapters: `TallyEngine` first,
`NativeEngine` second. Ship the wrapper, grow the engine behind the same screen.

---

## 1. What "full clone of TallyPrime" actually contains

Before deciding, look at what you are signing up for:

| Area | What Tally already carries |
|---|---|
| Double entry | 30 years of posting rules, voucher classes, auto-adjustments, rounding, rounding-off ledgers |
| **GST** | GSTR-1/2B/3B/9, HSN/SAC, ITC set-off, reverse charge, composition, e-invoice (IRP), e-way bill, IMS, amendment/credit-note flows, place-of-supply rules |
| **Direct tax** | TDS/TCS nature-of-payment masters, 26Q/27Q/16, challans, lower-deduction certificates |
| **Payroll** | Pay heads, structures, attendance, PF/ESI/PT/gratuity, payslips, statutory registers |
| **Inventory** | Batches, godowns, expiry, FIFO/weighted-average/standard valuation, BOM, manufacturing, job work, stock journals, physical stock, price levels, discounts |
| **Corporate law** | Schedule III, Companies Act financial statements, audit trail (mandatory in India), MSME |
| Reporting | hundreds of reports with multi-level drill-down, ratios, cash/funds flow, cost/profit centres |
| Scale | ~2.7 million businesses, decades of data quirks, per-state variations |

And then the part nobody budgets for: **the statutory treadmill**. GST rates, forms, return
formats and e-invoice schemas change *several times a year, forever*. Tally has a team for this.
You would need one too — before you have revenue.

That is why the honest comparison is not "Tally vs our clone". It is "Tally's compliance team vs
our compliance team", and they started 30 years earlier.

---

## 2. Where the clone instinct is nonetheless correct

Three real advantages a native engine has over wrapping Tally:

1. **The API is the bottleneck.** Tally's XML/TDL interface is awkward, single-threaded, and
   version-fragile. Every non-trivial question becomes a TDL exercise. With your own schema,
   AI answers a question with SQL, not TDL.
2. **No ceiling on UX.** You control transactions, drafts, undo, approvals, real-time
   collaboration. You cannot build those on top of Tally's API.
3. **No dependency.** No TSS subscription required, no Tally version compatibility, no
   "please install Tally first", no port-9000 conversations with accountants.

Those are strong reasons to *eventually* own the engine. They are not reasons to start there.

---

## 3. The strategy that gets both: one UI, two engines

```
                 Vue 3 web UI  (one product, one codebase)
                        |
        internal accounting API  (your contract, not Tally's)
                        |
        +---------------+---------------+
        |                               |
   TallyEngine                     NativeEngine
   (XML/JSON to Tally)             (your own schema in Postgres)
   - ships in months               - grows behind the same screen
   - revenue now                   - imports Tally data
   - zero compliance liability     - compliance added incrementally
```

Sketch of the contract:

```go
type LedgerEngine interface {
    Companies(ctx) ([]Company, error)
    Ledgers(ctx, companyID) ([]Ledger, error)
    Vouchers(ctx, companyID, VoucherQuery) ([]Voucher, error)
    PostVoucher(ctx, companyID, Voucher) (VoucherID, error)
    AlterVoucher(ctx, companyID, VoucherID, Voucher) error
    TrialBalance(ctx, companyID, DateRange) ([]Balance, error)
    ProfitAndLoss(ctx, companyID, DateRange) (Report, error)
    BalanceSheet(ctx, companyID, DateRange) (Report, error)
}
```

The `tally-unified` router built earlier is already the seed of this: it already treats the
live API and the on-disk files as interchangeable providers with a policy layer. Add one more
provider name and the interface is basically there.

**Why this ordering matters:** you get paying users and real accounting feedback in months. Every
month of operating the wrapper teaches you which 20% of Tally you actually have to reimplement.
By the time you write the ledger, you are writing the *right* ledger, not the one you imagined.

---

## 4. Do not write the accounting core from scratch either

If and when you build `NativeEngine`, the double-entry + GST core is the last thing to
hand-roll. Evaluate, in this order:

1. **ERPNext + the India Compliance app** (Frappe, Python). Already implements GST, e-invoice,
   e-way bill, TDS, Schedule III, and an accounting core. Driving its API from a Vue UI is
   dramatically cheaper than cloning Tally.
2. **A Go double-entry library** for the ledger primitives, with you owning the Indian statutory
   layer.
3. **From scratch** — only if your niche genuinely needs semantics none of the above has.

Note this contradicts a pure "Go + Vue" plan: the fastest route to a credible accounting core is
probably **Vue in front, Python/Frappe or Go behind**, not a Go rewrite of a compliance engine.
Be honest about whether the Go requirement is technical or aesthetic.

---

## 5. Reframe the reverse-engineering work as a migration tool

The `.1800`/`.900` reader in TallyWeb is genuinely valuable — but its best use is
**migration and analytics**, not as the runtime backend of a product you sell.

And prefer the clean path: **migrate from Tally's own XML/JSON export**, which is documented,
supported, and customer-initiated. That sidesteps the reverse-engineering question entirely.

Use the binary reader only where the official export is weak:

- opening balances and historical years the export omits
- ERP 9 archives with no running install
- bulk analytics without touching the live server

This gives you a real GTM weapon — *"import your entire Tally history in one click"* — without
making your product's legality depend on an undocumented format. Get legal advice on the EULA
before commercialising the binary path, and keep it optional.

---

## 6. If you do build the native engine, cut hard

A credible India-GST v1 — the smallest thing an accountant would accept:

**Include:** company/FY setup; chart of accounts; ledgers and parties; Sales, Purchase, Receipt,
Payment, Journal, Contra, Credit/Debit Note; items and units; one valuation method; GST tax
ledgers and tax invoices with HSN; day book, ledger, trial balance, P&L, balance sheet,
outstanding, GST summary; audit trail; users and roles; CSV bank reconciliation; Tally import.

**Cut from v1:** payroll, TDS/TCS, manufacturing/BOM/job work, job costing, budgets and scenarios,
multi-currency, cost centres, e-invoice and e-way bill (both need IRP/GSP integration), Schedule
III, multi-branch inventory, POS.

**Rough sizing (estimate, not a promise):** a strong team of 4–6 engineers plus one practising CA
as domain owner, 9–15 months to a v1 that a small business would genuinely run its books on —
*if* you cut as above. Ongoing statutory maintenance is a permanent fraction of one engineer plus
the CA. Add e-invoicing and you add a GSP/IRP integration project.

For a *full* TallyPrime clone including inventory, manufacturing, payroll and all statutory
modules, the honest number is in the dozens of person-years plus a standing compliance team. That
is a funded-company bet, not a side project.

---

## 7. Decision table

| Situation | Recommendation |
|---|---|
| You want revenue inside 6 months | **Wrap Tally.** Full clone is not viable at that horizon. |
| You want the CA / accounting channel to trust you | Wrap first; their trust is built on Tally's arithmetic being right, which wrapping gives you free. |
| You need AI that reasons over the books properly | Build the native engine — but phase it, behind the same UI. |
| You are targeting a niche with simple needs (service businesses, freelancers, a non-India market) | A **focused** native product is feasible and a full Tally clone is unnecessary. Cut GST/inventory entirely. |
| You are targeting Indian SMEs with inventory + GST | Do not start with the clone. Consider ERPNext + India Compliance before writing anything. |
| You are funded to build a Tally competitor | Clone is defensible, but budget the compliance team and the 5-year horizon. |

---

## 8. Recommendation

1. **Now:** Vue 3 UI on an internal `LedgerEngine` contract, with `TallyEngine` as the only
   implementation. Sell it as the web workspace for Tally users. Revenue in months.
2. **Next:** add `NativeEngine` for the *subset* in section 6, even if it only runs alongside
   Tally initially. Let customers choose per company: "run on Tally" or "run on us".
3. **Then:** one-click Tally import via official XML/JSON export. This is the moment you become a
   real alternative rather than an accessory.
4. **Only after real usage** decide how much more of Tally to absorb — informed by what your
   customers actually asked for, not by Tally's feature list.

The Go + Vue instinct is fine for the *product*. Just be clear that the hard part of this business
is Indian statutory accounting depth and the eternal update treadmill, not the web framework — and
that both the wrap-first path and the ERPNext path exist to buy you time before you take that on.
