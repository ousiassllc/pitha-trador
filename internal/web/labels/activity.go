package labels

import (
	"log/slog"
	"regexp"
	"strings"
)

// ActivityTypes names the activity feed's event types (domain.ActivityType*).
var ActivityTypes = map[string]string{
	"job":           "ジョブ",
	"jev_scout":     "Jev Scout",
	"jev_trader":    "Jev Trader",
	"kill_switch":   "Kill Switch",
	"news_feed":     "ニュース取得",
	"broker_notice": "証券会社からのお知らせ",
}

// ActivityType is the name of an activity event type.
func ActivityType(code string) string {
	return lookup("activity_type", ActivityTypes, code, Other)
}

// JobQueues names the job queues (internal/repository/jobqueue JobQueue*).
var JobQueues = map[string]string{
	"market-data":      "市況データの取得",
	"feature-calc":     "特徴量の計算",
	"jev-scout":        "Jev Scout",
	"jev-trader":       "Jev Trader",
	"outcome-labeling": "結果のラベル付け",
	"analytics":        "分析",
}

// JobQueue is the name of a job queue.
func JobQueue(code string) string { return lookup("job_queue", JobQueues, code, Other) }

// JobStatuses names the job statuses (jobqueue.JobStatus*).
var JobStatuses = map[string]string{
	"pending":   "待機中",
	"running":   "実行中",
	"succeeded": "成功",
	"failed":    "失敗",
}

// JobStatus is the name of a job status.
func JobStatus(code string) string { return lookup("job_status", JobStatuses, code, Other) }

// detailUnavailable is shown for an activity detail whose shape this screen
// does not know (the text itself is not printed: it is an internal format).
const detailUnavailable = "（詳細を表示できません）"

var (
	jobDetail  = regexp.MustCompile(`(?s)^queue=(\S+) status=(\S+) attempts=(\d+)(?: (note|error)=(.*))?$`) // (?s): a job's error text may span lines
	jevTrader  = regexp.MustCompile(`^(?:direction=(\S+))?(?: ?confidence=(\S+))?$`)
	scoutQuery = regexp.MustCompile(`^question_version=(\S+)$`)
	killDetail = regexp.MustCompile(`^reason=(\S+)(?: resolved_by=(\S+))?$`)
)

// ActivityDetail is the 内容 of an activity event: the feed's "key=value"
// text (internal/service/activityfeed) with the keys and codes named. The
// free text a job's note or error carries is shown as it is; the news and
// broker notices are sentences already.
func ActivityDetail(eventType, detail string) string {
	switch eventType {
	case "job":
		if m := jobDetail.FindStringSubmatch(detail); m != nil {
			s := JobQueue(m[1]) + "・" + JobStatus(m[2]) + "・試行 " + m[3] + " 回"
			switch m[4] {
			case "note":
				s += "・メモ: " + m[5]
			case "error":
				s += "・エラー: " + m[5]
			}
			return s
		}
	case "jev_scout":
		if m := scoutQuery.FindStringSubmatch(detail); m != nil {
			return "質問の版: " + m[1]
		}
	case "jev_trader":
		if m := jevTrader.FindStringSubmatch(detail); m != nil {
			var parts []string
			if m[1] != "" {
				parts = append(parts, "方向: "+m[1])
			}
			if m[2] != "" {
				parts = append(parts, "確信度: "+m[2])
			}
			return strings.Join(parts, "・")
		}
	case "kill_switch":
		if m := killDetail.FindStringSubmatch(detail); m != nil {
			return KillDetail(m[1], m[2])
		}
	default:
		return detail
	}
	slog.Warn("labels: an activity detail has a shape without a screen format", "type", eventType)
	return detailUnavailable
}

// KillDetail is one Kill Switch event in words: the reason and, once it is
// lifted, who lifted it ("" is "not lifted yet").
func KillDetail(reason, resolvedBy string) string {
	s := "理由: " + KillReason(reason)
	if resolvedBy != "" {
		return s + "（解除: " + ResolvedBy(resolvedBy) + "）"
	}
	return s + "（未解除）"
}
