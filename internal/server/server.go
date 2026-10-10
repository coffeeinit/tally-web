package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"tallywebui/internal/ai"
	"tallywebui/internal/config"
	"tallywebui/internal/engine"
	"tallywebui/internal/mcp"
	"tallywebui/internal/web"
)

// Server wires the registry, the assistant settings and the embedded UI into
// one http.Handler. Everything the product does is reachable from here, so the
// whole app is a single binary with a single port.
type Server struct {
	cfg      config.Config
	registry *engine.Registry
	settings *ai.Store
	mcp      *mcp.Server
	mux      *http.ServeMux
}

func New(cfg config.Config, registry *engine.Registry, settings *ai.Store) *Server {
	// The MCP server is built here, in the same process as the UI, bound to the
	// same engine registry. There is no second service to run or keep in sync.
	mcpServer := mcp.NewServer("tally-web-ui", "0.1.0")
	mcp.RegisterTallyTools(mcpServer, registry, cfg.AllowWrites)
	mcp.RegisterOperationTools(mcpServer, registry, cfg.AllowWrites)

	s := &Server{
		cfg:      cfg,
		registry: registry,
		settings: settings,
		mcp:      mcpServer,
		mux:      http.NewServeMux(),
	}
	s.routes()
	return s
}

// MCP exposes the tool server so it can also be driven over stdio.
func (s *Server) MCP() *mcp.Server { return s.mcp }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.mux.ServeHTTP(w, r)
	_ = start
}

func (s *Server) routes() {
	m := s.mux

	m.HandleFunc("GET /api/health", s.handleHealth)
	m.HandleFunc("GET /api/instances", s.handleInstances)
	m.HandleFunc("POST /api/instances/default", s.handleSetDefault)

	m.HandleFunc("GET /api/companies", s.handleCompanies)
	m.HandleFunc("GET /api/ledgers", s.handleLedgers)
	m.HandleFunc("GET /api/stock-items", s.handleStockItems)
	m.HandleFunc("POST /api/stock-items", s.handleCreateStockItem)
	m.HandleFunc("GET /api/vouchers", s.handleVouchers)
	m.HandleFunc("GET /api/reports", s.handleReport)
	m.HandleFunc("POST /api/vouchers", s.handleCreateVoucher)
	m.HandleFunc("POST /api/ledgers", s.handleCreateLedger)
	m.HandleFunc("POST /api/raw-xml", s.handleRawXML)

	m.HandleFunc("GET /api/ai/presets", s.handleAIPresets)
	m.HandleFunc("GET /api/ai/settings", s.handleAISettingsGet)
	m.HandleFunc("PUT /api/ai/settings", s.handleAISettingsPut)
	m.HandleFunc("GET /api/ai/tools", s.handleAITools)
	m.HandleFunc("POST /api/ai/chat", s.handleAIChat)

	m.HandleFunc("GET /api/mcp/info", s.handleMCPInfo)
	m.HandleFunc("/mcp", s.mcp.ServeHTTP)

	m.HandleFunc("/", s.handleStatic)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func (s *Server) engineFor(r *http.Request) (engine.Engine, error) {
	return s.registry.Resolve(r.URL.Query().Get("instance"))
}

func (s *Server) companyFor(r *http.Request) string {
	if c := r.URL.Query().Get("company"); c != "" {
		return c
	}
	return ""
}

func intParam(r *http.Request, name string, def int) int {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// ── core API ────────────────────────────────────────────────────────────────

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	cur := s.settings.Get()
	local := strings.Contains(cur.BaseURL, "localhost") || strings.Contains(cur.BaseURL, "127.0.0.1")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"service":      "tally-web-ui",
		"default":      s.registry.Default(),
		"registry":     s.registry.Path(),
		"allowWrites":  s.cfg.AllowWrites,
		"aiConfigured": cur.APIKey != "" || local,
		"aiModel":      cur.Model,
		"mcpEndpoint":  "/mcp",
		"mcpToolCount": s.mcp.Count(),
		"time":         time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleInstances(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, map[string]any{
		"default":   s.registry.Default(),
		"instances": s.registry.List(ctx),
	})
}

func (s *Server) handleSetDefault(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.registry.WriteRegistryDefault(body.ID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"default": body.ID})
}

func (s *Server) handleCompanies(w http.ResponseWriter, r *http.Request) {
	eng, err := s.engineFor(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	companies, err := eng.Companies(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": eng.ID(), "product": eng.Product(), "companies": companies})
}

func (s *Server) handleLedgers(w http.ResponseWriter, r *http.Request) {
	eng, err := s.engineFor(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	ledgers, err := eng.Ledgers(ctx, s.companyFor(r))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": eng.ID(), "count": len(ledgers), "ledgers": ledgers})
}

func (s *Server) handleStockItems(w http.ResponseWriter, r *http.Request) {
	eng, err := s.engineFor(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	items, err := eng.StockItems(ctx, s.companyFor(r))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": eng.ID(), "count": len(items), "stock_items": items})
}

func (s *Server) handleCreateStockItem(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowWrites {
		writeErr(w, http.StatusForbidden, fmt.Errorf("writes are disabled; start the server with TWUI_ALLOW_WRITES=1"))
		return
	}
	eng, err := s.engineFor(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Instance    string  `json:"instance"`
		Company     string  `json:"company"`
		Name        string  `json:"name"`
		Parent      string  `json:"parent"`
		BaseUnit    string  `json:"base_unit"`
		OpeningQty  float64 `json:"opening_qty"`
		OpeningRate float64 `json:"opening_rate"`
		GSTRate     float64 `json:"gst_rate"`
		HSNCode     string  `json:"hsn_code"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := eng.CreateStockItem(ctx, body.Company, engine.StockItemInput{
		Name: body.Name, Parent: body.Parent, BaseUnit: body.BaseUnit,
		OpeningQty: body.OpeningQty, OpeningRate: body.OpeningRate,
		GSTRate: body.GSTRate, HSNCode: body.HSNCode,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleVouchers(w http.ResponseWriter, r *http.Request) {
	eng, err := s.engineFor(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	q := engine.Query{
		Type:  r.URL.Query().Get("type"),
		From:  r.URL.Query().Get("from"),
		To:    r.URL.Query().Get("to"),
		Party: r.URL.Query().Get("party"),
		Limit: intParam(r, "limit", 500),
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	vouchers, err := eng.Vouchers(ctx, s.companyFor(r), q)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": eng.ID(), "count": len(vouchers), "vouchers": vouchers})
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	eng, err := s.engineFor(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	name := r.URL.Query().Get("report")
	if name == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("report is required"))
		return
	}
	q := engine.Query{From: r.URL.Query().Get("from"), To: r.URL.Query().Get("to")}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	rep, err := eng.NamedReport(ctx, s.companyFor(r), name, q)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instance": eng.ID(), "report": rep})
}

func (s *Server) handleCreateLedger(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowWrites {
		writeErr(w, http.StatusForbidden, fmt.Errorf("writes are disabled; start the server with TWUI_ALLOW_WRITES=1"))
		return
	}
	eng, err := s.engineFor(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Instance string `json:"instance"`
		Company  string `json:"company"`
		Name     string `json:"name"`
		Parent   string `json:"parent"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := eng.CreateLedger(ctx, body.Company, engine.LedgerInput{Name: body.Name, Parent: body.Parent})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleCreateVoucher(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowWrites {
		writeErr(w, http.StatusForbidden, fmt.Errorf("writes are disabled; start the server with TWUI_ALLOW_WRITES=1"))
		return
	}
	eng, err := s.engineFor(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Instance      string             `json:"instance"`
		Company       string             `json:"company"`
		Type          string             `json:"type"`
		Date          string             `json:"date"`
		Party         string             `json:"party"`
		Narration     string             `json:"narration"`
		Entries       []engine.Entry     `json:"entries"`
		Items         []engine.ItemLine  `json:"items"`
		VoucherNumber string             `json:"voucher_number"`
		PlaceOfSupply string             `json:"place_of_supply"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := eng.CreateVoucher(ctx, body.Company, engine.VoucherInput{
		Type: body.Type, Date: body.Date, Party: body.Party, Narration: body.Narration,
		Entries: body.Entries, Items: body.Items,
		VoucherNumber: body.VoucherNumber, PlaceOfSupply: body.PlaceOfSupply,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleRawXML(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowWrites {
		writeErr(w, http.StatusForbidden, fmt.Errorf("raw XML is disabled; start the server with TWUI_ALLOW_WRITES=1"))
		return
	}
	eng, err := s.engineFor(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Instance string `json:"instance"`
		Company  string `json:"company"`
		XML      string `json:"xml"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	out, err := eng.RawXML(ctx, body.Company, body.XML)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": out})
}

// ── AI ──────────────────────────────────────────────────────────────────────

func (s *Server) handleMCPInfo(w http.ResponseWriter, r *http.Request) {
	names := s.mcp.ToolNames()
	host := r.Host
	if host == "" {
		host = "127.0.0.1"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"endpoint":       "http://" + host + "/mcp",
		"transport":      "streamable-http (stateless JSON-RPC, SSE when requested)",
		"stdio":          "run with --mcp-stdio to serve MCP over stdio as well",
		"protocol":       mcp.ProtocolVersion,
		"toolCount":      len(names),
		"tools":          names,
		"writesEnabled":  s.cfg.AllowWrites,
	})
}

func (s *Server) handleAIPresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"presets": ai.Presets})
}

func (s *Server) handleAISettingsGet(w http.ResponseWriter, r *http.Request) {
	cur := s.settings.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":        cur.View(),
		"writesAllowedAtServer": s.cfg.AllowWrites,
	})
}

func (s *Server) handleAISettingsPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind         string `json:"kind"`
		BaseURL      string `json:"baseUrl"`
		Model        string `json:"model"`
		APIKey       string `json:"apiKey"`
		MaxSteps     int    `json:"maxSteps"`
		SystemPrompt string `json:"systemPrompt"`
		AllowWrites  bool   `json:"allowWrites"`
		ClearKey     bool   `json:"clearKey"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	patch := ai.Settings{
		Kind: body.Kind, BaseURL: body.BaseURL, Model: body.Model,
		APIKey: body.APIKey, MaxSteps: body.MaxSteps,
		SystemPrompt: body.SystemPrompt, AllowWrites: body.AllowWrites,
	}
	updated, err := s.settings.Update(patch, body.APIKey != "" || body.ClearKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": updated.View()})
}

func (s *Server) handleAITools(w http.ResponseWriter, r *http.Request) {
	box := &ai.Toolbox{Registry: s.registry, AllowWrites: s.cfg.AllowWrites}
	writeJSON(w, http.StatusOK, map[string]any{"tools": box.Specs()})
}

func (s *Server) handleAIChat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Messages []ai.Message `json:"messages"`
		Instance string       `json:"instance"`
		Company  string       `json:"company"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	current := s.settings.Get()
	provider, err := ai.New(current)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("streaming is not supported by this server"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	send := func(e ai.Event) {
		raw, err := json.Marshal(e)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", raw)
		flusher.Flush()
	}

	box := &ai.Toolbox{
		Registry:    s.registry,
		Instance:    body.Instance,
		Company:     body.Company,
		AllowWrites: s.cfg.AllowWrites && current.AllowWrites,
		MaxRows:     80,
	}
	agent := &ai.Agent{Provider: provider, Settings: current, Tools: box}
	if _, err := agent.Run(r.Context(), body.Messages, send); err != nil {
		send(ai.Event{Type: "error", Text: err.Error()})
	}
}

// ── embedded UI ─────────────────────────────────────────────────────────────

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no such endpoint: %s", r.URL.Path))
		return
	}
	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		http.Error(w, "UI is not embedded in this build", http.StatusInternalServerError)
		return
	}
	if s.cfg.WebDir != "" {
		if _, err := fs.Stat(sub, "index.html"); err != nil {
			http.Error(w, "UI is not embedded in this build", http.StatusInternalServerError)
			return
		}
	}

	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	if !strings.Contains(path.Base(name), ".") {
		name = "index.html"
	}

	data, err := fs.ReadFile(sub, name)
	if err != nil {
		data, err = fs.ReadFile(sub, "index.html")
		name = "index.html"
		if err != nil {
			http.Error(w, "UI is not embedded in this build", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", contentType(name))
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	_, _ = w.Write(data)
}

func contentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".js"), strings.HasSuffix(name, ".mjs"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".json"):
		return "application/json; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".ico"):
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}
