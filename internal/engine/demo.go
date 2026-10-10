package engine

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// DemoEngine is an in-memory company that makes the whole product runnable
// and testable without a Tally installation. It is also the fixture the test
// suite and any future engineering work uses, so it is deterministic.
type DemoEngine struct{}

func NewDemoEngine() *DemoEngine { return &DemoEngine{} }

func (e *DemoEngine) ID() string      { return "demo" }
func (e *DemoEngine) Kind() string    { return "demo" }
func (e *DemoEngine) Product() string { return "demo" }

func (e *DemoEngine) Health(ctx context.Context) Health {
	return Health{OK: true, Product: "demo", Address: "in-memory", Message: "sample company (no Tally required)"}
}

func (e *DemoEngine) Companies(ctx context.Context) ([]Company, error) {
	return []Company{
		{ID: "demo", Name: "Demo Traders Pvt Ltd (2025-26)", Folder: "demo"},
		{ID: "demo-archive", Name: "Demo Traders (2024-25) - archive", Folder: "demo-archive"},
	}, nil
}

type demoLedger struct {
	name, parent string
	opening      float64
}

var demoLedgers = []demoLedger{
	{"Capital Account", "Capital Account", -2500000},
	{"HDFC Bank", "Bank Accounts", 812340.55},
	{"Cash-in-Hand", "Cash-in-Hand", 42180},
	{"Sundry Debtors", "Sundry Debtors", 0},
	{"Sharma Enterprises", "Sundry Debtors", -184500},
	{"Krishna Traders", "Sundry Debtors", -96250},
	{"Mehta & Sons", "Sundry Debtors", -231780},
	{"Sundry Creditors", "Sundry Creditors", 0},
	{"Ganesh Suppliers", "Sundry Creditors", 152300},
	{"Bharat Hardware", "Sundry Creditors", 88420},
	{"Sales Accounts", "Sales Accounts", 0},
	{"Local Sales", "Sales Accounts", -1245000},
	{"Export Sales", "Sales Accounts", -486200},
	{"Purchase Accounts", "Purchase Accounts", 0},
	{"Local Purchases", "Purchase Accounts", 812400},
	{"CGST", "Duties & Taxes", 24180},
	{"SGST", "Duties & Taxes", 24180},
	{"IGST", "Duties & Taxes", 0},
	{"Salary & Wages", "Indirect Expenses", 328000},
	{"Rent", "Indirect Expenses", 96000},
	{"Electricity", "Indirect Expenses", 21450},
	{"Furniture & Fixtures", "Fixed Assets", 340000},
	{"Computers", "Fixed Assets", 187500},
}

func (e *DemoEngine) Ledgers(ctx context.Context, company string) ([]Ledger, error) {
	out := make([]Ledger, 0, len(demoLedgers))
	for _, l := range demoLedgers {
		out = append(out, Ledger{
			Name:           l.name,
			Parent:         l.parent,
			OpeningBalance: l.opening,
			ClosingBalance: l.opening,
		})
	}
	return out, nil
}

type demoVoucher struct {
	date, vtype, number, party, narration string
	amount                                float64
}

var demoVouchers = func() []demoVoucher {
	base := []demoVoucher{
		{"2025-04-03", "Sales", "DT/0001", "Sharma Enterprises", "Tax invoice - cement bags", 118000},
		{"2025-04-11", "Receipt", "DT/0002", "Sharma Enterprises", "NEFT received against DT/0001", 118000},
		{"2025-04-18", "Purchase", "DT/0003", "Ganesh Suppliers", "Purchase - hardware", 94400},
		{"2025-04-25", "Payment", "DT/0004", "Ganesh Suppliers", "RTGS paid", 94400},
		{"2025-05-02", "Sales", "DT/0005", "Krishna Traders", "Tax invoice - fittings", 56200},
		{"2025-05-09", "Sales", "DT/0006", "Mehta & Sons", "Tax invoice - bulk order", 236400},
		{"2025-05-15", "Journal", "DT/0007", "", "Depreciation on computers", 12500},
		{"2025-05-21", "Purchase", "DT/0008", "Bharat Hardware", "Purchase - raw material", 143800},
		{"2025-06-01", "Contra", "DT/0009", "", "Cash deposited into HDFC Bank", 60000},
		{"2025-06-07", "Sales", "DT/0010", "Sharma Enterprises", "Tax invoice - repeat order", 87400},
		{"2025-06-14", "Payment", "DT/0011", "Bharat Hardware", "Part payment", 50000},
		{"2025-06-22", "Receipt", "DT/0012", "Krishna Traders", "UPI received", 56200},
		{"2025-07-05", "Sales", "DT/0013", "Mehta & Sons", "Export invoice", 198600},
		{"2025-07-19", "Purchase", "DT/0014", "Ganesh Suppliers", "Purchase - consumables", 41200},
		{"2025-08-02", "Sales", "DT/0015", "Krishna Traders", "Tax invoice", 74100},
		{"2025-08-16", "Journal", "DT/0016", "", "Provision for salary", 82000},
	}
	return base
}()

func (e *DemoEngine) Vouchers(ctx context.Context, company string, q Query) ([]Voucher, error) {
	out := make([]Voucher, 0, len(demoVouchers))
	for _, v := range demoVouchers {
		if q.Type != "" && !strings.EqualFold(q.Type, v.vtype) {
			continue
		}
		if !inDateRange(v.date, q.From, q.To) {
			continue
		}
		if q.Party != "" && !strings.Contains(strings.ToLower(v.party), strings.ToLower(q.Party)) {
			continue
		}
		out = append(out, Voucher{
			ID:        v.number,
			Number:    v.number,
			Type:      v.vtype,
			Date:      v.date,
			Party:     v.party,
			Narration: v.narration,
			Amount:    v.amount,
		})
	}
	sortVouchersDesc(out)
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (e *DemoEngine) NamedReport(ctx context.Context, company, name string, q Query) (Report, error) {
	ledgers, _ := e.Ledgers(ctx, company)
	rep := Report{Name: name, Note: "Demo data."}

	switch name {
	case "trial_balance", "ledger_balances":
		var rows []map[string]string
		for _, l := range ledgers {
			dr, cr := 0.0, 0.0
			if l.ClosingBalance >= 0 {
				dr = l.ClosingBalance
			} else {
				cr = math.Abs(l.ClosingBalance)
			}
			rows = append(rows, map[string]string{
				"LEDGER": l.Name, "GROUP": l.Parent,
				"DEBIT":  fmt.Sprintf("%.2f", dr),
				"CREDIT": fmt.Sprintf("%.2f", cr),
			})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i]["LEDGER"] < rows[j]["LEDGER"] })
		rep.Rows = rows
		return rep, nil

	case "balance_sheet", "profit_loss":
		byGroup := map[string]float64{}
		for _, l := range ledgers {
			byGroup[l.Parent] += l.ClosingBalance
		}
		var rows []map[string]string
		for g, v := range byGroup {
			rows = append(rows, map[string]string{"GROUP": g, "AMOUNT": fmt.Sprintf("%.2f", v)})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i]["GROUP"] < rows[j]["GROUP"] })
		rep.Rows = rows
		rep.Note = "Demo aggregation by group."
		return rep, nil

	case "day_book":
		vouchers, _ := e.Vouchers(ctx, company, q)
		var rows []map[string]string
		for _, v := range vouchers {
			rows = append(rows, map[string]string{
				"DATE": v.Date, "TYPE": v.Type, "NUMBER": v.Number,
				"PARTY": v.Party, "AMOUNT": fmt.Sprintf("%.2f", v.Amount),
			})
		}
		rep.Rows = rows
		return rep, nil

	case "outstanding", "ageing":
		var rows []map[string]string
		for _, l := range ledgers {
			if l.Parent != "Sundry Debtors" && l.Parent != "Sundry Creditors" {
				continue
			}
			rows = append(rows, map[string]string{
				"PARTY": l.Name, "GROUP": l.Parent,
				"BALANCE": fmt.Sprintf("%.2f", math.Abs(l.ClosingBalance)),
				"DAYS":    "45",
			})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i]["PARTY"] < rows[j]["PARTY"] })
		rep.Rows = rows
		return rep, nil

	case "stock_summary":
		rep.Rows = []map[string]string{
			{"ITEM": "Cement Bags 50kg", "GODOWN": "Main", "CLOSING": "412", "VALUE": "247200.00"},
			{"ITEM": "Steel Rods 12mm", "GODOWN": "Main", "CLOSING": "88", "VALUE": "184800.00"},
			{"ITEM": "Paint Bucket 20L", "GODOWN": "Main", "CLOSING": "34", "VALUE": "61200.00"},
		}
		return rep, nil

	default:
		rep.Rows = []map[string]string{{"INFO": "The demo engine does not implement " + name}}
		return rep, nil
	}
}

func (e *DemoEngine) CreateLedger(ctx context.Context, company string, in LedgerInput) (WriteResult, error) {
	if strings.TrimSpace(in.Name) == "" {
		return WriteResult{}, fmt.Errorf("ledger name is required")
	}
	return WriteResult{OK: true, Detail: "demo mode: ledger " + in.Name + " accepted but not persisted"}, nil
}

func (e *DemoEngine) CreateVoucher(ctx context.Context, company string, in VoucherInput) (WriteResult, error) {
	if len(in.Entries) == 0 {
		return WriteResult{}, fmt.Errorf("at least one ledger entry is required")
	}
	detail := fmt.Sprintf("demo mode: %s voucher with %d ledger entries", in.Type, len(in.Entries))
	if len(in.Items) > 0 {
		detail += fmt.Sprintf(" and %d inventory line(s)", len(in.Items))
	}
	return WriteResult{OK: true, Detail: detail + " accepted but not persisted"}, nil
}

func (e *DemoEngine) RawXML(ctx context.Context, company, body string) (string, error) {
	return "<ENVELOPE><BODY><DATA>demo engine ignores raw XML (" +
		fmt.Sprintf("%d chars", len(body)) + ")</DATA></BODY></ENVELOPE>", nil
}

var _ = time.Now

// ── Tally control ──────────────────────────────────────────────────────────
//
// The demo engine has no Tally behind it, so it echoes rather than pretending.

func (e *DemoEngine) QueryCollection(ctx context.Context, company, name, tdl string, vars map[string]string) (string, error) {
	return "<ENVELOPE><BODY><DATA>demo engine received collection " + name + "</DATA></BODY></ENVELOPE>", nil
}

func (e *DemoEngine) EvaluateFunction(ctx context.Context, company, expression string) (string, error) {
	return "<ENVELOPE><BODY><DATA>demo engine received expression</DATA></BODY></ENVELOPE>", nil
}

func (e *DemoEngine) ExportObject(ctx context.Context, company, subtype, objectID string, fields []string) (string, error) {
	return "<ENVELOPE><BODY><DATA>demo engine received " + subtype + " " + objectID + "</DATA></BODY></ENVELOPE>", nil
}

func (e *DemoEngine) ExecuteAction(ctx context.Context, company, actionID string) (string, error) {
	return "<ENVELOPE><BODY><DATA>demo engine received action " + actionID + "</DATA></BODY></ENVELOPE>", nil
}

func (e *DemoEngine) ImportData(ctx context.Context, company, reportName, data string) (string, error) {
	return "", fmt.Errorf("the demo engine does not import; switch to a real connection")
}

// ── inventory ──────────────────────────────────────────────────────────────

var demoStockItems = []StockItem{
	{Name: "Cement Bags 50kg", Parent: "Building Material", BaseUnit: "Nos", ClosingQty: 412, ClosingRate: 600, ClosingValue: 247200, GSTRate: 18, HSNCode: "2523"},
	{Name: "Steel Rods 12mm", Parent: "Building Material", BaseUnit: "Kgs", ClosingQty: 88, ClosingRate: 2100, ClosingValue: 184800, GSTRate: 18, HSNCode: "7214"},
	{Name: "Paint Bucket 20L", Parent: "Finishes", BaseUnit: "Nos", ClosingQty: 34, ClosingRate: 1800, ClosingValue: 61200, GSTRate: 18, HSNCode: "3208"},
	{Name: "PVC Pipe 4in", Parent: "Plumbing", BaseUnit: "Nos", ClosingQty: 260, ClosingRate: 320, ClosingValue: 83200, GSTRate: 18, HSNCode: "3917"},
	{Name: "Wall Putty 40kg", Parent: "Finishes", BaseUnit: "Bags", ClosingQty: 96, ClosingRate: 950, ClosingValue: 91200, GSTRate: 18, HSNCode: "3214"},
}

func (e *DemoEngine) StockItems(ctx context.Context, company string) ([]StockItem, error) {
	out := make([]StockItem, 0, len(demoStockItems))
	for _, item := range demoStockItems {
		out = append(out, item)
	}
	return out, nil
}

func (e *DemoEngine) CreateStockItem(ctx context.Context, company string, in StockItemInput) (WriteResult, error) {
	if strings.TrimSpace(in.Name) == "" {
		return WriteResult{}, fmt.Errorf("stock item name is required")
	}
	return WriteResult{OK: true, Detail: "demo mode: stock item " + in.Name + " accepted but not persisted"}, nil
}
