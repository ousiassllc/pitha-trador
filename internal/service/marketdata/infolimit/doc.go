// Package infolimit is the process-wide rate limiter for kabuステーション
// information and register REST calls (GetBoard, GetSymbol,
// RegisterSymbols). The official cap is 10 requests/second (FAQ); the
// default is 8 to leave a safety margin. Token issuance is not limited.
// Issue #514.
package infolimit
