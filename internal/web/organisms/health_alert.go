package organisms

// HealthAlertTitle is the fixed headline of a health alert code
// (domain.HealthAlertKabuNotLoggedIn / HealthAlertKabuUnreachable /
// HealthAlertMarketDataIdle), the Home screen's 要対応 title (issue #896).
func HealthAlertTitle(code string) string {
	switch code {
	case "kabu_not_logged_in":
		return "取引日の朝からkabuステーションに未ログインです。"
	case "kabu_unreachable":
		return "取引日の朝からkabuステーションに接続できません（未起動の疑い）。"
	case "market_data_idle":
		return "立会中ですが、市況データの取得が止まっています。"
	}
	return "市況データの警告があります。"
}

// HealthAlertAction tells the operator what to check for an alert code (the
// Home screen's 要対応 detail).
func HealthAlertAction(code string) string {
	switch code {
	case "kabu_not_logged_in":
		return "kabuステーションにログインしてください（ログイン後に自動で再発行します）。"
	case "kabu_unreachable":
		return "kabuステーションを起動してログインしてください（API利用の設定も確認してください。起動後に自動で再接続します）。"
	case "market_data_idle":
		return "アプリが動いているか、ジョブが詰まっていないか（アクティビティ画面）を確認してください。"
	}
	return ""
}
