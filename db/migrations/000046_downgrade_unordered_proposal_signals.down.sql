-- 000046 で直した行（reject_reason が 'proposal_rejected_after_risk:' で始まる）を、直す前の形（risk_passed = 1・reject_reason なし）に戻す。
-- この接頭辞はアプリがこの移行以降に直した行にも付くので、それも戻る（移行前のアプリは risk_passed = 1 のまま残していた）。
UPDATE trade_signals
SET risk_passed = 1,
    reject_reason = NULL
WHERE risk_passed = 0
  AND reject_reason GLOB 'proposal_rejected_after_risk:*';
