package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FileEngine talks to the TallyWeb Go backend (TallyWeb-main/backend), which
// parses the on-disk database files directly. It understands BOTH generations:
// Manager.1800 / TranMgr.1800 for TallyPrime and Manager.900 / TranMgr.900 for
// Tally ERP 9. It is read-mostly and needs no running Tally, which is what
// makes archive and bulk-report work possible.
type FileEngine struct {
	cfg     FileConfig
	client  *http.Client
	token   string
	baseURL string
}

type FileConfig struct {
	ID         string
	Label      string
	Product    string
	BaseURL    string
	Username   string
	Password   string
	CompanyDir string
	Timeout    time.Duration
}

func NewFileEngine(cfg FileConfig) *FileEngine {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.Username == "" {
		cfg.Username = "admin"
	}
	if cfg.Password == "" {
		cfg.Password = "admin"
	}
	return &FileEngine{
		cfg:     cfg,
		client:  &http.Client{Timeout: cfg.Timeout},
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
	}
}

func (e *FileEngine) ID() string      { return e.cfg.ID }
func (e *FileEngine) Kind() string    { return "tallyweb-file" }
func (e *FileEngine) Product() string { return orDefault(e.cfg.Product, "tallyprime") }

func (e *FileEngine) login(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{"username": e.cfg.Username, "password": e.cfg.Password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach the TallyWeb backend at %s: %w", e.baseURL, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TallyWeb login failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.Token == "" {
		return fmt.Errorf("TallyWeb login returned no token")
	}
	e.token = parsed.Token
	return nil
}

func (e *FileEngine) get(ctx context.Context, path string, out any) error {
	if e.token == "" && !strings.HasPrefix(path, "/api/health") {
		if err := e.login(ctx); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+path, nil)
	if err != nil {
		return err
	}
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach the TallyWeb backend: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		e.token = ""
		if err := e.login(ctx); err != nil {
			return err
		}
		return e.get(ctx, path, out)
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("TallyWeb %s returned HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (e *FileEngine) withCompany(path string) string {
	if e.cfg.CompanyDir == "" {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "company=" + e.cfg.CompanyDir
}

func (e *FileEngine) Health(ctx context.Context) Health {
	h := Health{Product: e.Product(), Address: e.baseURL}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var health struct {
		Status    string `json:"status"`
		Mode      string `json:"mode"`
		Companies int    `json:"companies"`
	}
	if err := e.get(cctx, "/api/health", &health); err != nil {
		h.Message = err.Error()
		return h
	}
	h.OK = true
	h.Message = fmt.Sprintf("file mode, %d companies", health.Companies)
	return h
}

func (e *FileEngine) Companies(ctx context.Context) ([]Company, error) {
	var folders []string
	if err := e.get(ctx, "/api/companies", &folders); err != nil {
		return nil, err
	}
	var out []Company
	for _, f := range folders {
		c := Company{ID: f, Name: f, Folder: f}
		var info struct {
			Name   string `json:"name"`
			Folder string `json:"folder"`
		}
		if err := e.get(ctx, e.withCompany("/api/company/"+f), &info); err == nil && info.Name != "" {
			c.Name = info.Name
		}
		out = append(out, c)
	}
	return out, nil
}

// fileLedger mirrors the JSON the TallyWeb backend emits.
type fileLedger struct {
	Name           string  `json:"name"`
	Parent         string  `json:"parent"`
	OpeningBalance float64 `json:"opening_balance"`
	ClosingBalance float64 `json:"closing_balance"`
}

func (e *FileEngine) Ledgers(ctx context.Context, company string) ([]Ledger, error) {
	_ = company
	var rows []fileLedger
	if err := e.get(ctx, e.withCompany("/api/ledgers"), &rows); err != nil {
		return nil, err
	}
	out := make([]Ledger, 0, len(rows))
	for _, r := range rows {
		out = append(out, Ledger{
			Name:           r.Name,
			Parent:         r.Parent,
			OpeningBalance: r.OpeningBalance,
			ClosingBalance: r.ClosingBalance,
		})
	}
	return out, nil
}

type fileVoucher struct {
	Number    string  `json:"number"`
	Type      string  `json:"type"`
	Date      string  `json:"date"`
	Party     string  `json:"party"`
	Narration string  `json:"narration"`
	Amount    float64 `json:"amount"`
}

func (e *FileEngine) Vouchers(ctx context.Context, company string, q Query) ([]Voucher, error) {
	_ = company
	path := e.withCompany("/api/vouchers")
	params := []string{}
	if q.Type != "" {
		params = append(params, "type="+urlEncode(q.Type))
	}
	if q.From != "" {
		params = append(params, "from="+urlEncode(ddMMyyyy(q.From)))
	}
	if q.To != "" {
		params = append(params, "to="+urlEncode(ddMMyyyy(q.To)))
	}
	if q.Party != "" {
		params = append(params, "party="+urlEncode(q.Party))
	}
	if len(params) > 0 {
		path += "&" + strings.Join(params, "&")
	}

	var rows []fileVoucher
	if err := e.get(ctx, path, &rows); err != nil {
		return nil, err
	}
	out := make([]Voucher, 0, len(rows))
	for _, r := range rows {
		out = append(out, Voucher{
			ID:        r.Number,
			Number:    r.Number,
			Type:      r.Type,
			Date:      isoDate(r.Date),
			Party:     r.Party,
			Narration: r.Narration,
			Amount:    r.Amount,
		})
	}
	sortVouchersDesc(out)
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// reportAlias maps the UI's report ids onto the TallyWeb endpoint names.
var reportAlias = map[string]string{
	"trial_balance":   "trial-balance",
	"profit_loss":     "profit-loss",
	"balance_sheet":   "trial-balance",
	"day_book":        "day-book",
	"outstanding":     "outstanding",
	"ageing":          "aging",
	"stock_summary":   "stock-balance",
	"ledger_balances": "ledger-balances",
	"cash_flow":       "cash-flow",
	"funds_flow":      "funds-flow",
	"ratio_analysis":  "ratio-analysis",
	"statistics":      "statistics",
	"payables":        "payables",
	"cash_bank_book":  "cash-bank-book",
	"gst_returns":     "gst-returns",
	"gstr1":           "gstr-1",
}

func (e *FileEngine) NamedReport(ctx context.Context, company, name string, q Query) (Report, error) {
	_ = company
	endpoint, ok := reportAlias[name]
	if !ok {
		return Report{}, fmt.Errorf("report %q is not available from the file engine", name)
	}
	var payload any
	if err := e.get(ctx, e.withCompany("/api/reports/"+endpoint), &payload); err != nil {
		return Report{}, err
	}
	return Report{
		Name: name,
		Rows: toRows(payload),
		Note: "Read from on-disk files. Opening balances are not decoded by the file engine, so balance-derived reports can differ from Tally.",
	}, nil
}

// The file engine cannot create vouchers. TallyWeb's own docs say so, and
// pretending otherwise would risk corrupting a live company.
func (e *FileEngine) CreateLedger(ctx context.Context, company string, in LedgerInput) (WriteResult, error) {
	_ = ctx
	_ = company
	return WriteResult{}, fmt.Errorf(
		"the file engine cannot create masters through this API. Use a Tally XML instance, " +
			"or the TallyWeb backend's own POST /api/ledgers with a template ledger (%s)", in.Name)
}

func (e *FileEngine) CreateVoucher(ctx context.Context, company string, in VoucherInput) (WriteResult, error) {
	_ = ctx
	_ = company
	_ = in
	return WriteResult{}, fmt.Errorf(
		"voucher creation needs a live Tally XML instance. The reverse-engineered binary writer cannot create vouchers.")
}

func (e *FileEngine) RawXML(ctx context.Context, company, body string) (string, error) {
	_ = ctx
	_ = company
	_ = body
	return "", fmt.Errorf("raw Tally XML is only available on a Tally XML instance")
}

// ── helpers ─────────────────────────────────────────────────────────────────

func toRows(payload any) []map[string]string {
	var rows []map[string]string
	switch v := payload.(type) {
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				rows = append(rows, flattenAny(m))
			}
		}
	case map[string]any:
		rows = append(rows, flattenAny(v))
	}
	if len(rows) > 500 {
		rows = rows[:500]
	}
	return rows
}

func flattenAny(m map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		switch t := v.(type) {
		case nil:
		case string:
			out[strings.ToUpper(k)] = t
		case float64:
			out[strings.ToUpper(k)] = fmt.Sprintf("%.2f", t)
		case bool:
			out[strings.ToUpper(k)] = fmt.Sprintf("%t", t)
		default:
			if b, err := json.Marshal(t); err == nil && len(b) < 200 {
				out[strings.ToUpper(k)] = string(b)
			}
		}
	}
	return out
}

func ddMMyyyy(iso string) string {
	if len(iso) == 10 && iso[4] == '-' {
		return iso[8:10] + "-" + iso[5:7] + "-" + iso[0:4]
	}
	return iso
}

func urlEncode(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~':
			b.WriteRune(r)
		case r == ' ':
			b.WriteString("%20")
		default:
			for _, byteVal := range []byte(string(r)) {
				const hex = "0123456789ABCDEF"
				b.WriteByte('%')
				b.WriteByte(hex[byteVal>>4])
				b.WriteByte(hex[byteVal&0x0F])
			}
		}
	}
	return b.String()
}

// ── Tally control: not available offline ───────────────────────────────────

func (e *FileEngine) QueryCollection(ctx context.Context, company, name, tdl string, vars map[string]string) (string, error) {
	return "", ErrNoTallyControl
}

func (e *FileEngine) EvaluateFunction(ctx context.Context, company, expression string) (string, error) {
	return "", ErrNoTallyControl
}

func (e *FileEngine) ExportObject(ctx context.Context, company, subtype, objectID string, fields []string) (string, error) {
	return "", ErrNoTallyControl
}

func (e *FileEngine) ExecuteAction(ctx context.Context, company, actionID string) (string, error) {
	return "", ErrNoTallyControl
}

func (e *FileEngine) ImportData(ctx context.Context, company, reportName, data string) (string, error) {
	return "", fmt.Errorf("the file engine cannot import into Tally; use a tally-xml instance")
}

// ── inventory ──────────────────────────────────────────────────────────────

type fileStockItem struct {
	Name         string  `json:"name"`
	Parent       string  `json:"parent"`
	BaseUnit     string  `json:"base_unit"`
	ClosingQty   float64 `json:"closing_qty"`
	ClosingRate  float64 `json:"closing_rate"`
	ClosingValue float64 `json:"closing_value"`
}

func (e *FileEngine) StockItems(ctx context.Context, company string) ([]StockItem, error) {
	_ = company
	var rows []fileStockItem
	if err := e.get(ctx, e.withCompany("/api/stock-items"), &rows); err != nil {
		return nil, err
	}
	out := make([]StockItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, StockItem{
			Name:         r.Name,
			Parent:       r.Parent,
			BaseUnit:     r.BaseUnit,
			ClosingQty:   r.ClosingQty,
			ClosingRate:  r.ClosingRate,
			ClosingValue: r.ClosingValue,
		})
	}
	return out, nil
}

func (e *FileEngine) CreateStockItem(ctx context.Context, company string, in StockItemInput) (WriteResult, error) {
	return WriteResult{}, fmt.Errorf(
		"the file engine cannot create inventory masters through this API; use a tally-xml instance")
}
