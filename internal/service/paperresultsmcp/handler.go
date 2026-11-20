package paperresultsmcp

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/jsonrpc"
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
		jsonrpc.WriteError(w, http.StatusBadRequest, nil, jsonrpc.InvalidRequest, "リクエストが不正です。")
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

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request, raw []byte) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		// Batching was removed from MCP in 2025-06-18; not supported.
		jsonrpc.WriteError(w, http.StatusBadRequest, nil, jsonrpc.InvalidRequest, "リクエストが不正です。")
		return
	}
	var req jsonrpc.Request
	if err := json.Unmarshal(trimmed, &req); err != nil {
		jsonrpc.WriteError(w, http.StatusBadRequest, nil, jsonrpc.ParseError, "リクエストを解釈できません。")
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		jsonrpc.WriteError(w, http.StatusBadRequest, req.ID, jsonrpc.InvalidRequest, "リクエストが不正です。")
		return
	}
	if len(req.ID) == 0 {
		// A notification (e.g. notifications/initialized): no response body.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		jsonrpc.WriteResult(w, req.ID, jsonrpc.InitializeResult(req.Params, "pitha-trador の Paper 実績・Jev 判断・収集状況を読む読み取りツールと、ホワイトリストにある基本設定を読む・変更する settings_* ツール、Paper の注文案を出して状態を読む order_proposal* ツールを提供します。注文案は受け付けて記録するだけで、発注するのはアプリです（Risk check のあと）。Live の発注・送金・Kill Switch の操作・Live の値・秘密情報は扱えません。"))
	case "ping":
		jsonrpc.WriteResult(w, req.ID, map[string]any{})
	case "tools/list":
		jsonrpc.WriteResult(w, req.ID, map[string]any{"tools": toolList()})
	case "tools/call":
		s.handleToolCall(w, r, req)
	default:
		jsonrpc.WriteError(w, http.StatusOK, req.ID, jsonrpc.MethodNotFound, "未対応のメソッドです。")
	}
}

func toolList() []map[string]any {
	list := make([]map[string]any, len(tools))
	for i, t := range tools {
		list[i] = map[string]any{
			"name":        t.name,
			"description": t.description,
			"inputSchema": t.schema,
			"annotations": map[string]any{"readOnlyHint": !t.writes, "destructiveHint": false, "openWorldHint": false},
		}
	}
	return list
}

// writeError answers with a spec error: the HTTP status of the errors
// document, a JSON-RPC error whose message is the fixed text, and the spec
// code in both error.data.code and the top-level "code".
func writeError(w http.ResponseWriter, id json.RawMessage, code Code) {
	writeFailure(w, id, &toolError{code: code})
}

// writeFailure is writeError for a tool failure: it adds error.data.detail
// when the failure carries one.
func writeFailure(w http.ResponseWriter, id json.RawMessage, te *toolError) {
	code := te.code
	info := codeTable[code]
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	data := map[string]string{"code": string(code)}
	if te.detail != "" {
		data["detail"] = te.detail
	}
	jsonrpc.WriteJSON(w, info.status, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"code":    string(code),
		"message": info.message,
		"error": jsonrpc.ErrorBody{
			Code:    jsonrpc.ServerError,
			Message: info.message,
			Data:    data,
		},
	})
}
