package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"tallywebui/internal/engine"
)

const defaultMaxRows = 100

// reportTools maps a friendly tool name to the Tally report it runs.
var reportTools = []struct {
	Tool   string
	Report string
	About  string
}{
	{"tally_trial_balance", "Trial Balance", "Trial balance: every ledger with its debit or credit balance."},
	{"tally_profit_and_loss", "Profit and Loss A/c", "Profit and loss account."},
	{"tally_balance_sheet", "Balance Sheet", "Balance sheet. Needs a live Tally instance: the file engine cannot decode opening balances."},
	{"tally_day_book", "Day Book", "Day book: all vouchers in a date range."},
	{"tally_outstanding", "Outstanding Receivables", "Outstanding receivables by party."},
	{"tally_payables", "Outstanding Payables", "Outstanding payables by party."},
	{"tally_ageing", "Ageing Analysis", "Ageing analysis of outstanding balances."},
	{"tally_stock_summary", "Stock Summary", "Stock summary with quantities and values."},
	{"tally_ledger_balances", "Ledger Balances", "Ledger balances at a glance."},
	{"tally_gst_returns", "GST Returns", "GST return data as Tally holds it."},
	{"tally_statistics", "Statistics", "Tally statistics for the company."},
	{"tally_cash_bank_book", "Cash/Bank Book", "Cash and bank book."},
}

type tallyTools struct {
	reg         *engine.Registry
	allowWrites bool
}

// RegisterTallyTools binds every Tally operation to the MCP server. The tools
// call exactly the same engine the web UI calls, so the two can never diverge.
func RegisterTallyTools(s *Server, reg *engine.Registry, allowWrites bool) {
	t := &tallyTools{reg: reg, allowWrites: allowWrites}

	s.Add(Tool{
		Name:        "tally_list_instances",
		Description: "List every configured Tally connection with its kind, product, address and live reachability.",
		Schema:      objectSchema(map[string]any{}, nil),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return map[string]any{
				"default":   reg.Default(),
				"instances": reg.List(ctx),
			}, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_describe",
		Description: "Describe the active connection: product, engine kind, reachability, available companies and available reports. Call this first when unsure what is connected.",
		Schema:      objectSchema(map[string]any{"instance": instanceProp()}, nil),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			companies, cerr := eng.Companies(ctx)
			names := make([]string, 0, len(companies))
			for _, c := range companies {
				names = append(names, c.Name)
			}
			out := map[string]any{
				"instance":            eng.ID(),
				"engine_kind":         eng.Kind(),
				"product":             eng.Product(),
				"health":              eng.Health(ctx),
				"selected_company":    company,
				"available_companies": names,
				"available_reports":   reports(),
				"writes_enabled":      t.allowWrites,
			}
			if cerr != nil {
				out["companies_error"] = cerr.Error()
			}
			return out, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_list_companies",
		Description: "List the companies (sets of books) available on the connection.",
		Schema:      objectSchema(map[string]any{"instance": instanceProp()}, nil),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			eng, _, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			companies, err := eng.Companies(ctx)
			if err != nil {
				return nil, err
			}
			return map[string]any{"instance": eng.ID(), "count": len(companies), "companies": companies}, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_list_ledgers",
		Description: "List ledger accounts with their parent group and closing balance. Use search to find one account.",
		Schema: objectSchema(map[string]any{
			"company":  companyProp(),
			"instance": instanceProp(),
			"search":   strProp("Case-insensitive substring matched against the ledger name or its group."),
			"limit":    intProp("Maximum rows to return."),
		}, nil),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			ledgers, err := eng.Ledgers(ctx, company)
			if err != nil {
				return nil, err
			}
			needle := lower(strArg(args, "search"))
			var out []engine.Ledger
			for _, l := range ledgers {
				if needle != "" && !contains(lower(l.Name), needle) && !contains(lower(l.Parent), needle) {
					continue
				}
				out = append(out, l)
			}
			sortLedgers(out)
			max := intArg(args, "limit", defaultMaxRows)
			truncated := len(out) > max
			if truncated {
				out = out[:max]
			}
			return map[string]any{
				"instance": eng.ID(), "company": company,
				"count": len(out), "truncated": truncated, "ledgers": out,
			}, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_list_vouchers",
		Description: "List vouchers with optional filters. Dates are YYYY-MM-DD.",
		Schema: objectSchema(map[string]any{
			"company":  companyProp(),
			"instance": instanceProp(),
			"type":     strProp("Sales, Purchase, Payment, Receipt, Journal, Contra, Credit Note or Debit Note."),
			"from":     dateProp("Start date, YYYY-MM-DD."),
			"to":       dateProp("End date, YYYY-MM-DD."),
			"party":    strProp("Substring of the party ledger name."),
			"limit":    intProp("Maximum rows to return."),
		}, nil),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			vouchers, err := eng.Vouchers(ctx, company, engine.Query{
				Type:  strArg(args, "type"),
				From:  strArg(args, "from"),
				To:    strArg(args, "to"),
				Party: strArg(args, "party"),
				Limit: intArg(args, "limit", defaultMaxRows),
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"instance": eng.ID(), "company": company,
				"count": len(vouchers), "vouchers": vouchers,
			}, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_get_report",
		Description: "Run any supported Tally report by name and return its rows.",
		Schema: objectSchema(map[string]any{
			"report":   enumProp("Which report to run.", reports()),
			"company":  companyProp(),
			"instance": instanceProp(),
			"from":     dateProp("Start date, YYYY-MM-DD."),
			"to":       dateProp("End date, YYYY-MM-DD."),
			"max_rows": intProp("Maximum rows to return."),
		}, []string{"report"}),
		Handler: t.runReport,
	})

	for _, def := range reportTools {
		s.Add(Tool{
			Name:        def.Tool,
			Description: def.About,
			Schema: objectSchema(map[string]any{
				"company":  companyProp(),
				"instance": instanceProp(),
				"from":     dateProp("Start date, YYYY-MM-DD."),
				"to":       dateProp("End date, YYYY-MM-DD."),
				"max_rows": intProp("Maximum rows to return."),
			}, nil),
			Handler: t.namedReport(def.Report),
		})
	}

	s.Add(Tool{
		Name:        "tally_create_ledger",
		Description: "Create an accounting ledger in Tally. This writes to the books and requires the server to be started with --allow-writes.",
		Schema: objectSchema(map[string]any{
			"name":     strProp("Unique ledger name."),
			"parent":   strProp("Parent group, for example Sundry Debtors, Bank Accounts, Indirect Expenses."),
			"company":  companyProp(),
			"instance": instanceProp(),
		}, []string{"name", "parent"}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := t.requireWrites("creating a ledger"); err != nil {
				return nil, err
			}
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			res, err := eng.CreateLedger(ctx, company, engine.LedgerInput{
				Name:   strArg(args, "name"),
				Parent: strArg(args, "parent"),
			})
			if err != nil {
				return nil, err
			}
			return res, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_create_voucher",
		Description: "Create a voucher in Tally. Debits and credits must balance. Tally convention is that a debit entry carries a negative amount, which this tool handles. Requires --allow-writes.",
		Schema: objectSchema(map[string]any{
			"type":      strProp("Sales, Purchase, Payment, Receipt, Journal or Contra."),
			"date":      dateProp("Voucher date, YYYY-MM-DD."),
			"party":     strProp("Party ledger name."),
			"narration": strProp("Narration text."),
			"company":   companyProp(),
			"instance":  instanceProp(),
			"entries": map[string]any{
				"type":        "array",
				"description": "Ledger entries.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"ledger":   strProp("Ledger name."),
						"amount":   map[string]any{"type": "number", "description": "Positive amount."},
						"is_debit": map[string]any{"type": "boolean", "description": "true for a debit entry."},
					},
					"required": []string{"ledger", "amount", "is_debit"},
				},
			},
		}, []string{"type", "date", "entries"}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := t.requireWrites("creating a voucher"); err != nil {
				return nil, err
			}
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			in := engine.VoucherInput{
				Type:      strArg(args, "type"),
				Date:      strArg(args, "date"),
				Party:     strArg(args, "party"),
				Narration: strArg(args, "narration"),
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
				return nil, err
			}
			return res, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_query_collection",
		Description: "Run an inline TDL collection against a live Tally. This is the escape hatch: anything Tally can express in TDL is reachable here. The TDL must define a COLLECTION whose NAME matches the name argument.",
		Schema: objectSchema(map[string]any{
			"name":        strProp("Collection name; must match the NAME attribute in the TDL."),
			"tdl":         strProp("Full TDL, for example <COLLECTION NAME=\"X\" ISMODIFY=\"No\"><TYPE>Ledger</TYPE><NATIVEMETHOD>Name</NATIVEMETHOD></COLLECTION>"),
			"static_vars": map[string]any{"type": "object", "description": "Extra STATICVARIABLES as string key/value pairs."},
			"company":     companyProp(),
			"instance":    instanceProp(),
		}, []string{"name", "tdl"}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			xml, err := eng.QueryCollection(ctx, company, strArg(args, "name"), strArg(args, "tdl"), strMapArg(args, "static_vars"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"instance": eng.ID(), "raw_xml": xml}, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_evaluate_function",
		Description: "Evaluate a TDL expression on a live Tally, for example $$Version:Company:##SVCurrentCompany or a report function. Useful for probing what a Tally build supports.",
		Schema: objectSchema(map[string]any{
			"expression": strProp("The TDL expression to evaluate."),
			"company":    companyProp(),
			"instance":   instanceProp(),
		}, []string{"expression"}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			xml, err := eng.EvaluateFunction(ctx, company, strArg(args, "expression"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"instance": eng.ID(), "raw_xml": xml}, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_export_object",
		Description: "Export a single Tally object by subtype and name, for example Ledger named Cash, or Voucher. Returns the raw object XML.",
		Schema: objectSchema(map[string]any{
			"subtype":  strProp("Ledger, Group, StockItem, Voucher, Company, CostCentre, Godown, Unit."),
			"id":       strProp("Object name or identifier."),
			"fields":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Fields to fetch. Defaults to Name."},
			"company":  companyProp(),
			"instance": instanceProp(),
		}, []string{"subtype", "id"}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			xml, err := eng.ExportObject(ctx, company, strArg(args, "subtype"), strArg(args, "id"), strSliceArg(args, "fields"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"instance": eng.ID(), "raw_xml": xml}, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_execute_action",
		Description: "Invoke a TDL action on a live Tally. Requires --allow-writes because actions can change state.",
		Schema: objectSchema(map[string]any{
			"action":   strProp("The TDL action identifier."),
			"company":  companyProp(),
			"instance": instanceProp(),
		}, []string{"action"}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := t.requireWrites("executing a TDL action"); err != nil {
				return nil, err
			}
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			xml, err := eng.ExecuteAction(ctx, company, strArg(args, "action"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"instance": eng.ID(), "raw_xml": xml}, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_send_raw_xml",
		Description: "Send a complete Tally XML envelope and return the response. The universal escape hatch. Requires --allow-writes.",
		Schema: objectSchema(map[string]any{
			"xml":      strProp("The full ENVELOPE to send."),
			"company":  companyProp(),
			"instance": instanceProp(),
		}, []string{"xml"}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := t.requireWrites("sending raw XML"); err != nil {
				return nil, err
			}
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			xml, err := eng.RawXML(ctx, company, strArg(args, "xml"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"instance": eng.ID(), "raw_xml": xml}, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_import_data",
		Description: "Import masters or vouchers into Tally. Use report_name All Masters for masters and Vouchers for vouchers. Requires --allow-writes.",
		Schema: objectSchema(map[string]any{
			"report_name": strProp("All Masters, Vouchers, or another Tally import report name."),
			"data":        strProp("The TALLYMESSAGE payload."),
			"company":     companyProp(),
			"instance":    instanceProp(),
		}, []string{"data"}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := t.requireWrites("importing data"); err != nil {
				return nil, err
			}
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			xml, err := eng.ImportData(ctx, company, strArg(args, "report_name"), strArg(args, "data"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"instance": eng.ID(), "raw_xml": xml}, nil
		},
	})
}

// ── handlers ────────────────────────────────────────────────────────────────

func (t *tallyTools) runReport(ctx context.Context, args map[string]any) (any, error) {
	return t.report(ctx, strArg(args, "report"), args)
}

func (t *tallyTools) namedReport(report string) func(context.Context, map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		return t.report(ctx, report, args)
	}
}

func (t *tallyTools) report(ctx context.Context, report string, args map[string]any) (any, error) {
	if report == "" {
		return nil, fmt.Errorf("report is required")
	}
	eng, company, err := t.engineFor(args)
	if err != nil {
		return nil, err
	}
	rep, err := eng.NamedReport(ctx, company, report, engine.Query{
		From: strArg(args, "from"),
		To:   strArg(args, "to"),
	})
	if err != nil {
		return nil, err
	}
	rows := rep.Rows
	max := intArg(args, "max_rows", defaultMaxRows)
	truncated := len(rows) > max
	if truncated {
		rows = rows[:max]
	}
	return map[string]any{
		"instance": eng.ID(), "company": company, "report": rep.Name,
		"note": rep.Note, "count": len(rows), "truncated": truncated, "rows": rows,
	}, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func (t *tallyTools) engineFor(args map[string]any) (engine.Engine, string, error) {
	eng, err := t.reg.Resolve(strArg(args, "instance"))
	if err != nil {
		return nil, "", err
	}
	return eng, strArg(args, "company"), nil
}

func (t *tallyTools) requireWrites(what string) error {
	if !t.allowWrites {
		return fmt.Errorf("writes are disabled on this server; restart it with --allow-writes to allow %s", what)
	}
	return nil
}

func reports() []string {
	out := make([]string, 0, len(reportTools))
	for _, def := range reportTools {
		out = append(out, def.Report)
	}
	return out
}

func objectSchema(props map[string]any, required []string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func dateProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func enumProp(desc string, values []string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}

func companyProp() map[string]any {
	return strProp("Company name; defaults to the company selected in the web UI.")
}

func instanceProp() map[string]any {
	return strProp("Connection id from the registry; defaults to the active one.")
}

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

func sortLedgers(l []engine.Ledger) {
	sort.Slice(l, func(i, j int) bool { return l[i].Name < l[j].Name })
}