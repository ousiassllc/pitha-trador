package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func TestProposalRepository_InsertAndGet(t *testing.T) {
	db := newTestDB(t)
	proposals := repository.NewProposalRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 15, 30, 0, 0, time.UTC)

	changesJSON, err := domain.EncodePolicyChanges([]domain.PolicyChange{
		{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.60`, NewValue: `0.65`},
	})
	if err != nil {
		t.Fatalf("EncodePolicyChanges: %v", err)
	}

	inserted, err := proposals.Insert(ctx, domain.PolicyProposal{
		ProposedAt:          now,
		RationaleJSON:       `{"brier_score":0.22}`,
		ProposedChangesJSON: changesJSON,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if inserted.ID == 0 {
		t.Fatalf("Insert().ID = 0, want nonzero")
	}
	if inserted.ProposedBy != "sol" || inserted.Status != domain.PolicyProposalStatusPending {
		t.Fatalf("Insert() defaults = %+v, want proposed_by=sol status=pending", inserted)
	}

	got, err := proposals.Get(ctx, inserted.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RationaleJSON != `{"brier_score":0.22}` || got.ProposedChangesJSON != changesJSON {
		t.Fatalf("Get() = %+v, want the inserted rationale/changes", got)
	}
	if !got.ProposedAt.Equal(now) {
		t.Fatalf("Get().ProposedAt = %v, want %v", got.ProposedAt, now)
	}
	if got.BacktestResultJSON != nil || got.ReviewJSON != nil || got.AppliedAt != nil || got.RolledBackAt != nil {
		t.Fatalf("Get() = %+v, want every nullable review/apply/rollback field unset", got)
	}
}

func TestProposalRepository_Get_NotFound(t *testing.T) {
	proposals := repository.NewProposalRepository(newTestDB(t))
	if _, err := proposals.Get(context.Background(), 999999); !errors.Is(err, repository.ErrPolicyProposalNotFound) {
		t.Fatalf("Get(unknown) error = %v, want ErrPolicyProposalNotFound", err)
	}
}

func TestProposalRepository_UpdateBacktestResultAndReview(t *testing.T) {
	db := newTestDB(t)
	proposals := repository.NewProposalRepository(db)
	ctx := context.Background()

	inserted, err := proposals.Insert(ctx, domain.PolicyProposal{
		RationaleJSON:       `{}`,
		ProposedChangesJSON: `[]`,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := proposals.UpdateBacktestResult(ctx, inserted.ID, `{"expectancy_ok":true}`); err != nil {
		t.Fatalf("UpdateBacktestResult: %v", err)
	}
	if err := proposals.UpdateReview(ctx, inserted.ID, domain.PolicyProposalStatusApproved, "opus", `{"approved":true}`); err != nil {
		t.Fatalf("UpdateReview: %v", err)
	}

	got, err := proposals.Get(ctx, inserted.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.BacktestResultJSON == nil || *got.BacktestResultJSON != `{"expectancy_ok":true}` {
		t.Fatalf("Get().BacktestResultJSON = %v, want the updated JSON", got.BacktestResultJSON)
	}
	if got.Status != domain.PolicyProposalStatusApproved {
		t.Fatalf("Get().Status = %q, want approved", got.Status)
	}
	if got.ReviewedBy == nil || *got.ReviewedBy != "opus" {
		t.Fatalf("Get().ReviewedBy = %v, want opus", got.ReviewedBy)
	}
	if got.ReviewJSON == nil || *got.ReviewJSON != `{"approved":true}` {
		t.Fatalf("Get().ReviewJSON = %v, want the updated JSON", got.ReviewJSON)
	}
}

func TestProposalRepository_MarkAppliedAndListByStatus(t *testing.T) {
	db := newTestDB(t)
	proposals := repository.NewProposalRepository(db)
	ctx := context.Background()
	appliedAt := time.Date(2026, 9, 27, 16, 0, 0, 0, time.UTC)

	pending, err := proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: `[]`})
	if err != nil {
		t.Fatalf("Insert pending: %v", err)
	}
	toApply, err := proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: `[]`})
	if err != nil {
		t.Fatalf("Insert toApply: %v", err)
	}

	if err := proposals.MarkApplied(ctx, toApply.ID, "sol-2", appliedAt); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}

	pendingList, err := proposals.ListByStatus(ctx, domain.PolicyProposalStatusPending)
	if err != nil {
		t.Fatalf("ListByStatus(pending): %v", err)
	}
	if len(pendingList) != 1 || pendingList[0].ID != pending.ID {
		t.Fatalf("ListByStatus(pending) = %+v, want exactly %d", pendingList, pending.ID)
	}

	appliedList, err := proposals.ListByStatus(ctx, domain.PolicyProposalStatusApplied)
	if err != nil {
		t.Fatalf("ListByStatus(applied): %v", err)
	}
	if len(appliedList) != 1 || appliedList[0].ID != toApply.ID {
		t.Fatalf("ListByStatus(applied) = %+v, want exactly %d", appliedList, toApply.ID)
	}
	got := appliedList[0]
	if got.AppliedPolicyVersion == nil || *got.AppliedPolicyVersion != "sol-2" {
		t.Fatalf("ListByStatus(applied)[0].AppliedPolicyVersion = %v, want sol-2", got.AppliedPolicyVersion)
	}
	if got.AppliedAt == nil || !got.AppliedAt.Equal(appliedAt) {
		t.Fatalf("ListByStatus(applied)[0].AppliedAt = %v, want %v", got.AppliedAt, appliedAt)
	}
}

func TestProposalRepository_MarkRolledBack(t *testing.T) {
	db := newTestDB(t)
	proposals := repository.NewProposalRepository(db)
	ctx := context.Background()
	rolledBackAt := time.Date(2026, 10, 4, 16, 0, 0, 0, time.UTC)

	inserted, err := proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: `[]`})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := proposals.MarkApplied(ctx, inserted.ID, "sol-3", rolledBackAt.Add(-5*24*time.Hour)); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}

	reason := "expectancy degraded 25% over 5 trading days (FR-SELFIMPROVE-6)"
	if err := proposals.MarkRolledBack(ctx, inserted.ID, rolledBackAt, reason); err != nil {
		t.Fatalf("MarkRolledBack: %v", err)
	}

	got, err := proposals.Get(ctx, inserted.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PolicyProposalStatusRolledBack {
		t.Fatalf("Get().Status = %q, want rolled_back", got.Status)
	}
	if got.RolledBackAt == nil || !got.RolledBackAt.Equal(rolledBackAt) {
		t.Fatalf("Get().RolledBackAt = %v, want %v", got.RolledBackAt, rolledBackAt)
	}
	if got.RolledBackReason == nil || *got.RolledBackReason != reason {
		t.Fatalf("Get().RolledBackReason = %v, want %q", got.RolledBackReason, reason)
	}
}

func TestProposalRepository_UpdateBacktestResult_NotFound(t *testing.T) {
	proposals := repository.NewProposalRepository(newTestDB(t))
	err := proposals.UpdateBacktestResult(context.Background(), 999999, `{}`)
	if !errors.Is(err, repository.ErrPolicyProposalNotFound) {
		t.Fatalf("UpdateBacktestResult(unknown) error = %v, want ErrPolicyProposalNotFound", err)
	}
}

func TestProposalRepository_List_FiltersByStatusOrdersNewestFirstAndCapsLimit(t *testing.T) {
	proposals := repository.NewProposalRepository(newTestDB(t))
	ctx := context.Background()
	base := time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)
	insert := func(dayOffset int, status string) domain.PolicyProposal {
		t.Helper()
		p, err := proposals.Insert(ctx, domain.PolicyProposal{
			ProposedAt: base.AddDate(0, 0, dayOffset), RationaleJSON: `{}`, ProposedChangesJSON: `[]`, Status: status,
		})
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}
		return p
	}
	oldest := insert(0, domain.PolicyProposalStatusRejected)
	middle := insert(1, domain.PolicyProposalStatusApplied)
	newest := insert(2, domain.PolicyProposalStatusRejected)

	all, err := proposals.List(ctx, "", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 || all[0].ID != newest.ID || all[1].ID != middle.ID || all[2].ID != oldest.ID {
		t.Fatalf("List(all) ids = %v, want newest first [%d %d %d]", ids(all), newest.ID, middle.ID, oldest.ID)
	}

	rejected, err := proposals.List(ctx, domain.PolicyProposalStatusRejected, 10)
	if err != nil {
		t.Fatalf("List(rejected): %v", err)
	}
	if len(rejected) != 2 || rejected[0].ID != newest.ID || rejected[1].ID != oldest.ID {
		t.Fatalf("List(rejected) ids = %v, want [%d %d]", ids(rejected), newest.ID, oldest.ID)
	}

	capped, err := proposals.List(ctx, "", 1)
	if err != nil || len(capped) != 1 || capped[0].ID != newest.ID {
		t.Fatalf("List(limit 1) = %v, %v, want only the newest", ids(capped), err)
	}

	if _, err := proposals.List(ctx, "", 0); err == nil {
		t.Fatal("List(limit 0) succeeded, want an error")
	}
}

func ids(ps []domain.PolicyProposal) []int64 {
	out := make([]int64, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}
