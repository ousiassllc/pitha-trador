// Package researchjobpanel builds the 夜間タスクの状態 panel of the Settings
// "研究用の日次評価の記録・学習・書き出し" group: per research task whether it is
// off, when it last succeeded and whether the last run failed (with a short reason).
// It only renders what researchjobstatus has read; it is operation-check
// information and feeds nothing (no trading decision, order, funds or promotion).
package researchjobpanel

import (
	"github.com/ousiassllc/pitha-trador/internal/service/researchjobstatus"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// Title is the panel's title.
const Title = "夜間タスクの状態"

// Items renders tasks as the panel's lines: a heading that says the panel is for
// checking only, then one line per task (plus a warning line while it failed).
func Items(tasks []researchjobstatus.Task) []molecules.SettingDerivedItem {
	items := []molecules.SettingDerivedItem{
		{Key: "heading", Heading: true, Label: "動作の確認", Detail: "確認用の表示です。取引の判断や昇格には使われません。"},
	}
	for _, t := range tasks {
		items = append(items, taskItems(t)...)
	}
	return items
}

func taskItems(t researchjobstatus.Task) []molecules.SettingDerivedItem {
	row := molecules.SettingDerivedItem{Key: t.Name, Label: t.Label}
	switch t.State {
	case researchjobstatus.Off:
		row.Value, row.Off = "オフ（動いていません）", true
		row.Detail = "失敗ではありません。" + lastSuccess("最後に成功した日: ", t.LastSuccessDate)
	case researchjobstatus.Succeeded:
		row.Value = "最終成功日 " + t.LastSuccessDate
	case researchjobstatus.Failed:
		row.Value, row.Off = "直近の実行が失敗しました（"+t.FailureDate+"）", true
		row.Detail = "成功した日はまだありません。"
		if t.LastSuccessDate != "" {
			row.Detail = "最終成功日: " + t.LastSuccessDate
		}
		return []molecules.SettingDerivedItem{row, {Key: t.Name + "-failure", Warn: true, Label: t.Label + "が失敗しています。" + reason(t.FailureReason)}}
	default:
		row.Value, row.Off = "オンですが、まだ成功していません", true
		row.Detail = "16:00 JST 以降のメンテナンスチェックで動きます。"
	}
	return []molecules.SettingDerivedItem{row}
}

// lastSuccess is prefix + day, or nothing while no run has succeeded yet.
func lastSuccess(prefix, day string) string {
	if day == "" {
		return ""
	}
	return prefix + day
}

func reason(r string) string {
	if r == "" {
		return "理由は記録されていません。"
	}
	return "理由: " + r
}
