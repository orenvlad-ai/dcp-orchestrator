package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

func dcpWBCReadmissionFromGen(row gen.DcpWbcReadmissionGeneration) domain.DCPWBCReadmissionGeneration {
	return domain.DCPWBCReadmissionGeneration{
		Sequence: row.Sequence, GenerationID: row.GenerationID, MarkerDigest: row.MarkerDigest,
		MarkerVersion: row.MarkerVersion, MarkerCommentID: row.MarkerCommentID, MarkerAuthor: row.MarkerAuthor,
		MarkerCreatedAt: row.MarkerCreatedAt, MarkerUpdatedAt: row.MarkerUpdatedAt, MarkerMainSHA: row.MarkerMainSha, TaskID: row.TaskID,
		SessionID: domain.SessionID(row.SessionID), OldAdmissionID: row.OldAdmissionID, PRURL: row.PRURL,
		PRNumber: row.PRNumber, Repository: row.Repository, BaseBranch: row.BaseBranch, Scope: row.Scope,
		HeadRef: row.HeadRef, SessionNumber: row.SessionNumber, AdmittedHeadSHA: row.AdmittedHeadSha,
		AdmittedBaseSHA: row.AdmittedBaseSha, ObservedHeadSHA: row.ObservedHeadSha, CurrentMainSHA: row.CurrentMainSha, ReadyEventID: row.ReadyEventID,
		AdmissionCheckID: row.AdmissionCheckID, HandoffProofID: row.HandoffProofID, Reason: row.Reason,
		Status: domain.DCPWBCReadmissionStatus(row.Status), LeaseID: row.LeaseID, MergeTreeSHA: row.MergeTreeSha,
		NewHeadSHA: row.NewHeadSha, ReviewActionID: row.ReviewActionID, ReviewRunID: row.ReviewRunID,
		AdmissionID: row.AdmissionID, ErrorCode: row.ErrorCode, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func dcpWBCReadmissionResult(row gen.DcpWbcReadmissionGeneration, err error) (domain.DCPWBCReadmissionGeneration, bool, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPWBCReadmissionGeneration{}, false, nil
	}
	if err != nil {
		return domain.DCPWBCReadmissionGeneration{}, false, err
	}
	return dcpWBCReadmissionFromGen(row), true, nil
}

func (s *Store) GetOpenDCPWBCReadmissionGenerationByTask(ctx context.Context, taskID string) (domain.DCPWBCReadmissionGeneration, bool, error) {
	row, err := s.qr.GetOpenDCPWBCReadmissionGenerationByTask(ctx, taskID)
	return dcpWBCReadmissionResult(row, err)
}

func (s *Store) GetLatestDCPWBCReadmissionGenerationByTask(ctx context.Context, taskID string) (domain.DCPWBCReadmissionGeneration, bool, error) {
	row, err := s.qr.GetLatestDCPWBCReadmissionGenerationByTask(ctx, taskID)
	return dcpWBCReadmissionResult(row, err)
}

func (s *Store) ListDCPWBCReadmissionGenerations(ctx context.Context) ([]domain.DCPWBCReadmissionGeneration, error) {
	rows, err := s.qr.ListDCPWBCReadmissionGenerations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPWBCReadmissionGeneration, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpWBCReadmissionFromGen(row))
	}
	return out, nil
}

// ObserveDCPWBCReadmissionGeneration binds one exact immutable marker to the
// current incident task/admission. A prior generation may be closed as
// superseded only when that same task's next admission is the marker source.
func (s *Store) ObserveDCPWBCReadmissionGeneration(ctx context.Context, row domain.DCPWBCReadmissionGeneration, task domain.DCPReviewLabPolicyTask, admission domain.DCPReviewLabAdmission) (domain.DCPWBCReadmissionGeneration, bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var result domain.DCPWBCReadmissionGeneration
	var created bool
	err := s.inTx(ctx, "observe DCP WBC readmission", func(q *gen.Queries) error {
		storedRow, err := q.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
		if err != nil {
			return err
		}
		stored := dcpPolicyTaskFromGen(storedRow)
		if stored.TaskID != task.TaskID || stored.Revision != task.Revision || stored.State != domain.DCPPolicyIncident ||
			stored.Target != "wb-core" || stored.AdmissionID != admission.ID || stored.SessionID != admission.SessionID ||
			stored.CurrentHeadSHA != admission.TargetSHA || stored.ReviewRunID != admission.ReviewRunID ||
			admission.Status != domain.DCPAdmissionIncident || row.TaskID != stored.TaskID || row.SessionID != stored.SessionID ||
			row.OldAdmissionID != admission.ID || row.PRURL != admission.PRURL || row.PRNumber != admission.PRNumber ||
			!strings.EqualFold(row.AdmittedHeadSHA, admission.TargetSHA) ||
			!strings.EqualFold(row.AdmittedBaseSHA, admission.AdmittedBaseSHA) {
			return errors.New("exact WBC readmission incident identity was unavailable")
		}
		if prior, priorErr := q.GetOpenDCPWBCReadmissionGenerationByTask(ctx, row.TaskID); priorErr == nil {
			if prior.Status != string(domain.DCPWBCReadmissionReleaseWait) || prior.AdmissionID != admission.ID {
				return errors.New("another WBC readmission generation is still active")
			}
			if n, failErr := q.FailDCPWBCReadmissionGeneration(ctx, gen.FailDCPWBCReadmissionGenerationParams{
				ErrorCode: "superseded_by_readmission", UpdatedAt: row.CreatedAt, GenerationID: prior.GenerationID,
			}); failErr != nil || n != 1 {
				return errors.Join(failErr, errors.New("prior WBC readmission generation could not close"))
			}
		} else if !errors.Is(priorErr, sql.ErrNoRows) {
			return priorErr
		}
		n, err := q.InsertDCPWBCReadmissionGeneration(ctx, gen.InsertDCPWBCReadmissionGenerationParams{
			GenerationID: row.GenerationID, MarkerDigest: row.MarkerDigest, MarkerVersion: row.MarkerVersion,
			MarkerCommentID: row.MarkerCommentID, MarkerAuthor: row.MarkerAuthor, MarkerCreatedAt: row.MarkerCreatedAt,
			MarkerUpdatedAt: row.MarkerUpdatedAt, MarkerMainSha: strings.ToLower(row.MarkerMainSHA), TaskID: row.TaskID, SessionID: string(row.SessionID),
			OldAdmissionID: row.OldAdmissionID, PRURL: row.PRURL, PRNumber: row.PRNumber, Repository: row.Repository,
			BaseBranch: row.BaseBranch, Scope: row.Scope, HeadRef: row.HeadRef, SessionNumber: row.SessionNumber,
			AdmittedHeadSha: strings.ToLower(row.AdmittedHeadSHA), ObservedHeadSha: strings.ToLower(row.ObservedHeadSHA),
			AdmittedBaseSha: strings.ToLower(row.AdmittedBaseSHA),
			CurrentMainSha:  strings.ToLower(row.CurrentMainSHA), ReadyEventID: row.ReadyEventID,
			AdmissionCheckID: row.AdmissionCheckID, HandoffProofID: row.HandoffProofID, Reason: row.Reason,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
		if err != nil {
			return err
		}
		persisted, err := q.GetDCPWBCReadmissionGenerationByMarkerComment(ctx, row.MarkerCommentID)
		if err != nil {
			return err
		}
		result, created = dcpWBCReadmissionFromGen(persisted), n == 1
		if result.MarkerDigest != row.MarkerDigest || !strings.EqualFold(result.MarkerMainSHA, row.MarkerMainSHA) ||
			result.TaskID != row.TaskID || result.OldAdmissionID != admission.ID {
			return errors.New("persisted WBC readmission marker identity drifted")
		}
		return nil
	})
	return result, created, err
}

func (s *Store) ClaimDCPWBCReadmissionGeneration(ctx context.Context, row domain.DCPWBCReadmissionGeneration, leaseID string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.ClaimDCPWBCReadmissionGeneration(ctx, gen.ClaimDCPWBCReadmissionGenerationParams{LeaseID: leaseID, UpdatedAt: now, GenerationID: row.GenerationID})
	return n == 1, err
}

func (s *Store) PrepareDCPWBCReadmissionGeneration(ctx context.Context, row domain.DCPWBCReadmissionGeneration, treeSHA, headSHA, currentMainSHA string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.PrepareDCPWBCReadmissionGeneration(ctx, gen.PrepareDCPWBCReadmissionGenerationParams{
		MergeTreeSha: strings.ToLower(treeSHA), NewHeadSha: strings.ToLower(headSHA), CurrentMainSha: strings.ToLower(currentMainSHA), UpdatedAt: now,
		GenerationID: row.GenerationID, LeaseID: row.LeaseID, CurrentMainSha_2: strings.ToLower(row.CurrentMainSHA),
	})
	return n == 1, err
}

// AdvanceDCPWBCReadmissionHead atomically records the provider-confirmed head
// and re-opens the same task at a model-free CI wait. The old admission remains
// immutable incident evidence.
func (s *Store) AdvanceDCPWBCReadmissionHead(ctx context.Context, row domain.DCPWBCReadmissionGeneration, task domain.DCPReviewLabPolicyTask, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	changed := false
	err := s.inTx(ctx, "advance DCP WBC readmission head", func(q *gen.Queries) error {
		n, err := q.MarkDCPWBCReadmissionHeadPushed(ctx, gen.MarkDCPWBCReadmissionHeadPushedParams{
			UpdatedAt: now, GenerationID: row.GenerationID, LeaseID: row.LeaseID, NewHeadSha: strings.ToLower(row.NewHeadSHA),
		})
		if err != nil || n != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		storedRow, err := q.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
		if err != nil {
			return err
		}
		stored := dcpPolicyTaskFromGen(storedRow)
		if stored.State != domain.DCPPolicyIncident || stored.Revision != task.Revision || stored.AdmissionID != row.OldAdmissionID ||
			stored.CurrentHeadSHA != row.AdmittedHeadSHA || stored.SessionID != row.SessionID {
			return ErrDCPPolicyStale
		}
		next := stored
		next.State, next.PreviousHeadSHA, next.CurrentHeadSHA = domain.DCPPolicyCIWaiting, stored.CurrentHeadSHA, strings.ToLower(row.NewHeadSHA)
		next.ReviewRunID, next.AdmissionID, next.ReleasePhase = "", "", domain.DCPWBCReleasePhaseNone
		next.ErrorCode, next.IncidentPacket, next.UpdatedAt = "", "", now
		n, err = q.UpdateDCPReviewLabPolicyTask(ctx, policyTaskUpdateParams(stored, next))
		if err != nil || n != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		changed = true
		return nil
	})
	return changed, err
}

func (s *Store) FailDCPWBCReadmissionGeneration(ctx context.Context, row domain.DCPWBCReadmissionGeneration, code string, conflict bool, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var n int64
	var err error
	if conflict {
		n, err = s.qw.ConflictDCPWBCReadmissionGeneration(ctx, gen.ConflictDCPWBCReadmissionGenerationParams{ErrorCode: code, UpdatedAt: now, GenerationID: row.GenerationID})
	} else {
		n, err = s.qw.FailDCPWBCReadmissionGeneration(ctx, gen.FailDCPWBCReadmissionGenerationParams{ErrorCode: code, UpdatedAt: now, GenerationID: row.GenerationID})
	}
	return n == 1, err
}

// QueueDCPWBCReadmissionReview atomically binds the generation to the exact
// fresh-head reviewer. A single bounded findings repair may replace that
// action binding with the second fresh reviewer, without creating a generation.
func (s *Store) QueueDCPWBCReadmissionReview(ctx context.Context, current, next domain.DCPReviewLabPolicyTask, action domain.DCPModelAction, generation domain.DCPWBCReadmissionGeneration) (domain.DCPModelAction, bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var result domain.DCPModelAction
	var created bool
	err := s.inTx(ctx, "queue DCP WBC readmission review", func(q *gen.Queries) error {
		storedRow, err := q.GetDCPReviewLabPolicyTaskByTaskID(ctx, current.TaskID)
		if err != nil {
			return err
		}
		stored := dcpPolicyTaskFromGen(storedRow)
		open, err := q.GetOpenDCPWBCReadmissionGenerationByTask(ctx, current.TaskID)
		if err != nil {
			return err
		}
		first := open.Status == string(domain.DCPWBCReadmissionHeadPushed) && action.ExactHeadSHA == open.NewHeadSha
		repair := open.Status == string(domain.DCPWBCReadmissionReviewQueue) && stored.RepairCount == 1 && action.ExactHeadSHA != open.NewHeadSha
		if stored.State != domain.DCPPolicyCIWaiting || stored.Revision != current.Revision || stored.SessionID != action.SessionID ||
			open.GenerationID != generation.GenerationID || open.TaskID != stored.TaskID || open.SessionID != string(stored.SessionID) ||
			(!first && !repair) {
			return ErrDCPPolicyStale
		}
		if existing, getErr := q.GetDCPModelActionByIdentity(ctx, gen.GetDCPModelActionByIdentityParams{
			TaskID: action.TaskID, Kind: string(action.Kind), ExactHeadSha: strings.ToLower(action.ExactHeadSHA), IncidentID: action.IncidentID,
		}); getErr == nil {
			result = dcpModelActionFromGen(existing)
			return nil
		} else if !errors.Is(getErr, sql.ErrNoRows) {
			return getErr
		}
		next.UpdatedAt = action.CreatedAt
		n, err := q.UpdateDCPReviewLabPolicyTask(ctx, policyTaskUpdateParams(stored, next))
		if err != nil || n != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		action.ExactHeadSHA = strings.ToLower(action.ExactHeadSHA)
		if err := q.InsertDCPModelAction(ctx, modelActionInsertParams(action)); err != nil {
			return err
		}
		n, err = q.BindDCPWBCReadmissionReviewAction(ctx, gen.BindDCPWBCReadmissionReviewActionParams{
			ReviewActionID: action.ID, ReviewActionID_2: action.ID, UpdatedAt: action.CreatedAt, GenerationID: open.GenerationID,
		})
		if err != nil || n != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		result, created = action, true
		return nil
	})
	return result, created, err
}

func (s *Store) UpdateDCPWBCReleasePhase(ctx context.Context, current domain.DCPReviewLabPolicyTask, phase domain.DCPWBCReleasePhase, now time.Time) (bool, error) {
	if current.State != domain.DCPPolicyReleaseWaiting || current.ReleasePhase == phase {
		return false, nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	next := current
	next.ReleasePhase, next.UpdatedAt = phase, now
	n, err := s.qw.UpdateDCPReviewLabPolicyTask(ctx, policyTaskUpdateParams(current, next))
	return n == 1, err
}
