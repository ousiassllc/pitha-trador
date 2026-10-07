-- positions.unrealized_pnl は保有中のみ有効で、クローズ済み行は 0（実現損益
-- realized_pnl と二重計上しない。issue #682）。#682 以前にクローズされた行は
-- クローズ直前の Mark 値が残っているため、既存のクローズ済み行を 0 へ揃える
-- （issue #688）。保有中の行（closed_at IS NULL）は触らない。
UPDATE positions SET unrealized_pnl = 0 WHERE closed_at IS NOT NULL AND unrealized_pnl <> 0;
