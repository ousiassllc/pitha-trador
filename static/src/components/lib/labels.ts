// Names of the internal codes the Lit components would otherwise print (issue
// #904). The code itself stays what it is in the API, the data-* attributes and
// the database; only the text an operator reads is named here. The server-side
// screens use the same names from internal/web/labels, and
// TestTypeScriptLabelsMatchGo keeps the two tables in step: edit both together.
//
// A code a table does not know is never printed: the reader gets
// UNKNOWN_REASON (or OTHER for a name of another kind) and the code goes to the
// console once, so a new code is noticed without leaking into the UI.
import { logger } from './logger';

export const UNKNOWN_REASON = 'その他（理由コードあり）';
export const OTHER = 'その他';

const warned = new Set<string>();

/** A warning shared with the API / MCP / log as the screen reads it: the wording is kept, only the setting key in it is named as the Settings screen names that item. Same as labels.ScreenWarning in Go. */
export const screenWarning = (warning: string): string =>
  warning.split('metalabel.llm_cutoff_model').join('モデル名');

export function labelOf(
  kind: string,
  table: Record<string, string>,
  code: string,
  fallback: string = UNKNOWN_REASON,
): string {
  const name = table[code];
  if (name !== undefined) return name;
  const key = `${kind}\u0000${code}`;
  if (!warned.has(key)) {
    warned.add(key);
    logger.warn('labels: a code without a screen label was replaced', { kind, code });
  }
  return fallback;
}

/** positions.exit_reason */
export const EXIT_REASONS: Record<string, string> = {
  stop_loss: '損切り',
  take_profit: '利確',
  trailing_stop: 'トレーリングストップ',
  jev_direction_reversed: 'Jev の方向が反転',
  continuation_probability_dropped: '継続確率が低下',
  vwap_cross: 'VWAP をまたいだ',
  max_holding: '最大保有時間に到達',
  force_flat_before_close: '大引け前の強制決済',
  manual: '手動で決済',
  force_close: 'Kill Switch による強制決済',
  proposal_close: 'AIの注文提案による決済',
};

/** kill_switch_events.reason */
export const KILL_REASONS: Record<string, string> = {
  daily_loss_limit: '日次の損失上限に達した',
  consecutive_losses: '連続で損失が出た',
  market_data_down: '市況データが止まった',
  jev_api_down: 'Jev の API が止まった',
  broker_api_error: '証券会社の API でエラーが出た',
  unexpected_position: '想定外のポジションがあった',
  fill_discrepancy: '約定の内容が食い違った',
  db_write_failure: 'データベースへの書き込みに失敗した',
  operator_heartbeat_timeout: '画面からの応答が途絶えた',
  operator_manual: '手動で発動した',
};

/** kill_switch_resolutions.resolved_by */
export const RESOLVED_BYS: Record<string, string> = {
  auto: '自動',
  manual: '手動',
};

/** The nightly retraining skip codes. */
export const RETRAIN_SKIPS: Record<string, string> = {
  too_few_rows_before_oos: '検証期間より前のラベル付き判断が不足',
  too_few_rows_after_purge: '重なり除去後の学習用データが不足',
  training_data_unusable: '件数はあるが学習できない（ラベルが片側のみ等）',
};

/** The activity feed's event types. */
export const ACTIVITY_TYPES: Record<string, string> = {
  job: 'ジョブ',
  jev_scout: 'Jev Scout',
  jev_trader: 'Jev Trader',
  kill_switch: 'Kill Switch',
  news_feed: 'ニュース取得',
  broker_notice: '証券会社からのお知らせ',
};

/** The job queues. */
export const JOB_QUEUES: Record<string, string> = {
  'market-data': '市況データの取得',
  'feature-calc': '特徴量の計算',
  'jev-scout': 'Jev Scout',
  'jev-trader': 'Jev Trader',
  'outcome-labeling': '結果のラベル付け',
  analytics: '分析',
};

/** The job statuses. */
export const JOB_STATUSES: Record<string, string> = {
  pending: '待機中',
  running: '実行中',
  succeeded: '成功',
  failed: '失敗',
};

/** metalabel_models.algorithm */
export const MODEL_ALGORITHMS: Record<string, string> = {
  logistic_regression: 'ロジスティック回帰',
};

/** The machine reject reasons of the nightly retraining (internal/service/metalabel/training Reject*). */
export const MODEL_REJECT_REASONS: Record<string, string> = {
  llm_cutoff_placeholder: 'LLM 学習データの終わりが仮置きのまま',
  llm_model_changed: 'Jev のモデルが切り替わった（cutoff 未再確認）',
  llm_cutoff_unverified: 'LLM 学習データの終わりの根拠が未記録（Live 前チェックで採用を止める）',
  oos_samples_below_minimum: 'OOS 件数が基準未満',
  oos_days_below_minimum: 'OOS の営業日数が基準未満',
  oos_takes_below_minimum: 'OOS の take 件数が基準未満',
  oos_ev_ci_not_positive: 'EV の信頼区間の下限が 0 以下',
  oos_auc_ci_not_above_minimum: 'AUC の信頼区間の下限が 0.5 以下',
  candidate_not_better_than_active: '現行モデルより有意に良くない',
  active_model_not_comparable: '現行モデルと比較できない',
  model_swap_rate_limited: 'モデル入れ替えの頻度制限中',
  policy_change_freeze: 'しきい値変更直後の凍結期間中',
};

export const exitReasonLabel = (code: string): string => labelOf('exit_reason', EXIT_REASONS, code);
export const killReasonLabel = (code: string): string => labelOf('kill_reason', KILL_REASONS, code);
export const retrainSkipLabel = (code: string): string =>
  labelOf('retrain_skip', RETRAIN_SKIPS, code);
export const activityTypeLabel = (code: string): string =>
  labelOf('activity_type', ACTIVITY_TYPES, code, OTHER);
export const jobQueueLabel = (code: string): string =>
  labelOf('job_queue', JOB_QUEUES, code, OTHER);
export const modelAlgorithmLabel = (code: string): string =>
  labelOf('model_algorithm', MODEL_ALGORITHMS, code, OTHER);
export const modelRejectReasonLabel = (code: string): string =>
  labelOf('model_reject_reason', MODEL_REJECT_REASONS, code);

/** One Kill Switch event in words; resolvedBy '' is "not lifted yet". */
export function killDetail(reason: string, resolvedBy: string): string {
  const head = `理由: ${killReasonLabel(reason)}`;
  if (resolvedBy === '') return `${head}（未解除）`;
  return `${head}（解除: ${labelOf('resolved_by', RESOLVED_BYS, resolvedBy, OTHER)}）`;
}

const JOB_DETAIL = /^queue=(\S+) status=(\S+) attempts=(\d+)(?: (note|error)=([\s\S]*))?$/;
const JEV_TRADER_DETAIL = /^(?:direction=(\S+))?(?: ?confidence=(\S+))?$/;
const JEV_SCOUT_DETAIL = /^question_version=(\S+)$/;
const KILL_DETAIL = /^reason=(\S+)(?: resolved_by=(\S+))?$/;

export const DETAIL_UNAVAILABLE = '（詳細を表示できません）';

/**
 * The 内容 of an activity event: the feed's "key=value" text with the keys and
 * codes named. A job's free note/error text and the news / broker notices (already
 * sentences) are shown as they are.
 */
export function activityDetail(type: string, detail: string): string {
  switch (type) {
    case 'job': {
      const m = JOB_DETAIL.exec(detail);
      if (m) {
        const status = labelOf('job_status', JOB_STATUSES, m[2] ?? '', OTHER);
        let s = `${jobQueueLabel(m[1] ?? '')}・${status}・試行 ${m[3]} 回`;
        if (m[4] === 'note') s += `・メモ: ${m[5]}`;
        if (m[4] === 'error') s += `・エラー: ${m[5]}`;
        return s;
      }
      break;
    }
    case 'jev_scout': {
      const m = JEV_SCOUT_DETAIL.exec(detail);
      if (m) return `質問の版: ${m[1]}`;
      break;
    }
    case 'jev_trader': {
      const m = JEV_TRADER_DETAIL.exec(detail);
      if (m) {
        const parts: string[] = [];
        if (m[1]) parts.push(`方向: ${m[1]}`);
        if (m[2]) parts.push(`確信度: ${m[2]}`);
        return parts.join('・');
      }
      break;
    }
    case 'kill_switch': {
      const m = KILL_DETAIL.exec(detail);
      if (m) return killDetail(m[1] ?? '', m[2] ?? '');
      break;
    }
    default:
      return detail;
  }
  logger.warn('labels: an activity detail has a shape without a screen format', { type });
  return DETAIL_UNAVAILABLE;
}
