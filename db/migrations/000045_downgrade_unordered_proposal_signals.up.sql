-- 注文案の trade_signals のうち、Risk を通ったが注文にならなかった行を risk_passed = 0 に直す（issue #860、親 #826）。
--
-- 注文案の処理（#828）は Risk が通った直後に risk_passed = 1 の trade_signals 行を書く。そのあと数量 0・最大損失超過・
-- Execution の拒否などで案が rejected になっても、その行は risk_passed = 1 のまま残っていた。MCP の risk_rejections の
-- risk_passed_count が「実際に注文に進んだ数」より多く出る原因。これからはアプリが案の状態を変えるのと同じトランザクションで
-- 直す（internal/repository/proposalstore）。この移行は、それ以前に残った行を同じ形に直す。
--
-- 直す行は 2 種類。どちらも「その行から出た paper_orders が無い」ことが条件。
--   (1) trade_signal_id: 案の trade_signal_id が指している行（上の状態の案）。
--   (2) retry_history: 案の trade_signal_id から外れた古い行。再試行（waiting_session のあとの再処理）は新しい signal を INSERT し
--       trade_signal_id を付け替えるので、古い行は案とのひも付きを失い、risk_passed = 1 のまま孤立していた。案の履歴に古い signal の
--       id は残っていない（この移行より前のイベントには書かれていない）ので、id では辿れない。そこで、次を**すべて**満たすときだけ
--       その案の行とみなす: 行が mcp-proposal-v1・LONG・risk_passed = 1・注文なし・どの案にも指されていない孤立行で、同じ銘柄
--       （instrument_id）の案の中に、行の時刻の前後 60 秒以内に「processing から waiting_session（outside_trading_session）」の
--       イベントを持つ案が**ちょうど 1 件**ある（signal を書いた直後に Execution が時間外で断ったときのイベント）。0 件または 2 件
--       以上（案を 1 つに決められない）の孤立行は推測になるので**書き換えず**、件数だけ起動時のログに出す。
-- 直し方: risk_passed = 0、reject_reason = 'proposal_rejected_after_risk: <コード>'（(1) は案の status_reason（空なら unknown）、
-- (2) は outside_trading_session）。注文が出た案（filled / submitted / closed）と、処理中（processing。起動時の回復が片づける）の
-- 案の行は触らない。
--
-- 直す前の値は trade_signal_rewrites_000045 に残す（signal_id = 直した trade_signals.id、proposal_id = 結び付けた案、
-- link = 上の 2 種類、old_risk_passed / old_reject_reason = 直す前の値、new_reject_reason = 書いた値、rewritten_at = 直した
-- 時刻 UTC）。down はこの表から元の値へ戻し、表を消す。直した件数と辿れず書き換えなかった孤立行の件数は、適用した起動で
-- ログに出す（internal/repository/sqlitedb の migration_report.go）。
CREATE TABLE trade_signal_rewrites_000045 (
    signal_id INTEGER PRIMARY KEY REFERENCES trade_signals (id),
    proposal_id INTEGER NOT NULL REFERENCES order_proposals (id),
    link TEXT NOT NULL CHECK (link IN ('trade_signal_id', 'retry_history')),
    old_risk_passed BOOLEAN NOT NULL,
    old_reject_reason VARCHAR(255),
    new_reject_reason VARCHAR(255) NOT NULL,
    rewritten_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- (1) 案の trade_signal_id が指している行
INSERT INTO trade_signal_rewrites_000045 (signal_id, proposal_id, link, old_risk_passed, old_reject_reason, new_reject_reason)
SELECT ts.id, op.id, 'trade_signal_id', ts.risk_passed, ts.reject_reason,
       'proposal_rejected_after_risk: ' || CASE WHEN op.status_reason = '' THEN 'unknown' ELSE op.status_reason END
FROM trade_signals ts
JOIN order_proposals op ON op.trade_signal_id = ts.id
WHERE ts.policy_version = 'mcp-proposal-v1'
  AND ts.risk_passed = 1
  AND op.status IN ('rejected', 'failed', 'expired', 'cancelled', 'waiting_session', 'received')
  AND NOT EXISTS (SELECT 1 FROM paper_orders o WHERE o.trade_signal_id = ts.id)
  AND op.id = (SELECT MAX(o2.id) FROM order_proposals o2 WHERE o2.trade_signal_id = ts.id);

-- (2) 付け替えで案から外れた古い行（上の条件をすべて満たし、案がちょうど 1 件に決まるものだけ）
INSERT INTO trade_signal_rewrites_000045 (signal_id, proposal_id, link, old_risk_passed, old_reject_reason, new_reject_reason)
SELECT ts.id,
       (SELECT op.id FROM order_proposals op
        WHERE op.instrument_id = ts.instrument_id
          AND EXISTS (SELECT 1 FROM order_proposal_events e
                      WHERE e.proposal_id = op.id AND e.from_status = 'processing' AND e.to_status = 'waiting_session'
                        AND e.reason_code = 'outside_trading_session'
                        AND abs(julianday(e.at) - julianday(ts.timestamp)) <= 60.0 / 86400.0)),
       'retry_history', ts.risk_passed, ts.reject_reason, 'proposal_rejected_after_risk: outside_trading_session'
FROM trade_signals ts
WHERE ts.policy_version = 'mcp-proposal-v1'
  AND ts.direction = 'LONG'
  AND ts.risk_passed = 1
  AND NOT EXISTS (SELECT 1 FROM paper_orders o WHERE o.trade_signal_id = ts.id)
  AND NOT EXISTS (SELECT 1 FROM order_proposals op WHERE op.trade_signal_id = ts.id)
  AND (SELECT COUNT(*) FROM order_proposals op
       WHERE op.instrument_id = ts.instrument_id
         AND EXISTS (SELECT 1 FROM order_proposal_events e
                     WHERE e.proposal_id = op.id AND e.from_status = 'processing' AND e.to_status = 'waiting_session'
                       AND e.reason_code = 'outside_trading_session'
                       AND abs(julianday(e.at) - julianday(ts.timestamp)) <= 60.0 / 86400.0)) = 1;

UPDATE trade_signals
SET risk_passed = 0,
    reject_reason = (SELECT w.new_reject_reason FROM trade_signal_rewrites_000045 w WHERE w.signal_id = trade_signals.id)
WHERE id IN (SELECT signal_id FROM trade_signal_rewrites_000045);
