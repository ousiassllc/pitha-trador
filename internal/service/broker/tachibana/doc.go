// Package tachibana is the 立花証券 e支店・API (v4r10) adapter behind
// broker.Broker (docs/architecture/overview/integrations.md §5.3, issue #724).
//
// This part is the foundation every request rides on:
//
//   - Client is the REQUEST I/F client shared by the REQUEST, MASTER and PRICE
//     virtual URLs: one request in flight at a time (a priority queue in
//     front of it), at most the configured requests per second, p_no/p_sd_date
//     stamping, Shift-JIS bodies, IPv4-only dialing.
//   - Session logs in (public-key scheme: the virtual URLs come back
//     RSA-OAEP encrypted), keeps one login for the day, re-authenticates every
//     morning after the 03:30 close (05:30 onwards) and only otherwise when
//     the broker reports the session gone (p_errno=2), and logs out when the
//     process stops. It watches the API version / 書面 notices of the login
//     response.
//
// The 認証ID, the 秘密鍵 and above all the virtual URLs (which are the bearer
// credential of the whole session) live in memory only and never appear in an
// error, a log line or a status text: URLs are held in a type that redacts
// itself, and transport errors are unwrapped from *url.Error before they are
// returned. There is deliberately no order-placing request: the adapter reads
// market data only (orders arrive with issue #55).
package tachibana
