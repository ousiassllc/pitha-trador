package domain

import "errors"

// Failure kinds of the JPX stock-list import (bootstrap/universe.Importer),
// matched with errors.Is. They live in domain so the web handler can classify
// a failure without importing bootstrap (architecture/overview.md §3). The
// returned error also wraps the low-level cause (URL/DNS, SQLite, ParseJPX
// detail) for logs; callers show the operator a fixed text per kind and never
// err.Error() (issue #700).
var (
	// ErrJPXConnect: JPX could not be reached or its response not read.
	ErrJPXConnect = errors.New("JPXに接続できませんでした（ネットワークを確認してください）")
	// ErrJPXFormat: JPX answered, but not with the expected list (HTTP
	// status, size, or the workbook layout changed).
	ErrJPXFormat = errors.New("JPXの銘柄一覧の形式を解釈できませんでした（JPX側で形式が変わった可能性があります）")
	// ErrJPXSave: the parsed stocks could not be saved to the master.
	ErrJPXSave = errors.New("銘柄マスタへの保存に失敗しました")
)
