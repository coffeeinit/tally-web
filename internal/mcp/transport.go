package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// ServeHTTP exposes the server over Streamable HTTP on the same port as the UI.
// Stateless: every POST carries a complete JSON-RPC message.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			writeJSONResponse(w, errorResponse(nil, CodeParseError, "could not read the request body"))
			return
		}
		var req Request
		if err := json.Unmarshal(raw, &req); err != nil {
			writeJSONResponse(w, errorResponse(nil, CodeParseError, "parse error: "+err.Error()))
			return
		}
		if req.IsNotification() {
			_ = s.Handle(r.Context(), req)
			w.WriteHeader(http.StatusAccepted)
			return
		}
		resp := s.Handle(r.Context(), req)
		if resp == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if wantsSSE(r) {
			writeSSEResponse(w, resp)
			return
		}
		writeJSONResponse(w, resp)

	case http.MethodGet:
		// A stateless server has no server-initiated stream to offer.
		w.Header().Set("Allow", "POST, DELETE")
		writeJSONResponse(w, errorResponse(nil, CodeInvalidRequest,
			"this endpoint is stateless; POST JSON-RPC messages to /mcp"))

	case http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)

	default:
		w.Header().Set("Allow", "POST, GET, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func wantsSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

func writeJSONResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeSSEResponse(w http.ResponseWriter, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		writeJSONResponse(w, errorResponse(nil, CodeInternalError, "could not encode the response"))
		return
	}
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("event: message\ndata: "))
	_, _ = w.Write(raw)
	_, _ = w.Write([]byte("\n\n"))
	if flusher != nil {
		flusher.Flush()
	}
}

// ServeStdio runs newline-delimited JSON-RPC over a pipe, which is how desktop
// MCP clients launch a server. stdout is the protocol channel, so nothing else
// may ever be written there.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 1<<20), 8<<20)
	encoder := json.NewEncoder(out)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		resp := s.Handle(ctx, req)
		if resp == nil {
			continue
		}
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}
