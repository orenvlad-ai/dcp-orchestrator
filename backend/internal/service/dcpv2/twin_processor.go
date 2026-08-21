package dcpv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type twinProcessor struct {
	store   *sqlite.Store
	adapter *TwinGitHubAdapter
	now     func() time.Time
}

func twinResultProofDigest(kind domain.DCPV2ResultKind, external, probe string) string {
	return digestCanonical(struct {
		Kind     string `json:"kind"`
		External string `json:"external"`
		Probe    string `json:"probe,omitempty"`
	}{Kind: string(kind), External: external, Probe: probe})
}

func (p *twinProcessor) Process(ctx context.Context, command domain.DCPV2Command, action *domain.DCPV2Action, fence func(string) error) (Outcome, error) {
	task, err := p.store.GetDCPV2Task(ctx, command.TaskID)
	if err != nil {
		return Outcome{}, err
	}
	revision, err := p.currentRevision(ctx, task)
	if err != nil {
		return Outcome{}, err
	}
	switch command.Kind {
	case domain.DCPV2CommandWorkerExecute, domain.DCPV2CommandRepairExecute:
		return p.completeWorker(ctx, task, revision, command, action)
	case domain.DCPV2CommandChecksObserve:
		return p.completeChecks(ctx, task, revision, command)
	case domain.DCPV2CommandReviewExecute:
		return p.completeReview(ctx, task, revision, command, action)
	case domain.DCPV2CommandAdmissionEnqueue:
		return p.enqueueAdmission(ctx, task, revision, command)
	case domain.DCPV2CommandReadmission:
		return p.materializeReadmission(ctx, task, revision, command, fence)
	case domain.DCPV2CommandReleaseDispatch:
		return p.dispatchRelease(ctx, task, revision, command, fence)
	case domain.DCPV2CommandMergeObserve:
		return p.observeRelease(ctx, task, revision, command)
	case domain.DCPV2CommandDeploymentObserve:
		return p.observeDeployment(ctx, task, revision, command)
	case domain.DCPV2CommandTerminalVerify:
		return p.verifyTerminal(ctx, task, revision, command)
	default:
		return Outcome{}, fmt.Errorf("DCP v2 twin command %s is not activated", command.Kind)
	}
}

func (p *twinProcessor) ReconcileEffect(ctx context.Context, command domain.DCPV2Command, event *domain.DCPV2ExternalEvent) (Outcome, bool, error) {
	task, err := p.store.GetDCPV2Task(ctx, command.TaskID)
	if err != nil || task.CurrentRevisionID != command.RevisionID {
		return Outcome{}, false, errors.Join(err, errors.New("DCP v2 fenced effect Revision drifted"))
	}
	revision, err := p.currentRevision(ctx, task)
	if err != nil {
		return Outcome{}, false, err
	}
	switch command.Kind {
	case domain.DCPV2CommandReleaseDispatch:
		if event == nil || event.Kind != "github/release.completed" || event.TaskID != task.TaskID ||
			event.RevisionID != revision.RevisionID {
			return Outcome{}, false, nil
		}
		admission, err := p.currentAdmission(ctx, task)
		if err != nil || command.EffectFence != "release:"+admission.ManifestDigest || event.PrerequisiteDigest != admission.ManifestDigest {
			return Outcome{}, false, errors.Join(err, errors.New("DCP v2 fenced release Admission drifted"))
		}
		observed, err := p.adapter.ObserveRelease(ctx, task, revision, admission, event.ProviderSequence)
		if err != nil || observed.EvidenceDigest != event.PayloadDigest {
			return Outcome{}, false, errors.Join(err, errors.New("DCP v2 fenced release proof drifted"))
		}
		switch admission.Status {
		case domain.DCPV2AdmissionLeased:
			if err := p.store.DispatchDCPV2Admission(ctx, admission.AdmissionID, admission.LeaseOwner, admission.LeaseEpoch, admission.LeaseToken, p.now().UTC()); err != nil {
				return Outcome{}, false, err
			}
		case domain.DCPV2AdmissionDispatched:
		default:
			return Outcome{}, false, errors.New("DCP v2 fenced release Admission state drifted")
		}
		return p.releaseDispatchOutcome(task, revision, admission, observed.EvidenceDigest), true, nil
	case domain.DCPV2CommandReadmission:
		payload, err := readmissionPayloadFor(command)
		if err != nil || !strings.HasPrefix(command.EffectFence, "readmission:") {
			return Outcome{}, false, errors.Join(err, errors.New("DCP v2 readmission effect fence is malformed"))
		}
		newHead := strings.TrimPrefix(command.EffectFence, "readmission:")
		effect := ports.DCPV2RepositoryEffect{ExternalID: command.EffectFence, OldHeadSHA: strings.ToLower(revision.HeadSHA),
			NewHeadSHA: strings.ToLower(newHead), BaseSHA: strings.ToLower(payload.CurrentMainSHA)}
		effect.EvidenceDigest = digestCanonical(effect)
		native, found, getErr := p.store.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
		if getErr != nil || !found {
			return Outcome{}, false, errors.Join(getErr, errors.New("DCP v2 readmission native worktree is unavailable"))
		}
		if _, err := p.adapter.PublishReadmission(ctx, revision, effect, native.WorktreePath); err != nil {
			return Outcome{}, false, err
		}
		return p.readmissionOutcome(task, revision, command, effect), true, nil
	default:
		return Outcome{}, false, nil
	}
}

func (p *twinProcessor) completeWorker(ctx context.Context, task domain.DCPV2Task, current domain.DCPV2Revision, command domain.DCPV2Command, action *domain.DCPV2Action) (Outcome, error) {
	if action == nil {
		return Outcome{}, errors.New("DCP v2 worker completion lacks an Action")
	}
	legacy, found, err := p.store.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	if err != nil || !found || legacy.SourceBranch == "" || legacy.SessionID == "" {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 worker native identity is unavailable"))
	}
	facts, err := p.adapter.ObserveBranch(ctx, legacy.SourceBranch)
	if err != nil {
		return Outcome{}, err
	}
	if action.ResultDigest != facts.EvidenceHash || facts.BaseSHA != facts.MainSHA {
		return Outcome{}, errors.New("DCP v2 worker result or exact main binding drifted")
	}
	now := p.now().UTC()
	kind := domain.DCPV2RevisionWorker
	if command.Kind == domain.DCPV2CommandRepairExecute {
		kind = domain.DCPV2RevisionRepair
	}
	nextRevision := domain.DCPV2Revision{
		RevisionID: stableID(task.TaskID, "revision", strconv.FormatInt(current.Sequence+1, 10), facts.HeadSHA),
		TaskID:     task.TaskID, Sequence: current.Sequence + 1, Kind: kind, Repository: TwinRepository,
		BaseRef: TwinBase, BaseSHA: facts.BaseSHA, HeadRef: facts.HeadRef, HeadSHA: facts.HeadSHA,
		PredecessorRevisionID: current.RevisionID, CauseCommandID: command.CommandID, PRNumber: facts.PRNumber,
		EvidenceDigest: facts.EvidenceHash, CreatedAt: now,
	}
	next := newCommand(task.TaskID, nextRevision.RevisionID, domain.DCPV2CommandChecksObserve,
		task.StateRevision+1, map[string]any{"head": facts.HeadSHA, "pr": facts.PRNumber, "check": facts.CheckRunID}, facts.EvidenceHash, now)
	return Outcome{PauseDrain: true, NextTaskState: domain.DCPV2TaskChecksWaiting, CommandResultDigest: action.ResultDigest,
		NextRevision: &nextRevision, NextCommand: &next}, nil
}

func (p *twinProcessor) completeChecks(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command) (Outcome, error) {
	event, err := p.exactWake(ctx, task, command, "github/check.completed")
	if err != nil {
		return Outcome{}, err
	}
	facts, err := p.adapter.ObserveChecks(ctx, revision.HeadRef)
	if err != nil || !facts.CheckPassed || facts.HeadSHA != revision.HeadSHA || facts.PRNumber != revision.PRNumber {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 exact check observation drifted"))
	}
	now := p.now().UTC()
	next := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandReviewExecute,
		task.StateRevision+1, map[string]any{"head": revision.HeadSHA, "checkRunId": facts.CheckRunID}, facts.EvidenceHash, now)
	action := newAction(next, domain.DCPV2ActionReviewer, facts.EvidenceHash, now)
	return Outcome{NextTaskState: domain.DCPV2TaskReviewQueued, CommandResultDigest: facts.EvidenceHash,
		ExternalEventDeliveryID: event.DeliveryID, NextCommand: &next, NextAction: &action}, nil
}

func (p *twinProcessor) completeReview(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command, action *domain.DCPV2Action) (Outcome, error) {
	if action == nil {
		return Outcome{}, errors.New("DCP v2 review completion lacks an Action")
	}
	legacy, found, err := p.store.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	if err != nil || !found || legacy.ReviewRunID == "" {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 exact review run is unavailable"))
	}
	run, found, err := p.store.GetReviewRun(ctx, legacy.ReviewRunID)
	if err != nil || !found || run.TargetSHA != revision.HeadSHA || action.ResultDigest != reviewActionDigest(run) {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 exact review result drifted"))
	}
	now := p.now().UTC()
	switch run.Verdict {
	case domain.VerdictApproved:
		next := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandAdmissionEnqueue,
			task.StateRevision+1, map[string]any{"head": revision.HeadSHA, "reviewRunId": run.ID}, action.ResultDigest, now)
		return Outcome{NextTaskState: domain.DCPV2TaskAdmissionWaiting, CommandResultDigest: action.ResultDigest, NextCommand: &next}, nil
	case domain.VerdictChangesRequested:
		if task.RepairUsed != 0 {
			return Outcome{}, errors.New("DCP v2 task-level repair allowance is exhausted")
		}
		next := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandRepairExecute,
			task.StateRevision+1, map[string]any{"head": revision.HeadSHA, "reviewRunId": run.ID}, action.ResultDigest, now)
		repair := newAction(next, domain.DCPV2ActionRepair, action.ResultDigest, now)
		return Outcome{NextTaskState: domain.DCPV2TaskRepairQueued, RepairIncrement: true,
			CommandResultDigest: action.ResultDigest, NextCommand: &next, NextAction: &repair}, nil
	default:
		return Outcome{}, errors.New("DCP v2 reviewer returned a nonterminal verdict")
	}
}

func (p *twinProcessor) enqueueAdmission(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command) (Outcome, error) {
	legacy, found, err := p.store.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	if err != nil || !found || legacy.ReviewRunID == "" {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 admission lacks a native review projection"))
	}
	run, found, err := p.store.GetReviewRun(ctx, legacy.ReviewRunID)
	if err != nil || !found || run.Verdict != domain.VerdictApproved || run.TargetSHA != revision.HeadSHA {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 admission review binding drifted"))
	}
	facts, err := p.adapter.ObserveChecks(ctx, revision.HeadRef)
	if err != nil || facts.HeadSHA != revision.HeadSHA || facts.MainSHA != revision.BaseSHA {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 admission repository binding drifted"))
	}
	review, err := p.adapter.PublishExactReview(ctx, facts, run)
	if err != nil {
		return Outcome{}, err
	}
	existing, err := p.store.ListDCPV2Admissions(ctx, task.TaskID)
	if err != nil || int64(len(existing)) != task.ReadmissionCount {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 Admission generation cardinality drifted"))
	}
	for _, prior := range existing {
		if prior.RevisionID == revision.RevisionID {
			return Outcome{}, errors.New("DCP v2 current Revision already owns an Admission")
		}
	}
	now := p.now().UTC()
	admission := domain.DCPV2Admission{
		Sequence: int64(len(existing) + 1), AdmissionID: stableID(task.TaskID, "admission", revision.RevisionID),
		LineKey: TwinRepository + ":" + TwinBase, TaskID: task.TaskID, RevisionID: revision.RevisionID,
		PRNumber: revision.PRNumber, HeadSHA: revision.HeadSHA, BaseSHA: revision.BaseSHA, MainSHA: facts.MainSHA,
		RequiredCheckID: strconv.FormatInt(facts.CheckRunID, 10),
		ReviewID:        strconv.FormatInt(review.ReviewID, 10) + ":" + review.ReviewDigest,
		Status:          domain.DCPV2AdmissionWaiting, CreatedAt: now, UpdatedAt: now,
	}
	manifest, err := BuildTwinManifest(task, revision, admission)
	if err != nil {
		return Outcome{}, err
	}
	admission.ManifestDigest = manifest.ManifestDigest
	next := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandReleaseDispatch,
		task.StateRevision+1, manifest, manifest.ManifestDigest, now)
	return Outcome{NextTaskState: domain.DCPV2TaskReleaseWaiting, CommandResultDigest: digestCanonical(admission),
		NextCommand: &next, Admission: &admission}, nil
}

func (p *twinProcessor) dispatchRelease(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command, fence func(string) error) (Outcome, error) {
	admission, err := p.currentAdmission(ctx, task)
	if err != nil || (admission.Status != domain.DCPV2AdmissionWaiting && admission.Status != domain.DCPV2AdmissionLeased) {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 release has no waiting Admission"))
	}
	manifest, err := BuildTwinManifest(task, revision, admission)
	if err != nil || manifest.ManifestDigest != admission.ManifestDigest {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 release manifest drifted"))
	}
	now := p.now().UTC()
	leased := &admission
	if admission.Status == domain.DCPV2AdmissionWaiting {
		leased, err = p.store.ClaimNextDCPV2Admission(ctx, admission.LineKey, "dcp-v2-daemon", "stage5", stableID(admission.AdmissionID, "lease"), now)
		if err != nil || leased == nil || leased.AdmissionID != admission.AdmissionID {
			return Outcome{}, errors.Join(err, errors.New("DCP v2 FIFO Admission lease is unavailable"))
		}
	}
	if leased.DispatchFence == "" {
		if err := p.store.FenceDCPV2AdmissionDispatch(ctx, leased.AdmissionID, leased.LeaseOwner, leased.LeaseEpoch, leased.LeaseToken, leased.ManifestDigest, now); err != nil {
			return Outcome{}, err
		}
		leased.DispatchFence = leased.ManifestDigest
	} else if leased.DispatchFence != leased.ManifestDigest {
		return Outcome{}, errors.New("DCP v2 Admission dispatch fence drifted")
	}
	if err := fence("release:" + admission.ManifestDigest); err != nil {
		return Outcome{}, err
	}
	receipt, err := p.adapter.DispatchAdmission(ctx, task, revision, *leased, leased.ManifestDigest)
	if err != nil {
		return Outcome{}, errors.Join(ErrEffectReconciliationPending, err)
	}
	if err := p.store.DispatchDCPV2Admission(ctx, leased.AdmissionID, leased.LeaseOwner, leased.LeaseEpoch, leased.LeaseToken, now); err != nil {
		return Outcome{}, errors.Join(ErrEffectReconciliationPending, err)
	}
	return p.releaseDispatchOutcome(task, revision, admission, receipt.EvidenceDigest), nil
}

func (p *twinProcessor) releaseDispatchOutcome(task domain.DCPV2Task, revision domain.DCPV2Revision, admission domain.DCPV2Admission, evidence string) Outcome {
	next := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandMergeObserve,
		task.StateRevision+1, map[string]string{"admissionId": admission.AdmissionID}, admission.ManifestDigest, p.now().UTC())
	return Outcome{PauseDrain: true, NextTaskState: domain.DCPV2TaskMergeObserving,
		CommandResultDigest: evidence, NextCommand: &next}
}

type readmissionPayload struct {
	CurrentMainSHA string `json:"currentMainSha"`
	ProofID        string `json:"proofId"`
}

func readmissionPayloadFor(command domain.DCPV2Command) (readmissionPayload, error) {
	var payload readmissionPayload
	if command.Kind != domain.DCPV2CommandReadmission || json.Unmarshal([]byte(command.PayloadJSON), &payload) != nil ||
		!validV2SHA(payload.CurrentMainSHA) || !validV2Digest(payload.ProofID) || command.PrerequisiteDigest != payload.ProofID {
		return readmissionPayload{}, errors.New("DCP v2 readmission payload identity drifted")
	}
	return payload, nil
}

func (p *twinProcessor) materializeReadmission(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command, fence func(string) error) (Outcome, error) {
	payload, err := readmissionPayloadFor(command)
	if err != nil {
		return Outcome{}, err
	}
	native, found, err := p.store.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID)
	if err != nil || !found || native.TaskID != task.TaskID || native.SessionID == "" || native.WorktreePath == "" {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 readmission native worktree identity drifted"))
	}
	effect, err := p.adapter.MaterializeReadmission(ctx, task, revision, payload.CurrentMainSHA, native.WorktreePath)
	if err != nil {
		return Outcome{}, err
	}
	if err := fence(effect.ExternalID); err != nil {
		return Outcome{}, err
	}
	if _, err := p.adapter.PublishReadmission(ctx, revision, effect, native.WorktreePath); err != nil {
		return Outcome{}, errors.Join(ErrEffectReconciliationPending, err)
	}
	return p.readmissionOutcome(task, revision, command, effect), nil
}

func (p *twinProcessor) readmissionOutcome(task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command, effect ports.DCPV2RepositoryEffect) Outcome {
	now := p.now().UTC()
	nextRevision := domain.DCPV2Revision{
		RevisionID: stableID(task.TaskID, "revision", strconv.FormatInt(revision.Sequence+1, 10), effect.NewHeadSHA),
		TaskID:     task.TaskID, Sequence: revision.Sequence + 1, Kind: domain.DCPV2RevisionReadmission,
		Repository: revision.Repository, BaseRef: revision.BaseRef, BaseSHA: effect.BaseSHA,
		HeadRef: revision.HeadRef, HeadSHA: effect.NewHeadSHA, PredecessorRevisionID: revision.RevisionID,
		CauseCommandID: command.CommandID, PRNumber: revision.PRNumber, EvidenceDigest: effect.EvidenceDigest, CreatedAt: now,
	}
	next := newCommand(task.TaskID, nextRevision.RevisionID, domain.DCPV2CommandChecksObserve,
		task.StateRevision+1, map[string]any{"head": effect.NewHeadSHA, "pr": revision.PRNumber}, effect.EvidenceDigest, now)
	return Outcome{PauseDrain: true, NextTaskState: domain.DCPV2TaskChecksWaiting,
		CommandResultDigest: effect.EvidenceDigest, NextRevision: &nextRevision, NextCommand: &next}
}

func (p *twinProcessor) observeRelease(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command) (Outcome, error) {
	event, err := p.exactWake(ctx, task, command, "github/release.completed")
	if err != nil {
		return Outcome{}, err
	}
	admission, err := p.currentAdmission(ctx, task)
	if err != nil || admission.Status != domain.DCPV2AdmissionDispatched {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 release observation lacks a dispatched Admission"))
	}
	observed, err := p.adapter.ObserveRelease(ctx, task, revision, admission, event.ProviderSequence)
	if err != nil || observed.EvidenceDigest != event.PayloadDigest {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 release event/proof digest drifted"))
	}
	if observed.Readmission {
		now := p.now().UTC()
		next := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandReadmission,
			task.StateRevision+1, readmissionPayload{CurrentMainSHA: observed.CurrentMainSHA, ProofID: observed.ProofID}, observed.EvidenceDigest, now)
		return Outcome{NextTaskState: domain.DCPV2TaskReadmission, ReadmissionIncrement: true,
			CommandResultDigest: observed.EvidenceDigest, ExternalEventDeliveryID: event.DeliveryID, NextCommand: &next,
			CompleteAdmissionID: admission.AdmissionID, AdmissionLeaseOwner: admission.LeaseOwner,
			AdmissionLeaseEpoch: admission.LeaseEpoch, AdmissionLeaseToken: admission.LeaseToken,
			AdmissionCompletion: domain.DCPV2AdmissionReadmissionRequired}, nil
	}
	if observed.MergeSHA == "" || observed.ArtifactDigest == "" {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 release proof is incomplete"))
	}
	now := p.now().UTC()
	releaseProofDigest := twinResultProofDigest(domain.DCPV2ResultRelease, observed.EvidenceDigest, "")
	result := domain.DCPV2Result{ResultID: stableID(task.TaskID, "release-result", admission.AdmissionID),
		TaskID: task.TaskID, RevisionID: revision.RevisionID, AdmissionID: admission.AdmissionID, CommandID: command.CommandID,
		Kind: domain.DCPV2ResultRelease, Provider: observed.Provider, ProofID: observed.ProofID, RunID: observed.RunID,
		Actor: observed.Actor, ManifestDigest: observed.ManifestDigest, ProofDigest: releaseProofDigest,
		MergeSHA: observed.MergeSHA, ArtifactDigest: observed.ArtifactDigest, Verified: true, CreatedAt: now}
	next := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandDeploymentObserve,
		task.StateRevision+1, map[string]string{"releaseResultId": result.ResultID}, result.ProofDigest, now)
	return Outcome{NextTaskState: domain.DCPV2TaskDeploymentWaiting, CommandResultDigest: result.ProofDigest,
		ExternalEventDeliveryID: event.DeliveryID, NextCommand: &next, Result: &result,
		CompleteAdmissionID: admission.AdmissionID, AdmissionLeaseOwner: admission.LeaseOwner,
		AdmissionLeaseEpoch: admission.LeaseEpoch, AdmissionLeaseToken: admission.LeaseToken,
		AdmissionCompletion: domain.DCPV2AdmissionSucceeded, AdmissionResultID: result.ResultID}, nil
}

func (p *twinProcessor) observeDeployment(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command) (Outcome, error) {
	admission, err := p.currentAdmission(ctx, task)
	if err != nil || admission.Status != domain.DCPV2AdmissionSucceeded {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 deployment observation lacks a successful Admission"))
	}
	results, err := p.store.ListDCPV2Results(ctx, task.TaskID)
	if err != nil || len(results) != 1 || results[0].Kind != domain.DCPV2ResultRelease {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 deployment observation lacks one release result"))
	}
	release := results[0]
	observed, err := p.adapter.ObserveDeployment(ctx, task, revision, admission, release.MergeSHA)
	if err != nil || !observed.Succeeded {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 deployment proof is not successful"))
	}
	now := p.now().UTC()
	deploymentProofDigest := twinResultProofDigest(domain.DCPV2ResultDeployment, observed.EvidenceDigest, observed.ProbeDigest)
	result := domain.DCPV2Result{ResultID: stableID(task.TaskID, "deployment-result", admission.AdmissionID),
		TaskID: task.TaskID, RevisionID: revision.RevisionID, AdmissionID: admission.AdmissionID, CommandID: command.CommandID,
		Kind: domain.DCPV2ResultDeployment, Provider: observed.Provider, ProofID: observed.ProofID, RunID: observed.RunID,
		Actor: observed.Actor, ManifestDigest: observed.ManifestDigest, ProofDigest: deploymentProofDigest,
		MergeSHA: observed.MergeSHA, ArtifactDigest: observed.ArtifactDigest, DeployedSHA: observed.DeployedSHA,
		Environment: observed.Environment, Service: observed.Service, ProbeDigest: observed.ProbeDigest,
		Verified: true, CreatedAt: now}
	next := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandTerminalVerify,
		task.StateRevision+1, map[string]string{"deploymentResultId": result.ResultID}, result.ProofDigest, now)
	return Outcome{NextTaskState: domain.DCPV2TaskDeploymentObserve, CommandResultDigest: result.ProofDigest,
		NextCommand: &next, Result: &result}, nil
}

func (p *twinProcessor) verifyTerminal(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command) (Outcome, error) {
	results, err := p.store.ListDCPV2Results(ctx, task.TaskID)
	if err != nil {
		return Outcome{}, err
	}
	var deployment *domain.DCPV2Result
	for i := range results {
		if results[i].Kind == domain.DCPV2ResultDeployment && results[i].Verified {
			deployment = &results[i]
		}
	}
	if deployment == nil || deployment.RevisionID != revision.RevisionID || deployment.DeployedSHA != deployment.MergeSHA {
		return Outcome{}, errors.New("DCP v2 terminal deployment binding drifted")
	}
	return Outcome{NextTaskState: domain.DCPV2TaskDeployed, TerminalResultID: deployment.ResultID,
		CommandResultDigest: deployment.ProofDigest}, nil
}

func (p *twinProcessor) currentRevision(ctx context.Context, task domain.DCPV2Task) (domain.DCPV2Revision, error) {
	revisions, err := p.store.ListDCPV2Revisions(ctx, task.TaskID)
	if err != nil {
		return domain.DCPV2Revision{}, err
	}
	for _, revision := range revisions {
		if revision.RevisionID == task.CurrentRevisionID {
			return revision, nil
		}
	}
	return domain.DCPV2Revision{}, errors.New("DCP v2 current Revision is unavailable")
}

func (p *twinProcessor) currentAdmission(ctx context.Context, task domain.DCPV2Task) (domain.DCPV2Admission, error) {
	rows, err := p.store.ListDCPV2Admissions(ctx, task.TaskID)
	if err != nil {
		return domain.DCPV2Admission{}, err
	}
	var matched []domain.DCPV2Admission
	for _, row := range rows {
		if row.RevisionID == task.CurrentRevisionID {
			matched = append(matched, row)
		}
	}
	if len(matched) != 1 {
		return domain.DCPV2Admission{}, fmt.Errorf("DCP v2 current Admission cardinality is %d", len(matched))
	}
	return matched[0], nil
}

func (p *twinProcessor) exactWake(ctx context.Context, task domain.DCPV2Task, command domain.DCPV2Command, kind string) (domain.DCPV2ExternalEvent, error) {
	events, err := p.store.ListDCPV2ExternalEvents(ctx, task.TaskID)
	if err != nil {
		return domain.DCPV2ExternalEvent{}, err
	}
	var matched []domain.DCPV2ExternalEvent
	for _, event := range events {
		if event.Status == "retained" && event.Kind == kind && event.RevisionID == task.CurrentRevisionID &&
			event.PrerequisiteDigest == command.PrerequisiteDigest {
			matched = append(matched, event)
		}
	}
	if len(matched) != 1 {
		return domain.DCPV2ExternalEvent{}, fmt.Errorf("DCP v2 %s wake cardinality is %d", kind, len(matched))
	}
	return matched[0], nil
}

func newCommand(taskID, revisionID string, kind domain.DCPV2CommandKind, generation int64, payload any, prerequisite string, now time.Time) domain.DCPV2Command {
	b, _ := json.Marshal(payload)
	payloadDigest := digestCanonical(json.RawMessage(b))
	return domain.DCPV2Command{CommandID: stableID(taskID, "command", strconv.FormatInt(generation, 10), string(kind)),
		TaskID: taskID, RevisionID: revisionID, Kind: kind, PayloadJSON: string(b), PayloadDigest: payloadDigest,
		PrerequisiteDigest: prerequisite, IdempotencyKey: taskID + "/" + string(kind) + "/" + strconv.FormatInt(generation, 10),
		Status: domain.DCPV2CommandPending, CreatedAt: now, UpdatedAt: now}
}

func newAction(command domain.DCPV2Command, role domain.DCPV2ActionRole, inputDigest string, now time.Time) domain.DCPV2Action {
	return domain.DCPV2Action{ActionID: stableID(command.TaskID, "action", command.CommandID), CommandID: command.CommandID,
		TaskID: command.TaskID, RevisionID: command.RevisionID, Role: role, Model: "codex/default", Reasoning: "high",
		TokenBudget: 20000, TimeBudgetSec: 1800, InputDigest: inputDigest, Attempt: 1,
		Status: domain.DCPV2ActionQueued, CreatedAt: now, UpdatedAt: now}
}

func stableID(parts ...string) string {
	return "v2-" + digestCanonical(strings.Join(parts, "\x00"))[:40]
}

func reviewActionDigest(run domain.ReviewRun) string {
	return digestCanonical(map[string]string{"id": run.ID, "head": run.TargetSHA, "status": string(run.Status),
		"verdict": string(run.Verdict), "body": run.Body})
}

var _ ports.DCPV2Repository = (*TwinGitHubAdapter)(nil)
var _ ports.DCPV2Release = (*TwinGitHubAdapter)(nil)
var _ ports.DCPV2Deployment = (*TwinGitHubAdapter)(nil)
