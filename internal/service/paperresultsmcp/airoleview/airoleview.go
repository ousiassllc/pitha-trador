// Package airoleview is the read model behind the Paper results MCP's ai_roles
// tool (issue #1080): which provider and model each AI role is set to, which
// pair each role has run on since the last start, and the recent history of
// saved changes and starts.
//
// It reads runtime_settings (the roles' settings) and ai_role_history over the
// MCP's read-only connection. It never reads the secrets table, so no API key,
// URL or any other secret can reach its output: only provider ids and model names
// go in. Nothing in the MCP can change a role's provider or model; the Settings
// screen is the only writer.
package airoleview

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Description is the ai_roles tool's description. There is deliberately no tool that changes a role's provider or
// model: the live Jev role decides what is traded, so those settings are the Settings screen's alone (and none of
// their keys is on the settings_set whitelist).
const Description = "AI の役割（Jev の売買判断 / Sol の改善提案 / Opus の提案の検証 / Luna のニュース分類）ごとの、プロバイダー（custom＝独自 API / openrouter）とモデルの設定。" +
	"configured（再起動後に効く設定。source は stored＝Settings で保存した値 / default＝既定。既定は今までと同じ接続先・キー・モデル）・running（最後の起動で実際に使い始めた組。記録がなければ null）・live（取引に使われる役割）と、" +
	"直近の変更の履歴（Settings での保存 saved と、起動時の記録 started。いつ・何から何へ）を返す。" +
	"API キー・接続先 URL などの秘密は返さない（キーの有無も返さない）。どの役割も MCP からは変えられない（changeable_via_mcp は常に false。変更は Settings 画面だけ。読み取り専用）"

// HistoryLimit is how many recent history rows Read returns.
const HistoryLimit = 20

// Pair is a provider and model.
type Pair struct {
	Provider string `json:"provider"`
	// Model is "" when the role names none (the provider's own default applies).
	Model string `json:"model"`
}

// Role is one role's state.
type Role struct {
	Role string `json:"role"`
	Name string `json:"name"`
	// Live marks the role that decides what is traded.
	Live bool `json:"live"`
	// Configured is the setting in force after the next restart; Source says
	// whether it is the stored setting ("stored") or the default ("default").
	Configured Pair   `json:"configured"`
	Source     string `json:"source"`
	// Running is the pair the role has run on since the last start (nil: no start
	// recorded yet).
	Running *Pair `json:"running"`
	// ChangeableViaMCP is false for every role: a role's provider and model are
	// changed on the Settings screen only.
	ChangeableViaMCP bool `json:"changeable_via_mcp"`
}

// Change is one history row.
type Change struct {
	At     string `json:"at"`
	Kind   string `json:"kind"` // "saved" (Settings screen) or "started" (the app's start)
	Role   string `json:"role"`
	Before *Pair  `json:"before"`
	After  Pair   `json:"after"`
}

// View is the tool's result.
type View struct {
	Providers []string `json:"providers"`
	Roles     []Role   `json:"roles"`
	History   []Change `json:"history"`
}

// Read returns the current view.
func Read(ctx context.Context, q query.Q) (View, error) {
	loaded := airole.Load(ctx, mcpset.ReadStore{Q: q})
	v := View{Providers: airole.Providers(), Roles: []Role{}, History: []Change{}}
	for _, info := range airole.Infos() {
		set := loaded.Settings[info.ID]
		r := Role{Role: info.ID, Name: info.Name, Live: info.Live, Configured: Pair{set.Provider, set.Model}, Source: "default"}
		if loaded.Stored[info.ID] {
			r.Source = "stored"
		}
		running, err := lastStarted(ctx, q, info.ID)
		if err != nil {
			return View{}, err
		}
		r.Running = running
		v.Roles = append(v.Roles, r)
	}
	rows, err := q.QueryContext(ctx, `
SELECT at, kind, role, before_provider, before_model, provider, model FROM ai_role_history ORDER BY id DESC LIMIT ?`, HistoryLimit)
	if err != nil {
		return View{}, fmt.Errorf("airoleview: read history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var c Change
		var at string
		var bp, bm *string
		if err := rows.Scan(&at, &c.Kind, &c.Role, &bp, &bm, &c.After.Provider, &c.After.Model); err != nil {
			return View{}, fmt.Errorf("airoleview: scan history: %w", err)
		}
		c.At = query.FormatOut(at)
		if bp != nil && bm != nil {
			c.Before = &Pair{*bp, *bm}
		}
		v.History = append(v.History, c)
	}
	if err := rows.Err(); err != nil {
		return View{}, fmt.Errorf("airoleview: read history: %w", err)
	}
	return v, nil
}

func lastStarted(ctx context.Context, q query.Q, role string) (*Pair, error) {
	var p Pair
	err := q.QueryRowContext(ctx, `SELECT provider, model FROM ai_role_history WHERE role = ? AND kind = 'started' ORDER BY id DESC LIMIT 1`, role).Scan(&p.Provider, &p.Model)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("airoleview: read running pair: %w", err)
	}
	return &p, nil
}
