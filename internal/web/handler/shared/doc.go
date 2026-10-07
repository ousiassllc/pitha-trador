// Package shared holds the helpers every handler subpackage reuses:
// Toast/ErrorPage responders, Templ rendering, and the WebSocket accept and
// PollWebSocket / WriteJSON primitives.
// It is a leaf of the handler tree: it MUST NOT import any sibling handler
// subpackage or internal/service, and every other handler package may import it.
package shared
