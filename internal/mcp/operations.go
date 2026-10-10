package mcp

import (
	"context"
	"fmt"

	"tallywebui/internal/engine"
)

// voucherTool describes one business document a business issues every day.
type voucherTool struct {
	Tool      string
	VType     string
	Inventory bool
	About     string
}

// The day-to-day documents, in the order a small business touches them.
var voucherTools = []voucherTool{
	{
		Tool: "tally_create_sales_invoice", VType: "Sales", Inventory: true,
		About: "Create a sales invoice (tax invoice) with inventory lines, a party ledger and the tax ledgers. Tally handles numbering, GST posting and stock movement.",
	},
	{
		Tool: "tally_create_purchase_invoice", VType: "Purchase", Inventory: true,
		About: "Create a purchase invoice from a supplier with inventory lines and tax ledgers.",
	},
	{
		Tool: "tally_create_credit_note", VType: "Credit Note", Inventory: true,
		About: "Create a credit note, which is how a sales return is recorded.",
	},
	{
		Tool: "tally_create_debit_note", VType: "Debit Note", Inventory: true,
		About: "Create a debit note, which is how a purchase return is recorded.",
	},
	{
		Tool: "tally_create_payment", VType: "Payment", Inventory: false,
		About: "Record a payment made: debit the party, credit the bank or cash ledger.",
	},
	{
		Tool: "tally_create_receipt", VType: "Receipt", Inventory: false,
		About: "Record money received: debit the bank or cash ledger, credit the party.",
	},
	{
		Tool: "tally_create_journal", VType: "Journal", Inventory: false,
		About: "Post a journal entry between any two or more ledgers, for example depreciation or a provision.",
	},
	{
		Tool: "tally_create_contra", VType: "Contra", Inventory: false,
		About: "Move money between cash and bank, such as a cash deposit or a withdrawal.",
	},
}

// RegisterOperationTools adds the inventory masters and the daily documents.
func RegisterOperationTools(s *Server, reg *engine.Registry, allowWrites bool) {
	t := &tallyTools{reg: reg, allowWrites: allowWrites}

	s.Add(Tool{
		Name:        "tally_list_stock_items",
		Description: "List inventory masters with unit, closing quantity, rate and value. Use this to find the exact stock item name before raising an invoice.",
		Schema: objectSchema(map[string]any{
			"company":  companyProp(),
			"instance": instanceProp(),
			"search":   strProp("Case-insensitive substring matched against the item name or its group."),
			"limit":    intProp("Maximum rows to return."),
		}, nil),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			items, err := eng.StockItems(ctx, company)
			if err != nil {
				return nil, err
			}
			needle := lower(strArg(args, "search"))
			var out []engine.StockItem
			for _, item := range items {
				if needle != "" && !contains(lower(item.Name), needle) && !contains(lower(item.Parent), needle) {
					continue
				}
				out = append(out, item)
			}
			max := intArg(args, "limit", defaultMaxRows)
			truncated := len(out) > max
			if truncated {
				out = out[:max]
			}
			return map[string]any{
				"instance": eng.ID(), "company": company,
				"count": len(out), "truncated": truncated, "stock_items": out,
			}, nil
		},
	})

	s.Add(Tool{
		Name:        "tally_create_stock_item",
		Description: "Create an inventory master with its unit, opening quantity, opening rate and optional GST rate and HSN code.",
		Schema: objectSchema(map[string]any{
			"name":         strProp("Unique stock item name."),
			"parent":       strProp("Stock group, for example Primary or Building Material."),
			"base_unit":    strProp("Unit of measure, for example Nos, Kgs, Bags, Ltr."),
			"opening_qty":  map[string]any{"type": "number", "description": "Opening quantity."},
			"opening_rate": map[string]any{"type": "number", "description": "Opening rate per unit."},
			"gst_rate":     map[string]any{"type": "number", "description": "GST rate percentage, for example 18."},
			"hsn_code":     strProp("HSN or SAC code."),
			"company":      companyProp(),
			"instance":     instanceProp(),
		}, []string{"name"}),
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			if err := t.requireWrites("creating a stock item"); err != nil {
				return nil, err
			}
			eng, company, err := t.engineFor(args)
			if err != nil {
				return nil, err
			}
			res, err := eng.CreateStockItem(ctx, company, engine.StockItemInput{
				Name:        strArg(args, "name"),
				Parent:      strArg(args, "parent"),
				BaseUnit:    strArg(args, "base_unit"),
				OpeningQty:  numArg(args, "opening_qty"),
				OpeningRate: numArg(args, "opening_rate"),
				GSTRate:     numArg(args, "gst_rate"),
				HSNCode:     strArg(args, "hsn_code"),
			})
			if err != nil {
				return nil, err
			}
			return res, nil
		},
	})

	for _, def := range voucherTools {
		s.Add(Tool{
			Name:        def.Tool,
			Description: def.About + " Requires the server to be started with --allow-writes.",
			Schema:      voucherSchema(def.Inventory),
			Handler:     t.createDocument(def),
		})
	}
}

func voucherSchema(inventory bool) map[string]any {
	props := map[string]any{
		"date":           dateProp("Voucher date, YYYY-MM-DD."),
		"party":          strProp("Party ledger name. Use tally_list_ledgers to find it."),
		"narration":      strProp("Narration text."),
		"voucher_number": strProp("Optional voucher number; Tally allocates one when omitted."),
		"place_of_supply": strProp("Optional place of supply, for inter-state GST."),
		"company":        companyProp(),
		"instance":       instanceProp(),
		"entries": map[string]any{
			"type":        "array",
			"description": "Accounting legs. For a sales invoice: the party as a debit, plus each tax ledger as a credit.",
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
	}
	if inventory {
		props["items"] = map[string]any{
			"type":        "array",
			"description": "Inventory lines. Each line is allocated to a sales or purchase ledger.",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"stock_item": strProp("Stock item name."),
					"quantity":   map[string]any{"type": "number", "description": "Quantity greater than zero."},
					"rate":       map[string]any{"type": "number", "description": "Rate per unit."},
					"unit":       strProp("Unit of measure, for example Nos."),
					"discount":   map[string]any{"type": "number", "description": "Line discount."},
					"ledger":     strProp("Sales or purchase ledger this line posts to."),
					"godown":     strProp("Optional godown."),
				},
				"required": []string{"stock_item", "quantity", "rate"},
			},
		}
	}
	return objectSchema(props, []string{"date", "entries"})
}

func (t *tallyTools) createDocument(def voucherTool) func(context.Context, map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		if err := t.requireWrites("creating a " + def.VType); err != nil {
			return nil, err
		}
		eng, company, err := t.engineFor(args)
		if err != nil {
			return nil, err
		}
		in := engine.VoucherInput{
			Type:          def.VType,
			Date:          strArg(args, "date"),
			Party:         strArg(args, "party"),
			Narration:     strArg(args, "narration"),
			VoucherNumber: strArg(args, "voucher_number"),
			PlaceOfSupply: strArg(args, "place_of_supply"),
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
				Godown:    strArg(item, "godown"),
				Discount:  numArg(item, "discount"),
				Amount:    numArg(item, "amount"),
				Ledger:    strArg(item, "ledger"),
			})
		}
		if len(in.Entries) == 0 && len(in.Items) == 0 {
			return nil, fmt.Errorf("provide entries, items, or both")
		}
		res, err := eng.CreateVoucher(ctx, company, in)
		if err != nil {
			return nil, err
		}
		return res, nil
	}
}
