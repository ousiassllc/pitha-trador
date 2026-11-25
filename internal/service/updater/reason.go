package updater

// BlockedReason words the safety-gate condition holding a newer release back
// (issue #241). It is the one wording of the Settings panel and of the Paper
// results MCP's update tools (issue #1222); the gate's own log detail (counts,
// durations) stays in the log.
func BlockedReason(kind BlockKind) string {
	switch kind {
	case BlockOpenPositions:
		return "ポジションを保有しているため"
	case BlockKillSwitch:
		return "Kill Switch が発動しているため"
	case BlockRecentOrder:
		return "直近に発注があったため"
	case BlockMarketHours:
		return "取引時間中（9:00〜15:30）のため"
	case BlockCheckFailed:
		return "安全条件を確認できなかったため"
	default:
		return "理由を特定できません"
	}
}

// ErrorReason words why a check failed (issue #241) without exposing the raw
// error text, which may carry URLs or local paths; that stays in the log.
func ErrorReason(kind ErrorKind) string {
	switch kind {
	case ErrorNetwork:
		return "ネットワークまたは GitHub 側の問題で接続できませんでした"
	case ErrorRateLimit:
		return "GitHub API のレート制限に達しました"
	case ErrorAccess:
		return "GitHub のリリース情報にアクセスできません"
	case ErrorVerification:
		return "ダウンロードしたインストーラーの検証に失敗しました（更新は中止されました）"
	case ErrorRelease:
		return "取得したリリース情報の内容が不正です（タグ名またはインストーラー資産を確認してください）"
	default:
		return "原因を特定できませんでした（詳細はログを参照してください）"
	}
}
