package paperresultsmcp

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const (
	// maxBodyBytes bounds a JSON-RPC request; tool arguments are tiny.
	maxBodyBytes = 64 << 10

	headerAccessID     = "CF-Access-Client-Id"
	headerAccessSecret = "CF-Access-Client-Secret"
)

// Handler returns the MCP endpoint. Every path and method is authenticated
// first; only then is anything parsed, and only a tools/call reaches SQL.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	// Authentication comes before everything else: no body is read and no
	// SQL or database connection is touched for an unauthenticated request.
	if code, ok := s.authenticate(r); !ok {
		s.log.Warn("paper-results-mcp: request rejected", "tool", "", "code", string(code))
		writeError(w, nil, code)
		return
	}
	if r.Method != http.MethodPost {
		// No server-initiated stream and no session to delete (Streamable
		// HTTP permits answering these with 405).
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeRPCError(w, http.StatusBadRequest, nil, rpcInvalidRequest, "リクエストが不正です。")
		return
	}
	s.handleRPC(w, r, raw)
}

// authenticate applies the optional Access header check (§3.5, before the
// key) and then the mandatory Bearer key (§3.3). Both compare in constant
// time and neither value is logged or returned.
func (s *Server) authenticate(r *http.Request) (Code, bool) {
	cfg := s.opts.Settings
	if cfg.VerifyAccessHeaders {
		idOK := secretEqual(r.Header.Get(headerAccessID), cfg.AccessClientID)
		secretOK := secretEqual(r.Header.Get(headerAccessSecret), cfg.AccessClientSecret)
		if !idOK || !secretOK {
			return CodeAccessDenied, false
		}
	}
	token, ok := bearerToken(r.Header.Get("Authorization"))
	// cfg.APIKey is never empty here: Listen refuses an empty key. Guarding
	// again keeps a Handler built around an empty key closed, not open.
	if !ok || cfg.APIKey == "" || !secretEqual(token, cfg.APIKey) {
		return CodeUnauthorized, false
	}
	return "", true
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

// secretEqual compares in constant time regardless of length by comparing
// SHA-256 digests.
func secretEqual(got, want string) bool {
	g, w := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(g[:], w[:]) == 1
}

// JSON-RPC 2.0 error codes.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcServerError    = -32000 // spec error codes ride in error.data.code
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErrorBody   `json:"error,omitempty"`
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request, raw []byte) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		// Batching was removed from MCP in 2025-06-18; not supported.
		writeRPCError(w, http.StatusBadRequest, nil, rpcInvalidRequest, "リクエストが不正です。")
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(trimmed, &req); err != nil {
		writeRPCError(w, http.StatusBadRequest, nil, rpcParseError, "リクエストを解釈できません。")
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		writeRPCError(w, http.StatusBadRequest, req.ID, rpcInvalidRequest, "リクエストが不正です。")
		return
	}
	if len(req.ID) == 0 {
		// A notification (e.g. notifications/initialized): no response body.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		writeResult(w, req.ID, initializeResult(req.Params))
	case "ping":
		writeResult(w, req.ID, map[string]any{})
	case "tools/list":
		writeResult(w, req.ID, map[string]any{"tools": toolList()})
	case "tools/call":
		s.handleToolCall(w, r, req)
	default:
		writeRPCError(w, http.StatusOK, req.ID, rpcMethodNotFound, "未対応のメソッドです。")
	}
}

// supportedVersions are the MCP protocol revisions this server speaks, the
// newest first. The tool JSON is identical in all of them.
var supportedVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

func initializeResult(params json.RawMessage) map[string]any {
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
		"serverInfo":      map[string]any{"name": "paper-results-mcp", "version": "1.0.0"},
		"instructions":    "pitha-trador の Paper 実績・Jev 判断・収集状況を読む読み取り専用ツールだけを提供します。",
	}
}

func toolList() []map[string]any {
	list := make([]map[string]any, len(tools))
	for i, t := range tools {
		list[i] = map[string]any{
			"name":        t.name,
			"description": t.description,
			"inputSchema": t.schema,
			"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false},
		}
	}
	return list
}

func writeJSON(w http.ResponseWriter, status int, v any) {
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

func writeResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeRPCError(w http.ResponseWriter, status int, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	writeJSON(w, status, rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcErrorBody{Code: code, Message: message}})
}

// writeError answers with a spec error: the HTTP status of the errors
// document, a JSON-RPC error whose message is the fixed text, and the spec
// code in both error.data.code and the top-level "code".
func writeError(w http.ResponseWriter, id json.RawMessage, code Code) {
	info := codeTable[code]
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	writeJSON(w, info.status, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"code":    string(code),
		"message": info.message,
		"error": rpcErrorBody{
			Code:    rpcServerError,
			Message: info.message,
			Data:    map[string]string{"code": string(code)},
		},
	})
}
