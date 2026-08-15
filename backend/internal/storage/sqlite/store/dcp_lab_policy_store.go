package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

var (
	ErrDCPPolicyTaskNotFound = errors.New("dcp review-lab policy task not found")
	ErrDCPPolicyConflict     = errors.New("dcp review-lab policy task payload conflict")
	ErrDCPPolicyStale        = errors.New("dcp review-lab policy state is stale")
)

type DCPPolicyReserveResult struct {
	Task    domain.DCPReviewLabPolicyTask
	Session domain.SessionRecord
	Created bool
}

// ReserveDCPReviewLabPolicyTask atomically allocates the stock per-project
// card/session number and binds it to the immutable policy task. A crash can
// therefore continue this exact seed; it can never allocate a replacement.
func (s *Store) ReserveDCPReviewLabPolicyTask(ctx context.Context, task domain.DCPReviewLabPolicyTask, seed domain.SessionRecord, worktreeRoot string) (DCPPolicyReserveResult, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	var result DCPPolicyReserveResult
	err := s.inTx(ctx, "reserve DCP review-lab policy task", func(q *gen.Queries) error {
		existing, err := q.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
		switch {
		case err == nil:
			mapped := dcpPolicyTaskFromGen(existing)
			if mapped.PayloadJSON != task.PayloadJSON || mapped.PayloadDigest != task.PayloadDigest ||
				mapped.Target != task.Target || mapped.Profile != task.Profile || mapped.Repository != task.Repository ||
				mapped.PolicyVersion != task.PolicyVersion || mapped.Prompt != task.Prompt {
				return ErrDCPPolicyConflict
			}
			session, getErr := q.GetSession(ctx, mapped.SessionID)
			if getErr != nil {
				return fmt.Errorf("load bound native session: %w", getErr)
			}
			result = DCPPolicyReserveResult{Task: mapped, Session: rowToRecord(session), Created: false}
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("load task identity: %w", err)
		}

		num, err := q.NextSessionNum(ctx, seed.ProjectID)
		if err != nil {
			return fmt.Errorf("allocate native card: %w", err)
		}
		spec, exact := domain.DCPPolicyTargetForTask(task)
		if !exact || string(seed.ProjectID) != spec.Target || num < spec.MinimumCardNumber {
			return fmt.Errorf("future policy card is outside the exact target range: target=%s next=%d", seed.ProjectID, num)
		}
		seed.ID = domain.SessionID(fmt.Sprintf("%s-%d", seed.ProjectID, num))
		if err := q.InsertSession(ctx, recordToInsert(seed, num)); err != nil {
			return fmt.Errorf("insert native session %s: %w", seed.ID, err)
		}

		task.SessionID = seed.ID
		task.CardNumber = num
		task.WorktreePath = filepath.Join(worktreeRoot, string(seed.ProjectID), string(seed.ID))
		task.SourceBranch = "ao/" + string(seed.ID) + "/root"
		if err := q.InsertDCPReviewLabPolicyTask(ctx, policyTaskInsertParams(task)); err != nil {
			return fmt.Errorf("insert policy task: %w", err)
		}
		action := domain.DCPModelAction{
			ID: "dcp-model-" + task.TaskID + "-worker-1", TaskID: task.TaskID, SessionID: task.SessionID,
			Kind: domain.DCPActionInitialWorker, Status: domain.DCPActionQueued,
			CreatedAt: task.CreatedAt, UpdatedAt: task.CreatedAt,
		}
		if err := q.InsertDCPModelAction(ctx, modelActionInsertParams(action)); err != nil {
			return fmt.Errorf("insert initial worker action: %w", err)
		}
		result = DCPPolicyReserveResult{Task: task, Session: seed, Created: true}
		return nil
	})
	if err != nil {
		return DCPPolicyReserveResult{}, err
	}
	return result, nil
}

func (s *Store) GetDCPReviewLabPolicyTaskByTaskID(ctx context.Context, taskID string) (domain.DCPReviewLabPolicyTask, bool, error) {
	row, err := s.qr.GetDCPReviewLabPolicyTaskByTaskID(ctx, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPReviewLabPolicyTask{}, false, nil
	}
	if err != nil {
		return domain.DCPReviewLabPolicyTask{}, false, err
	}
	return dcpPolicyTaskFromGen(row), true, nil
}

func (s *Store) GetDCPReviewLabPolicyTaskBySession(ctx context.Context, id domain.SessionID) (domain.DCPReviewLabPolicyTask, bool, error) {
	row, err := s.qr.GetDCPReviewLabPolicyTaskBySessionID(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPReviewLabPolicyTask{}, false, nil
	}
	if err != nil {
		return domain.DCPReviewLabPolicyTask{}, false, err
	}
	return dcpPolicyTaskFromGen(row), true, nil
}

func (s *Store) ListDCPReviewLabPolicyTasks(ctx context.Context) ([]domain.DCPReviewLabPolicyTask, error) {
	rows, err := s.qr.ListDCPReviewLabPolicyTasks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DCPReviewLabPolicyTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpPolicyTaskFromGen(row))
	}
	return out, nil
}

// UpdateDCPReviewLabPolicyTaskCAS advances only the mutable projection by one
// revision. Callers pass a full next snapshot to keep every identity check
// explicit at the transition site.
func (s *Store) UpdateDCPReviewLabPolicyTaskCAS(ctx context.Context, current, next domain.DCPReviewLabPolicyTask) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.UpdateDCPReviewLabPolicyTask(ctx, policyTaskUpdateParams(current, next))
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (s *Store) GetDCPModelActionByID(ctx context.Context, id string) (domain.DCPModelAction, bool, error) {
	row, err := s.qr.GetDCPModelActionByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPModelAction{}, false, nil
	}
	if err != nil {
		return domain.DCPModelAction{}, false, err
	}
	return dcpModelActionFromGen(row), true, nil
}

func (s *Store) GetDCPModelActionByIdentity(ctx context.Context, taskID string, kind domain.DCPModelActionKind, exactHead string) (domain.DCPModelAction, bool, error) {
	row, err := s.qr.GetDCPModelActionByIdentity(ctx, gen.GetDCPModelActionByIdentityParams{TaskID: taskID, Kind: string(kind), ExactHeadSha: strings.ToLower(exactHead), IncidentID: ""})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPModelAction{}, false, nil
	}
	if err != nil {
		return domain.DCPModelAction{}, false, err
	}
	return dcpModelActionFromGen(row), true, nil
}

func (s *Store) GetActiveDCPModelActionBySession(ctx context.Context, id domain.SessionID) (domain.DCPModelAction, bool, error) {
	row, err := s.qr.GetActiveDCPModelActionBySession(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DCPModelAction{}, false, nil
	}
	if err != nil {
		return domain.DCPModelAction{}, false, err
	}
	return dcpModelActionFromGen(row), true, nil
}

func (s *Store) ListDCPModelActions(ctx context.Context) ([]domain.DCPModelAction, error) {
	rows, err := s.qr.ListDCPModelActions(ctx)
	if err != nil {
		return nil, err
	}
	return mapDCPModelActions(rows), nil
}

func (s *Store) ListActiveDCPModelActions(ctx context.Context) ([]domain.DCPModelAction, error) {
	rows, err := s.qr.ListActiveDCPModelActions(ctx)
	if err != nil {
		return nil, err
	}
	return mapDCPModelActions(rows), nil
}

// ClaimNextDCPModelAction claims only the global FIFO head and the lowest free
// slot. It atomically advances the owning task to its running projection.
func (s *Store) ClaimNextDCPModelAction(ctx context.Context, now time.Time) (domain.DCPModelAction, bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var claimed domain.DCPModelAction
	var ok bool
	err := s.inTx(ctx, "claim DCP model action", func(q *gen.Queries) error {
		active, err := q.ListActiveDCPModelActions(ctx)
		if err != nil {
			return err
		}
		used := map[int64]bool{}
		for _, row := range active {
			used[row.Slot] = true
		}
		var slot int64
		for candidate := int64(1); candidate <= 3; candidate++ {
			if !used[candidate] {
				slot = candidate
				break
			}
		}
		if slot == 0 {
			return nil
		}
		queued, err := q.ListQueuedDCPModelActions(ctx)
		if err != nil {
			return err
		}
		if len(queued) == 0 {
			return nil
		}
		action := dcpModelActionFromGen(queued[0])
		taskRow, err := q.GetDCPReviewLabPolicyTaskByTaskID(ctx, action.TaskID)
		if err != nil {
			return err
		}
		task := dcpPolicyTaskFromGen(taskRow)
		wantState, runningState := queuedAndRunningState(action.Kind)
		if task.State != wantState || task.SessionID != action.SessionID {
			return ErrDCPPolicyStale
		}
		rows, err := q.ClaimDCPModelAction(ctx, gen.ClaimDCPModelActionParams{Slot: slot, UpdatedAt: now, ID: action.ID})
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		if action.Kind == domain.DCPActionArbiter {
			rows, err = q.ClaimDCPFutureArbiterIncident(ctx, gen.ClaimDCPFutureArbiterIncidentParams{UpdatedAt: now, IncidentID: action.IncidentID, ModelActionID: action.ID})
			if err != nil || rows != 1 {
				return errors.Join(err, ErrDCPPolicyStale)
			}
			action.Status, action.Slot, action.UpdatedAt = domain.DCPActionClaimed, slot, now
			claimed, ok = action, true
			return nil
		}
		next := task
		next.State = runningState
		next.UpdatedAt = now
		rows, err = q.UpdateDCPReviewLabPolicyTask(ctx, policyTaskUpdateParams(task, next))
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		action.Status, action.Slot, action.UpdatedAt = domain.DCPActionClaimed, slot, now
		claimed, ok = action, true
		return nil
	})
	return claimed, ok, err
}

func (s *Store) StartDCPModelAction(ctx context.Context, action domain.DCPModelAction, launchID, reviewRunID string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	started := false
	err := s.inTx(ctx, "start DCP model action", func(q *gen.Queries) error {
		rows, err := q.StartDCPModelAction(ctx, gen.StartDCPModelActionParams{
			LaunchID: launchID, ReviewRunID: reviewRunID, UpdatedAt: now,
			ID: action.ID, Slot: action.Slot,
		})
		if err != nil || rows != 1 {
			return err
		}
		if action.Kind == domain.DCPActionArbiter {
			rows, err = q.StartDCPFutureArbiterIncident(ctx, gen.StartDCPFutureArbiterIncidentParams{UpdatedAt: now, IncidentID: action.IncidentID, ModelActionID: action.ID})
			if err != nil || rows != 1 {
				return errors.Join(err, ErrDCPPolicyStale)
			}
		}
		started = true
		return nil
	})
	return started, err
}

// FinishDCPModelAction atomically releases the slot and advances the owning
// task. There is no retry transition from a failed action.
func (s *Store) FinishDCPModelAction(ctx context.Context, action domain.DCPModelAction, next domain.DCPReviewLabPolicyTask, status domain.DCPModelActionStatus, errorCode string, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var changed bool
	err := s.inTx(ctx, "finish DCP model action", func(q *gen.Queries) error {
		var err error
		changed, err = finishDCPModelActionTx(ctx, q, action, next, status, errorCode, now)
		return err
	})
	return changed, err
}

// FinishDCPModelActionAndQueue atomically consumes the first exact-head
// reviewer and creates the sole bounded repair action. A crash can observe
// neither a repair projection without its action nor a duplicate action.
func (s *Store) FinishDCPModelActionAndQueue(ctx context.Context, action domain.DCPModelAction, next domain.DCPReviewLabPolicyTask, queued domain.DCPModelAction, now time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var changed bool
	err := s.inTx(ctx, "finish DCP review and queue bounded repair", func(q *gen.Queries) error {
		var err error
		changed, err = finishDCPModelActionTx(ctx, q, action, next, domain.DCPActionSucceeded, "", now)
		if err != nil || !changed {
			return err
		}
		if queued.TaskID != action.TaskID || queued.SessionID != action.SessionID || queued.Kind != domain.DCPActionRepairWorker ||
			queued.ExactHeadSHA == "" || queued.ExactHeadSHA != next.CurrentHeadSHA || queued.Status != domain.DCPActionQueued ||
			queued.Slot != 0 || queued.LaunchID != "" || queued.ReviewRunID != "" {
			return ErrDCPPolicyStale
		}
		if err := q.InsertDCPModelAction(ctx, modelActionInsertParams(queued)); err != nil {
			return err
		}
		return nil
	})
	return changed, err
}

func finishDCPModelActionTx(ctx context.Context, q *gen.Queries, action domain.DCPModelAction, next domain.DCPReviewLabPolicyTask, status domain.DCPModelActionStatus, errorCode string, now time.Time) (bool, error) {
	currentAction, err := q.GetDCPModelActionByID(ctx, action.ID)
	if err != nil {
		return false, err
	}
	mappedAction := dcpModelActionFromGen(currentAction)
	if (mappedAction.Status != domain.DCPActionClaimed && mappedAction.Status != domain.DCPActionRunning) ||
		mappedAction.Slot != action.Slot || mappedAction.TaskID != action.TaskID {
		return false, nil
	}
	currentTaskRow, err := q.GetDCPReviewLabPolicyTaskByTaskID(ctx, action.TaskID)
	if err != nil {
		return false, err
	}
	currentTask := dcpPolicyTaskFromGen(currentTaskRow)
	if next.TaskID != currentTask.TaskID || next.Revision != currentTask.Revision {
		return false, ErrDCPPolicyStale
	}
	rows, err := q.FinishDCPModelAction(ctx, gen.FinishDCPModelActionParams{
		Status: string(status), ErrorCode: errorCode, UpdatedAt: now,
		ID: action.ID, Slot: mappedAction.Slot,
	})
	if err != nil || rows != 1 {
		return false, errors.Join(err, ErrDCPPolicyStale)
	}
	next.UpdatedAt = now
	rows, err = q.UpdateDCPReviewLabPolicyTask(ctx, policyTaskUpdateParams(currentTask, next))
	if err != nil || rows != 1 {
		return false, errors.Join(err, ErrDCPPolicyStale)
	}
	return true, nil
}

// QueueDCPModelAction inserts one exact bounded action and advances the task in
// the same transaction. Equal replay returns the existing action; a different
// action identity is rejected by the table uniqueness constraints.
func (s *Store) QueueDCPModelAction(ctx context.Context, current, next domain.DCPReviewLabPolicyTask, action domain.DCPModelAction) (domain.DCPModelAction, bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var result domain.DCPModelAction
	var created bool
	err := s.inTx(ctx, "queue DCP model action", func(q *gen.Queries) error {
		existing, err := q.GetDCPModelActionByIdentity(ctx, gen.GetDCPModelActionByIdentityParams{TaskID: action.TaskID, Kind: string(action.Kind), ExactHeadSha: strings.ToLower(action.ExactHeadSHA), IncidentID: action.IncidentID})
		if err == nil {
			result = dcpModelActionFromGen(existing)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		taskRow, err := q.GetDCPReviewLabPolicyTaskByTaskID(ctx, current.TaskID)
		if err != nil {
			return err
		}
		stored := dcpPolicyTaskFromGen(taskRow)
		if stored.State != current.State || stored.Revision != current.Revision || stored.SessionID != action.SessionID {
			return ErrDCPPolicyStale
		}
		next.UpdatedAt = action.CreatedAt
		rows, err := q.UpdateDCPReviewLabPolicyTask(ctx, policyTaskUpdateParams(stored, next))
		if err != nil || rows != 1 {
			return errors.Join(err, ErrDCPPolicyStale)
		}
		action.ExactHeadSHA = strings.ToLower(action.ExactHeadSHA)
		if err := q.InsertDCPModelAction(ctx, modelActionInsertParams(action)); err != nil {
			return err
		}
		result, created = action, true
		return nil
	})
	return result, created, err
}

func mapDCPModelActions(rows []gen.DcpModelAction) []domain.DCPModelAction {
	out := make([]domain.DCPModelAction, 0, len(rows))
	for _, row := range rows {
		out = append(out, dcpModelActionFromGen(row))
	}
	return out
}

func dcpPolicyTaskFromGen(row gen.DcpReviewLabPolicyTask) domain.DCPReviewLabPolicyTask {
	return domain.DCPReviewLabPolicyTask{
		TaskID: row.TaskID, PayloadJSON: row.PayloadJson, PayloadDigest: row.PayloadDigest,
		Target: row.Target, Profile: row.Profile, Repository: row.Repository, PolicyVersion: row.PolicyVersion,
		SessionID: domain.SessionID(row.SessionID), CardNumber: row.CardNumber,
		WorktreePath: row.WorktreePath, SourceBranch: row.SourceBranch, Prompt: row.Prompt,
		State: domain.DCPReviewLabPolicyState(row.State), Revision: row.Revision, RepairCount: row.RepairCount,
		PRURL: row.PRURL, PRNumber: row.PRNumber, CurrentHeadSHA: row.CurrentHeadSha, PreviousHeadSHA: row.PreviousHeadSha,
		ReviewRunID: row.ReviewRunID, AdmissionID: row.AdmissionID, MergeCommitSHA: row.MergeCommitSha,
		ErrorCode: row.ErrorCode, IncidentPacket: row.IncidentPacket, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func dcpModelActionFromGen(row gen.DcpModelAction) domain.DCPModelAction {
	return domain.DCPModelAction{
		Sequence: row.Sequence, ID: row.ID, TaskID: row.TaskID, SessionID: domain.SessionID(row.SessionID),
		Kind: domain.DCPModelActionKind(row.Kind), ExactHeadSHA: row.ExactHeadSha,
		Status: domain.DCPModelActionStatus(row.Status), Slot: row.Slot, LaunchID: row.LaunchID,
		ReviewRunID: row.ReviewRunID, IncidentID: row.IncidentID, ErrorCode: row.ErrorCode, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func policyTaskInsertParams(task domain.DCPReviewLabPolicyTask) gen.InsertDCPReviewLabPolicyTaskParams {
	return gen.InsertDCPReviewLabPolicyTaskParams{
		TaskID: task.TaskID, PayloadJson: task.PayloadJSON, PayloadDigest: task.PayloadDigest,
		Target: task.Target, Profile: task.Profile, Repository: task.Repository, PolicyVersion: task.PolicyVersion,
		SessionID: string(task.SessionID), CardNumber: task.CardNumber, WorktreePath: task.WorktreePath,
		SourceBranch: task.SourceBranch, Prompt: task.Prompt, State: string(task.State), Revision: task.Revision,
		RepairCount: task.RepairCount, PRURL: task.PRURL, PRNumber: task.PRNumber,
		CurrentHeadSha: task.CurrentHeadSHA, PreviousHeadSha: task.PreviousHeadSHA, ReviewRunID: task.ReviewRunID,
		AdmissionID: task.AdmissionID, MergeCommitSha: task.MergeCommitSHA, ErrorCode: task.ErrorCode,
		IncidentPacket: task.IncidentPacket, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}
}

func policyTaskUpdateParams(current, next domain.DCPReviewLabPolicyTask) gen.UpdateDCPReviewLabPolicyTaskParams {
	return gen.UpdateDCPReviewLabPolicyTaskParams{
		State: string(next.State), RepairCount: next.RepairCount, PRURL: next.PRURL, PRNumber: next.PRNumber,
		CurrentHeadSha: next.CurrentHeadSHA, PreviousHeadSha: next.PreviousHeadSHA, ReviewRunID: next.ReviewRunID,
		AdmissionID: next.AdmissionID, MergeCommitSha: next.MergeCommitSHA, ErrorCode: next.ErrorCode,
		IncidentPacket: next.IncidentPacket, UpdatedAt: next.UpdatedAt,
		TaskID: current.TaskID, State_2: string(current.State), Revision: current.Revision,
	}
}

func modelActionInsertParams(action domain.DCPModelAction) gen.InsertDCPModelActionParams {
	return gen.InsertDCPModelActionParams{
		ID: action.ID, TaskID: action.TaskID, SessionID: string(action.SessionID), Kind: string(action.Kind),
		ExactHeadSha: strings.ToLower(action.ExactHeadSHA), Status: string(action.Status), Slot: action.Slot,
		LaunchID: action.LaunchID, ReviewRunID: action.ReviewRunID, IncidentID: action.IncidentID, ErrorCode: action.ErrorCode,
		CreatedAt: action.CreatedAt, UpdatedAt: action.UpdatedAt,
	}
}

func queuedAndRunningState(kind domain.DCPModelActionKind) (domain.DCPReviewLabPolicyState, domain.DCPReviewLabPolicyState) {
	switch kind {
	case domain.DCPActionInitialWorker:
		return domain.DCPPolicyWorkerQueued, domain.DCPPolicyWorkerRunning
	case domain.DCPActionRepairWorker:
		return domain.DCPPolicyRepairQueued, domain.DCPPolicyRepairRunning
	case domain.DCPActionReviewer:
		return domain.DCPPolicyReviewQueued, domain.DCPPolicyReviewRunning
	case domain.DCPActionArbiter:
		return domain.DCPPolicyIncident, domain.DCPPolicyIncident
	default:
		return "", ""
	}
}
