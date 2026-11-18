package promotiondiag

import (
	"context"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/ledgerdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Check computes the checklist of req.Candidate over [r.From, r.To). It reads only.
func Check(ctx context.Context, q query.Q, r query.Range, req Request) (*Result, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	criteria, eff, err := effectiveCriteria(ctx, q, req)
	if err != nil {
		return nil, err
	}
	cur := req.current()
	ledger, err := ledgerdiag.Compute(ctx, q, r, req.ledgerRequest(cur))
	if err != nil {
		return nil, err
	}
	want := req.Candidate.ledgerID()
	var entry *ledgerdiag.Entry
	for i := range ledger.Candidates {
		if ledger.Candidates[i].ID == want {
			entry = &ledger.Candidates[i]
		}
	}
	horizon := req.Horizon
	if horizon == "" {
		horizon = ledgerdiag.Horizon10m
	}
	out := &Result{
		From: ledger.From, To: ledger.To, Horizon: horizon, Candidate: *req.Candidate, Current: cur,
		Notice: notice, Criteria: criteria, Definitions: definitions(),
	}
	if entry == nil { // the ledger always lists an Include candidate; defensive
		return nil, ledgerdiag.ErrUnsupportedReference
	}
	out.Items, out.KeyNumbers = evaluate(ledger, entry, eff, req)
	out.KeyNumbers.FamilySizeExtra = req.FamilySizeExtra
	out.EligibleAsEvidence = true
	for _, it := range out.Items {
		if it.Status != StatusPass {
			out.EligibleAsEvidence = false
			if out.FirstUnmet == nil {
				name := it.Name
				out.FirstUnmet = &name
			}
		}
	}
	if req.Candidate.Kind == ledgerdiag.KindResearchModel && !strings.HasSuffix(req.Candidate.ID, ":"+horizon) {
		// A direction learned on one label horizon is not evidence at another: graded on the wrong labels.
		out.EligibleAsEvidence = false
		out.HorizonNote = "研究用モデルの学習ホライズン（id の末尾）と、この比較のホライズン（horizon）が違う。モデルが学習していない時間軸のラベルで採点した数字なので、提案の根拠にしない。horizon を学習ホライズンに合わせて呼び直す"
	}
	return out, nil
}
