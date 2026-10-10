package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// TallyXMLConfig points at one running Tally installation.
type TallyXMLConfig struct {
	ID      string
	Label   string
	Product string
	Host    string
	Port    int
	Company string
	Timeout time.Duration
}

// TallyXMLEngine speaks Tally's XML server.
//
// Two behaviours are non-negotiable and are copied from tally-mcp-main, which
// proved them in production:
//   - UTF-16LE request and response bodies
//   - one serial request queue per instance, because Tally's XML server is
//     single-threaded and concurrent calls freeze the accountant's desktop
type TallyXMLEngine struct {
	cfg    TallyXMLConfig
	client *http.Client
	gate   chan struct{}
}

func NewTallyXMLEngine(cfg TallyXMLConfig) *TallyXMLEngine {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &TallyXMLEngine{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		gate:   make(chan struct{}, 1),
	}
}

func (e *TallyXMLEngine) ID() string      { return e.cfg.ID }
func (e *TallyXMLEngine) Kind() string    { return "tally-xml" }
func (e *TallyXMLEngine) Product() string { return orDefault(e.cfg.Product, "tallyprime") }

func (e *TallyXMLEngine) url() string {
	return fmt.Sprintf("http://%s:%d", e.cfg.Host, e.cfg.Port)
}

// ── wire ────────────────────────────────────────────────────────────────────

func (e *TallyXMLEngine) post(ctx context.Context, body string) (string, error) {
	select {
	case e.gate <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-e.gate }()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url(), bytes.NewReader(encodeUTF16LE(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "text/xml;charset=utf-16")
	req.Header.Set("Accept", "text/xml")

	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach Tally at %s: %w", e.url(), err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return "", err
	}
	out := decodeUTF16LE(raw)
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("Tally returned HTTP %d", resp.StatusCode)
	}
	if ex := tallyException(out); ex != "" {
		return out, fmt.Errorf("Tally error: %s", ex)
	}
	return out, nil
}

func encodeUTF16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	buf := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[i*2:], u)
	}
	return buf
}

func decodeUTF16LE(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
	} else if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		units := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			units = append(units, binary.BigEndian.Uint16(b[i:i+2]))
		}
		return string(utf16.Decode(units))
	}
	if looksUTF16LE(b) {
		units := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			units = append(units, binary.LittleEndian.Uint16(b[i:i+2]))
		}
		return string(utf16.Decode(units))
	}
	return string(b)
}

// looksUTF16LE samples the payload: in UTF-16LE ASCII text every odd byte is 0.
func looksUTF16LE(b []byte) bool {
	n := len(b)
	if n > 400 {
		n = 400
	}
	if n < 4 {
		return false
	}
	odd, total := 0, 0
	for i := 1; i < n; i += 2 {
		total++
		if b[i] == 0 {
			odd++
		}
	}
	return total > 0 && odd*100/total > 40
}

func tallyException(resp string) string {
	up := strings.ToUpper(resp)
	idx := strings.Index(up, "<EXCEPTION>")
	if idx < 0 {
		return ""
	}
	end := strings.Index(up[idx:], "</EXCEPTION>")
	if end < 0 {
		return "unspecified"
	}
	msg := resp[idx+len("<EXCEPTION>") : idx+end]
	return strings.TrimSpace(stripTags(msg))
}

func tallyLineError(resp string) string {
	up := strings.ToUpper(resp)
	idx := strings.Index(up, "<LINEERROR>")
	if idx < 0 {
		return ""
	}
	end := strings.Index(up[idx:], "</LINEERROR>")
	if end < 0 {
		return "unspecified"
	}
	return strings.TrimSpace(stripTags(resp[idx+len("<LINEERROR>") : idx+end]))
}

func stripTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// ── request builders ────────────────────────────────────────────────────────

func (e *TallyXMLEngine) staticVars(company string, extra map[string]string) string {
	var b strings.Builder
	e.addVar(&b, "SVCURRENTCOMPANY", orDefault(company, e.cfg.Company))
	for k, v := range extra {
		e.addVar(&b, k, v)
	}
	b.WriteString("<SVEXPORTFORMAT>$$SysName:XML</SVEXPORTFORMAT>")
	return b.String()
}

func (e *TallyXMLEngine) addVar(b *strings.Builder, name, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.WriteString("<" + name + ">" + xmlEscape(value) + "</" + name + ">")
}

func (e *TallyXMLEngine) reportRequest(name, company string, extra map[string]string) string {
	return "<ENVELOPE><HEADER><VERSION>1</VERSION><TALLYREQUEST>Export Data</TALLYREQUEST>" +
		"<TYPE>Data</TYPE><ID>" + xmlEscape(name) + "</ID></HEADER><BODY><DESC>" +
		"<STATICVARIABLES>" + e.staticVars(company, extra) + "</STATICVARIABLES>" +
		"<REPORTNAME>" + xmlEscape(name) + "</REPORTNAME></DESC></BODY></ENVELOPE>"
}

func (e *TallyXMLEngine) collectionRequest(id, tdl, company string, extra map[string]string) string {
	return "<ENVELOPE><HEADER><VERSION>1</VERSION><TALLYREQUEST>Export Data</TALLYREQUEST>" +
		"<TYPE>Collection</TYPE><ID>" + xmlEscape(id) + "</ID></HEADER><BODY><DESC>" +
		"<STATICVARIABLES>" + e.staticVars(company, extra) + "</STATICVARIABLES>" +
		"<TDL><TDLMESSAGE>" + tdl + "</TDLMESSAGE></TDL></DESC></BODY></ENVELOPE>"
}

func (e *TallyXMLEngine) importRequest(data, reportName, company string) string {
	return "<ENVELOPE><HEADER><VERSION>1</VERSION><TALLYREQUEST>Import Data</TALLYREQUEST></HEADER>" +
		"<BODY><IMPORTDATA><REQUESTDESC><REPORTNAME>" + xmlEscape(reportName) + "</REPORTNAME>" +
		"<STATICVARIABLES>" + e.staticVars(company, nil) + "</STATICVARIABLES></REQUESTDESC>" +
		"<REQUESTDATA>" + data + "</REQUESTDATA></IMPORTDATA></BODY></ENVELOPE>"
}

// ── Engine ──────────────────────────────────────────────────────────────────

func (e *TallyXMLEngine) Health(ctx context.Context) Health {
	h := Health{Product: e.Product(), Address: e.url()}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	companies, err := e.Companies(cctx)
	if err != nil {
		h.Message = err.Error()
		return h
	}
	h.OK = true
	h.Message = fmt.Sprintf("%d companies", len(companies))
	return h
}

func (e *TallyXMLEngine) Companies(ctx context.Context) ([]Company, error) {
	resp, err := e.post(ctx, e.reportRequest("List of Companies", e.cfg.Company, nil))
	if err != nil {
		return nil, err
	}
	root, _ := parseXML(resp)

	var out []Company
	seen := map[string]bool{}
	for _, n := range root.FindAll("COMPANY") {
		name := firstNonEmpty(n.Attr("NAME"), n.LeafText("NAME"), strings.TrimSpace(n.Text))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, Company{ID: name, Name: name})
	}
	if len(out) == 0 {
		for _, n := range root.FindAll("NAME") {
			name := strings.TrimSpace(n.Text)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, Company{ID: name, Name: name})
		}
	}
	if len(out) == 0 && e.cfg.Company != "" {
		out = append(out, Company{ID: e.cfg.Company, Name: e.cfg.Company})
	}
	return out, nil
}

const ledgerMethods = "<NATIVEMETHOD>Name</NATIVEMETHOD><NATIVEMETHOD>Parent</NATIVEMETHOD>" +
	"<NATIVEMETHOD>OpeningBalance</NATIVEMETHOD><NATIVEMETHOD>ClosingBalance</NATIVEMETHOD>"

func (e *TallyXMLEngine) Ledgers(ctx context.Context, company string) ([]Ledger, error) {
	tdl := "<COLLECTION NAME=\"TWUILedgers\" ISMODIFY=\"No\"><TYPE>Ledger</TYPE>" + ledgerMethods + "</COLLECTION>"
	resp, err := e.post(ctx, e.collectionRequest("TWUILedgers", tdl, company, nil))
	if err != nil {
		return nil, err
	}
	root, _ := parseXML(resp)

	seen := map[string]bool{}
	var out []Ledger
	for _, n := range root.FindAll("LEDGER") {
		name := firstNonEmpty(n.Attr("NAME"), n.LeafText("NAME"))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, Ledger{
			Name:           name,
			Parent:         firstNonEmpty(n.LeafText("PARENT"), n.Attr("PARENT")),
			OpeningBalance: parseAmount(n.LeafText("OPENINGBALANCE")),
			ClosingBalance: parseAmount(n.LeafText("CLOSINGBALANCE")),
		})
	}
	return out, nil
}

const voucherMethods = "<NATIVEMETHOD>Date</NATIVEMETHOD><NATIVEMETHOD>VoucherNumber</NATIVEMETHOD>" +
	"<NATIVEMETHOD>VoucherTypeName</NATIVEMETHOD><NATIVEMETHOD>PartyLedgerName</NATIVEMETHOD>" +
	"<NATIVEMETHOD>Amount</NATIVEMETHOD><NATIVEMETHOD>Narration</NATIVEMETHOD>"

func (e *TallyXMLEngine) Vouchers(ctx context.Context, company string, q Query) ([]Voucher, error) {
	extra := map[string]string{}
	if q.From != "" {
		extra["SVFROMDATE"] = tallyDate(q.From)
	}
	if q.To != "" {
		extra["SVTODATE"] = tallyDate(q.To)
	}

	collection := "<COLLECTION NAME=\"TWUIVouchers\" ISMODIFY=\"No\"><TYPE>Voucher</TYPE>" +
		voucherMethods + "</COLLECTION>"
	resp, err := e.post(ctx, e.collectionRequest("TWUIVouchers", collection, company, extra))
	if err != nil {
		return nil, err
	}
	// Tally periodically rejects the date static-variables on some releases.
	// Rather than fail the whole read, retry unfiltered and filter here.
	if le := tallyLineError(resp); le != "" && (q.From != "" || q.To != "") {
		if retry, rerr := e.post(ctx, e.collectionRequest("TWUIVouchers", collection, company, nil)); rerr == nil {
			resp = retry
		}
	}

	root, _ := parseXML(resp)
	seen := map[string]bool{}
	var out []Voucher
	for _, n := range root.FindAll("VOUCHER") {
		v := Voucher{
			ID:        firstNonEmpty(n.Attr("NAME"), n.Attr("VOUCHERID")),
			Number:    n.LeafText("VOUCHERNUMBER"),
			Type:      n.LeafText("VOUCHERTYPENAME"),
			Date:      isoDate(n.LeafText("DATE")),
			Party:     n.LeafText("PARTYLEDGERNAME"),
			Narration: n.LeafText("NARRATION"),
			Amount:    parseAmount(n.LeafText("AMOUNT")),
		}
		key := v.Date + "|" + v.Type + "|" + v.Number + "|" + v.Party
		if seen[key] {
			continue
		}
		seen[key] = true
		if !inDateRange(v.Date, q.From, q.To) {
			continue
		}
		if q.Type != "" && !strings.EqualFold(strings.TrimSpace(q.Type), v.Type) {
			continue
		}
		if q.Party != "" && !strings.Contains(strings.ToLower(v.Party), strings.ToLower(q.Party)) {
			continue
		}
		out = append(out, v)
	}
	sortVouchersDesc(out)
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (e *TallyXMLEngine) NamedReport(ctx context.Context, company, name string, q Query) (Report, error) {
	extra := map[string]string{}
	if q.From != "" {
		extra["SVFROMDATE"] = tallyDate(q.From)
	}
	if q.To != "" {
		extra["SVTODATE"] = tallyDate(q.To)
	}
	resp, err := e.post(ctx, e.reportRequest(name, company, extra))
	if err != nil {
		return Report{}, err
	}
	root, _ := parseXML(resp)
	rep := Report{
		Name: name,
		Rows: flattenReport(root),
		Raw:  resp,
		Note: "Rows are a best-effort flattening of a Tally display report. The raw XML envelope is authoritative.",
	}
	if le := tallyLineError(resp); le != "" {
		rep.Note = "Tally reported: " + le
	}
	return rep, nil
}

func (e *TallyXMLEngine) CreateLedger(ctx context.Context, company string, in LedgerInput) (WriteResult, error) {
	if strings.TrimSpace(in.Name) == "" {
		return WriteResult{}, fmt.Errorf("ledger name is required")
	}
	action := orDefault(in.Action, "Create")
	payload := "<TALLYMESSAGE xmlns:UDF=\"TallyUDF\"><LEDGER NAME=\"" + xmlEscape(in.Name) +
		"\" ACTION=\"" + xmlEscape(action) + "\">" +
		"<PARENT>" + xmlEscape(orDefault(in.Parent, "Sundry Debtors")) + "</PARENT>" +
		"</LEDGER></TALLYMESSAGE>"

	req := e.importRequest(payload, "All Masters", company)
	resp, err := e.postImport(ctx, req)
	if err != nil {
		return WriteResult{Raw: resp}, err
	}
	if le := tallyLineError(resp); le != "" {
		return WriteResult{Raw: resp}, fmt.Errorf("Tally rejected the ledger: %s", le)
	}
	return WriteResult{OK: true, Detail: action + " ledger " + in.Name, Raw: resp}, nil
}

func (e *TallyXMLEngine) CreateVoucher(ctx context.Context, company string, in VoucherInput) (WriteResult, error) {
	if strings.TrimSpace(in.Type) == "" {
		return WriteResult{}, fmt.Errorf("voucher type is required")
	}
	if len(in.Entries) == 0 && len(in.Items) == 0 {
		return WriteResult{}, fmt.Errorf("at least one ledger entry or inventory line is required")
	}

	var b strings.Builder
	b.WriteString("<TALLYMESSAGE xmlns:UDF=\"TallyUDF\">")
	b.WriteString("<VOUCHER VCHTYPE=\"" + xmlEscape(in.Type) + "\" ACTION=\"Create\" OBJVIEW=\"Invoice Voucher View\">")
	if in.Date != "" {
		b.WriteString("<DATE>" + xmlEscape(tallyDate(in.Date)) + "</DATE>")
	}
	b.WriteString("<VOUCHERTYPENAME>" + xmlEscape(in.Type) + "</VOUCHERTYPENAME>")
	if in.VoucherNumber != "" {
		b.WriteString("<VOUCHERNUMBER>" + xmlEscape(in.VoucherNumber) + "</VOUCHERNUMBER>")
	}
	if in.Narration != "" {
		b.WriteString("<NARRATION>" + xmlEscape(in.Narration) + "</NARRATION>")
	}
	if in.Party != "" {
		b.WriteString("<PARTYLEDGERNAME>" + xmlEscape(in.Party) + "</PARTYLEDGERNAME>")
		b.WriteString("<BASICBUYERNAME>" + xmlEscape(in.Party) + "</BASICBUYERNAME>")
	}
	if in.PlaceOfSupply != "" {
		b.WriteString("<PLACEOFSUPPLY>" + xmlEscape(in.PlaceOfSupply) + "</PLACEOFSUPPLY>")
	}

	// Accounting legs: the party, the tax ledgers and any other adjustment.
	for _, entry := range in.Entries {
		if strings.TrimSpace(entry.Ledger) == "" {
			return WriteResult{}, fmt.Errorf("every ledger entry needs a ledger name")
		}
		b.WriteString("<ALLLEDGERENTRIES.LIST>")
		b.WriteString("<LEDGERNAME>" + xmlEscape(entry.Ledger) + "</LEDGERNAME>")
		b.WriteString("<ISDEEMEDPOSITIVE>" + yesNo(entry.IsDebit) + "</ISDEEMEDPOSITIVE>")
		sign := ""
		if entry.IsDebit {
			sign = "-"
		}
		b.WriteString("<AMOUNT>" + sign + money(entry.Amount) + "</AMOUNT>")
		b.WriteString("</ALLLEDGERENTRIES.LIST>")
	}

	// Inventory legs: the goods on the invoice. Each line is allocated to a
	// sales or purchase ledger, which is what makes Tally post the accounts.
	// The side depends on the document: a sale credits the item, a purchase
	// debits it, exactly as Tally's own exports do.
	inventorySide := yesNo(inventoryIsDebit(in.Type))
	for _, item := range in.Items {
		if strings.TrimSpace(item.StockItem) == "" {
			return WriteResult{}, fmt.Errorf("every inventory line needs a stock item")
		}
		if item.Quantity <= 0 {
			return WriteResult{}, fmt.Errorf("stock item %q needs a quantity greater than zero", item.StockItem)
		}
		amount := item.Amount
		if amount == 0 {
			amount = item.Quantity*item.Rate - item.Discount
		}
		unit := orDefault(item.Unit, "Nos")
		qty := money(item.Quantity)

		b.WriteString("<ALLINVENTORYENTRIES.LIST>")
		b.WriteString("<STOCKITEMNAME>" + xmlEscape(item.StockItem) + "</STOCKITEMNAME>")
		b.WriteString("<ISDEEMEDPOSITIVE>" + inventorySide + "</ISDEEMEDPOSITIVE>")
		b.WriteString("<RATE>" + money(item.Rate) + "/" + xmlEscape(unit) + "</RATE>")
		b.WriteString("<AMOUNT>" + money(amount) + "</AMOUNT>")
		b.WriteString("<ACTUALQTY>" + qty + " " + xmlEscape(unit) + "</ACTUALQTY>")
		b.WriteString("<BILLEDQTY>" + qty + " " + xmlEscape(unit) + "</BILLEDQTY>")
		if item.Discount != 0 {
			b.WriteString("<DISCOUNT>" + money(item.Discount) + "</DISCOUNT>")
		}
		if item.Godown != "" {
			b.WriteString("<GODOWNNAME>" + xmlEscape(item.Godown) + "</GODOWNNAME>")
		}
		if item.Ledger != "" {
			b.WriteString("<ACCOUNTINGALLOCATIONS.LIST>")
			b.WriteString("<LEDGERNAME>" + xmlEscape(item.Ledger) + "</LEDGERNAME>")
			b.WriteString("<ISDEEMEDPOSITIVE>" + inventorySide + "</ISDEEMEDPOSITIVE>")
			b.WriteString("<AMOUNT>" + money(amount) + "</AMOUNT>")
			b.WriteString("</ACCOUNTINGALLOCATIONS.LIST>")
		}
		b.WriteString("</ALLINVENTORYENTRIES.LIST>")
	}

	b.WriteString("</VOUCHER></TALLYMESSAGE>")

	resp, err := e.postImport(ctx, e.importRequest(b.String(), "Vouchers", company))
	if err != nil {
		return WriteResult{Raw: resp}, err
	}
	if le := tallyLineError(resp); le != "" {
		return WriteResult{Raw: resp}, fmt.Errorf("Tally rejected the voucher: %s", le)
	}
	detail := "created " + in.Type + " voucher"
	if len(in.Items) > 0 {
		detail = fmt.Sprintf("created %s voucher with %d inventory line(s)", in.Type, len(in.Items))
	}
	return WriteResult{OK: true, Detail: detail, Raw: resp}, nil
}

// postImport mirrors post but returns the body even on a Tally-line error, so
// the caller can show the accountant exactly what Tally complained about.
func (e *TallyXMLEngine) postImport(ctx context.Context, body string) (string, error) {
	select {
	case e.gate <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-e.gate }()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url(), bytes.NewReader(encodeUTF16LE(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "text/xml;charset=utf-16")
	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach Tally at %s: %w", e.url(), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	out := decodeUTF16LE(raw)
	if ex := tallyException(out); ex != "" {
		return out, fmt.Errorf("Tally error: %s", ex)
	}
	return out, nil
}

func (e *TallyXMLEngine) RawXML(ctx context.Context, company, body string) (string, error) {
	_ = company
	return e.post(ctx, body)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func flattenReport(root *Node) []map[string]string {
	skip := map[string]bool{
		"ENVELOPE": true, "HEADER": true, "BODY": true, "DESC": true, "DATA": true,
		"TALLYMESSAGE": true, "STATICVARIABLES": true, "TDL": true, "TDLMESSAGE": true,
		"COLLECTION": true, "REQUESTDESC": true, "REQUESTDATA": true, "IMPORTDATA": true,
	}
	var rows []map[string]string
	seen := map[string]bool{}

	var walk func(*Node)
	walk = func(n *Node) {
		if !skip[n.Name] {
			row := map[string]string{}
			leaves := 0
			for _, c := range n.Children {
				if len(c.Children) == 0 {
					if v := strings.TrimSpace(c.Text); v != "" {
						row[c.Name] = v
						leaves++
					}
				}
			}
			if a := n.Attr("NAME"); a != "" {
				row["NAME"] = a
			}
			if leaves >= 2 {
				key := rowKey(row)
				if !seen[key] {
					seen[key] = true
					rows = append(rows, row)
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if len(rows) > 500 {
		rows = rows[:500]
	}
	return rows
}

func rowKey(row map[string]string) string {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(row[k])
		b.WriteByte(';')
	}
	return b.String()
}

func parseAmount(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	neg := false
	low := strings.ToLower(s)
	switch {
	case strings.HasSuffix(low, "dr"):
		s = strings.TrimSpace(s[:len(s)-2])
	case strings.HasSuffix(low, "cr"):
		s = strings.TrimSpace(s[:len(s)-2])
		neg = true
	}
	s = strings.ReplaceAll(s, ",", "")
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	if neg {
		return -v
	}
	return v
}

// isoDate normalises Tally's date spellings to YYYY-MM-DD.
func isoDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) == 8 && isDigits(s) {
		return s[0:4] + "-" + s[4:6] + "-" + s[6:8]
	}
	layouts := []string{"2-Jan-06", "02-Jan-06", "2-Jan-2006", "02-01-2006", "2006-01-02", "2/1/2006"}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return s
}

// tallyDate converts an ISO date to Tally's YYYYMMDD.
func tallyDate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 8 && isDigits(s) {
		return s
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("20060102")
	}
	return strings.ReplaceAll(s, "-", "")
}

func inDateRange(date, from, to string) bool {
	if date == "" {
		return true
	}
	if from != "" && date < from {
		return false
	}
	if to != "" && date > to {
		return false
	}
	return true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;")
	return r.Replace(s)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func sortVouchersDesc(v []Voucher) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j].Date > v[j-1].Date; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

// ── Tally integration methods ───────────────────────────────────────────────
//
// These four cover the whole surface of Tally's integration API. First-class
// screens and MCP tools handle the common cases; these handle everything else,
// which is why no capability is lost by not porting 209 named tools verbatim.

func (e *TallyXMLEngine) QueryCollection(ctx context.Context, company, name, tdl string, vars map[string]string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("collection name is required")
	}
	if strings.TrimSpace(tdl) == "" {
		return "", fmt.Errorf("the TDL collection definition is required")
	}
	return e.post(ctx, e.collectionRequest(name, tdl, company, vars))
}

func (e *TallyXMLEngine) EvaluateFunction(ctx context.Context, company, expression string) (string, error) {
	if strings.TrimSpace(expression) == "" {
		return "", fmt.Errorf("a TDL expression is required")
	}
	const fn = "TWUIEvaluate"
	tdl := "<FUNCTION NAME=\"" + fn + "\" RETURNTYPE=\"String\"><SET>" + xmlEscape(expression) + "</SET></FUNCTION>"
	envelope := "<ENVELOPE><HEADER><VERSION>1</VERSION><TALLYREQUEST>Export Data</TALLYREQUEST>" +
		"<TYPE>Function</TYPE><ID>" + fn + "</ID></HEADER><BODY><DESC>" +
		"<STATICVARIABLES>" + e.staticVars(company, nil) + "</STATICVARIABLES>" +
		"<TDL><TDLMESSAGE>" + tdl + "</TDLMESSAGE></TDL></DESC></BODY></ENVELOPE>"
	return e.post(ctx, envelope)
}

func (e *TallyXMLEngine) ExportObject(ctx context.Context, company, subtype, objectID string, fields []string) (string, error) {
	if strings.TrimSpace(subtype) == "" {
		return "", fmt.Errorf("object subtype is required, for example Ledger, Group, StockItem, Voucher")
	}
	if strings.TrimSpace(objectID) == "" {
		return "", fmt.Errorf("object id is required")
	}
	var fetch strings.Builder
	fetch.WriteString("<FETCHLIST>")
	if len(fields) == 0 {
		fetch.WriteString("<FETCH>Name</FETCH>")
	} else {
		for _, f := range fields {
			fetch.WriteString("<FETCH>" + xmlEscape(f) + "</FETCH>")
		}
	}
	fetch.WriteString("</FETCHLIST>")

	tdl := "<OBJECT NAME=\"" + xmlEscape(subtype) + "\" ISMODIFY=\"No\">" + fetch.String() + "</OBJECT>"
	envelope := "<ENVELOPE><HEADER><VERSION>1</VERSION><TALLYREQUEST>Export Data</TALLYREQUEST>" +
		"<TYPE>Object</TYPE><SUBTYPE>" + xmlEscape(subtype) + "</SUBTYPE>" +
		"<ID TYPE=\"Name\">" + xmlEscape(objectID) + "</ID></HEADER><BODY><DESC>" +
		"<STATICVARIABLES>" + e.staticVars(company, nil) + "</STATICVARIABLES>" +
		"<TDL><TDLMESSAGE>" + tdl + "</TDLMESSAGE></TDL></DESC></BODY></ENVELOPE>"
	return e.post(ctx, envelope)
}

func (e *TallyXMLEngine) ExecuteAction(ctx context.Context, company, actionID string) (string, error) {
	if strings.TrimSpace(actionID) == "" {
		return "", fmt.Errorf("action id is required")
	}
	envelope := "<ENVELOPE><HEADER><VERSION>1</VERSION><TALLYREQUEST>Action</TALLYREQUEST></HEADER>" +
		"<BODY><ACTION><TYPE>" + xmlEscape(actionID) + "</TYPE></ACTION></BODY></ENVELOPE>"
	return e.post(ctx, envelope)
}

// ImportData accepts any Tally import payload: masters under "All Masters",
// vouchers under "Vouchers", or any other report name Tally understands.
func (e *TallyXMLEngine) ImportData(ctx context.Context, company, reportName, data string) (string, error) {
	if strings.TrimSpace(data) == "" {
		return "", fmt.Errorf("import data is required")
	}
	if strings.TrimSpace(reportName) == "" {
		reportName = "All Masters"
	}
	resp, err := e.postImport(ctx, e.importRequest(data, reportName, company))
	if err != nil {
		return resp, err
	}
	if le := tallyLineError(resp); le != "" {
		return resp, fmt.Errorf("Tally rejected the import: %s", le)
	}
	return resp, nil
}

// ── inventory masters ──────────────────────────────────────────────────────

// StockItems reads the inventory masters with their closing position.
func (e *TallyXMLEngine) StockItems(ctx context.Context, company string) ([]StockItem, error) {
	tdl := "<COLLECTION NAME=\"TWUIStockItems\" ISMODIFY=\"No\"><TYPE>StockItem</TYPE>" +
		"<NATIVEMETHOD>Name</NATIVEMETHOD><NATIVEMETHOD>Parent</NATIVEMETHOD>" +
		"<NATIVEMETHOD>BaseUnits</NATIVEMETHOD><NATIVEMETHOD>ClosingQuantity</NATIVEMETHOD>" +
		"<NATIVEMETHOD>ClosingRate</NATIVEMETHOD><NATIVEMETHOD>ClosingValue</NATIVEMETHOD>" +
		"</COLLECTION>"
	resp, err := e.post(ctx, e.collectionRequest("TWUIStockItems", tdl, company, nil))
	if err != nil {
		return nil, err
	}
	root, _ := parseXML(resp)

	seen := map[string]bool{}
	var out []StockItem
	for _, n := range root.FindAll("STOCKITEM") {
		name := firstNonEmpty(n.Attr("NAME"), n.LeafText("NAME"))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, StockItem{
			Name:         name,
			Parent:       n.LeafText("PARENT"),
			BaseUnit:     n.LeafText("BASEUNITS"),
			ClosingQty:   parseLeadingFloat(n.LeafText("CLOSINGQUANTITY")),
			ClosingRate:  parseLeadingFloat(n.LeafText("CLOSINGRATE")),
			ClosingValue: parseAmount(n.LeafText("CLOSINGVALUE")),
		})
	}
	return out, nil
}

// CreateStockItem creates an inventory master. GST details are included only
// when supplied, because the GST block shape varies between Tally releases.
func (e *TallyXMLEngine) CreateStockItem(ctx context.Context, company string, in StockItemInput) (WriteResult, error) {
	if strings.TrimSpace(in.Name) == "" {
		return WriteResult{}, fmt.Errorf("stock item name is required")
	}
	action := orDefault(in.Action, "Create")
	unit := orDefault(in.BaseUnit, "Nos")

	var b strings.Builder
	b.WriteString("<TALLYMESSAGE xmlns:UDF=\"TallyUDF\">")
	b.WriteString("<STOCKITEM NAME=\"" + xmlEscape(in.Name) + "\" ACTION=\"" + xmlEscape(action) + "\">")
	b.WriteString("<PARENT>" + xmlEscape(orDefault(in.Parent, "Primary")) + "</PARENT>")
	b.WriteString("<BASEUNITS>" + xmlEscape(unit) + "</BASEUNITS>")
	if in.OpeningQty != 0 {
		b.WriteString("<OPENINGBALANCE>" + money(in.OpeningQty) + " " + xmlEscape(unit) + "</OPENINGBALANCE>")
	}
	if in.OpeningRate != 0 {
		b.WriteString("<OPENINGRATE>" + money(in.OpeningRate) + "/" + xmlEscape(unit) + "</OPENINGRATE>")
		b.WriteString("<OPENINGVALUE>" + money(in.OpeningQty*in.OpeningRate) + "</OPENINGVALUE>")
	}
	if in.GSTRate != 0 || in.HSNCode != "" {
		b.WriteString("<GSTDETAILS.LIST>")
		if in.HSNCode != "" {
			b.WriteString("<HSNCODE>" + xmlEscape(in.HSNCode) + "</HSNCODE>")
		}
		if in.GSTRate != 0 {
			b.WriteString("<GSTRATE>" + money(in.GSTRate) + "</GSTRATE>")
		}
		b.WriteString("<TAXABILITY>Taxable</TAXABILITY>")
		b.WriteString("</GSTDETAILS.LIST>")
	}
	b.WriteString("</STOCKITEM></TALLYMESSAGE>")

	resp, err := e.postImport(ctx, e.importRequest(b.String(), "All Masters", company))
	if err != nil {
		return WriteResult{Raw: resp}, err
	}
	if le := tallyLineError(resp); le != "" {
		return WriteResult{Raw: resp}, fmt.Errorf("Tally rejected the stock item: %s", le)
	}
	return WriteResult{OK: true, Detail: action + " stock item " + in.Name, Raw: resp}, nil
}

// inventoryIsDebit reports which side of the books an inventory line sits on.
// Sales and credit notes credit the goods side; purchases and debit notes
// debit it.
func inventoryIsDebit(voucherType string) bool {
	switch strings.ToLower(strings.TrimSpace(voucherType)) {
	case "purchase", "debit note", "stock journal", "material in", "rejections in", "job work in":
		return true
	}
	return false
}

// money renders an amount the way Tally expects: plain, two decimals, no
// thousands separators and no currency symbol.
func money(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// parseLeadingFloat reads the number at the start of a value like "200.00 Nos".
func parseLeadingFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	end := 0
	for end < len(s) {
		c := s[end]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(s[:end], 64)
	if err != nil {
		return 0
	}
	return v
}
