package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

const dcpCard12ColdStartRecoveryID = "dcp-card12-cold-start-recovery-087176dbe56428dc97a99823a94daa4687c41b15c14a08de21db2c6c602f0f2f"

// EstablishDCPGovernedStartupQuarantine atomically validates and touches the
// durable classification before daemon wiring may construct a runtime. An
// exact live lab with missing/unknown rows fails closed. Historical cards
// 11/12 retain their immutable quarantine rows; future policy sessions are
// also held out of stock restoration and are classified only by their exact
// additive policy rows.
func (s *Store) EstablishDCPGovernedStartupQuarantine(ctx context.Context, now time.Time) (map[domain.SessionID]struct{}, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	quarantine := make(map[domain.SessionID]struct{})
	err := s.inTx(ctx, "establish DCP governed startup quarantine", func(q *gen.Queries) error {
		card11, card11Found, err := getSessionForStartupQuarantine(ctx, q, "dcp-review-lab-11")
		if err != nil {
			return err
		}
		card12, card12Found, err := getSessionForStartupQuarantine(ctx, q, "dcp-review-lab-12")
		if err != nil {
			return err
		}
		if card11Found || card12Found {
			if !card11Found || !card12Found || card11.ProjectID != "dcp-review-lab" || card12.ProjectID != "dcp-review-lab" {
				return errors.New("governed DCP sessions are incomplete or foreign")
			}
			if _, err := q.BootstrapDCPCard12ColdStartRecovery(ctx, now); err != nil {
				return err
			}
			if _, err := q.BootstrapDCPCard11StartupQuarantine(ctx, now); err != nil {
				return err
			}
			if _, err := q.BootstrapDCPCard12StartupQuarantine(ctx, now); err != nil {
				return err
			}
			count, err := q.CountExactDCPGovernedStartupQuarantine(ctx)
			if err != nil || count != 2 {
				return errors.Join(err, errors.New("exact governed startup quarantine is unavailable"))
			}
			touched, err := q.TouchDCPGovernedStartupQuarantine(ctx, now)
			if err != nil || touched != 2 {
				return errors.Join(err, errors.New("governed startup quarantine fence was not atomically established"))
			}
			rows, err := q.ListDCPGovernedStartupQuarantine(ctx)
			if err != nil || len(rows) != 2 || rows[0] != "dcp-review-lab-11" || rows[1] != "dcp-review-lab-12" {
				return errors.Join(err, errors.New("governed startup quarantine identity drifted"))
			}
			quarantine[domain.SessionID(rows[0])] = struct{}{}
			quarantine[domain.SessionID(rows[1])] = struct{}{}
		}
		policyTasks, err := q.ListDCPReviewLabPolicyTasks(ctx)
		if err != nil {
			return err
		}
		for _, task := range policyTasks {
			session, found, getErr := getSessionForStartupQuarantine(ctx, q, task.SessionID)
			if getErr != nil || !found || !isExactDCPPolicyStartupQuarantineSession(task, session) {
				return errors.Join(getErr, errors.New("future DCP policy session classification drifted"))
			}
			quarantine[domain.SessionID(task.SessionID)] = struct{}{}
		}
		return nil
	})
	return quarantine, err
}

func isExactDCPPolicyStartupQuarantineSession(task gen.DcpReviewLabPolicyTask, session gen.Session) bool {
	prefix := ""
	minimumCard := int64(0)
	switch {
	case task.Target == "dcp-review-lab" && task.Profile == "synthetic-pr" &&
		task.Repository == "orenvlad-ai/dcp-review-lab" && task.PolicyVersion == "dcp.review-lab.happy-path/v1":
		prefix = "dcp-review-lab"
		minimumCard = 12
	case task.Target == "wb-price-extension" && task.Profile == "repo-only" &&
		task.Repository == "orenvlad-ai/wb-price-extension" && task.PolicyVersion == "dcp.repo-only.happy-path/v1":
		prefix = "wb-price-extension"
	default:
		return false
	}
	return task.CardNumber > minimumCard && session.ProjectID == domain.ProjectID(prefix) &&
		session.Num == task.CardNumber && task.SessionID == prefix+"-"+fmt.Sprint(task.CardNumber) &&
		session.ID == domain.SessionID(task.SessionID)
}

func getSessionForStartupQuarantine(ctx context.Context, q *gen.Queries, id string) (gen.Session, bool, error) {
	row, err := q.GetSession(ctx, domain.SessionID(id))
	if errors.Is(err, sql.ErrNoRows) {
		return gen.Session{}, false, nil
	}
	return row, err == nil, err
}

func (s *Store) GetDCPCard12ColdStartRecovery(ctx context.Context, id string) (domain.DCPCard12ColdStartRecovery, bool, error) {
	row, err := s.qr.GetDCPCard12ColdStartRecovery(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPCard12ColdStartRecovery{}, false, nil
	}
	if err != nil {
		return domain.DCPCard12ColdStartRecovery{}, false, err
	}
	return dcpCard12ColdStartRecoveryFromRow(row), true, nil
}

func (s *Store) ListDCPCard12ColdStartRecoveries(ctx context.Context) ([]domain.DCPCard12ColdStartRecovery, error) {
	rows, err := s.qr.ListDCPCard12ColdStartRecoveries(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPCard12ColdStartRecovery, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpCard12ColdStartRecoveryFromRow(row))
	}
	return out, nil
}

func (s *Store) HasExactDCPCard12ColdStartToolPathRecovery(ctx context.Context) (bool, error) {
	count, err := s.qr.CountExactDCPCard12ColdStartToolPathRecovery(ctx)
	return count == 1, err
}

func (s *Store) HasExactDCPCard12ColdStartAutoMergeRecovery(ctx context.Context) (bool, error) {
	count, err := s.qr.CountExactDCPCard12ColdStartAutoMergeRecovery(ctx)
	return count == 1, err
}

func (s *Store) PersistDCPCard12ColdStartBackup(ctx context.Context, row domain.DCPCard12ColdStartRecovery, path, digest string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.PersistDCPCard12ColdStartBackup(ctx, gen.PersistDCPCard12ColdStartBackupParams{
		BackupPath: path, BackupDigest: digest, UpdatedAt: now, RecoveryID: row.RecoveryID, Revision: row.Revision,
	})
	return n == 1, err
}

func (s *Store) StartDCPCard12ColdStartRecovery(ctx context.Context, row domain.DCPCard12ColdStartRecovery, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.StartDCPCard12ColdStartRecovery(ctx, gen.StartDCPCard12ColdStartRecoveryParams{
		UpdatedAt: now, RecoveryID: row.RecoveryID, Revision: row.Revision,
	})
	return n == 1, err
}

func (s *Store) CompleteDCPCard12ColdStartRecoveryAction(ctx context.Context, row domain.DCPCard12ColdStartRecovery, head string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.CompleteDCPCard12ColdStartRecoveryAction(ctx, gen.CompleteDCPCard12ColdStartRecoveryActionParams{
		NewHead: head, UpdatedAt: now, RecoveryID: row.RecoveryID, Revision: row.Revision,
	})
	return n == 1, err
}

func (s *Store) FailDCPCard12ColdStartRecovery(ctx context.Context, id, code string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPCard12ColdStartRecovery(ctx, gen.FailDCPCard12ColdStartRecoveryParams{
		ErrorCode: code, UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true}, RecoveryID: id,
	})
	return n == 1, err
}

func (s *Store) FailDCPCard12ColdStartRecoveryReview(ctx context.Context, row domain.DCPCard12ColdStartRecovery, run domain.ReviewRun, code string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.FailDCPCard12ColdStartRecoveryReview(ctx, gen.FailDCPCard12ColdStartRecoveryReviewParams{
		ErrorCode: code, UpdatedAt: now, FinishedAt: sql.NullTime{Time: now, Valid: true},
		RecoveryID: row.RecoveryID, ReviewRunID: run.ID, TargetSha: run.TargetSHA,
	})
	return n == 1, err
}

func (s *Store) RebindDCPAdmissionAfterCard12ColdStartRecovery(ctx context.Context, admission domain.DCPReviewLabAdmission, recovery domain.DCPCard12ColdStartRecovery, run domain.ReviewRun, reviewBaseSHA, checkID string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rebound := false
	err := s.inTx(ctx, "rebind DCP admission after card-12 cold-start recovery", func(q *gen.Queries) error {
		n, err := q.RebindDCPAdmissionAfterCard12ColdStartRecovery(ctx, gen.RebindDCPAdmissionAfterCard12ColdStartRecoveryParams{
			NewReviewRunID: run.ID, NewReviewID: run.ReviewID, NewTargetSha: run.TargetSHA,
			NewReviewBaseSha: reviewBaseSHA, UpdatedAt: now, AdmissionID: admission.ID,
			OldReviewRunID: admission.ReviewRunID, RecoveryID: recovery.RecoveryID,
		})
		if err != nil || n != 1 {
			return errors.Join(err, errors.New("exact card-12 cold-start admission rebind was unavailable"))
		}
		n, err = q.MarkDCPCard12ColdStartRecoveryReviewed(ctx, gen.MarkDCPCard12ColdStartRecoveryReviewedParams{
			CheckID: checkID, UpdatedAt: now, RecoveryID: recovery.RecoveryID,
			ReviewRunID: run.ID, TargetSha: run.TargetSHA,
		})
		if err != nil || n != 1 {
			return errors.Join(err, errors.New("exact card-12 cold-start review transition was unavailable"))
		}
		rebound = true
		return nil
	})
	return rebound, err
}

func dcpCard12ColdStartRecoveryFromRow(row gen.DcpReviewLabCard12ColdStartRecovery) domain.DCPCard12ColdStartRecovery {
	result := domain.DCPCard12ColdStartRecovery{
		RecoveryID: row.RecoveryID, Generation: row.Generation, IdentityDigest: row.IdentityDigest, ContractCommit: row.ContractCommit,
		PredecessorContinuationID: row.PredecessorContinuationID, IncidentID: row.IncidentID, AdmissionID: row.AdmissionID,
		SessionID: domain.SessionID(row.SessionID), TaskID: row.TaskID, ProjectID: row.ProjectID, Repository: row.Repository,
		WorktreePath: row.WorktreePath, SourceBranch: row.SourceBranch, PRURL: row.PRURL, PRNumber: row.PRNumber,
		OldHead: row.OldHead, CurrentMain: row.CurrentMain, ProviderBase: row.ProviderBase, ConflictPath: row.ConflictPath,
		MarkerDigest: row.MarkerDigest, StatusDigest: row.StatusDigest, Stage1Blob: row.Stage1Blob, Stage2Blob: row.Stage2Blob,
		Stage3Blob: row.Stage3Blob, ResolvedBytesDigest: row.ResolvedBytesDigest, ResolvedBlob: row.ResolvedBlob,
		PushRef: row.PushRef, PushLeaseOldHead: row.PushLeaseOldHead,
		UnauthorizedWorkerThread11: row.UnauthorizedWorkerThread11, UnauthorizedWorkerThread12: row.UnauthorizedWorkerThread12,
		UnauthorizedWorkerTokens11: row.UnauthorizedWorkerTokens11, UnauthorizedWorkerTokens12: row.UnauthorizedWorkerTokens12,
		WorkerModelCallCount: row.WorkerModelCallCount, ArbiterModelCallCount: row.ArbiterModelCallCount,
		ModelFreeActionCount: row.ModelFreeActionCount, ReviewerModelCallCount: row.ReviewerModelCallCount,
		BackupPath: row.BackupPath, BackupDigest: row.BackupDigest, LocalRefBefore: row.LocalRefBefore, LocalRefAfter: row.LocalRefAfter,
		NewHead: row.NewHead, NewCommit: row.NewCommit, ProviderNewHead: row.ProviderNewHead,
		RecoveryReviewRunID: row.RecoveryReviewRunID, RecoveryReviewID: row.RecoveryReviewID,
		RecoveryReviewBatchID: row.RecoveryReviewBatchID, RecoveryCheckID: row.RecoveryCheckID,
		MergeCommitSHA: row.MergeCommitSha, Status: domain.DCPCard12ColdStartRecoveryStatus(row.Status),
		Revision: row.Revision, ErrorCode: row.ErrorCode, AuthorizedAt: row.AuthorizedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.FinishedAt.Valid {
		finished := row.FinishedAt.Time
		result.FinishedAt = &finished
	}
	return result
}
