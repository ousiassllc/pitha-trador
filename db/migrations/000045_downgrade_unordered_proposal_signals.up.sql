-- 注文案の trade_signals のうち、Risk を通ったが注文にならなかった行を risk_passed = 0 に直す（issue #860、親 #826）。
--
-- 注文案の処理（#828）は Risk が通った直後に risk_passed = 1 の trade_signals 行を書く。そのあと数量 0・最大損失超過・
-- Execution の拒否などで案が rejected になっても、その行は risk_passed = 1 のまま残っていた。MCP の risk_rejections の
-- risk_passed_count が「実際に注文に進んだ数」より多く出る原因。これからはアプリが案の状態を変えるのと同じトランザクションで
-- 直す（internal/repository/proposalstore）。この移行は、それ以前に残った行を同じ形に直す。
--
-- 直す行: 注文案の行（policy_version = 'mcp-proposal-v1'）で、risk_passed = 1、紐づく案が「注文にならなかった」状態
-- （rejected / failed / expired / cancelled / waiting_session / received）で、その行から出た paper_orders が無いもの。
-- 直し方: risk_passed = 0、reject_reason = 'proposal_rejected_after_risk: <案の status_reason>'（空なら unknown）。
-- 注文が出た案（filled / submitted / closed）と、処理中（processing。起動時の回復が片づける）の案の行は触らない。
--
-- 直す前の値は trade_signal_rewrites_000045 に残す（signal_id = 直した trade_signals.id、old_risk_passed / old_reject_reason =
-- 直す前の値、rewritten_at = 直した時刻 UTC）。down はこの表から元の値へ戻し、表を消す。直した件数は、適用直後の起動で
-- この表の行数をログに出す（internal/repository/sqlitedb の migration 000045 の報告）。
CREATE TABLE trade_signal_rewrites_000045 (
    signal_id INTEGER PRIMARY KEY REFERENCES trade_signals (id),
    old_risk_passed BOOLEAN NOT NULL,
    old_reject_reason VARCHAR(255),
    rewritten_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO trade_signal_rewrites_000045 (signal_id, old_risk_passed, old_reject_reason)
SELECT ts.id, ts.risk_passed, ts.reject_reason
FROM trade_signals ts
WHERE ts.policy_version = 'mcp-proposal-v1'
  AND ts.risk_passed = 1
  AND EXISTS (SELECT 1 FROM order_proposals op
              WHERE op.trade_signal_id = ts.id
                AND op.status IN ('rejected', 'failed', 'expired', 'cancelled', 'waiting_session', 'received'))
  AND NOT EXISTS (SELECT 1 FROM paper_orders o WHERE o.trade_signal_id = ts.id);

UPDATE trade_signals
SET risk_passed = 0,
    reject_reason = 'proposal_rejected_after_risk: ' ||
        CASE WHEN (SELECT status_reason FROM order_proposals op WHERE op.trade_signal_id = trade_signals.id ORDER BY op.id DESC LIMIT 1) = ''
             THEN 'unknown'
             ELSE (SELECT status_reason FROM order_proposals op WHERE op.trade_signal_id = trade_signals.id ORDER BY op.id DESC LIMIT 1)
        END
WHERE id IN (SELECT signal_id FROM trade_signal_rewrites_000045);
