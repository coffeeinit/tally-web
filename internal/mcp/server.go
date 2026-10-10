package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Tool is one callable MCP tool.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	// Handler returns any JSON-serialisable value; it is rendered as text.
	Handler func(ctx context.Context, args map[string]any) (any, error)
}

// Server dispatches JSON-RPC to tools. It is transport agnostic: the same
// instance serves stdio for desktop clients and HTTP for the web.
type Server struct {
	Name    string
	Version string
	tools   map[string]*Tool
	order   []string
}

func NewServer(name, version string) *Server {
	return &Server{Name: name, Version: version, tools: map[string]*Tool{}}
}

func (s *Server) Add(t Tool) {
	if _, exists := s.tools[t.Name]; !exists {
		s.order = append(s.order, t.Name)
	}
	s.tools[t.Name] = &t
}

func (s *Server) ToolNames() []string {
	out := append([]string(nil), s.order...)
	sort.Strings(out)
	return out
}

func (s *Server) Count() int { return len(s.tools) }

// Handle executes one JSON-RPC message. It returns nil for notifications.
func (s *Server) Handle(ctx context.Context, req Request) *Response {
	if req.IsNotification() {
		// notifications/initialized and friends need no work.
		return nil
	}
	switch req.Method {
	case "initialize":
		return &Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    s.Name,
				"version": s.Version,
			},
			"instructions": "Tally control plane. Tools are named tally_*. " +
				"Reads work against a live Tally XML instance, the on-disk file engine, or the built-in demo. " +
				"Writes only work when the server was started with --allow-writes.",
		}}

	case "ping":
		return &Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}

	case "tools/list":
		return &Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": s.listTools()}}

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "invalid params: "+err.Error())
		}
		tool, ok := s.tools[params.Name]
		if !ok {
			return errorResponse(req.ID, CodeInvalidParams, "unknown tool: "+params.Name)
		}
		if params.Arguments == nil {
			params.Arguments = map[string]any{}
		}
		value, err := tool.Handler(ctx, params.Arguments)
		if err != nil {
			return &Response{JSONRPC: "2.0", ID: req.ID, Result: callResult{
				Content: []contentItem{{Type: "text", Text: err.Error()}},
				IsError: true,
			}}
		}
		text := ""
		switch v := value.(type) {
		case string:
			text = v
		default:
			raw, merr := json.MarshalIndent(v, "", "  ")
			if merr != nil {
				text = fmt.Sprint(v)
			} else {
				text = string(raw)
			}
		}
		return &Response{JSONRPC: "2.0", ID: req.ID, Result: callResult{
			Content: []contentItem{{Type: "text", Text: text}},
		}}

	case "resources/list":
		return &Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"resources": []any{}}}

	case "prompts/list":
		return &Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"prompts": []any{}}}

	default:
		return errorResponse(req.ID, CodeMethodNotFound, "method not found: "+req.Method)
	}
}

func (s *Server) listTools() []map[string]any {
	out := make([]map[string]any, 0, len(s.order))
	for _, name := range s.order {
		tool := s.tools[name]
		out = append(out, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.Schema,
		})
	}
	return out
}

func errorResponse(id json.RawMessage, code int, message string) *Response {
	return &Response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: message}}
}

// ── argument helpers shared by the tool handlers ────────────────────────────

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

func strMapArg(m map[string]any, key string) map[string]string {
	out := map[string]string{}
	if v, ok := m[key]; ok {
		if mm, ok := v.(map[string]any); ok {
			for k, val := range mm {
				if s, ok := val.(string); ok {
					out[k] = s
				}
			}
		}
	}
	return out
}

func strSliceArg(m map[string]any, key string) []string {
	var out []string
	for _, item := range sliceArg(m, key) {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
