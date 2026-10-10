# Tally Web Workspace — product and technical strategy

Written after reviewing the workspace, the three installed design skills
(`oil-ui`, `draw-ui`, `oil-motion`) and Tally's current official
capabilities.

---

## 1. Verdict on the plan

Three of your four instincts are right. One needs changing.

| Your idea | Verdict |
|---|---|
| Keep Tally as the **engine**, don't rewrite accounting | **Correct.** Rewriting the ledger, GST and statutory logic is years of work and a liability. |
| Build a **web UI** rather than only a desktop add-on | **Correct.** That is where the UX freedom is. |
| Users still on desktop Tally must keep working | **Correct, and it constrains the design.** Two-way coexistence is a hard requirement, not a nice-to-have. |
| Sell it as a **"UI plugin for Tally"** | **Change this.** Tally's plug-in channel (Release 5.0+) puts you *inside Tally's own window*, using Tally's chrome and Tally's release cycle. You cannot be "better than Tally's UI" while rendering inside Tally's UI. |

**Recommended framing:** sell a **separate web workspace that is powered by the customer's own
Tally** (XML/JSON API). The customer already owns Tally + TSS; you add the experience Tally does
not provide. That is a different product from a plug-in, and a much better one.

---

## 2. What changed in 2026 — check this before committing

This matters, because "web UI for Tally" and "AI for Tally" are no longer empty categories.

| Fact | Source |
|---|---|
| **TallyPrime 7.0+ has native JSON integration**, alongside XML. Tally can act as **server** (your app pushes/pulls JSON) *and* as **client** (Tally sends GET/POST to *your* web service). Native JSON needs no TDL. | [JSON integration](https://help.tallysolutions.com/tally-prime-integration-using-json-1/) |
| **TallyPrime 7.1** (launched 27 Jun 2026) is marketed as "AI-first". Connected Banking now covers ICICI, Axis, Kotak, SBI. | [7.1 update](https://tallysolutions.com/darpan/product-update-tallyprime-7-1/) |
| **TallyIra / Docs by Ira** — official AI invoice processing, "up to 80% less manual data entry". Requires active TSS, **TallyPrime 7.1+**, **India only**. | [TallyIra](https://tallysolutions.com/tally-ira/) |
| **Reports on Browser** and **mobile access** already exist — read-only reports, subscription. | [Browser FAQ](https://tallysolutions.com/tally/faqs-tally-business-reports-on-browser/) |
| **Plug-ins / Add-ons** are baked into TallyPrime from Release 5.0 (Schedule III, Amazon import, Bharat Connect, TallyCapital). | [TallyHelp](https://help.tallysolutions.com/tdls-and-add-ons-for-tallyprime/) |
| **TallyPrime API Explorer** — official sandbox for XML and JSON requests. | [API Explorer](https://tallysolutions.com/tallyprime-api-explorer/) |
| Tally has a **Developer Hub**, a **partner programme**, and **TallyPrime Server** for concurrent access. | tallysolutions.com |

**Read:** Tally owns *reports on the web* and *document-to-voucher AI*. If your pitch is
"Tally in a browser" or "AI that reads invoices", you are late and you will be undercut.
The good news: Tally's browser product is **reports-only**, its AI is **OCR-to-voucher only**, its
plug-ins extend rather than replace, and **ERP 9 users get none of it**.

---

## 3. Where the real gap is

Four things Tally structurally does not sell, and probably will not:

1. **Multi-instance operations.** A CA firm or a group runs 20–200 Tally companies. Tally's model
   is one company at a time on one desktop. There is no good console.
2. **Data entry on the web.** Tally's browser story is read-only. A real web workplace with
   voucher entry, approvals and maker-checker does not exist.
3. **Cross-version.** Prime and ERP 9 side by side, or an ERP 9 archive next to live Prime. Tally
   has no incentive to make ERP 9 pleasant.
4. **AI that audits rather than types.** TallyIra enters documents. Nobody sells
   reconciliation, mismatch detection, anomaly sampling and month-end close assistance.

The instance registry already built in this workspace is precisely the missing primitive for (1)
and (3).

---

## 4. Pick one wedge

| Option | Appeal | Risk |
|---|---|---|
| **A. Single-company web client** ("better Tally for one business") | Easy to explain, big market | You compete with desktop Tally's keyboard speed and with Tally's own cloud. Weakest moat. |
| **B. Multi-client console for CA firms / groups** | Nobody serves it. High willingness to pay. Natural per-client pricing. Uses the multi-instance core you already have. | Sales cycle needs trust; support burden across many client setups. |
| **C. AI bookkeeping layer for Prime *and* ERP 9** | Felt daily, demoes well, differentiates | Standalone AI is hard to sell; better bundled. |

**Recommendation: B as the product, C inside it, A as the free entry point that upsells into B.**

Concretely: *"One login. All your clients' Tally companies. Review, enter, reconcile, approve —
on the web, with an AI that does the checking."* Sell to CA firms and multi-branch businesses.

---

## 5. Architecture

```
Browser (React/Next SPA — keyboard-first, dense, no page reloads)
   |  HTTPS
   v
BFF / API gateway  <-- auth, RBAC, audit log, rate limit, ONE SERIAL QUEUE PER TALLY INSTANCE
   |                                        |
   | official Tally API                     | optional, read-only, local
   v                                        v
TallyPrime 7.x (JSON if >= 7.0,            local file reader / mirror DB
XML fallback) or Tally ERP 9 (XML)         (bulk + analytical reports, never writes)
```

Non-negotiables:

1. **Never expose Tally's API port to the internet.** The XML/JSON server has no authentication.
   The BFF lives next to Tally; only the BFF is exposed.
2. **One serial queue per Tally instance.** Tally's server is single-threaded; concurrent requests
   freeze the accountant's desktop. The `tally-unified` router already enforces this.
3. **Prefer JSON on 7.0+, fall back to XML.** One more provider behind the same router — the
   version problem is already solved there.
4. **All writes go through Tally's API.** Never write `.1800`/`.900` in production.
   That keeps GST/statutory validity and Tally's own audit trail intact.
5. **Heavy reads come from a mirror, not from Tally.** Nightly/incremental ingest into a local DB
   (the file reader is genuinely good at this) keeps reports instant and Tally idle.
6. **Run a dedicated Tally instance for the web app** — ideally TallyPrime Server — so a user's
   desktop session never blocks the web.
7. **Company switch per request** via `SVCURRENTCOMPANY`. Never assume "the active company".
8. **Queue and replay writes** when Tally is down; show connection state honestly.

Packaging: ship a **Tauri or Electron installer** that runs the BFF as a Windows service, manages
the Tally connection, and opens the web UI. That is how you get the "install it and it just works
on the Tally PC" feel without giving up a real web app.

---

## 6. Being genuinely better than Tally's UI (this is the hard part)

Tally users are **keyboard-first power typists**. A pretty UI that needs a mouse is *slower*, and
slower gets rejected regardless of how good it looks. Table stakes before any delight:

- Full keyboard navigation and voucher entry; a deliberate accelerator map that respects the
  muscle memory accountants already have (date, create, alter, print).
- Tally's exact vocabulary — voucher types, ledger groups, GST wording. Never invent synonyms.
- Instant search, no page reloads, tolerant of a slow Tally, offline-draft capable.

Where you can actually win:

- **Search across everything** (Tally's search is weak).
- **Real multi-company switching and consolidated views.**
- **Draft vouchers, undo, and maker-checker approvals** — Tally has none of this.
- **Proper tables**: sortable, filterable, virtualised, one-click export.
- **Explainable reports**: drill summary to voucher to source document.
- **Mobile read + approve.**
- **AI at the point of work**, not in a separate tool.

Do **not** clone every Tally screen. Replace the 20% that hurts: common voucher entry, master
creation, bank reconciliation, GST recon, reports, approvals, exports.

---

## 7. AI roadmap, ranked by what customers feel

1. **Document to voucher for Prime *and* ERP 9** + WhatsApp/email intake. (TallyIra covers Prime
   7.1+ only; ERP 9 is a large unserved base.)
2. **GST reconciliation**: GSTR-2B vs purchase register, ITC mismatch list, action items. This is
   the single most painful recurring accountant task.
3. **Bank reconciliation assistant** — fetch statement, match, propose entries.
4. **Anomaly / audit assistant for CAs**: duplicate bills, round numbers, missing GSTIN,
   expense spikes, sampling and working papers.
5. **Natural-language reporting**: "top 10 overdue debtors above 60 days, Maharashtra, this FY"
   to chart/export. The MCP layer in this workspace makes this nearly free.
6. **Month-end close assistant**: what is unreconciled, pending, or out of balance, with links.
7. **WhatsApp approvals** for vouchers/POs.
8. **ERP 9 to Prime migration verification** by diffing both through the unified reader.

---

## 8. Business model

| Model | Notes |
|---|---|
| Freemium | Free read-only reports for one company |
| **Per client-company per month** (CA firms) | The natural metric; they already think this way. ₹299–999/client by tier |
| Per seat | Multi-branch businesses |
| Add-on packs | GST recon, Audit pack, WhatsApp pack |
| Channel | CA firms refer; Tally partners resell |

Rough shape to validate: 200 CA firms x 15 clients x ₹499 = about ₹15 lakh MRR. That is a real
business and it does not require competing with Tally on data entry.

---

## 9. How the three skills fit

They are **design-methodology skills, not component libraries** — they will not give you an
accounting-grade data grid or a keyboard system. Budget for building those.

| Skill | Use it for | Don't use it for |
|---|---|---|
| `oil-ui` | Three genuinely different design directions for the voucher-entry and company-list screens; its side-by-side comparison page, reviewed as real screenshots at desktop and phone widths | Choosing your architecture |
| `draw-ui` | Mockup to implementable component specs, with a clean code-vs-image-asset split | Business data validation |
| `oil-motion` | Marketing site / onboarding hero only | Inside the ERP screens — its own docs say interface transitions and micro-interactions belong in regular front-end work |

Caveats from their own documentation: scripts are **tested on macOS**; Windows has PowerShell
entry points but is **not verified on real machines**; they need **Python 3.10+**; oil-motion
generation needs a credential profile. oil-ui and draw-ui are MIT; oil-ui Pro is a paid upgrade
(¥69, one-time) and must not be installed alongside oil-ui.

---

## 10. Risks, honestly

| Risk | Severity | Mitigation |
|---|---|---|
| Tally ships your feature (browser entry, AI review) | **High** | Stay in the cross-company / cross-version / workflow lane; Tally's incentive is inside its own product |
| Legal exposure from the undocumented `.1800`/`.900` format | **High** | Build the product on documented APIs; keep file parsing read-only, local, optional; get legal review before commercialising |
| TSS dependency — API access typically needs an active subscription | Medium | Detect and message clearly; sell to firms that already keep TSS current |
| Single-threaded engine limits concurrency | Medium | One Tally per company; TallyPrime Server; read-heavy traffic from the mirror |
| ERP 9 is a sunset platform | Medium | Treat as archive/read path, not the core. Its users are a wedge, not the foundation |
| Support burden (Windows, LAN, firewall, versions) | Medium | Ship an installer; price for support |
| Design skills don't cover grids/forms/keyboard | Medium | Build a serious component layer first |
| The TallyWeb frontend is empty | Low | Expect zero UI head start |

---

## 11. First two weeks

1. Decide the wedge. If in doubt: **CA-firm multi-client console**.
2. Stand up **TallyPrime 7.1 + TSS** on a Windows box; enable XML and JSON; use TallyPrime Server
   if available. Point `tally-unified` at it and prove **read + one write**
   (create a real voucher) end to end.
3. Run **oil-ui** on two screens only — **voucher entry** and **company list**. Those two decide
   whether the product feels better or worse than Tally.
4. Build one vertical slice: login to company list to day book to open voucher to save to report.
   Nothing else.
5. Add one AI feature that demos in 30 seconds (natural-language overdue debtors, or GST recon).
6. Only then decide packaging and pricing.
