package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"tallywebui/internal/engine"
)

// Toolbox binds the assistant to the same Engine interface the UI uses, so the
// AI can only ever do what a user could do from a screen.
type Toolbox struct {
	Registry    *engine.Registry
	Instance    string
	Company     string
	AllowWrites bool
	MaxRows     int
}

func (t *Toolbox) maxRows() int {
	if t.MaxRows <= 0 {
		return 80
	}
	return t.MaxRows
}

func (t *Toolbox) reports() []string {
	return []string{
		"trial_balance", "profit_loss", "balance_sheet", "day_book",
		"outstanding", "ageing", "stock_summary", "ledger_balances", "gst_returns",
	}
}

func str(schema map[string]any) map[string]any { return schema }

// Specs is what the model is allowed to call.
func (t *Toolbox) Specs() []ToolSpec {
	companyProp := map[string]any{
		"type":        "string",
		"description": "Company name. Defaults to the company selected in the UI.",
	}
	return []ToolSpec{
		{
			Name:        "describe_context",
			Description: "Describe the connected Tally instance, the selected company and available reports. Call this first if unsure what is connected.",
			Schema:      str(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "list_companies",
			Description: "List the companies (sets of books) available on the connected Tally instance.",
			Schema:      str(map[string]any{"type": "object", "properties": map[string]any{}}),
		},
		{
			Name:        "list_ledgers",
			Description: "List ledger accounts with their parent group and closing balance. Use search to find one account.",
			Schema: str(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"company": companyProp,
					"search":  map[string]any{"type": "string", "description": "Case-insensitive substring to match against the ledger name or group."},
					"limit":   map[string]any{"type": "integer", "description": "Maximum rows."},
				},
			}),
		},
		{
			Name:        "list_vouchers",
			Description: "List vouchers in a date range with optional filters. Dates are YYYY-MM-DD.",
			Schema: str(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"company": companyProp,
					"type":    map[string]any{"type": "string", "description": "Sales, Purchase, Payment, Receipt, Journal, Contra, Credit Note, Debit Note."},
					"from":    map[string]any{"type": "string", "description": "Start date, YYYY-MM-DD."},
					"to":      map[string]any{"type": "string", "description": "End date, YYYY-MM-DD."},
					"party":   map[string]any{"type": "string", "description": "Substring of the party ledger name."},
					"limit":   map[string]any{"type": "integer", "description": "Maximum rows."},
				},
			}),
		},
		{
			Name:        "get_report",
			Description: "Run a Tally report and return its rows. Prefer this over guessing figures.",
			Schema: str(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"report":  map[string]any{"type": "string", "enum": t.reports(), "description": "Which report to run."},
					"company": companyProp,
					"from":    map[string]any{"type": "string", "description": "Start date, YYYY-MM-DD."},
					"to":      map[string]any{"type": "string", "description": "End date, YYYY-MM-DD."},
				},
				"required": []string{"report"},
			}),
		},
		{
			Name:        "create_ledger",
			Description: "Create an accounting ledger in the connected company. This WRITES to the books and only works when the user has enabled writes.",
			Schema: str(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":    map[string]any{"type": "string", "description": "Unique ledger name."},
					"parent":  map[string]any{"type": "string", "description": "Parent group, for example Sundry Debtors, Bank Accounts, Indirect Expenses."},
					"company": companyProp,
				},
				"required": []string{"name", "parent"},
			}),
		},
		{
			Name:        "list_stock_items",
			Description: "List inventory masters with unit, closing quantity, rate and value. Look items up here before raising an invoice.",
			Schema: str(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"company": companyProp,
					"search":  map[string]any{"type": "string", "description": "Case-insensitive substring matched against the item name or its group."},
					"limit":   map[string]any{"type": "integer", "description": "Maximum rows."},
				},
			}),
		},
		{
			Description: "Create a voucher. This WRITES to the books and only works when the user has enabled writes. Tally convention: a debit entry carries a negative amount, which this tool handles for you.",
			Schema: str(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"type":      map[string]any{"type": "string", "description": "Sales, Purchase, Payment, Receipt, Journal, Contra."},
					"date":      map[string]any{"type": "string", "description": "Voucher date, YYYY-MM-DD."},
					"party":     map[string]any{"type": "string", "description": "Party ledger name."},
					"narration": map[string]any{"type": "string"},
					"company":   companyProp,
					"items": map[string]any{
						"type":        "array",
						"description": "Inventory lines for invoices and returns. Each line is allocated to a sales or purchase ledger. Omit for a plain accounting voucher such as a payment.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"stock_item": map[string]any{"type": "string"},
								"quantity":   map[string]any{"type": "number"},
								"rate":       map[string]any{"type": "number"},
								"unit":       map[string]any{"type": "string"},
								"ledger":     map[string]any{"type": "string", "description": "Sales or purchase ledger this line posts to."},
							},
							"required": []string{"stock_item", "quantity", "rate"},
						},
					},
					"entries": map[string]any{
						"type":        "array",
						"description": "Ledger entries. Debits and credits must balance.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"ledger":   map[string]any{"type": "string"},
								"amount":   map[string]any{"type": "number"},
								"is_debit": map[string]any{"type": "boolean", "description": "true for a debit entry."},
							},
							"required": []string{"ledger", "amount", "is_debit"},
						},
					},
				},
				"required": []string{"type", "date", "entries"},
			}),
		},
	}
}

// Execute runs one tool call and returns a compact JSON result for the model.
func (t *Toolbox) Execute(ctx context.Context, name, argsJSON string) (string, error) {
	args := map[string]any{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("arguments were not valid JSON: %w", err)
		}
	}
	eng, err := t.Registry.Resolve(t.Instance)
	if err != nil {
		return "", err
	}
	company := t.pick(args, "company", t.Company)

	switch name {
	case "describe_context":
		companies, cerr := eng.Companies(ctx)
		names := make([]string, 0, len(companies))
		for _, c := range companies {
			names = append(names, c.Name)
		}
		return jsonResult(map[string]any{
			"instance":            eng.ID(),
			"engine_kind":         eng.Kind(),
			"product":             eng.Product(),
			"health":              eng.Health(ctx),
			"selected_company":    company,
			"available_companies": names,
			"available_reports":   t.reports(),
			"writes_enabled":      t.AllowWrites,
			"companies_error":     errText(cerr),
		})

	case "list_companies":
		companies, err := eng.Companies(ctx)
		if err != nil {
			return "", err
		}
		return jsonResult(map[string]any{"companies": companies})

	case "list_ledgers":
		ledgers, err := eng.Ledgers(ctx, company)
		if err != nil {
			return "", err
		}
		search := strings.ToLower(strArg(args, "search"))
		var out []engine.Ledger
		for _, l := range ledgers {
			if search != "" && !strings.Contains(strings.ToLower(l.Name), search) && !strings.Contains(strings.ToLower(l.Parent), search) {
				continue
			}
			out = append(out, l)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		truncated := len(out) > t.maxRows()
		if truncated {
			out = out[:t.maxRows()]
		}
		return jsonResult(map[string]any{"company": company, "count": len(out), "truncated": truncated, "ledgers": out})

	case "list_vouchers":
		limit := intArg(args, "limit", t.maxRows())
		vouchers, err := eng.Vouchers(ctx, company, engine.Query{
			Type:  strArg(args, "type"),
			From:  strArg(args, "from"),
			To:    strArg(args, "to"),
			Party: strArg(args, "party"),
			Limit: limit,
		})
		if err != nil {
			return "", err
		}
		return jsonResult(map[string]any{"company": company, "count": len(vouchers), "vouchers": vouchers})

	case "get_report":
		report := strArg(args, "report")
		if report == "" {
			return "", fmt.Errorf("report is required")
		}
		rep, err := eng.NamedReport(ctx, company, report, engine.Query{
			From: strArg(args, "from"),
			To:   strArg(args, "to"),
		})
		if err != nil {
			return "", err
		}
		rows := rep.Rows
		truncated := len(rows) > t.maxRows()
		if truncated {
			rows = rows[:t.maxRows()]
		}
		return jsonResult(map[string]any{
			"company": company, "report": rep.Name, "note": rep.Note,
			"count": len(rows), "truncated": truncated, "rows": rows,
		})

	case "create_ledger":
		if !t.AllowWrites {
			return "", fmt.Errorf("writes are disabled; ask the user to turn on 'Allow the assistant to write' in AI settings")
		}
		res, err := eng.CreateLedger(ctx, company, engine.LedgerInput{
			Name:   strArg(args, "name"),
			Parent: strArg(args, "parent"),
		})
		if err != nil {
			return jsonResult(map[string]any{"ok": false, "error": err.Error()})
		}
		return jsonResult(res)

	case "list_stock_items":
		items, err := eng.StockItems(ctx, company)
		if err != nil {
			return "", err
		}
		needle := strings.ToLower(strArg(args, "search"))
		var kept []engine.StockItem
		for _, item := range items {
			if needle != "" && !strings.Contains(strings.ToLower(item.Name), needle) && !strings.Contains(strings.ToLower(item.Parent), needle) {
				continue
			}
			kept = append(kept, item)
		}
		if len(kept) > t.maxRows() {
			kept = kept[:t.maxRows()]
		}
		return jsonResult(map[string]any{"company": company, "count": len(kept), "stock_items": kept})

	case "create_voucher":
		if !t.AllowWrites {
			return "", fmt.Errorf("writes are disabled; ask the user to turn on 'Allow the assistant to write' in AI settings")
		}
		in := engine.VoucherInput{
			Type:      strArg(args, "type"),
			Date:      strArg(args, "date"),
			Party:     strArg(args, "party"),
			Narration: strArg(args, "narration"),
		}
		for _, raw := range sliceArg(args, "items") {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			in.Items = append(in.Items, engine.ItemLine{
				StockItem: strArg(item, "stock_item"),
				Quantity:  numArg(item, "quantity"),
				Rate:      numArg(item, "rate"),
				Unit:      strArg(item, "unit"),
				Ledger:    strArg(item, "ledger"),
			})
		}
		for _, raw := range sliceArg(args, "entries") {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			in.Entries = append(in.Entries, engine.Entry{
				Ledger:  strArg(entry, "ledger"),
				Amount:  numArg(entry, "amount"),
				IsDebit: boolArg(entry, "is_debit"),
			})
		}
		res, err := eng.CreateVoucher(ctx, company, in)
		if err != nil {
			return jsonResult(map[string]any{"ok": false, "error": err.Error()})
		}
		return jsonResult(res)

	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func (t *Toolbox) pick(args map[string]any, key, fallback string) string {
	if v := strArg(args, key); v != "" {
		return v
	}
	return fallback
}

func jsonResult(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func strArg(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func numArg(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case float64:
			return t
		case int:
			return float64(t)
		case json.Number:
			f, _ := t.Float64()
			return f
		}
	}
	return 0
}

func intArg(m map[string]any, key string, def int) int {
	if v := numArg(m, key); v > 0 {
		return int(v)
	}
	return def
}

func boolArg(m map[string]any, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
		if s, ok := v.(string); ok {
			return strings.EqualFold(s, "true") || s == "1"
		}
	}
	return false
}

func sliceArg(m map[string]any, key string) []any {
	if v, ok := m[key]; ok {
		if s, ok := v.([]any); ok {
			return s
		}
	}
	return nil
}