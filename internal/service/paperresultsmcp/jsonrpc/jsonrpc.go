// Package jsonrpc is the JSON-RPC 2.0 envelope and the MCP initialize answer of
// the paper-results MCP server: the wire shapes and writers that know nothing
// of the tools. It was split out of the server package to keep that directory
// under its line limit (issue #1085).
package jsonrpc

import (
	"encoding/json"
	"net/http"
)

// JSON-RPC 2.0 error codes.
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	ServerError    = -32000 // spec error codes ride in error.data.code
)

// Request is one JSON-RPC request or notification (no id).
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// ErrorBody is the error member of a response.
type ErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Response is one JSON-RPC response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *ErrorBody      `json:"error,omitempty"`
}

// supportedVersions are the MCP protocol revisions this server speaks, the
// newest first. The tool JSON is identical in all of them.
var supportedVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// InitializeResult answers initialize: the newest supported protocol revision
// the client offered (else the newest), the capabilities and instructions.
func InitializeResult(params json.RawMessage, instructions string) map[string]any {
	version := supportedVersions[0]
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if json.Unmarshal(params, &p) == nil {
		for _, v := range supportedVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]any{"name": "paper-results-mcp", "version": "1.1.0"},
		"instructions":    instructions,
	}
}

// WriteJSON writes v as an uncached JSON body with status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// WriteResult answers request id with result.
func WriteResult(w http.ResponseWriter, id json.RawMessage, result any) {
	WriteJSON(w, http.StatusOK, Response{JSONRPC: "2.0", ID: id, Result: result})
}

// WriteError answers with a JSON-RPC error; a missing id is answered as null.
func WriteError(w http.ResponseWriter, status int, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	WriteJSON(w, status, Response{JSONRPC: "2.0", ID: id, Error: &ErrorBody{Code: code, Message: message}})
}
