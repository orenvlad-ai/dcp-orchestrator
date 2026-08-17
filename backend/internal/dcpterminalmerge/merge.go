// Package dcpterminalmerge owns the bounded I13 mechanical Admission
// Controller for the synthetic DCP review lab. It extends the historical
// exact-head terminal merge without becoming a general auto-merge policy:
// native cards and ReviewRuns keep their identity, SQLite owns one FIFO lease,
// and every repository/PR/head/review/check/provider fact is fail-closed.
package dcpterminalmerge

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	ProjectID          = "dcp-review-lab"
	SessionPrefix      = "dcp-review-lab"
	ProfileAgentRules  = domain.DCPReviewLabPolicyAgentRules
	TaskDisplayPrefix  = "DCP:"
	TaskPromptPrefix   = "DCP synthetic task "
	RepositoryFullName = "orenvlad-ai/dcp-review-lab"
	RepositoryURL      = "https://github.com/orenvlad-ai/dcp-review-lab.git"
	TargetBranch       = "main"
	RequiredCheckName  = "dcp-review-lab"
	HistoricalSession  = "dcp-review-lab-7"
	AdmissionSessionA  = "dcp-review-lab-9"
	AdmissionSessionB  = "dcp-review-lab-10"
	structuredChannel  = "structured_dcp_v1"
)

var (
	errCanonicalDiverged  = errors.New("dcp admission: canonical main cannot fast-forward to provider base")
	errCanonicalBaseDrift = errors.New("dcp admission: provider and fetched main differ")
)

type Store interface {
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	ListAllSessions(context.Context) ([]domain.SessionRecord, error)
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	ListPRsBySession(context.Context, domain.SessionID) ([]domain.PullRequest, error)
	ListReviewRunsBySession(context.Context, domain.SessionID) ([]domain.ReviewRun, error)
	EnqueueDCPReviewLabAdmission(context.Context, domain.DCPReviewLabAdmission) (domain.DCPReviewLabAdmission, bool, error)
	GetDCPReviewLabAdmissionByRun(context.Context, string) (domain.DCPReviewLabAdmission, bool, error)
	GetClaimedDCPReviewLabAdmission(context.Context) (domain.DCPReviewLabAdmission, bool, error)
	ListDCPReviewLabAdmissions(context.Context) ([]domain.DCPReviewLabAdmission, error)
	GetRefreshingDCPReviewLabAdmissionBySession(context.Context, domain.SessionID) (domain.DCPReviewLabAdmission, bool, error)
	RecoverDCPReviewLabCanonicalBaseIncident(context.Context, domain.DCPReviewLabAdmission, time.Time) (bool, error)
	ResumeDCPReviewLabAdmissionAfterRefresh(context.Context, domain.DCPReviewLabAdmission, domain.ReviewRun, string, time.Time) (bool, error)
	ClaimDCPReviewLabAdmission(context.Context, domain.DCPReviewLabAdmission, string, string, time.Time) (bool, error)
	CompleteDCPReviewLabAdmission(context.Context, domain.DCPReviewLabAdmission, string, time.Time) (bool, error)
	FailDCPReviewLabAdmission(context.Context, domain.DCPReviewLabAdmission, string, time.Time) (bool, error)
	StartDCPReviewLabRefresh(context.Context, domain.DCPReviewLabAdmission, string, string, time.Time) (bool, error)
	RecordDCPReviewLabIncident(context.Context, domain.DCPReviewLabAdmission, string, string, string, string, time.Time) (bool, error)
}

type policyStore interface {
	GetDCPReviewLabPolicyTaskBySession(context.Context, domain.SessionID) (domain.DCPReviewLabPolicyTask, bool, error)
	UpdateDCPReviewLabPolicyTaskCAS(context.Context, domain.DCPReviewLabPolicyTask, domain.DCPReviewLabPolicyTask) (bool, error)
	EnqueueDCPReviewLabPolicyAdmission(context.Context, domain.DCPReviewLabAdmission, domain.DCPReviewLabPolicyTask) (domain.DCPReviewLabAdmission, bool, error)
	ClaimDCPReleaseTrainAdmission(context.Context, domain.DCPReviewLabAdmission, domain.DCPReviewLabPolicyTask, string, string, time.Time) (bool, error)
	CompleteDCPReviewLabPolicyAdmission(context.Context, domain.DCPReviewLabAdmission, domain.DCPReviewLabPolicyTask, string, time.Time) (bool, error)
	RecordDCPReviewLabPolicyIncident(context.Context, domain.DCPReviewLabAdmission, domain.DCPReviewLabPolicyTask, string, string, string, string, time.Time) (bool, error)
}

type SCM interface {
	FetchPullRequests(context.Context, []ports.SCMPRRef) ([]ports.SCMObservation, error)
	FetchReviewThreads(context.Context, ports.SCMPRRef) (ports.SCMReviewObservation, error)
	MergePullRequest(context.Context, ports.SCMMergeRequest) (ports.SCMMergeResult, error)
}

type RefreshWaker func(context.Context, domain.SessionID, string) error

// AdmissionCommitSignal is the exact durable identity returned only after the
// future-policy admission transaction has committed. Delivery owns no SCM,
// claim, lease, Git or merge action; it only schedules another ordinary Try.
type AdmissionCommitSignal struct {
	AdmissionID string
	ReviewRunID string
	SessionID   domain.SessionID
	TargetSHA   string
}

type AdmissionCommittedHandler func(context.Context, AdmissionCommitSignal)

type Engine struct {
	store                  Store
	scm                    SCM
	dataDir                string
	mu                     sync.Mutex
	git                    func(context.Context, string, ...string) (string, error)
	providerRepository     func(context.Context) (string, error)
	providerRepositoryFor  func(context.Context, string) (string, error)
	wake                   RefreshWaker
	arbiter                ArbiterLauncher
	freshWorker            FreshWorkerLauncher
	modelFreeRebase        ModelFreeRebaseExecutor
	coldStartRecovery      ColdStartRecoveryExecutor
	rebaseHeadFinalization RebaseHeadFinalizationExecutor
	modelFreeReviewTrigger func(context.Context, domain.SessionID) error
	futureArbiter          FutureArbiterLauncher
	policyActionDrain      func(context.Context) error
	admissionCommitted     AdmissionCommittedHandler
	clock                  func() time.Time
}

func New(store Store, scm SCM, dataDir string) *Engine {
	return &Engine{
		store: store, scm: scm, dataDir: filepath.Clean(dataDir),
		git: gitOutput, providerRepository: publicRepositoryIdentity, providerRepositoryFor: publicRepositoryIdentityFor,
		clock: func() time.Time { return time.Now().UTC() },
	}
}

func (e *Engine) SetRefreshWaker(wake RefreshWaker) { e.wake = wake }

func (e *Engine) SetAdmissionCommittedHandler(handler AdmissionCommittedHandler) {
	e.admissionCommitted = handler
}

// ReconcileStartup first fences or completes the one persisted merge owner,
// then deterministically enrols exact approved sessions and drains at most the
// bounded FIFO. It starts no model merely to discover state.
func (e *Engine) ReconcileStartup(ctx context.Context) error {
	if err := e.configured(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if claimed, ok, err := e.store.GetClaimedDCPReviewLabAdmission(ctx); err != nil {
		return err
	} else if ok {
		continued, reconcileErr := e.reconcileClaimed(ctx, claimed)
		if reconcileErr != nil || !continued {
			return reconcileErr
		}
	}
	if err := e.recoverCanonicalBaseIncidents(ctx); err != nil {
		return err
	}
	if err := e.reconcileStage2Arbiter(ctx); err != nil {
		return err
	}
	if err := e.reconcileStage2ArbiterSuccessor(ctx); err != nil {
		return err
	}
	if err := e.reconcileCard12FreshWorker(ctx); err != nil {
		return err
	}
	if err := e.reconcileCard12ModelFreeRebase(ctx); err != nil {
		return err
	}
	if err := e.reconcileCard12ColdStartRecovery(ctx); err != nil {
		return err
	}
	if err := e.reconcileCard12RebaseHeadFinalization(ctx); err != nil {
		return err
	}
	if err := e.reconcileFutureArbiters(ctx); err != nil {
		return err
	}

	sessions, err := e.store.ListAllSessions(ctx)
	if err != nil {
		return err
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].CreatedAt.Equal(sessions[j].CreatedAt) {
			return sessions[i].ID < sessions[j].ID
		}
		return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
	})
	for _, session := range sessions {
		if eligible, eligibleErr := e.eligibleSession(ctx, session.ID); eligibleErr != nil {
			return eligibleErr
		} else if eligible {
			if err := e.enrol(ctx, session.ID); err != nil {
				return err
			}
		}
	}
	if err := e.reconcileFutureArbiters(ctx); err != nil {
		return err
	}
	if err := e.drain(ctx); err != nil {
		return err
	}
	return e.reconcileFutureArbiters(ctx)
}

// recoverCanonicalBaseIncidents is a one-shot startup repair for the exact
// false-positive produced when the first cohort merge advanced origin/main
// while the provider still reported the reviewed base SHA on the compatible
// second PR. It preserves the original packet in SQLite, proves both advances
// are fast-forwards plus a clean merge tree, and never wakes a model.
func (e *Engine) recoverCanonicalBaseIncidents(ctx context.Context) error {
	rows, err := e.store.ListDCPReviewLabAdmissions(ctx)
	if err != nil {
		return err
	}
	for _, admission := range rows {
		if admission.Status != domain.DCPAdmissionIncident || admission.ErrorCode != "canonical_main_diverged" ||
			admission.RefreshWakeCount != 0 || admission.RecoveredIncidentPacket != "" {
			continue
		}
		if store, ok := e.store.(policyStore); ok {
			if _, futurePolicy, policyErr := store.GetDCPReviewLabPolicyTaskBySession(ctx, admission.SessionID); policyErr != nil {
				return policyErr
			} else if futurePolicy {
				// The historical one-shot false-positive recovery is immutable
				// qualification evidence, never a future-policy retry mechanism.
				continue
			}
		}
		candidate, ok, candidateErr := e.candidateForAdmission(ctx, admission)
		if candidateErr != nil {
			return candidateErr
		}
		if !ok {
			continue
		}
		observation, review, freshErr := e.fresh(ctx, candidate.pr)
		if freshErr != nil {
			return freshErr
		}
		if !admissionFacts(candidate, observation, review) {
			continue
		}
		canonicalBase, syncErr := e.syncCanonicalMain(ctx, candidate, strings.ToLower(observation.PR.BaseSHA))
		if syncErr != nil || strings.EqualFold(canonicalBase, observation.PR.BaseSHA) {
			continue
		}
		if compatibilityErr := e.validateMergeCompatibility(ctx, candidate, observation.PR.HeadSHA, canonicalBase); compatibilityErr != nil {
			continue
		}
		recovered, recoverErr := e.store.RecoverDCPReviewLabCanonicalBaseIncident(ctx, admission, e.clock())
		if recoverErr != nil {
			return recoverErr
		}
		if !recovered {
			return errors.New("dcp admission: exact canonical-base incident recovery was unavailable")
		}
	}
	return nil
}

// Try is the single event entry. Lifecycle and stock SCM callbacks may race,
// but the process mutex and SQLite's partial unique index admit one owner.
func (e *Engine) Try(ctx context.Context, sessionID domain.SessionID) error {
	if err := e.configured(); err != nil {
		return err
	}
	if eligible, err := e.eligibleSession(ctx, sessionID); err != nil {
		return err
	} else if !eligible {
		return nil
	}
	var committed *AdmissionCommitSignal
	e.mu.Lock()
	if err := e.enrolAfterCommit(ctx, sessionID, func(signal AdmissionCommitSignal) { committed = &signal }); err != nil {
		e.mu.Unlock()
		return err
	}
	if committed != nil && e.admissionCommitted != nil {
		handler := e.admissionCommitted
		e.mu.Unlock()
		handler(ctx, *committed)
		return nil
	}
	defer e.mu.Unlock()
	if err := e.reconcileFutureArbiters(ctx); err != nil {
		return err
	}
	if err := e.drain(ctx); err != nil {
		return err
	}
	return e.reconcileFutureArbiters(ctx)
}

func (e *Engine) configured() error {
	if e == nil || e.store == nil || e.scm == nil || strings.TrimSpace(e.dataDir) == "" || e.clock == nil {
		return errors.New("dcp admission: dependencies are not configured")
	}
	return nil
}

func (e *Engine) enrol(ctx context.Context, sessionID domain.SessionID) error {
	return e.enrolAfterCommit(ctx, sessionID, nil)
}

func (e *Engine) enrolAfterCommit(ctx context.Context, sessionID domain.SessionID, afterCommit func(AdmissionCommitSignal)) error {
	candidate, ok, err := e.candidate(ctx, sessionID)
	if err != nil {
		return err
	}
	if !ok {
		return e.incidentPolicyBeforeAdmission(ctx, sessionID, "admission_identity_drift")
	}
	if candidate.run.TerminalMergeStatus != "" {
		return nil
	}
	if existing, ok, err := e.store.GetDCPReviewLabAdmissionByRun(ctx, candidate.run.ID); err != nil {
		return err
	} else if ok {
		if candidate.policy {
			if err := e.bindPolicyAdmission(ctx, candidate, existing); err != nil {
				return err
			}
		}
		if existing.Status == domain.DCPAdmissionRefreshing && candidate.session.Metadata.RuntimeLaunchID == "" {
			observation, _, freshErr := e.fresh(ctx, candidate.pr)
			if freshErr != nil {
				return freshErr
			}
			if strings.EqualFold(observation.PR.HeadSHA, existing.TargetSHA) {
				return e.recordIncident(ctx, existing, candidate, observation, "refresh_did_not_produce_new_head")
			}
		}
		return nil
	}
	observation, review, err := e.fresh(ctx, candidate.pr)
	if err != nil {
		return err
	}
	if !admissionFacts(candidate, observation, review) {
		if candidate.policy {
			return e.incidentPolicyBeforeAdmission(ctx, sessionID, "admission_facts_drift")
		}
		return nil
	}
	now := e.clock()
	if handled, recoveryErr := e.tryRebindRebaseHeadFinalization(ctx, candidate, observation, now); handled {
		return recoveryErr
	}
	if handled, recoveryErr := e.tryRebindColdStartRecovery(ctx, candidate, observation, now); handled {
		return recoveryErr
	}
	if handled, recoveryErr := e.tryRebindModelFreeRebase(ctx, candidate, observation, now); handled {
		return recoveryErr
	}
	if handled, recoveryErr := e.tryRebindFreshRecovery(ctx, candidate, observation, now); handled {
		return recoveryErr
	}
	if handled, recoveryErr := e.tryRebindFutureArbiter(ctx, candidate, observation, now); handled {
		return recoveryErr
	}
	if refreshing, ok, err := e.store.GetRefreshingDCPReviewLabAdmissionBySession(ctx, candidate.session.ID); err != nil {
		return err
	} else if ok {
		if refreshing.PRURL != candidate.pr.URL || refreshing.PRNumber != int64(candidate.pr.Number) ||
			strings.EqualFold(refreshing.TargetSHA, candidate.run.TargetSHA) || refreshing.RefreshWakeCount != 1 {
			return e.recordIncident(ctx, refreshing, candidate, observation, "refresh_identity_drift")
		}
		updated, updateErr := e.store.ResumeDCPReviewLabAdmissionAfterRefresh(ctx, refreshing, candidate.run, strings.ToLower(observation.PR.BaseSHA), now)
		if updateErr != nil {
			return updateErr
		}
		if !updated {
			return e.recordIncident(ctx, refreshing, candidate, observation, "refresh_transition_rejected")
		}
		return nil
	}
	if candidate.session.ID == ArbiterSessionA || candidate.session.ID == ArbiterSessionB {
		arbiterStore, storeErr := e.arbiterStore()
		if storeErr != nil {
			return storeErr
		}
		arbiter, ok, err := arbiterStore.GetDCPReleaseArbiterIncidentBySession(ctx, candidate.session.ID)
		if err != nil {
			return err
		}
		if ok {
			if successorStore, successorOK := e.store.(ArbiterSuccessorStore); successorOK {
				successor, successorFound, successorErr := successorStore.GetDCPReleaseArbiterSuccessorAttemptByIncident(ctx, arbiter.IncidentID)
				if successorErr != nil {
					return successorErr
				}
				if successorFound && successor.Status == domain.DCPArbiterSuccessorRepairing {
					original, found, getErr := arbiterStore.GetDCPReviewLabAdmissionByID(ctx, arbiter.AdmissionID)
					if getErr != nil || !found {
						return errors.New("dcp arbiter successor: repairing admission is unavailable")
					}
					if !exactSuccessorAuthorization(successor, arbiter) || original.Status != domain.DCPAdmissionIncident ||
						original.SessionID != candidate.session.ID || original.PRURL != candidate.pr.URL || original.TargetSHA != arbiter.TargetSHA ||
						strings.EqualFold(candidate.run.TargetSHA, arbiter.TargetSHA) || !strings.EqualFold(observation.PR.BaseSHA, arbiter.CurrentBaseSHA) ||
						successor.RecoveryOwnerSessionID != candidate.session.ID || successor.RecoveryPath != "same_worker_conflict_repair" ||
						successor.RecoveryWakeCount != 1 || successor.PolicyMaxWorkerCalls != 1 || successor.PolicyMaxFreshReviews != 1 {
						_, _ = successorStore.FailDCPReleaseArbiterSuccessorAfterDecision(ctx, successor.AttemptID, "repair_identity_drift", now)
						return errors.New("dcp arbiter successor: repaired exact-head identity drifted")
					}
					if validateErr := e.validateArbiterRecoveryCandidate(ctx, candidate, arbiter); validateErr != nil {
						_, _ = successorStore.FailDCPReleaseArbiterSuccessorAfterDecision(ctx, successor.AttemptID, "repair_scope_drift", now)
						return validateErr
					}
					rebound, updateErr := successorStore.RebindDCPAdmissionAfterArbiterSuccessorRepair(ctx, original, arbiter, successor, candidate.run, strings.ToLower(observation.PR.BaseSHA), now)
					if updateErr != nil || !rebound {
						_, _ = successorStore.FailDCPReleaseArbiterSuccessorAfterDecision(ctx, successor.AttemptID, "repair_rebind_rejected", now)
						return errors.Join(updateErr, errors.New("dcp arbiter successor: repaired exact-head rebind was rejected"))
					}
					return nil
				}
			}
		}
		if ok && arbiter.Status == domain.DCPArbiterRepairing {
			original, found, getErr := arbiterStore.GetDCPReviewLabAdmissionByID(ctx, arbiter.AdmissionID)
			if getErr != nil || !found {
				return errors.New("dcp arbiter: repairing admission is unavailable")
			}
			if original.Status != domain.DCPAdmissionIncident || original.SessionID != candidate.session.ID ||
				original.PRURL != candidate.pr.URL || original.TargetSHA != arbiter.TargetSHA ||
				strings.EqualFold(candidate.run.TargetSHA, arbiter.TargetSHA) ||
				!strings.EqualFold(observation.PR.BaseSHA, arbiter.CurrentBaseSHA) ||
				arbiter.RecoveryOwnerSessionID != candidate.session.ID || arbiter.RecoveryPath != "same_worker_conflict_repair" ||
				arbiter.RecoveryWakeCount != 1 {
				_, _ = arbiterStore.FailDCPReleaseArbiterAfterDecision(ctx, arbiter.IncidentID, "repair_identity_drift", now)
				return errors.New("dcp arbiter: repaired exact-head identity drifted")
			}
			if validateErr := e.validateArbiterRecoveryCandidate(ctx, candidate, arbiter); validateErr != nil {
				_, _ = arbiterStore.FailDCPReleaseArbiterAfterDecision(ctx, arbiter.IncidentID, "repair_scope_drift", now)
				return validateErr
			}
			rebound, updateErr := arbiterStore.RebindDCPAdmissionAfterArbiterRepair(ctx, original, arbiter, candidate.run, strings.ToLower(observation.PR.BaseSHA), now)
			if updateErr != nil || !rebound {
				_, _ = arbiterStore.FailDCPReleaseArbiterAfterDecision(ctx, arbiter.IncidentID, "repair_rebind_rejected", now)
				return errors.Join(updateErr, errors.New("dcp arbiter: repaired exact-head rebind was rejected"))
			}
			return nil
		}
	}
	requested := domain.DCPReviewLabAdmission{
		ID: "dcp-admission-" + candidate.run.ID, ReviewRunID: candidate.run.ID, ReviewID: candidate.run.ReviewID,
		SessionID: candidate.session.ID, PRURL: candidate.pr.URL, PRNumber: int64(candidate.pr.Number),
		TargetSHA: strings.ToLower(candidate.run.TargetSHA), ReviewBaseSHA: strings.ToLower(observation.PR.BaseSHA),
		Status: domain.DCPAdmissionWaiting, CreatedAt: now, UpdatedAt: now,
	}
	if candidate.policy {
		persisted, created, err := e.store.(policyStore).EnqueueDCPReviewLabPolicyAdmission(ctx, requested, candidate.policyTask)
		if err == nil && created && afterCommit != nil {
			if persisted.ID != requested.ID || persisted.ReviewRunID != requested.ReviewRunID ||
				persisted.SessionID != requested.SessionID || !strings.EqualFold(persisted.TargetSHA, requested.TargetSHA) {
				return errors.New("dcp admission: committed policy admission signal identity drifted")
			}
			afterCommit(AdmissionCommitSignal{
				AdmissionID: persisted.ID,
				ReviewRunID: persisted.ReviewRunID,
				SessionID:   persisted.SessionID,
				TargetSHA:   strings.ToLower(persisted.TargetSHA),
			})
		}
		return err
	}
	_, _, err = e.store.EnqueueDCPReviewLabAdmission(ctx, requested)
	return nil
}

func (e *Engine) incidentPolicyBeforeAdmission(ctx context.Context, sessionID domain.SessionID, reason string) error {
	store, ok := e.store.(policyStore)
	if !ok {
		return nil
	}
	current, found, err := store.GetDCPReviewLabPolicyTaskBySession(ctx, sessionID)
	if err != nil || !found || current.State.Terminal() {
		return err
	}
	if current.State != domain.DCPPolicyAdmissionWait || current.AdmissionID != "" || current.CurrentHeadSHA == "" || current.ReviewRunID == "" {
		return nil
	}
	packet, err := json.Marshal(map[string]string{
		"schemaVersion": "dcp.review-lab.policy-incident/v1", "reason": reason,
		"sessionId": string(sessionID), "reviewRunId": current.ReviewRunID,
		"targetSha": current.CurrentHeadSHA, "recordedAt": e.clock().Format(time.RFC3339Nano),
	})
	if err != nil {
		return err
	}
	next := current
	next.State, next.ErrorCode, next.IncidentPacket, next.UpdatedAt = domain.DCPPolicyIncident, reason, string(packet), e.clock()
	updated, err := store.UpdateDCPReviewLabPolicyTaskCAS(ctx, current, next)
	if err != nil || !updated {
		return errors.Join(err, errors.New("dcp admission: pre-admission policy incident was not persisted"))
	}
	return nil
}

// drain processes only the durable queue head. A successful merge immediately
// re-reads the next row in this same model-free event; pending provider facts,
// refresh, failure, or incident stop without a timer or poll loop.
func (e *Engine) drain(ctx context.Context) error {
	for {
		if claimed, ok, err := e.store.GetClaimedDCPReviewLabAdmission(ctx); err != nil {
			return err
		} else if ok {
			continued, reconcileErr := e.reconcileClaimed(ctx, claimed)
			if reconcileErr != nil || !continued {
				return reconcileErr
			}
			continue
		}
		admission, ok, err := e.nextPending(ctx)
		if err != nil || !ok {
			return err
		}
		if admission.Status == domain.DCPAdmissionRefreshing || admission.Status == domain.DCPAdmissionIncident {
			return nil
		}
		if admission.Status != domain.DCPAdmissionWaiting {
			return errors.New("dcp admission: invalid pending queue state")
		}
		if ready, cohortErr := e.cohortReady(ctx, admission); cohortErr != nil || !ready {
			return cohortErr
		}
		continued, err := e.processWaiting(ctx, admission)
		if err != nil {
			return err
		}
		if !continued {
			terminalIncident, terminalErr := e.policyAdmissionIncidentTerminal(ctx, admission)
			if terminalErr != nil || !terminalIncident {
				return terminalErr
			}
			if futureIncident, futureErr := e.pendingFutureArbiterIncident(ctx, admission); futureErr != nil || futureIncident {
				return futureErr
			}
		}
	}
}

func (e *Engine) pendingFutureArbiterIncident(ctx context.Context, admission domain.DCPReviewLabAdmission) (bool, error) {
	if _, ok := e.store.(FutureArbiterStore); !ok {
		return false, nil
	}
	fresh, found, err := e.store.GetDCPReviewLabAdmissionByRun(ctx, admission.ReviewRunID)
	if err != nil || !found {
		return false, err
	}
	if fresh.Status != domain.DCPAdmissionIncident || !eligibleFutureArbiterKind(fresh.ErrorCode) {
		return false, nil
	}
	store, ok := e.store.(policyStore)
	if !ok {
		return false, nil
	}
	task, found, err := store.GetDCPReviewLabPolicyTaskBySession(ctx, fresh.SessionID)
	return found && err == nil && task.State == domain.DCPPolicyIncident && task.AdmissionID == fresh.ID, err
}

func (e *Engine) nextPending(ctx context.Context) (domain.DCPReviewLabAdmission, bool, error) {
	rows, err := e.store.ListDCPReviewLabAdmissions(ctx)
	if err != nil {
		return domain.DCPReviewLabAdmission{}, false, err
	}
	for _, row := range rows {
		switch row.Status {
		case domain.DCPAdmissionIncident:
			terminal, terminalErr := e.policyAdmissionIncidentTerminal(ctx, row)
			if terminalErr != nil {
				return domain.DCPReviewLabAdmission{}, false, terminalErr
			}
			if terminal {
				continue
			}
			return row, true, nil
		case domain.DCPAdmissionWaiting, domain.DCPAdmissionRefreshing:
			return row, true, nil
		case domain.DCPAdmissionClaimed:
			return domain.DCPReviewLabAdmission{}, false, errors.New("dcp admission: claimed row escaped owner reconciliation")
		}
	}
	return domain.DCPReviewLabAdmission{}, false, nil
}

func (e *Engine) policyAdmissionIncidentTerminal(ctx context.Context, admission domain.DCPReviewLabAdmission) (bool, error) {
	if admission.Status != domain.DCPAdmissionIncident {
		fresh, found, err := e.store.GetDCPReviewLabAdmissionByRun(ctx, admission.ReviewRunID)
		if err != nil || !found {
			return false, err
		}
		admission = fresh
	}
	store, ok := e.store.(policyStore)
	if !ok {
		return false, nil
	}
	task, found, err := store.GetDCPReviewLabPolicyTaskBySession(ctx, admission.SessionID)
	if err != nil || !found {
		return false, err
	}
	return admission.Status == domain.DCPAdmissionIncident && task.State == domain.DCPPolicyIncident &&
		task.AdmissionID == admission.ID && task.ReviewRunID == admission.ReviewRunID && task.CurrentHeadSHA == admission.TargetSHA &&
		task.IncidentPacket == admission.IncidentPacket && task.ErrorCode == admission.ErrorCode, nil
}

func (e *Engine) cohortReady(ctx context.Context, admission domain.DCPReviewLabAdmission) (bool, error) {
	cohortA, cohortB := domain.SessionID(AdmissionSessionA), domain.SessionID(AdmissionSessionB)
	if admission.SessionID == ArbiterSessionA || admission.SessionID == ArbiterSessionB {
		cohortA, cohortB = ArbiterSessionA, ArbiterSessionB
	} else if admission.SessionID != AdmissionSessionA && admission.SessionID != AdmissionSessionB {
		return true, nil
	}
	rows, err := e.store.ListDCPReviewLabAdmissions(ctx)
	if err != nil {
		return false, err
	}
	present := map[domain.SessionID]bool{}
	for _, row := range rows {
		if row.SessionID == cohortA || row.SessionID == cohortB {
			present[row.SessionID] = true
		}
	}
	return present[cohortA] && present[cohortB], nil
}

func (e *Engine) reconcileClaimed(ctx context.Context, admission domain.DCPReviewLabAdmission) (bool, error) {
	candidate, ok, err := e.candidateForClaimedAdmission(ctx, admission)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, e.recordIncident(ctx, admission, mergeCandidate{}, ports.SCMObservation{}, "claimed_identity_drift")
	}
	if candidate.spec.UsesWBCReleaseTrain() {
		return e.reconcileWBCRelease(ctx, admission, candidate)
	}
	observation, _, err := e.fresh(ctx, candidate.pr)
	if err != nil {
		return false, err
	}
	if observation.PR.Merged && strings.EqualFold(observation.PR.HeadSHA, admission.TargetSHA) && validSHA(observation.PR.MergeCommitSHA) {
		mergeSHA := strings.ToLower(observation.PR.MergeCommitSHA)
		var updated bool
		var updateErr error
		if candidate.policy {
			updated, updateErr = e.store.(policyStore).CompleteDCPReviewLabPolicyAdmission(ctx, admission, candidate.policyTask, mergeSHA, e.clock())
		} else {
			updated, updateErr = e.store.CompleteDCPReviewLabAdmission(ctx, admission, mergeSHA, e.clock())
		}
		if updateErr != nil {
			return false, updateErr
		}
		if !updated {
			return false, errors.New("dcp admission: claimed action could not be reconciled")
		}
		return true, nil
	}
	return false, e.recordIncident(ctx, admission, candidate, observation, "uncertain_restart")
}

func (e *Engine) processWaiting(ctx context.Context, admission domain.DCPReviewLabAdmission) (bool, error) {
	if allowed, err := e.futureArbiterAllowsAdmission(ctx, admission); err != nil || !allowed {
		return false, err
	}
	candidate, ok, err := e.candidateForAdmission(ctx, admission)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, e.recordIncident(ctx, admission, mergeCandidate{}, ports.SCMObservation{}, "waiting_identity_drift")
	}
	observation, review, err := e.fresh(ctx, candidate.pr)
	if err != nil {
		return false, err
	}
	if !admissionFacts(candidate, observation, review) {
		if providerIdentityDrift(candidate, observation) {
			return false, e.recordIncident(ctx, admission, candidate, observation, "provider_identity_drift")
		}
		if candidate.policy {
			return false, e.recordIncident(ctx, admission, candidate, observation, "admission_facts_drift")
		}
		return false, nil
	}
	baseSHA := strings.ToLower(observation.PR.BaseSHA)
	switch mergeDisposition(observation) {
	case dispositionWait:
		return false, nil
	case dispositionIncident:
		return false, e.recordIncident(ctx, admission, candidate, observation, "merge_conflict_or_ambiguity")
	case dispositionRefresh:
		if candidate.policy {
			// Future policy heads may merge only with fresh provider CLEAN facts.
			// BEHIND is not upgraded by a local merge-tree proof and cannot borrow
			// the historical worker-refresh allowance.
			return false, e.recordIncident(ctx, admission, candidate, observation, "provider_not_clean")
		}
		if admission.SessionID == HistoricalSession {
			return false, e.recordIncident(ctx, admission, candidate, observation, "refresh_not_authorized")
		}
		canonicalBase, err := e.syncCanonicalMain(ctx, candidate, baseSHA)
		if err != nil {
			if errors.Is(err, errCanonicalDiverged) || errors.Is(err, errCanonicalBaseDrift) {
				return false, e.recordIncident(ctx, admission, candidate, observation, "canonical_main_diverged")
			}
			return false, err
		}
		if err := e.validateGit(ctx, candidate, observation.PR.HeadSHA, canonicalBase); err != nil {
			return false, err
		}
		leaseID := "dcp-refresh-" + admission.ID
		started, err := e.store.StartDCPReviewLabRefresh(ctx, admission, leaseID, canonicalBase, e.clock())
		if err != nil || !started {
			return false, err
		}
		admission.Status, admission.LeaseID, admission.AdmittedBaseSHA, admission.RefreshWakeCount = domain.DCPAdmissionRefreshing, leaseID, canonicalBase, 1
		if e.wake == nil {
			return false, e.recordIncident(ctx, admission, candidate, observation, "refresh_waker_unavailable")
		}
		if err := e.wake(ctx, admission.SessionID, refreshPrompt(candidate, admission, canonicalBase)); err != nil {
			if incidentErr := e.recordIncident(ctx, admission, candidate, observation, "refresh_launch_failed"); incidentErr != nil {
				return false, errors.Join(err, incidentErr)
			}
			return false, err
		}
		return false, nil
	case dispositionMerge:
		canonicalBase, err := e.syncCanonicalMain(ctx, candidate, baseSHA)
		if err != nil {
			if errors.Is(err, errCanonicalDiverged) || errors.Is(err, errCanonicalBaseDrift) {
				return false, e.recordIncident(ctx, admission, candidate, observation, "canonical_main_diverged")
			}
			return false, err
		}
		if !strings.EqualFold(canonicalBase, baseSHA) {
			if err := e.validateMergeCompatibility(ctx, candidate, observation.PR.HeadSHA, canonicalBase); err != nil {
				return false, e.recordIncident(ctx, admission, candidate, observation, "merge_conflict_or_ambiguity")
			}
		}
		if err := e.validateGit(ctx, candidate, observation.PR.HeadSHA, canonicalBase); err != nil {
			return false, err
		}
		if candidate.spec.UsesWBCReleaseTrain() {
			return e.handoffWBCRelease(ctx, admission, candidate, observation, canonicalBase)
		}
		return e.mergeWaiting(ctx, admission, candidate, observation, canonicalBase)
	default:
		return false, errors.New("dcp admission: unknown disposition")
	}
}

func (e *Engine) handoffWBCRelease(ctx context.Context, admission domain.DCPReviewLabAdmission, candidate mergeCandidate, observation ports.SCMObservation, canonicalBase string) (bool, error) {
	release, ok := e.scm.(ports.SCMReleaseTrain)
	if !ok {
		return false, e.recordIncident(ctx, admission, candidate, observation, "release_train_provider_unavailable")
	}
	leaseID := "dcp-release-" + admission.ID
	claimed, err := e.store.(policyStore).ClaimDCPReleaseTrainAdmission(ctx, admission, candidate.policyTask, leaseID, canonicalBase, e.clock())
	if err != nil || !claimed {
		return false, err
	}
	admission.Status, admission.LeaseID, admission.AdmittedBaseSHA = domain.DCPAdmissionClaimed, leaseID, canonicalBase
	candidate.policyTask.State = domain.DCPPolicyReleaseWaiting
	candidate.policyTask.Revision++
	request := ports.SCMReleaseReadyRequest{
		PR: ports.SCMPRRef{
			Repo:   ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "wb-core", Repo: candidate.spec.Repository},
			Number: candidate.pr.Number, URL: candidate.pr.URL,
		},
		ExpectedHeadSHA: candidate.run.TargetSHA, ExpectedBaseBranch: candidate.spec.DefaultBranch,
		RequiredTaskLabel: "task:standard", RequiredScopeLabel: "scope:repo-only",
	}
	if err := release.ApplyReleaseReady(ctx, request); err != nil {
		if incidentErr := e.recordIncident(ctx, admission, candidate, observation, "release_handoff_failed"); incidentErr != nil {
			return false, errors.Join(err, incidentErr)
		}
		return false, err
	}
	return false, nil
}

func (e *Engine) reconcileWBCRelease(ctx context.Context, admission domain.DCPReviewLabAdmission, candidate mergeCandidate) (bool, error) {
	release, ok := e.scm.(ports.SCMReleaseTrain)
	if !ok {
		return false, e.recordIncident(ctx, admission, candidate, ports.SCMObservation{}, "release_train_provider_unavailable")
	}
	ref := ports.SCMPRRef{
		Repo:   ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "wb-core", Repo: candidate.spec.Repository},
		Number: candidate.pr.Number, URL: candidate.pr.URL,
	}
	observed, err := release.ObserveRelease(ctx, ref)
	if err != nil {
		return false, err
	}
	incidentObservation := ports.SCMObservation{PR: ports.SCMPRObservation{BaseSHA: admission.AdmittedBaseSHA}}
	exactIdentity := observed.Number == candidate.pr.Number && observed.URL == candidate.pr.URL &&
		observed.HeadRepository == candidate.spec.Repository && observed.HeadBranch == candidate.pr.SourceBranch &&
		observed.BaseBranch == candidate.spec.DefaultBranch && observed.Author == "orenvlad-ai"
	if !exactIdentity {
		return false, e.recordIncident(ctx, admission, candidate, incidentObservation, "release_identity_drift")
	}
	if !strings.EqualFold(observed.HeadSHA, admission.TargetSHA) {
		return false, e.recordIncident(ctx, admission, candidate, incidentObservation, "release_head_drift")
	}
	if !exactWBCReleaseLabels(observed.Labels) {
		return false, e.recordIncident(ctx, admission, candidate, incidentObservation, "release_label_drift")
	}
	if !observed.Merged {
		if observed.State != "open" || observed.Draft || !exactWBCReleasePhase(observed.Labels, false) {
			return false, e.recordIncident(ctx, admission, candidate, incidentObservation, "release_state_drift")
		}
		return false, nil
	}
	mergeSHA := strings.ToLower(observed.MergeCommitSHA)
	proof := fmt.Sprintf("<!-- wb-core-release-completion-proof contour=repo-only merge=%s pr=%d -->", mergeSHA, observed.Number)
	if observed.State != "closed" || !validSHA(mergeSHA) || !exactWBCReleasePhase(observed.Labels, true) ||
		strings.Count(observed.Body, proof) != 1 {
		return false, e.recordIncident(ctx, admission, candidate, incidentObservation, "release_terminal_proof_invalid")
	}
	updated, err := e.store.(policyStore).CompleteDCPReviewLabPolicyAdmission(ctx, admission, candidate.policyTask, mergeSHA, e.clock())
	if err != nil {
		return false, err
	}
	if !updated {
		return false, errors.New("dcp admission: Release Train completion could not be recorded")
	}
	return true, nil
}

func exactWBCReleasePhase(labels []string, terminal bool) bool {
	allowed, seen := "release:ready", 0
	if terminal {
		allowed = "release:done"
	}
	for _, label := range labels {
		if !strings.HasPrefix(label, "release:") {
			continue
		}
		if !terminal && label == "release:running" {
			seen++
			continue
		}
		if label != allowed {
			return false
		}
		seen++
	}
	return seen == 1
}

func exactWBCReleaseLabels(labels []string) bool {
	taskLabels, scopeLabels := 0, 0
	for _, label := range labels {
		switch {
		case strings.HasPrefix(label, "task:"):
			taskLabels++
			if label != "task:standard" {
				return false
			}
		case strings.HasPrefix(label, "scope:"):
			scopeLabels++
			if label != "scope:repo-only" {
				return false
			}
		}
	}
	return taskLabels == 1 && scopeLabels == 1
}

func hasExactLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func (e *Engine) mergeWaiting(ctx context.Context, admission domain.DCPReviewLabAdmission, candidate mergeCandidate, observation ports.SCMObservation, canonicalBase string) (bool, error) {
	if err := e.validateGit(ctx, candidate, observation.PR.HeadSHA, canonicalBase); err != nil {
		return false, err
	}
	leaseID := "dcp-merge-" + admission.ID
	claimed, err := e.store.ClaimDCPReviewLabAdmission(ctx, admission, leaseID, canonicalBase, e.clock())
	if err != nil || !claimed {
		return false, err
	}
	admission.Status, admission.LeaseID, admission.AdmittedBaseSHA = domain.DCPAdmissionClaimed, leaseID, canonicalBase
	result, mergeErr := e.scm.MergePullRequest(ctx, ports.SCMMergeRequest{
		PR: ports.SCMPRRef{
			Repo: ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai",
				Name: strings.TrimPrefix(candidate.spec.Repository, "orenvlad-ai/"), Repo: candidate.spec.Repository},
			Number: candidate.pr.Number, URL: candidate.pr.URL,
		},
		ExpectedHeadSHA: candidate.run.TargetSHA,
		Method:          ports.SCMMergeSquash,
	})
	if mergeErr != nil {
		if incidentErr := e.recordIncident(ctx, admission, candidate, observation, mergeErrorCode(mergeErr)); incidentErr != nil {
			return false, errors.Join(mergeErr, incidentErr)
		}
		return false, mergeErr
	}
	if !validSHA(result.MergeCommitSHA) {
		return false, e.recordIncident(ctx, admission, candidate, observation, "invalid_merge_result")
	}
	mergeSHA := strings.ToLower(result.MergeCommitSHA)
	var updated bool
	if candidate.policy {
		updated, err = e.store.(policyStore).CompleteDCPReviewLabPolicyAdmission(ctx, admission, candidate.policyTask, mergeSHA, e.clock())
	} else {
		updated, err = e.store.CompleteDCPReviewLabAdmission(ctx, admission, mergeSHA, e.clock())
	}
	if err != nil {
		return false, err
	}
	if !updated {
		return false, errors.New("dcp admission: completed provider mutation could not be recorded")
	}
	return true, nil
}

func (e *Engine) bindPolicyAdmission(ctx context.Context, candidate mergeCandidate, admission domain.DCPReviewLabAdmission) error {
	if !candidate.policy {
		return nil
	}
	store, ok := e.store.(policyStore)
	if !ok {
		return errors.New("dcp admission: policy store unavailable")
	}
	current, found, err := store.GetDCPReviewLabPolicyTaskBySession(ctx, candidate.session.ID)
	if err != nil || !found {
		return errors.Join(err, errors.New("dcp admission: policy task unavailable"))
	}
	if current.State != domain.DCPPolicyAdmissionWait || current.CurrentHeadSHA != admission.TargetSHA ||
		current.ReviewRunID != admission.ReviewRunID || (current.AdmissionID != "" && current.AdmissionID != admission.ID) {
		return errors.New("dcp admission: policy task exact-head binding drifted")
	}
	if current.AdmissionID == admission.ID {
		return nil
	}
	next := current
	next.AdmissionID, next.UpdatedAt = admission.ID, e.clock()
	updated, err := store.UpdateDCPReviewLabPolicyTaskCAS(ctx, current, next)
	if err != nil || !updated {
		return errors.Join(err, errors.New("dcp admission: policy binding was rejected"))
	}
	return nil
}

type mergeCandidate struct {
	session    domain.SessionRecord
	project    domain.ProjectRecord
	pr         domain.PullRequest
	run        domain.ReviewRun
	policyTask domain.DCPReviewLabPolicyTask
	spec       domain.DCPPolicyTargetSpec
	policy     bool
}

func (e *Engine) candidateForAdmission(ctx context.Context, admission domain.DCPReviewLabAdmission) (mergeCandidate, bool, error) {
	return e.candidateForAdmissionInPolicyState(ctx, admission, domain.DCPPolicyAdmissionWait)
}

func (e *Engine) candidateForClaimedAdmission(ctx context.Context, admission domain.DCPReviewLabAdmission) (mergeCandidate, bool, error) {
	state := domain.DCPPolicyAdmissionWait
	if store, ok := e.store.(policyStore); ok {
		if task, found, err := store.GetDCPReviewLabPolicyTaskBySession(ctx, admission.SessionID); err != nil {
			return mergeCandidate{}, false, err
		} else if found {
			if spec, exact := domain.DCPPolicyTargetForTask(task); exact && spec.UsesWBCReleaseTrain() {
				state = domain.DCPPolicyReleaseWaiting
			}
		}
	}
	return e.candidateForAdmissionInPolicyState(ctx, admission, state)
}

func (e *Engine) candidateForFutureArbiterAdmission(ctx context.Context, admission domain.DCPReviewLabAdmission) (mergeCandidate, bool, error) {
	return e.candidateForAdmissionInPolicyState(ctx, admission, domain.DCPPolicyIncident)
}

func (e *Engine) candidateForAdmissionInPolicyState(ctx context.Context, admission domain.DCPReviewLabAdmission, state domain.DCPReviewLabPolicyState) (mergeCandidate, bool, error) {
	candidate, ok, err := e.candidateInPolicyState(ctx, admission.SessionID, state)
	if err != nil || !ok {
		return mergeCandidate{}, false, err
	}
	if candidate.run.ID != admission.ReviewRunID || candidate.run.ReviewID != admission.ReviewID ||
		candidate.pr.URL != admission.PRURL || int64(candidate.pr.Number) != admission.PRNumber ||
		!strings.EqualFold(candidate.run.TargetSHA, admission.TargetSHA) {
		return mergeCandidate{}, false, nil
	}
	return candidate, true, nil
}

func (e *Engine) candidate(ctx context.Context, id domain.SessionID) (mergeCandidate, bool, error) {
	return e.candidateInPolicyState(ctx, id, domain.DCPPolicyAdmissionWait)
}

func (e *Engine) candidateInPolicyState(ctx context.Context, id domain.SessionID, state domain.DCPReviewLabPolicyState) (mergeCandidate, bool, error) {
	var policyTask domain.DCPReviewLabPolicyTask
	policy := false
	if ps, ok := e.store.(policyStore); ok {
		var policyErr error
		policyTask, policy, policyErr = ps.GetDCPReviewLabPolicyTaskBySession(ctx, id)
		if policyErr != nil {
			return mergeCandidate{}, false, policyErr
		}
	}
	spec, _ := domain.DCPPolicyTarget("dcp-review-lab", "synthetic-pr")
	if policy {
		var exact bool
		spec, exact = domain.DCPPolicyTargetForTask(policyTask)
		if !exact {
			return mergeCandidate{}, false, nil
		}
	}
	session, ok, err := e.store.GetSession(ctx, id)
	if err != nil || !ok {
		return mergeCandidate{}, false, err
	}
	if session.ProjectID != domain.ProjectID(spec.Target) || session.Kind != domain.KindWorker || session.Harness != domain.HarnessCodex ||
		session.ReviewerHarness != "" || session.IssueID != "" || session.Activity.State != domain.ActivityIdle || session.IsTerminated ||
		session.TerminateOnPRMerge || session.Metadata.RuntimeLaunchID != "" || !validOptionalNativeBase(session.Metadata.DiffBaseSHA, session.Metadata.DiffBaseRef) ||
		(!policy && !validTaskIdentity(session)) {
		return mergeCandidate{}, false, nil
	}
	if policy && (!validPolicyTaskIdentity(policyTask, session, e.dataDir) || policyTask.State != state) {
		return mergeCandidate{}, false, nil
	}
	if session.ID == ArbiterSessionA || session.ID == ArbiterSessionB {
		if _, _, exact := arbiterTask(session); !exact {
			return mergeCandidate{}, false, nil
		}
	}
	expectedWorkspace := filepath.Join(e.dataDir, "worktrees", spec.Target, string(id))
	expectedBranch := "ao/" + string(id) + "/root"
	if !sameExactPath(session.Metadata.WorkspacePath, expectedWorkspace) || session.Metadata.Branch != expectedBranch {
		return mergeCandidate{}, false, nil
	}
	project, ok, err := e.store.GetProject(ctx, spec.Target)
	if err != nil || !ok {
		return mergeCandidate{}, false, err
	}
	expectedProjectPath := filepath.Join(filepath.Dir(e.dataDir), "targets", spec.Target)
	if !sameExactPath(project.Path, expectedProjectPath) || project.Kind.WithDefault() != domain.ProjectKindSingleRepo || project.RepoOriginURL != spec.OriginURL ||
		project.Config.DefaultBranch != spec.DefaultBranch || project.Config.SessionPrefix != spec.SessionPrefix ||
		project.Config.AgentRules != spec.AgentRules || project.Config.AgentRulesFile != "" || project.Config.OrchestratorRules != "" ||
		!project.Config.AgentConfig.IsZero() || project.Config.Worker != (domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Permissions: domain.PermissionModeAcceptEdits, DCPReviewLabNetwork: true}}) ||
		project.Config.Orchestrator != (domain.RoleOverride{}) || project.Config.TrackerIntake != (domain.TrackerIntakeConfig{}) ||
		project.Config.ContainerReap != (domain.ContainerReapConfig{}) ||
		len(project.Config.Reviewers) != 1 || project.Config.Reviewers[0].Harness != domain.ReviewerCodex ||
		len(project.Config.Env) != 0 || len(project.Config.Symlinks) != 0 || len(project.Config.PostCreate) != 0 {
		return mergeCandidate{}, false, nil
	}
	if policy {
		if e.providerRepositoryFor == nil {
			return mergeCandidate{}, false, nil
		}
		var identity string
		var identityErr error
		if spec.Target == ProjectID && e.providerRepository != nil {
			identity, identityErr = e.providerRepository(ctx)
		} else {
			identity, identityErr = e.providerRepositoryFor(ctx, spec.Repository)
		}
		if identityErr != nil || identity != policyProviderIdentity(spec) {
			return mergeCandidate{}, false, nil
		}
	}
	prs, err := e.store.ListPRsBySession(ctx, id)
	if err != nil || len(prs) != 1 {
		return mergeCandidate{}, false, err
	}
	pr := prs[0]
	if pr.Provider != "github" || pr.Host != "github.com" || pr.Repo != spec.Repository || pr.TargetBranch != spec.DefaultBranch ||
		pr.SourceBranch != expectedBranch || pr.Author != "orenvlad-ai" || pr.HTMLURL != pr.URL ||
		!validPRURL(spec, pr.URL, pr.Number) || !validSHA(pr.HeadSHA) || !validSHA(pr.BaseSHA) ||
		(!policy && session.Metadata.DiffBaseSHA != "" && !strings.EqualFold(pr.BaseSHA, session.Metadata.DiffBaseSHA)) {
		return mergeCandidate{}, false, nil
	}
	runs, err := e.store.ListReviewRunsBySession(ctx, id)
	if err != nil {
		return mergeCandidate{}, false, err
	}
	var exact []domain.ReviewRun
	for _, run := range runs {
		if run.PRURL == pr.URL && strings.EqualFold(run.TargetSHA, pr.HeadSHA) {
			exact = append(exact, run)
		}
	}
	if len(exact) != 1 {
		return mergeCandidate{}, false, nil
	}
	run := exact[0]
	if run.Status != domain.ReviewRunComplete || run.Verdict != domain.VerdictApproved || run.ResultChannel != structuredChannel ||
		run.Harness != domain.ReviewerCodex || run.ID == "" || run.ReviewID == "" || run.BatchID == "" || run.Body == "" || run.GithubReviewID != "" {
		return mergeCandidate{}, false, nil
	}
	switch run.TerminalMergeStatus {
	case "":
		if pr.Draft || pr.Merged || pr.Closed || pr.ProviderState != "OPEN" {
			return mergeCandidate{}, false, nil
		}
	case "running":
		if pr.ProviderState != "OPEN" && pr.ProviderState != "MERGED" && pr.ProviderState != "CLOSED" {
			return mergeCandidate{}, false, nil
		}
	case "succeeded", "failed":
	default:
		return mergeCandidate{}, false, nil
	}
	return mergeCandidate{session: session, project: project, pr: pr, run: run, policyTask: policyTask, spec: spec, policy: policy}, true, nil
}

func (e *Engine) fresh(ctx context.Context, pr domain.PullRequest) (ports.SCMObservation, ports.SCMReviewObservation, error) {
	spec, exact := policySpecForRepository(pr.Repo)
	if !exact {
		return ports.SCMObservation{}, ports.SCMReviewObservation{}, errors.New("dcp admission: repository is not allowlisted")
	}
	ref := ports.SCMPRRef{
		Repo:   ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: strings.TrimPrefix(spec.Repository, "orenvlad-ai/"), Repo: spec.Repository},
		Number: pr.Number, URL: pr.URL,
	}
	observations, err := e.scm.FetchPullRequests(ctx, []ports.SCMPRRef{ref})
	if err != nil {
		return ports.SCMObservation{}, ports.SCMReviewObservation{}, err
	}
	if len(observations) != 1 || !observations[0].Fetched {
		return ports.SCMObservation{}, ports.SCMReviewObservation{}, errors.New("dcp admission: exact PR could not be refreshed")
	}
	review, err := e.scm.FetchReviewThreads(ctx, ref)
	if err != nil {
		return ports.SCMObservation{}, ports.SCMReviewObservation{}, err
	}
	return observations[0], review, nil
}

func admissionFacts(candidate mergeCandidate, observation ports.SCMObservation, review ports.SCMReviewObservation) bool {
	if providerIdentityDrift(candidate, observation) || review.Partial || !knownNonBlockingReviewDecision(review.Decision) || hasBlockingReview(review) {
		return false
	}
	if len(observation.CI.Checks) == 0 || observation.CI.Summary != string(domain.CIPassing) || !strings.EqualFold(observation.CI.HeadSHA, candidate.run.TargetSHA) {
		return false
	}
	required := 0
	for _, check := range observation.CI.Checks {
		if check.Status != string(domain.PRCheckPassed) {
			return false
		}
		if check.Name == candidate.spec.RequiredCheck {
			if check.Status != string(domain.PRCheckPassed) || check.Conclusion != "success" || (candidate.policy && !validCheckURL(candidate.spec, check.URL)) {
				return false
			}
			required++
		}
	}
	return required == 1
}

func providerIdentityDrift(candidate mergeCandidate, observation ports.SCMObservation) bool {
	pr := observation.PR
	return observation.Provider != "github" || observation.Host != "github.com" || observation.Repo != candidate.spec.Repository ||
		pr.Number != candidate.pr.Number || pr.URL != candidate.pr.URL || pr.HeadRepo != candidate.spec.Repository ||
		pr.SourceBranch != candidate.pr.SourceBranch || pr.TargetBranch != candidate.spec.DefaultBranch ||
		!strings.EqualFold(pr.HeadSHA, candidate.run.TargetSHA) || !validSHA(pr.BaseSHA) ||
		pr.State != string(domain.PRStateOpen) || pr.ProviderState != "OPEN" || pr.Author != "orenvlad-ai" || pr.HTMLURL != pr.URL ||
		pr.Draft || pr.Merged || pr.Closed
}

type disposition int

const (
	dispositionWait disposition = iota
	dispositionMerge
	dispositionRefresh
	dispositionIncident
)

func mergeDisposition(observation ports.SCMObservation) disposition {
	pr := observation.PR
	if pr.ProviderMergeable == "MERGEABLE" && pr.ProviderMergeStateStatus == "CLEAN" &&
		observation.Mergeability.State == string(domain.MergeMergeable) && observation.Mergeability.Mergeable && len(observation.Mergeability.Blockers) == 0 {
		return dispositionMerge
	}
	if pr.ProviderMergeable == "MERGEABLE" && pr.ProviderMergeStateStatus == "BEHIND" &&
		observation.Mergeability.State == string(domain.MergeMergeable) && observation.Mergeability.Mergeable {
		return dispositionRefresh
	}
	if pr.ProviderMergeable == "CONFLICTING" || pr.ProviderMergeStateStatus == "DIRTY" || observation.Mergeability.State == string(domain.MergeConflicting) {
		return dispositionIncident
	}
	return dispositionWait
}

func ready(candidate mergeCandidate, observation ports.SCMObservation, review ports.SCMReviewObservation) bool {
	return admissionFacts(candidate, observation, review) && mergeDisposition(observation) == dispositionMerge
}

func hasBlockingReview(review ports.SCMReviewObservation) bool {
	for _, thread := range review.Threads {
		if !thread.Resolved {
			return true
		}
	}
	return false
}

func knownNonBlockingReviewDecision(decision string) bool {
	return decision == string(domain.ReviewNone) || decision == string(domain.ReviewApproved)
}

func (e *Engine) syncCanonicalMain(ctx context.Context, candidate mergeCandidate, baseSHA string) (string, error) {
	if !validSHA(baseSHA) {
		return "", errors.New("dcp admission: provider base SHA is invalid")
	}
	projectPath := candidate.project.Path
	prechecks := []struct {
		args []string
		want string
	}{
		{[]string{"rev-parse", "--show-toplevel"}, projectPath},
		{[]string{"branch", "--show-current"}, candidate.spec.DefaultBranch},
		{[]string{"remote"}, "origin"},
		{[]string{"remote", "get-url", "origin"}, candidate.spec.OriginURL},
		{[]string{"status", "--porcelain"}, ""},
	}
	for _, check := range prechecks {
		got, err := e.git(ctx, projectPath, check.args...)
		if err != nil || got != check.want {
			return "", errors.New("dcp admission: canonical repository identity is not exact and clean")
		}
	}
	if _, err := e.git(ctx, projectPath, "fetch", "--no-tags", "origin", candidate.spec.DefaultBranch); err != nil {
		return "", fmt.Errorf("dcp admission: fetch canonical main: %w", err)
	}
	originMain, err := e.git(ctx, projectPath, "rev-parse", "origin/"+candidate.spec.DefaultBranch)
	if err != nil || !validSHA(originMain) {
		return "", errCanonicalBaseDrift
	}
	if !strings.EqualFold(originMain, baseSHA) {
		if _, err := e.git(ctx, projectPath, "merge-base", "--is-ancestor", baseSHA, originMain); err != nil {
			return "", errCanonicalBaseDrift
		}
	}
	head, err := e.git(ctx, projectPath, "rev-parse", "HEAD")
	if err != nil || !validSHA(head) {
		return "", errors.New("dcp admission: canonical main HEAD is invalid")
	}
	if !strings.EqualFold(head, originMain) {
		if _, err := e.git(ctx, projectPath, "merge-base", "--is-ancestor", head, originMain); err != nil {
			return "", errCanonicalDiverged
		}
		if _, err := e.git(ctx, projectPath, "merge", "--ff-only", "origin/main"); err != nil {
			return "", fmt.Errorf("%w: %v", errCanonicalDiverged, err)
		}
	}
	return strings.ToLower(originMain), nil
}

func (e *Engine) validateMergeCompatibility(ctx context.Context, candidate mergeCandidate, head, base string) error {
	if !validSHA(head) || !validSHA(base) {
		return errors.New("dcp admission: compatibility identity is invalid")
	}
	tree, err := e.git(ctx, candidate.project.Path, "merge-tree", "--write-tree", strings.ToLower(base), strings.ToLower(head))
	fields := strings.Fields(tree)
	if err != nil || len(fields) != 1 || !validSHA(fields[0]) {
		return errors.New("dcp admission: exact head is not proven compatible with current canonical main")
	}
	return nil
}

func (e *Engine) validateGit(ctx context.Context, candidate mergeCandidate, head, base string) error {
	projectPath := candidate.project.Path
	workspacePath := candidate.session.Metadata.WorkspacePath
	base = strings.ToLower(base)
	checks := []struct {
		path string
		args []string
		want string
	}{
		{projectPath, []string{"rev-parse", "--show-toplevel"}, projectPath},
		{projectPath, []string{"branch", "--show-current"}, candidate.spec.DefaultBranch},
		{projectPath, []string{"remote"}, "origin"},
		{projectPath, []string{"remote", "get-url", "origin"}, candidate.spec.OriginURL},
		{projectPath, []string{"rev-parse", "origin/" + candidate.spec.DefaultBranch}, base},
		{projectPath, []string{"rev-parse", "HEAD"}, base},
		{projectPath, []string{"status", "--porcelain"}, ""},
		{workspacePath, []string{"rev-parse", "--show-toplevel"}, workspacePath},
		{workspacePath, []string{"branch", "--show-current"}, candidate.session.Metadata.Branch},
		{workspacePath, []string{"remote"}, "origin"},
		{workspacePath, []string{"remote", "get-url", "origin"}, candidate.spec.OriginURL},
		{workspacePath, []string{"rev-parse", "HEAD"}, strings.ToLower(head)},
		{workspacePath, []string{"status", "--porcelain"}, ""},
	}
	for _, check := range checks {
		got, err := e.git(ctx, check.path, check.args...)
		if err != nil || got != check.want {
			return errors.New("dcp admission: local repository identity is not exact and clean")
		}
	}
	common, err := e.git(ctx, workspacePath, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || !sameExactPath(common, filepath.Join(projectPath, ".git")) {
		return errors.New("dcp admission: linked worktree common git directory is foreign")
	}
	private, err := e.git(ctx, workspacePath, "rev-parse", "--path-format=absolute", "--absolute-git-dir")
	if err != nil || !sameExactPath(private, filepath.Join(projectPath, ".git", "worktrees", string(candidate.session.ID))) {
		return errors.New("dcp admission: linked worktree private git directory is foreign")
	}
	if candidate.policy {
		taskBase := strings.ToLower(candidate.session.Metadata.DiffBaseSHA)
		if !validSHA(taskBase) || candidate.session.Metadata.DiffBaseRef == "" {
			return errors.New("dcp admission: policy task creation base is unavailable")
		}
		if _, err := e.git(ctx, workspacePath, "merge-base", "--is-ancestor", taskBase, strings.ToLower(head)); err != nil {
			return errors.New("dcp admission: policy head does not descend from its creation base")
		}
		// Count only commits that are not already part of the exact canonical
		// base admitted for this head. A successor repair is rebuilt directly on
		// a later main, so creationBase..head also contains unrelated commits
		// that reached main through earlier FIFO admissions. Those commits are
		// trusted by the canonical-base proof above, not worker output for this
		// task. The canonical delta still bounds every task-owned side commit to
		// the initial worker plus the one permitted repair.
		canonicalDelta := base + ".." + strings.ToLower(head)
		countText, err := e.git(ctx, workspacePath, "rev-list", "--count", canonicalDelta)
		count, parseErr := strconv.Atoi(countText)
		if err != nil || parseErr != nil || count < 1 || count > int(1+candidate.policyTask.RepairCount) {
			return errors.New("dcp admission: policy commit lineage exceeds its bounded worker actions")
		}
		if merges, err := e.git(ctx, workspacePath, "rev-list", "--merges", canonicalDelta); err != nil || merges != "" {
			return errors.New("dcp admission: policy commit lineage contains a merge commit")
		}
	}
	return nil
}

func refreshPrompt(candidate mergeCandidate, admission domain.DCPReviewLabAdmission, baseSHA string) string {
	return fmt.Sprintf("DCP bounded admission refresh for %s: the approved head %s is behind exact origin/main %s. Fetch origin/main, rebase only the current branch %s onto that exact SHA, and abort the rebase without commit or push if any conflict or ambiguity appears. If clean, run the repository check, push the same branch to the existing PR with --force-with-lease bound to %s, create no new PR/branch/worktree, change no task scope, then stop.",
		candidate.session.DisplayName, admission.TargetSHA, baseSHA, candidate.session.Metadata.Branch, admission.TargetSHA)
}

type incidentPacket struct {
	SchemaVersion            string `json:"schemaVersion"`
	Reason                   string `json:"reason"`
	Repository               string `json:"repository"`
	AdmissionID              string `json:"admissionId"`
	LeaseID                  string `json:"leaseId"`
	Sequence                 int64  `json:"sequence"`
	SessionID                string `json:"sessionId"`
	TaskDisplayName          string `json:"taskDisplayName"`
	SourceBranch             string `json:"sourceBranch"`
	ReviewID                 string `json:"reviewId"`
	ReviewRunID              string `json:"reviewRunId"`
	PRURL                    string `json:"prUrl"`
	PRNumber                 int64  `json:"prNumber"`
	TargetSHA                string `json:"targetSha"`
	ReviewBaseSHA            string `json:"reviewBaseSha"`
	CurrentBaseSHA           string `json:"currentBaseSha"`
	ProviderMergeable        string `json:"providerMergeable"`
	ProviderMergeStateStatus string `json:"providerMergeStateStatus"`
	EvidenceDigest           string `json:"evidenceDigest"`
	RecordedAt               string `json:"recordedAt"`
}

func (e *Engine) recordIncident(ctx context.Context, admission domain.DCPReviewLabAdmission, candidate mergeCandidate, observation ports.SCMObservation, reason string) error {
	if !candidate.policy {
		if store, ok := e.store.(policyStore); ok {
			task, found, lookupErr := store.GetDCPReviewLabPolicyTaskBySession(ctx, admission.SessionID)
			if lookupErr != nil {
				return lookupErr
			}
			if found {
				candidate.policy, candidate.policyTask = true, task
				candidate.spec, _ = domain.DCPPolicyTargetForTask(task)
				candidate.session.ID, candidate.session.DisplayName = task.SessionID, TaskDisplayPrefix+task.TaskID
				candidate.pr.SourceBranch = task.SourceBranch
			}
		}
	}
	now := e.clock()
	leaseID := admission.LeaseID
	if leaseID == "" {
		leaseID = "dcp-incident-" + admission.ID
	}
	baseSHA := strings.ToLower(observation.PR.BaseSHA)
	if !validSHA(baseSHA) {
		baseSHA = admission.AdmittedBaseSHA
	}
	if !validSHA(baseSHA) {
		baseSHA = admission.ReviewBaseSHA
	}
	if candidate.spec.Repository == "" {
		candidate.spec, _ = domain.DCPPolicyTarget("dcp-review-lab", "synthetic-pr")
	}
	evidence := strings.Join([]string{candidate.spec.Repository, admission.ID, leaseID, string(admission.SessionID), candidate.session.DisplayName, candidate.pr.SourceBranch, admission.ReviewRunID,
		admission.PRURL, strconv.FormatInt(admission.PRNumber, 10), strings.ToLower(admission.TargetSHA), strings.ToLower(baseSHA),
		observation.PR.ProviderMergeable, observation.PR.ProviderMergeStateStatus, reason}, "\x00")
	digest := sha256.Sum256([]byte(evidence))
	packet, err := json.Marshal(incidentPacket{
		SchemaVersion: "dcp.review-lab.arbiter-needed/v1", Reason: reason, Repository: candidate.spec.Repository,
		AdmissionID: admission.ID, LeaseID: leaseID, Sequence: admission.Sequence, SessionID: string(admission.SessionID),
		TaskDisplayName: candidate.session.DisplayName, SourceBranch: candidate.pr.SourceBranch,
		ReviewID: admission.ReviewID, ReviewRunID: admission.ReviewRunID, PRURL: admission.PRURL, PRNumber: admission.PRNumber,
		TargetSHA: strings.ToLower(admission.TargetSHA), ReviewBaseSHA: strings.ToLower(admission.ReviewBaseSHA), CurrentBaseSHA: strings.ToLower(baseSHA),
		ProviderMergeable: observation.PR.ProviderMergeable, ProviderMergeStateStatus: observation.PR.ProviderMergeStateStatus,
		EvidenceDigest: fmt.Sprintf("%x", digest), RecordedAt: now.Format(time.RFC3339Nano),
	})
	if err != nil {
		return err
	}
	var recorded bool
	if candidate.policy {
		store, ok := e.store.(policyStore)
		if !ok {
			return errors.New("dcp admission: policy store unavailable")
		}
		recorded, err = store.RecordDCPReviewLabPolicyIncident(ctx, admission, candidate.policyTask, leaseID, baseSHA, reason, string(packet), now)
	} else {
		recorded, err = e.store.RecordDCPReviewLabIncident(ctx, admission, leaseID, baseSHA, reason, string(packet), now)
	}
	if err != nil {
		return err
	}
	if !recorded {
		return errors.New("dcp admission: exact incident transition was rejected")
	}
	if reason == "merge_conflict_or_ambiguity" && (admission.SessionID == ArbiterSessionA || admission.SessionID == ArbiterSessionB) {
		return e.reconcileStage2Arbiter(ctx)
	}
	if candidate.policy && eligibleFutureArbiterKind(reason) {
		return e.reconcileFutureArbiters(ctx)
	}
	return nil
}

func gitOutput(ctx context.Context, repo string, args ...string) (string, error) {
	argv := append([]string{"-C", repo}, args...)
	out, err := exec.CommandContext(ctx, "git", argv...).Output()
	return strings.TrimSpace(string(out)), err
}

func publicRepositoryIdentity(ctx context.Context) (string, error) {
	return publicRepositoryIdentityFor(ctx, RepositoryFullName)
}

func publicRepositoryIdentityFor(ctx context.Context, repository string) (string, error) {
	out, err := exec.CommandContext(ctx, "gh", "api", "--method", "GET", "repos/"+repository).Output()
	if err != nil {
		return "", err
	}
	var response struct {
		Repository    *string `json:"full_name"`
		Private       *bool   `json:"private"`
		DefaultBranch *string `json:"default_branch"`
		RepositoryID  *int64  `json:"id"`
		Owner         *struct {
			ID *int64 `json:"id"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return "", err
	}
	if response.Repository == nil || response.Private == nil || response.DefaultBranch == nil ||
		response.RepositoryID == nil || response.Owner == nil || response.Owner.ID == nil {
		return "", errors.New("dcp admission: provider repository identity is incomplete")
	}
	return fmt.Sprintf("%s|%t|%s|%d|%d", *response.Repository, *response.Private,
		*response.DefaultBranch, *response.RepositoryID, *response.Owner.ID), nil
}

func policyProviderIdentity(spec domain.DCPPolicyTargetSpec) string {
	return fmt.Sprintf("%s|false|%s|%d|%d", spec.Repository, spec.DefaultBranch, spec.ProviderRepositoryID, spec.ProviderOwnerID)
}

func policySpecForRepository(repository string) (domain.DCPPolicyTargetSpec, bool) {
	return domain.DCPPolicyTargetForRepository(repository)
}

func eligibleSessionID(id domain.SessionID) bool {
	value := string(id)
	return value == HistoricalSession || value == AdmissionSessionA || value == AdmissionSessionB || value == ArbiterSessionA || value == ArbiterSessionB
}

func (e *Engine) eligibleSession(ctx context.Context, id domain.SessionID) (bool, error) {
	if eligibleSessionID(id) {
		return true, nil
	}
	store, ok := e.store.(policyStore)
	if !ok {
		return false, nil
	}
	task, found, err := store.GetDCPReviewLabPolicyTaskBySession(ctx, id)
	if err != nil || !found {
		return false, err
	}
	spec, exact := domain.DCPPolicyTargetForTask(task)
	return exact && task.CardNumber >= spec.MinimumCardNumber && task.SessionID == id, nil
}

func validPolicyTaskIdentity(task domain.DCPReviewLabPolicyTask, session domain.SessionRecord, dataDir string) bool {
	spec, exact := domain.DCPPolicyTargetForTask(task)
	return exact && task.CardNumber >= spec.MinimumCardNumber && task.SessionID == session.ID &&
		string(task.SessionID) == spec.SessionPrefix+"-"+strconv.FormatInt(task.CardNumber, 10) &&
		task.WorktreePath == filepath.Join(dataDir, "worktrees", spec.Target, string(session.ID)) &&
		task.SourceBranch == "ao/"+string(session.ID)+"/root" && session.DisplayName == TaskDisplayPrefix+task.TaskID &&
		session.Metadata.Prompt == policyTaskPrompt(task)
}

func policyTaskPrompt(task domain.DCPReviewLabPolicyTask) string {
	if task.Profile == "repo-only" {
		return "DCP repo-only task " + task.TaskID + ": " + task.Prompt
	}
	return TaskPromptPrefix + task.TaskID + ": " + task.Prompt
}

func validPRURL(spec domain.DCPPolicyTargetSpec, raw string, number int) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.RawQuery == "" && u.Fragment == "" &&
		u.Path == "/"+spec.Repository+"/pull/"+strconv.Itoa(number)
}

func validCheckURL(spec domain.DCPPolicyTargetSpec, raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.RawQuery == "" && u.Fragment == "" &&
		strings.HasPrefix(u.Path, "/"+spec.Repository+"/actions/runs/")
}

func validSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func validOptionalNativeBase(sha, ref string) bool {
	return (sha == "" && ref == "") || (validSHA(sha) && ref == "origin/main")
}

func sameExactPath(a, b string) bool {
	if a == "" || b == "" || !filepath.IsAbs(a) || !filepath.IsAbs(b) || filepath.Clean(a) != a || filepath.Clean(b) != b || a != b {
		return false
	}
	resolvedA, errA := filepath.EvalSymlinks(a)
	resolvedB, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && resolvedA == resolvedB
}

func validTaskIdentity(session domain.SessionRecord) bool {
	if !strings.HasPrefix(session.DisplayName, TaskDisplayPrefix) {
		return false
	}
	taskID := strings.TrimPrefix(session.DisplayName, TaskDisplayPrefix)
	if !validTaskID(taskID) {
		return false
	}
	prefix := TaskPromptPrefix + taskID + ": "
	if !strings.HasPrefix(session.Metadata.Prompt, prefix) {
		return false
	}
	prompt := strings.TrimPrefix(session.Metadata.Prompt, prefix)
	if prompt == "" || len(prompt) > 512 || !utf8.ValidString(prompt) {
		return false
	}
	for _, r := range prompt {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validTaskID(value string) bool {
	if len(value) == 0 || len(value) > 16 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func mergeErrorCode(err error) string {
	switch {
	case errors.Is(err, ports.ErrSCMHeadChanged):
		return "head_changed"
	case errors.Is(err, ports.ErrSCMNotMergeable):
		return "not_mergeable"
	case errors.Is(err, ports.ErrSCMNotFound):
		return "not_found"
	default:
		return "provider_failed"
	}
}
