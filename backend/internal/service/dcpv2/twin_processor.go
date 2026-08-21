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
	case domain.DCPV2CommandPublication:
		return p.publishWorkerOutput(ctx, task, revision, command, fence)
	case domain.DCPV2CommandWorkerExecute, domain.DCPV2CommandRepairExecute,
		domain.DCPV2CommandReviewExecute, domain.DCPV2CommandArbiterExecute:
		return Outcome{}, errors.New("DCP v2 model Commands complete only through a direct terminal receipt")
	case domain.DCPV2CommandChecksObserve:
		return p.completeChecks(ctx, task, revision, command)
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

func (p *twinProcessor) ProcessModelTerminal(ctx context.Context, command domain.DCPV2Command, action domain.DCPV2Action, receipt domain.DCPV2ModelTerminalReceipt) (Outcome, error) {
	task, err := p.store.GetDCPV2Task(ctx, command.TaskID)
	if err != nil {
		return Outcome{}, err
	}
	revision, err := p.currentRevision(ctx, task)
	if err != nil {
		return Outcome{}, err
	}
	if receipt.ActionID != action.ActionID || receipt.CommandID != command.CommandID || receipt.TaskID != task.TaskID ||
		receipt.RevisionID != revision.RevisionID || receipt.RuntimeID != action.RuntimeID || receipt.LaunchFence != action.LaunchFence ||
		receipt.Status != domain.DCPV2ModelTerminalSucceeded || receipt.ResultDigest == "" {
		return Outcome{}, errors.New("DCP v2 direct terminal identity drifted")
	}
	switch command.Kind {
	case domain.DCPV2CommandWorkerExecute, domain.DCPV2CommandRepairExecute:
		return p.completeWorkerReceipt(task, revision, command, receipt)
	case domain.DCPV2CommandReviewExecute:
		return p.completeReviewReceipt(task, revision, command, receipt)
	case domain.DCPV2CommandArbiterExecute:
		return p.completeArbiterReceipt(task, revision, command, receipt)
	default:
		return Outcome{}, errors.New("DCP v2 terminal receipt does not bind a model Command")
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
	case domain.DCPV2CommandPublication:
		request, err := publicationRequestFor(task, revision, command)
		if err != nil {
			return Outcome{}, false, err
		}
		receipt, found, err := p.adapter.ReconcilePublication(ctx, request)
		if err != nil || !found {
			return Outcome{}, false, err
		}
		return p.publicationOutcome(task, revision, command, receipt), true, nil
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
		worktree, getErr := p.worktreeForRevision(ctx, task.TaskID, revision.RevisionID)
		if getErr != nil {
			return Outcome{}, false, getErr
		}
		if _, err := p.adapter.PublishReadmission(ctx, revision, effect, worktree); err != nil {
			return Outcome{}, false, err
		}
		return p.readmissionOutcome(task, revision, command, effect), true, nil
	default:
		return Outcome{}, false, nil
	}
}

type publicationPayload struct {
	Branch, CommitSHA, TreeSHA, BaseSHA, ExpectedOldHead, Worktree, WorktreeDigest string
}

func publicationRequestFor(task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command) (ports.DCPV2PublicationRequest, error) {
	var payload publicationPayload
	if command.Kind != domain.DCPV2CommandPublication || json.Unmarshal([]byte(command.PayloadJSON), &payload) != nil ||
		payload.Branch != revision.HeadRef || payload.CommitSHA != revision.HeadSHA || payload.BaseSHA != revision.BaseSHA ||
		!validV2SHA(payload.TreeSHA) || payload.Worktree == "" || len(payload.WorktreeDigest) != 64 {
		return ports.DCPV2PublicationRequest{}, errors.New("DCP v2 publication payload identity drifted")
	}
	return ports.DCPV2PublicationRequest{TaskID: task.TaskID, RevisionID: revision.RevisionID, CommandID: command.CommandID,
		Repository: task.Repository, BaseRef: task.BaseRef, BaseSHA: payload.BaseSHA, Branch: payload.Branch,
		CommitSHA: payload.CommitSHA, TreeSHA: payload.TreeSHA, Worktree: payload.Worktree, WorktreeDigest: payload.WorktreeDigest,
		ExpectedOldHead: payload.ExpectedOldHead, EffectFence: command.EffectFence}, nil
}

func (p *twinProcessor) publishWorkerOutput(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command, fence func(string) error) (Outcome, error) {
	request, err := publicationRequestFor(task, revision, command)
	if err != nil {
		return Outcome{}, err
	}
	request.EffectFence = "publication:" + command.PayloadDigest
	if err := fence(request.EffectFence); err != nil {
		return Outcome{}, err
	}
	receipt, err := p.adapter.Publish(ctx, request)
	if err != nil {
		return Outcome{}, errors.Join(ErrEffectReconciliationPending, err)
	}
	return p.publicationOutcome(task, revision, command, receipt), nil
}

func (p *twinProcessor) publicationOutcome(task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command, receipt ports.DCPV2PublicationReceipt) Outcome {
	now := p.now().UTC()
	next := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandChecksObserve,
		task.StateRevision+1, map[string]any{"head": receipt.CommitSHA, "pr": receipt.PRNumber}, receipt.EvidenceDigest, now)
	return Outcome{PauseDrain: true, NextTaskState: domain.DCPV2TaskChecksWaiting,
		CommandResultDigest: receipt.EvidenceDigest, NextCommand: &next}
}

func (p *twinProcessor) completeWorkerReceipt(task domain.DCPV2Task, current domain.DCPV2Revision, command domain.DCPV2Command, receipt domain.DCPV2ModelTerminalReceipt) (Outcome, error) {
	if !validV2SHA(receipt.HeadSHA) || !validV2SHA(receipt.TreeSHA) || !validV2SHA(receipt.BaseSHA) ||
		receipt.HeadRef == "" || receipt.HeadRef == task.BaseRef || receipt.WorktreePath == "" ||
		len(receipt.WorktreeDigest) != 64 || receipt.BaseSHA != current.HeadSHA {
		return Outcome{}, errors.New("DCP v2 direct Worker repository receipt drifted")
	}
	now := p.now().UTC()
	kind := domain.DCPV2RevisionWorker
	if command.Kind == domain.DCPV2CommandRepairExecute {
		kind = domain.DCPV2RevisionRepair
	}
	nextRevision := domain.DCPV2Revision{
		RevisionID: stableID(task.TaskID, "revision", strconv.FormatInt(current.Sequence+1, 10), receipt.HeadSHA),
		TaskID:     task.TaskID, Sequence: current.Sequence + 1, Kind: kind, Repository: TwinRepository,
		BaseRef: TwinBase, BaseSHA: receipt.BaseSHA, HeadRef: receipt.HeadRef, HeadSHA: receipt.HeadSHA,
		PredecessorRevisionID: current.RevisionID, CauseCommandID: command.CommandID,
		EvidenceDigest: receipt.OutputDigest, CreatedAt: now,
	}
	expectedOldHead := ""
	if command.Kind == domain.DCPV2CommandRepairExecute {
		expectedOldHead = current.HeadSHA
	}
	payload := map[string]any{"branch": receipt.HeadRef, "commitSha": receipt.HeadSHA, "treeSha": receipt.TreeSHA,
		"baseSha": receipt.BaseSHA, "expectedOldHead": expectedOldHead, "worktree": receipt.WorktreePath, "worktreeDigest": receipt.WorktreeDigest}
	next := newCommand(task.TaskID, nextRevision.RevisionID, domain.DCPV2CommandPublication,
		task.StateRevision+1, payload, receipt.OutputDigest, now)
	return Outcome{PauseDrain: true, NextTaskState: domain.DCPV2TaskChecksWaiting, CommandResultDigest: receipt.ResultDigest,
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

type directReviewResult struct {
	Verdict  string   `json:"verdict"`
	HeadSHA  string   `json:"headSha"`
	Findings []string `json:"findings"`
}

func validDirectReviewResult(result directReviewResult) bool {
	if result.Verdict == "approved" {
		return result.Findings != nil && len(result.Findings) == 0
	}
	if result.Verdict != "changes_requested" || len(result.Findings) == 0 || len(result.Findings) > 20 {
		return false
	}
	for _, finding := range result.Findings {
		if strings.TrimSpace(finding) == "" || strings.ContainsAny(finding, "\x00") || len(finding) > 1000 {
			return false
		}
	}
	return true
}

func (p *twinProcessor) completeReviewReceipt(task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command, receipt domain.DCPV2ModelTerminalReceipt) (Outcome, error) {
	var result directReviewResult
	if decodeExactDirectJSON([]byte(receipt.OutputJSON), &result) != nil || result.HeadSHA != revision.HeadSHA || !validDirectReviewResult(result) {
		return Outcome{}, errors.New("DCP v2 exact direct review result drifted")
	}
	now := p.now().UTC()
	switch result.Verdict {
	case "approved":
		next := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandAdmissionEnqueue,
			task.StateRevision+1, map[string]any{"head": revision.HeadSHA, "reviewReceiptId": receipt.ReceiptID}, receipt.OutputDigest, now)
		return Outcome{NextTaskState: domain.DCPV2TaskAdmissionWaiting, CommandResultDigest: receipt.ResultDigest, NextCommand: &next}, nil
	case "changes_requested":
		if task.RepairUsed != 0 {
			return Outcome{}, errors.New("DCP v2 task-level repair allowance is exhausted")
		}
		next := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandRepairExecute,
			task.StateRevision+1, map[string]any{"head": revision.HeadSHA, "reviewReceiptId": receipt.ReceiptID,
				"findings": result.Findings}, receipt.OutputDigest, now)
		repair := newAction(next, domain.DCPV2ActionRepair, receipt.OutputDigest, now)
		return Outcome{NextTaskState: domain.DCPV2TaskRepairQueued, RepairIncrement: true,
			CommandResultDigest: receipt.ResultDigest, NextCommand: &next, NextAction: &repair}, nil
	default:
		return Outcome{}, errors.New("DCP v2 reviewer returned a nonterminal verdict")
	}
}

func (p *twinProcessor) completeArbiterReceipt(task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command, receipt domain.DCPV2ModelTerminalReceipt) (Outcome, error) {
	var result struct {
		Decision string `json:"decision"`
	}
	if decodeExactDirectJSON([]byte(receipt.OutputJSON), &result) != nil || result.Decision != "admit" {
		return Outcome{}, errors.New("DCP v2 Arbiter returned no admissible technical decision")
	}
	now := p.now().UTC()
	next := newCommand(task.TaskID, revision.RevisionID, domain.DCPV2CommandAdmissionEnqueue,
		task.StateRevision+1, map[string]string{"arbiterReceiptId": receipt.ReceiptID}, receipt.OutputDigest, now)
	return Outcome{NextTaskState: domain.DCPV2TaskAdmissionWaiting, CommandResultDigest: receipt.ResultDigest, NextCommand: &next}, nil
}

func (p *twinProcessor) enqueueAdmission(ctx context.Context, task domain.DCPV2Task, revision domain.DCPV2Revision, command domain.DCPV2Command) (Outcome, error) {
	receipt, err := p.latestRoleReceipt(ctx, task.TaskID, revision.RevisionID, domain.DCPV2ActionReviewer)
	if err != nil {
		return Outcome{}, err
	}
	var directReview directReviewResult
	if decodeExactDirectJSON([]byte(receipt.OutputJSON), &directReview) != nil || directReview.Verdict != "approved" ||
		directReview.HeadSHA != revision.HeadSHA || !validDirectReviewResult(directReview) {
		return Outcome{}, errors.New("DCP v2 admission direct review binding drifted")
	}
	facts, err := p.adapter.ObserveChecks(ctx, revision.HeadRef)
	if err != nil || facts.HeadSHA != revision.HeadSHA || facts.MainSHA != revision.BaseSHA {
		return Outcome{}, errors.Join(err, errors.New("DCP v2 admission repository binding drifted"))
	}
	review, err := p.adapter.PublishExactDirectReview(ctx, facts, receipt)
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
		PRNumber: facts.PRNumber, HeadSHA: revision.HeadSHA, BaseSHA: revision.BaseSHA, MainSHA: facts.MainSHA,
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
	worktree, err := p.worktreeForRevision(ctx, task.TaskID, revision.RevisionID)
	if err != nil {
		return Outcome{}, err
	}
	effect, err := p.adapter.MaterializeReadmission(ctx, task, revision, payload.CurrentMainSHA, worktree)
	if err != nil {
		return Outcome{}, err
	}
	if err := fence(effect.ExternalID); err != nil {
		return Outcome{}, err
	}
	if _, err := p.adapter.PublishReadmission(ctx, revision, effect, worktree); err != nil {
		return Outcome{}, errors.Join(ErrEffectReconciliationPending, err)
	}
	return p.readmissionOutcome(task, revision, command, effect), nil
}

func (p *twinProcessor) latestRoleReceipt(ctx context.Context, taskID, revisionID string, role domain.DCPV2ActionRole) (domain.DCPV2ModelTerminalReceipt, error) {
	actions, err := p.store.ListDCPV2Actions(ctx, taskID)
	if err != nil {
		return domain.DCPV2ModelTerminalReceipt{}, err
	}
	var matched []domain.DCPV2Action
	for _, action := range actions {
		if action.RevisionID == revisionID && action.Role == role && action.Status == domain.DCPV2ActionSucceeded {
			matched = append(matched, action)
		}
	}
	if len(matched) != 1 {
		return domain.DCPV2ModelTerminalReceipt{}, errors.New("DCP v2 direct role receipt cardinality drifted")
	}
	return p.store.GetDCPV2ModelTerminalReceiptByAction(ctx, matched[0].ActionID)
}

func (p *twinProcessor) worktreeForRevision(ctx context.Context, taskID, revisionID string) (string, error) {
	revisions, err := p.store.ListDCPV2Revisions(ctx, taskID)
	if err != nil {
		return "", err
	}
	for _, revision := range revisions {
		if revision.RevisionID != revisionID || revision.CauseCommandID == "" {
			continue
		}
		action, err := p.store.GetDCPV2ActionByCommand(ctx, revision.CauseCommandID)
		if err != nil {
			return "", err
		}
		receipt, err := p.store.GetDCPV2ModelTerminalReceiptByAction(ctx, action.ActionID)
		if err == nil && receipt.WorktreePath != "" {
			return receipt.WorktreePath, nil
		}
	}
	return "", errors.New("DCP v2 direct worktree receipt is unavailable")
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

var _ ports.DCPV2Repository = (*TwinGitHubAdapter)(nil)
var _ ports.DCPV2Release = (*TwinGitHubAdapter)(nil)
var _ ports.DCPV2Deployment = (*TwinGitHubAdapter)(nil)
