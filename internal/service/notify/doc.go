// Package notify implements non-functional.md §5.2's immediate Slack
// alert delivery over a Slack Incoming Webhook (architecture/overview.md
// §2 "アラート | Slack Incoming Webhook | 即時通知（§5.2）", §10.3).
//
// internal/service/risk.Notifier, internal/service/jev.AlertNotifier,
// and internal/service/selfimprove.Notifier are the narrow per-package
// interfaces each producing package declares for itself (mirrors
// internal/service/scheduler.HeartbeatChecker's own precedent, package
// doc.go's "service/ sub-packages depend only on domain, repository"
// layer rule: it keeps risk/jev/selfimprove from importing this package
// directly). SlackNotifier implements every method all three interfaces
// declare structurally, by posting a formatted Japanese message to a
// Slack Incoming Webhook URL.
package notify
