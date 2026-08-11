package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// EnqueueDCPReviewLabAdmission inserts one FIFO record only when the existing
// ReviewRun and current PR row still own the exact approved head. Replays return
// the same row without allocating another sequence.
func (s *Store) EnqueueDCPReviewLabAdmission(ctx context.Context, a domain.DCPReviewLabAdmission) (domain.DCPReviewLabAdmission, bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.InsertDCPReviewLabAdmission(ctx, gen.InsertDCPReviewLabAdmissionParams{
		ID:            a.ID,
		PRNumber:      a.PRNumber,
		ReviewBaseSha: a.ReviewBaseSHA,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
		ReviewRunID:   a.ReviewRunID,
		ReviewID:      a.ReviewID,
		SessionID:     a.SessionID,
		PRURL:         a.PRURL,
		TargetSha:     a.TargetSHA,
	})
	if err != nil {
		return domain.DCPReviewLabAdmission{}, false, err
	}
	row, err := s.qw.GetDCPReviewLabAdmissionByRun(ctx, a.ReviewRunID)
	if err != nil {
		return domain.DCPReviewLabAdmission{}, false, err
	}
	return dcpAdmissionFromRow(row), n == 1, nil
}

func (s *Store) GetDCPReviewLabAdmissionByRun(ctx context.Context, runID string) (domain.DCPReviewLabAdmission, bool, error) {
	row, err := s.qr.GetDCPReviewLabAdmissionByRun(ctx, runID)
	return dcpAdmissionResult(row, err)
}

func (s *Store) GetDCPReviewLabAdmissionByID(ctx context.Context, id string) (domain.DCPReviewLabAdmission, bool, error) {
	row, err := s.qr.GetDCPReviewLabAdmissionByID(ctx, id)
	return dcpAdmissionResult(row, err)
}

func (s *Store) GetClaimedDCPReviewLabAdmission(ctx context.Context) (domain.DCPReviewLabAdmission, bool, error) {
	row, err := s.qr.GetClaimedDCPReviewLabAdmission(ctx)
	return dcpAdmissionResult(row, err)
}

func (s *Store) GetNextWaitingDCPReviewLabAdmission(ctx context.Context) (domain.DCPReviewLabAdmission, bool, error) {
	row, err := s.qr.GetNextWaitingDCPReviewLabAdmission(ctx)
	return dcpAdmissionResult(row, err)
}

func (s *Store) GetRefreshingDCPReviewLabAdmissionBySession(ctx context.Context, sessionID domain.SessionID) (domain.DCPReviewLabAdmission, bool, error) {
	row, err := s.qr.GetRefreshingDCPReviewLabAdmissionBySession(ctx, string(sessionID))
	return dcpAdmissionResult(row, err)
}

func (s *Store) ResumeDCPReviewLabAdmissionAfterRefresh(ctx context.Context, a domain.DCPReviewLabAdmission, run domain.ReviewRun, reviewBaseSHA string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.ResumeDCPReviewLabAdmissionAfterRefresh(ctx, gen.ResumeDCPReviewLabAdmissionAfterRefreshParams{
		NewReviewRunID: run.ID, NewReviewID: run.ReviewID, NewTargetSha: run.TargetSHA,
		NewReviewBaseSha: reviewBaseSHA, UpdatedAt: now, ID: a.ID,
		OldReviewRunID: a.ReviewRunID, SessionID: string(a.SessionID), PRURL: a.PRURL,
		OldTargetSha: a.TargetSHA, ExpectedLeaseID: a.LeaseID,
	})
	return n == 1, err
}

func (s *Store) ListDCPReviewLabAdmissions(ctx context.Context) ([]domain.DCPReviewLabAdmission, error) {
	rows, err := s.qr.ListDCPReviewLabAdmissions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPReviewLabAdmission, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpAdmissionFromRow(row))
	}
	return out, nil
}

// ClaimDCPReviewLabAdmission atomically acquires the globally unique FIFO
// admission lease and consumes the exact ReviewRun's one-shot merge claim.
func (s *Store) ClaimDCPReviewLabAdmission(ctx context.Context, a domain.DCPReviewLabAdmission, leaseID, baseSHA string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	claimed := false
	err := s.inTx(ctx, "claim DCP review-lab admission", func(q *gen.Queries) error {
		n, err := q.ClaimDCPReviewLabAdmission(ctx, gen.ClaimDCPReviewLabAdmissionParams{
			LeaseID: leaseID, AdmittedBaseSha: baseSHA, UpdatedAt: now,
			ID: a.ID, ReviewRunID: a.ReviewRunID, SessionID: string(a.SessionID), PRURL: a.PRURL, TargetSha: a.TargetSHA,
		})
		if err != nil || n == 0 {
			return err
		}
		n, err = q.ClaimDCPReviewLabTerminalMerge(ctx, gen.ClaimDCPReviewLabTerminalMergeParams{
			RunID: a.ReviewRunID, SessionID: a.SessionID, PRURL: a.PRURL, TargetSha: a.TargetSHA,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("exact ReviewRun terminal claim was unavailable")
		}
		claimed = true
		return nil
	})
	return claimed, err
}

// CompleteDCPReviewLabAdmission stores the provider merge SHA on the existing
// ReviewRun and releases the exact admission lease in one transaction.
func (s *Store) CompleteDCPReviewLabAdmission(ctx context.Context, a domain.DCPReviewLabAdmission, mergeSHA string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	completed := false
	err := s.inTx(ctx, "complete DCP review-lab admission", func(q *gen.Queries) error {
		n, err := q.CompleteDCPReviewLabTerminalMerge(ctx, gen.CompleteDCPReviewLabTerminalMergeParams{MergeCommitSha: mergeSHA, RunID: a.ReviewRunID})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("exact ReviewRun terminal completion was unavailable")
		}
		n, err = q.CompleteDCPReviewLabAdmission(ctx, gen.CompleteDCPReviewLabAdmissionParams{
			MergeCommitSha: mergeSHA, UpdatedAt: now, ID: a.ID, ReviewRunID: a.ReviewRunID, LeaseID: a.LeaseID,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("exact admission completion was unavailable")
		}
		completed = true
		return nil
	})
	return completed, err
}

func (s *Store) FailDCPReviewLabAdmission(ctx context.Context, a domain.DCPReviewLabAdmission, errorCode string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	failed := false
	err := s.inTx(ctx, "fail DCP review-lab admission", func(q *gen.Queries) error {
		n, err := q.FailDCPReviewLabTerminalMerge(ctx, gen.FailDCPReviewLabTerminalMergeParams{ErrorCode: errorCode, RunID: a.ReviewRunID})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("exact ReviewRun terminal failure was unavailable")
		}
		n, err = q.FailDCPReviewLabAdmission(ctx, gen.FailDCPReviewLabAdmissionParams{
			ErrorCode: errorCode, UpdatedAt: now, ID: a.ID, ReviewRunID: a.ReviewRunID, LeaseID: a.LeaseID,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("exact admission failure was unavailable")
		}
		failed = true
		return nil
	})
	return failed, err
}

func (s *Store) StartDCPReviewLabRefresh(ctx context.Context, a domain.DCPReviewLabAdmission, leaseID, baseSHA string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.StartDCPReviewLabRefresh(ctx, gen.StartDCPReviewLabRefreshParams{
		LeaseID: leaseID, AdmittedBaseSha: baseSHA, UpdatedAt: now,
		ID: a.ID, ReviewRunID: a.ReviewRunID, SessionID: string(a.SessionID), TargetSha: a.TargetSHA,
	})
	return n == 1, err
}

// RecordDCPReviewLabIncident freezes the queue on one structured packet. If
// the candidate already held the merge lease, the ReviewRun claim is failed in
// the same transaction so restart cannot infer a second provider mutation.
func (s *Store) RecordDCPReviewLabIncident(ctx context.Context, a domain.DCPReviewLabAdmission, leaseID, baseSHA, errorCode, packet string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	recorded := false
	err := s.inTx(ctx, "record DCP review-lab incident", func(q *gen.Queries) error {
		if a.Status == domain.DCPAdmissionClaimed {
			n, err := q.FailDCPReviewLabTerminalMerge(ctx, gen.FailDCPReviewLabTerminalMergeParams{ErrorCode: errorCode, RunID: a.ReviewRunID})
			if err != nil {
				return err
			}
			if n != 1 {
				return errors.New("claimed ReviewRun could not be fenced")
			}
		}
		n, err := q.RecordDCPReviewLabIncident(ctx, gen.RecordDCPReviewLabIncidentParams{
			LeaseID: leaseID, AdmittedBaseSha: baseSHA, ErrorCode: errorCode,
			IncidentPacket: packet, UpdatedAt: now, ID: a.ID, ReviewRunID: a.ReviewRunID,
			SessionID: string(a.SessionID), TargetSha: a.TargetSHA, ExpectedLeaseID: a.LeaseID,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("exact admission incident transition was unavailable")
		}
		recorded = true
		return nil
	})
	return recorded, err
}

func dcpAdmissionResult(row gen.DcpReviewLabAdmission, err error) (domain.DCPReviewLabAdmission, bool, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPReviewLabAdmission{}, false, nil
	}
	if err != nil {
		return domain.DCPReviewLabAdmission{}, false, err
	}
	return dcpAdmissionFromRow(row), true, nil
}

func dcpAdmissionFromRow(row gen.DcpReviewLabAdmission) domain.DCPReviewLabAdmission {
	return domain.DCPReviewLabAdmission{
		Sequence: row.Sequence, ID: row.ID, ReviewRunID: row.ReviewRunID, ReviewID: row.ReviewID,
		SessionID: domain.SessionID(row.SessionID), PRURL: row.PRURL, PRNumber: row.PRNumber,
		TargetSHA: row.TargetSha, ReviewBaseSHA: row.ReviewBaseSha, AdmittedBaseSHA: row.AdmittedBaseSha,
		Status: domain.DCPAdmissionStatus(row.Status), LeaseID: row.LeaseID, MergeCommitSHA: row.MergeCommitSha,
		ErrorCode: row.ErrorCode, IncidentPacket: row.IncidentPacket, RefreshWakeCount: row.RefreshWakeCount,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
