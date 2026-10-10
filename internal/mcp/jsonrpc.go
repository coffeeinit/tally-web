// Package mcp implements the Model Context Protocol in Go, with no external
// dependencies, so the web UI and the MCP server are the same process, the same
// port and the same engine.
package mcp

import "encoding/json"

// ProtocolVersion is the MCP revision this server speaks.
const ProtocolVersion = "2025-06-18"

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification reports whether the message expects no reply.
func (r Request) IsNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callResult struct {
	Content []contentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}
