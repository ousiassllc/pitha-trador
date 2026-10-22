-- 000045 の up が直した行を、退避した元の値（risk_passed・reject_reason）へ戻し、退避表を消す。
-- 戻すのは退避表にある行だけ（接頭辞からの推測はしない）。up のあとにアプリが実行時に直した行
-- （reject_reason が 'proposal_rejected_after_risk:' の行）は退避していないので、そのまま残る。
UPDATE trade_signals
SET risk_passed = (SELECT w.old_risk_passed FROM trade_signal_rewrites_000045 w WHERE w.signal_id = trade_signals.id),
    reject_reason = (SELECT w.old_reject_reason FROM trade_signal_rewrites_000045 w WHERE w.signal_id = trade_signals.id)
WHERE id IN (SELECT signal_id FROM trade_signal_rewrites_000045);

DROP TABLE trade_signal_rewrites_000045;
