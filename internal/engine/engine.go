// Package engine is the single contract the whole product is built on.
//
// The web UI never talks to Tally directly. It talks to an Engine. Today there
// are two implementations: a live Tally XML engine and an offline file engine
// (the Go backend in TallyWeb-main). Later a native engine can be added without
// changing a single screen.
package engine

import (
	"context"
	"errors"
)

// Company is one set of books.
type Company struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Folder string `json:"folder,omitempty"`
}

// Ledger is a master account with its balance.
type Ledger struct {
	Name           string  `json:"name"`
	Parent         string  `json:"parent"`
	OpeningBalance float64 `json:"opening_balance"`
	ClosingBalance float64 `json:"closing_balance"`
}

// Voucher is one transaction.
type Voucher struct {
	ID        string  `json:"id"`
	Number    string  `json:"number"`
	Type      string  `json:"type"`
	Date      string  `json:"date"`
	Party     string  `json:"party"`
	Narration string  `json:"narration"`
	Amount    float64 `json:"amount"`
}

// Query filters a voucher read.
type Query struct {
	From  string
	To    string
	Type  string
	Party string
	Limit int
}

// Report is a best-effort tabular rendering of a named Tally report.
// Raw is always returned as well: named Tally reports are built for display,
// not for data exchange, so the raw envelope is the source of truth.
type Report struct {
	Name string              `json:"name"`
	Rows []map[string]string `json:"rows"`
	Raw  string              `json:"raw,omitempty"`
	Note string              `json:"note,omitempty"`
}

// LedgerInput creates or alters a ledger.
type LedgerInput struct {
	Name   string  `json:"name"`
	Parent string  `json:"parent"`
	Action string  `json:"action,omitempty"`
}

// Entry is one leg of a voucher.
type Entry struct {
	Ledger  string  `json:"ledger"`
	Amount  float64 `json:"amount"`
	IsDebit bool    `json:"is_debit"`
}

// ItemLine is one inventory line of an invoice. Ledger is the sales or
// purchase account the line is allocated to, which is how Tally links stock
// movement to accounting.
type ItemLine struct {
	StockItem string  `json:"stock_item"`
	Quantity  float64 `json:"quantity"`
	Rate      float64 `json:"rate"`
	Unit      string  `json:"unit,omitempty"`
	Godown    string  `json:"godown,omitempty"`
	Discount  float64 `json:"discount,omitempty"`
	// Amount is computed as quantity * rate - discount when left at zero.
	Amount float64 `json:"amount,omitempty"`
	Ledger string  `json:"ledger,omitempty"`
}

// VoucherInput creates a voucher. Entries carry the accounting legs; Items
// carry the inventory legs of an invoice. A plain accounting voucher such as a
// payment leaves Items empty.
type VoucherInput struct {
	Type          string     `json:"type"`
	Date          string     `json:"date"`
	Party         string     `json:"party"`
	Narration     string     `json:"narration"`
	Entries       []Entry    `json:"entries"`
	Items         []ItemLine `json:"items,omitempty"`
	VoucherNumber string     `json:"voucher_number,omitempty"`
	PlaceOfSupply string     `json:"place_of_supply,omitempty"`
}

// StockItem is an inventory master with its closing position.
type StockItem struct {
	Name         string  `json:"name"`
	Parent       string  `json:"parent,omitempty"`
	BaseUnit     string  `json:"base_unit,omitempty"`
	ClosingQty   float64 `json:"closing_qty"`
	ClosingRate  float64 `json:"closing_rate"`
	ClosingValue float64 `json:"closing_value"`
	GSTRate      float64 `json:"gst_rate,omitempty"`
	HSNCode      string  `json:"hsn_code,omitempty"`
}

// StockItemInput creates or alters a stock item.
type StockItemInput struct {
	Name        string  `json:"name"`
	Parent      string  `json:"parent,omitempty"`
	BaseUnit    string  `json:"base_unit,omitempty"`
	OpeningQty  float64 `json:"opening_qty,omitempty"`
	OpeningRate float64 `json:"opening_rate,omitempty"`
	GSTRate     float64 `json:"gst_rate,omitempty"`
	HSNCode     string  `json:"hsn_code,omitempty"`
	Action      string  `json:"action,omitempty"`
}

// WriteResult reports the outcome of a mutation.
type WriteResult struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Raw    string `json:"raw,omitempty"`
}

// Health describes whether an engine is usable right now.
type Health struct {
	OK      bool   `json:"ok"`
	Product string `json:"product"`
	Address string `json:"address"`
	Message string `json:"message,omitempty"`
}

// Engine is the only interface the UI and the AI agent depend on.
type Engine interface {
	ID() string
	Kind() string
	Product() string
	Health(ctx context.Context) Health

	Companies(ctx context.Context) ([]Company, error)
	Ledgers(ctx context.Context, company string) ([]Ledger, error)
	StockItems(ctx context.Context, company string) ([]StockItem, error)
	Vouchers(ctx context.Context, company string, q Query) ([]Voucher, error)
	NamedReport(ctx context.Context, company, name string, q Query) (Report, error)

	CreateLedger(ctx context.Context, company string, in LedgerInput) (WriteResult, error)
	CreateStockItem(ctx context.Context, company string, in StockItemInput) (WriteResult, error)
	CreateVoucher(ctx context.Context, company string, in VoucherInput) (WriteResult, error)

	// RawXML is the escape hatch. It keeps everything Tally can do reachable
	// even when this UI has no first-class screen for it yet.
	RawXML(ctx context.Context, company, body string) (string, error)

	// The four Tally integration methods, exposed generically. Between them
	// they cover every capability the original tooling had, so nothing is
	// lost when a first-class screen or MCP tool does not exist yet.
	QueryCollection(ctx context.Context, company, name, tdl string, vars map[string]string) (string, error)
	EvaluateFunction(ctx context.Context, company, expression string) (string, error)
	ExportObject(ctx context.Context, company, subtype, objectID string, fields []string) (string, error)
	ExecuteAction(ctx context.Context, company, actionID string) (string, error)
	ImportData(ctx context.Context, company, reportName, data string) (string, error)
}

// SupportsTallyControl reports whether an engine speaks raw Tally XML and TDL.
// The file and demo engines do not, and they say so instead of failing weirdly.
type TallyController interface {
	QueryCollection(ctx context.Context, company, name, tdl string, vars map[string]string) (string, error)
	EvaluateFunction(ctx context.Context, company, expression string) (string, error)
	ExportObject(ctx context.Context, company, subtype, objectID string, fields []string) (string, error)
	ExecuteAction(ctx context.Context, company, actionID string) (string, error)
	ImportData(ctx context.Context, company, reportName, data string) (string, error)
}

// ErrNoTallyControl is returned by engines that cannot speak raw XML or TDL.
var ErrNoTallyControl = errors.New("raw Tally XML and TDL are only available on a tally-xml instance")
