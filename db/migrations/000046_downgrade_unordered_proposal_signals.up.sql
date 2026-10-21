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
UPDATE trade_signals
SET risk_passed = 0,
    reject_reason = 'proposal_rejected_after_risk: ' ||
        CASE WHEN (SELECT status_reason FROM order_proposals op WHERE op.trade_signal_id = trade_signals.id ORDER BY op.id DESC LIMIT 1) = ''
             THEN 'unknown'
             ELSE (SELECT status_reason FROM order_proposals op WHERE op.trade_signal_id = trade_signals.id ORDER BY op.id DESC LIMIT 1)
        END
WHERE policy_version = 'mcp-proposal-v1'
  AND risk_passed = 1
  AND EXISTS (SELECT 1 FROM order_proposals op
              WHERE op.trade_signal_id = trade_signals.id
                AND op.status IN ('rejected', 'failed', 'expired', 'cancelled', 'waiting_session', 'received'))
  AND NOT EXISTS (SELECT 1 FROM paper_orders o WHERE o.trade_signal_id = trade_signals.id);
