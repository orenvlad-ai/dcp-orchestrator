package dcpv2

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	dcptasksvc "github.com/aoagents/agent-orchestrator/backend/internal/service/dcptask"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

const TwinCanaryTaskID = "dcp-v2-twin-canary-v1"

type TwinSubmitInput struct {
	TaskID string
	Prompt string
}

type TwinSubmitResult struct {
	Task       domain.DCPV2Task                `json:"task"`
	Native     domain.DCPReviewLabPolicyTask   `json:"native"`
	Projection domain.DCPV2LifecycleProjection `json:"projection"`
	Duplicate  bool                            `json:"duplicate"`
}

type TwinSnapshot struct {
	Task       domain.DCPV2Task                `json:"task"`
	Native     domain.DCPReviewLabPolicyTask   `json:"native"`
	Revisions  []domain.DCPV2Revision          `json:"revisions"`
	Commands   []domain.DCPV2Command           `json:"commands"`
	Actions    []domain.DCPV2Action            `json:"actions"`
	Admissions []domain.DCPV2Admission         `json:"admissions"`
	Results    []domain.DCPV2Result            `json:"results"`
	Incidents  []domain.DCPV2Incident          `json:"incidents"`
	Events     []domain.DCPV2ExternalEvent     `json:"events"`
	Projection domain.DCPV2LifecycleProjection `json:"projection"`
}

type tokenIdentity struct{}

func (tokenIdentity) Token(kind, id string) string { return stableID(kind, id) }

// TwinService is the only activated Stage 5 target binding. It composes the
// provider-neutral v2 engine with the exact GitHub/Release Train adapter and
// the predecessor native-session service used only as a bounded model shell.
type TwinService struct {
	store   *sqlite.Store
	legacy  *dcptasksvc.Service
	adapter *TwinGitHubAdapter
	engine  *Engine
	active  bool
	now     func() time.Time
}

func NewTwinService(store *sqlite.Store, legacy *dcptasksvc.Service, adapter *TwinGitHubAdapter, epoch string, now func() time.Time) (*TwinService, error) {
	if store == nil || legacy == nil || adapter == nil || epoch == "" {
		return nil, errors.New("DCP v2 twin service dependencies are incomplete")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	active := false
	activation, activationErr := store.GetDCPV2Stage5Activation(context.Background())
	switch {
	case activationErr == nil:
		if err := validateTwinActivation(activation); err != nil {
			return nil, fmt.Errorf("DCP v2 twin activation identity: %w", err)
		}
		active = true
	case errors.Is(activationErr, sqlitestore.ErrDCPV2NotFound):
		// Source/package tests and pre-install builds keep the adapter dormant.
	default:
		return nil, fmt.Errorf("read DCP v2 twin activation: %w", activationErr)
	}
	processor := &twinProcessor{store: store, adapter: adapter, now: now}
	engine, err := New(store, processor, tokenIdentity{}, "dcp-v2-daemon", epoch, 32, now)
	if err != nil {
		return nil, err
	}
	return &TwinService{store: store, legacy: legacy, adapter: adapter, engine: engine, active: active, now: now}, nil
}

func (s *TwinService) Startup(ctx context.Context) error {
	if !s.active {
		return nil
	}
	return s.engine.Startup(ctx)
}

func (s *TwinService) SubmitTwin(ctx context.Context, in TwinSubmitInput) (TwinSubmitResult, error) {
	if !s.active {
		return TwinSubmitResult{}, apierr.Invalid("DCP_V2_NOT_ACTIVATED", "the reviewed stopped Stage 5 activation is absent", nil)
	}
	if in.TaskID != TwinCanaryTaskID {
		return TwinSubmitResult{}, apierr.Invalid("DCP_V2_TASK_ID_INVALID", "only the exact authorized twin canary identity is accepted", nil)
	}
	if strings.TrimSpace(in.Prompt) == "" || strings.ContainsAny(in.Prompt, "\x00\r\n") || len(in.Prompt) > 512 {
		return TwinSubmitResult{}, apierr.Invalid("DCP_V2_PROMPT_INVALID", "prompt must be one line and at most 512 bytes", nil)
	}
	baseSHA, err := s.adapter.ObserveMain(ctx)
	if err != nil {
		return TwinSubmitResult{}, apierr.Invalid("DCP_V2_TARGET_INVALID", "exact twin main identity failed validation", nil)
	}
	now := s.now().UTC()
	requestDigest := digestCanonical(map[string]string{"taskId": in.TaskID, "prompt": in.Prompt})
	scopeDigest := digestCanonical(map[string]any{"repository": TwinRepository, "repositoryId": TwinRepositoryID,
		"ownerId": TwinOwnerID, "base": TwinBase, "profile": TwinProfile})
	policyDigest := domain.DCPWBCIntegrationTwinPolicyDigest()
	revision := domain.DCPV2Revision{RevisionID: stableID(in.TaskID, "revision", "1", baseSHA), TaskID: in.TaskID,
		Sequence: 1, Kind: domain.DCPV2RevisionWorkInput, Repository: TwinRepository, BaseRef: TwinBase,
		BaseSHA: baseSHA, HeadRef: TwinBase, HeadSHA: baseSHA, EvidenceDigest: digestCanonical(map[string]string{"main": baseSHA}), CreatedAt: now}
	task := domain.DCPV2Task{TaskID: in.TaskID, TargetSpecVersion: TwinTargetSpec, Repository: TwinRepository,
		RepositoryID: TwinRepositoryID, OwnerID: TwinOwnerID, BaseRef: TwinBase, Profile: TwinProfile,
		RequestDigest: requestDigest, ScopeDigest: scopeDigest, PolicyDigest: policyDigest,
		InitialWorkerBudget: 1, RepairBudget: 1, MaxReadmissions: 2, CurrentRevisionID: revision.RevisionID,
		State: domain.DCPV2TaskWorkerQueued, StateRevision: 1, CreatedAt: now, UpdatedAt: now}
	command := newCommand(in.TaskID, revision.RevisionID, domain.DCPV2CommandWorkerExecute, 1,
		map[string]string{"prompt": in.Prompt, "baseSha": baseSHA}, requestDigest, now)
	action := newAction(command, domain.DCPV2ActionWorker, requestDigest, now)
	created, err := s.store.CreateDCPV2Task(ctx, task, revision, command, action)
	if errors.Is(err, sqlitestore.ErrDCPV2IdentityConflict) {
		return TwinSubmitResult{}, apierr.Conflict("DCP_V2_TASK_CONFLICT", "the task id is bound to a different immutable request", nil)
	}
	if err != nil {
		return TwinSubmitResult{}, apierr.Internal("DCP_V2_SUBMIT_FAILED", "the v2 task could not be persisted atomically")
	}
	if created {
		if err := s.engine.Drain(ctx); err != nil {
			return TwinSubmitResult{}, apierr.Internal("DCP_V2_DRAIN_FAILED", "the initial v2 command could not be durably claimed")
		}
		if err := s.armQueuedAction(ctx, in.TaskID); err != nil {
			return TwinSubmitResult{}, apierr.Internal("DCP_V2_ACTION_FENCE_FAILED", "the initial v2 Action could not be fenced")
		}
	}
	legacy, found, err := s.store.GetDCPReviewLabPolicyTaskByTaskID(ctx, in.TaskID)
	if err != nil {
		return TwinSubmitResult{}, apierr.Internal("DCP_V2_NATIVE_READ_FAILED", "the native runtime projection could not be read")
	}
	if !found {
		legacyResult, submitErr := s.legacy.ProvisionV2RuntimePolicy(ctx, dcptasksvc.PolicySubmitInput{TaskID: in.TaskID,
			Target: TwinTarget, Profile: TwinProfile, Repository: TwinRepository, Prompt: in.Prompt})
		if submitErr != nil {
			return TwinSubmitResult{}, submitErr
		}
		legacy = legacyResult.Task
	}
	snapshot, err := s.Snapshot(ctx, in.TaskID)
	if err != nil {
		return TwinSubmitResult{}, err
	}
	return TwinSubmitResult{Task: snapshot.Task, Native: legacy, Projection: snapshot.Projection, Duplicate: !created}, nil
}

func (s *TwinService) PrepareLegacyReview(ctx context.Context, task domain.DCPReviewLabPolicyTask, _ domain.DCPModelAction) error {
	if !s.active {
		return errors.New("DCP v2 twin is not activated")
	}
	if task.TaskID != TwinCanaryTaskID {
		return nil
	}
	v2task, err := s.store.GetDCPV2Task(ctx, task.TaskID)
	if err != nil || v2task.State != domain.DCPV2TaskChecksWaiting {
		return errors.Join(err, errors.New("DCP v2 is not waiting for the exact check event"))
	}
	revisions, err := s.store.ListDCPV2Revisions(ctx, task.TaskID)
	if err != nil || len(revisions) < 2 {
		return errors.Join(err, errors.New("DCP v2 reviewed Revision is unavailable"))
	}
	current := revisions[len(revisions)-1]
	facts, err := s.adapter.ObserveChecks(ctx, current.HeadRef)
	if err != nil || facts.HeadSHA != current.HeadSHA {
		return errors.Join(err, errors.New("DCP v2 exact check event drifted"))
	}
	command, err := s.activeCommand(ctx, task.TaskID)
	if err != nil || command.Kind != domain.DCPV2CommandChecksObserve {
		return errors.Join(err, errors.New("DCP v2 check Command is unavailable"))
	}
	event := domain.DCPV2ExternalEvent{DeliveryID: "github-check-" + strconv.FormatInt(facts.CheckRunID, 10),
		Provider: "github", TaskID: task.TaskID, RevisionID: v2task.CurrentRevisionID, Kind: "github/check.completed",
		ProviderSequence: facts.CheckRunID, PayloadDigest: facts.EvidenceHash, PrerequisiteDigest: command.PrerequisiteDigest,
		CreatedAt: s.now().UTC(), UpdatedAt: s.now().UTC()}
	if err := s.engine.Event(ctx, event); err != nil {
		return err
	}
	return s.armQueuedAction(ctx, task.TaskID)
}

func (s *TwinService) StartLegacyAction(ctx context.Context, task domain.DCPReviewLabPolicyTask, legacy domain.DCPModelAction) error {
	if !s.active {
		return errors.New("DCP v2 twin is not activated")
	}
	if task.TaskID != TwinCanaryTaskID {
		return nil
	}
	action, err := s.launchingAction(ctx, task.TaskID, legacy.Kind)
	if err != nil {
		return err
	}
	runtimeID := legacy.LaunchID
	if runtimeID == "" {
		runtimeID = legacy.ReviewRunID
	}
	if runtimeID == "" {
		return errors.New("DCP v2 native runtime identity is empty")
	}
	return s.store.StartDCPV2Action(ctx, action.ActionID, action.Slot, action.LaunchFence, runtimeID, s.now().UTC())
}

func (s *TwinService) CompleteLegacyWorker(ctx context.Context, task domain.DCPReviewLabPolicyTask, legacy domain.DCPModelAction, success bool) error {
	if !s.active {
		return errors.New("DCP v2 twin is not activated")
	}
	if task.TaskID != TwinCanaryTaskID {
		return nil
	}
	action, err := s.activeV2Action(ctx, task.TaskID, legacy.Kind)
	if err != nil {
		return err
	}
	if !success {
		if err := s.store.FinishDCPV2Action(ctx, action.ActionID, action.Slot, action.LaunchFence, false, "", "native_worker_failed", s.now().UTC()); err != nil {
			return err
		}
	} else {
		facts, err := s.adapter.ObserveBranch(ctx, task.SourceBranch)
		if err != nil {
			return err
		}
		if err := s.store.FinishDCPV2Action(ctx, action.ActionID, action.Slot, action.LaunchFence, true, facts.EvidenceHash, "", s.now().UTC()); err != nil {
			return err
		}
	}
	return s.modelWake(ctx, task.TaskID, legacy, "runtime/worker.completed")
}

func (s *TwinService) CompleteLegacyReview(ctx context.Context, task domain.DCPReviewLabPolicyTask, legacy domain.DCPModelAction, run domain.ReviewRun) error {
	if !s.active {
		return errors.New("DCP v2 twin is not activated")
	}
	if task.TaskID != TwinCanaryTaskID {
		return nil
	}
	action, err := s.activeV2Action(ctx, task.TaskID, legacy.Kind)
	if err != nil {
		return err
	}
	digest := reviewActionDigest(run)
	if err := s.store.FinishDCPV2Action(ctx, action.ActionID, action.Slot, action.LaunchFence, true, digest, "", s.now().UTC()); err != nil {
		return err
	}
	if err := s.modelWake(ctx, task.TaskID, legacy, "runtime/reviewer.completed"); err != nil {
		return err
	}
	return s.armQueuedAction(ctx, task.TaskID)
}

func (s *TwinService) WakeRelease(ctx context.Context, taskID, deliveryID string, runID int64, payloadDigest string) (TwinSnapshot, error) {
	if !s.active {
		return TwinSnapshot{}, apierr.Invalid("DCP_V2_NOT_ACTIVATED", "the reviewed stopped Stage 5 activation is absent", nil)
	}
	if taskID != TwinCanaryTaskID || deliveryID == "" || runID < 1 || len(payloadDigest) != 64 {
		return TwinSnapshot{}, apierr.Invalid("DCP_V2_EVENT_INVALID", "release event identity is incomplete", nil)
	}
	task, err := s.store.GetDCPV2Task(ctx, taskID)
	if err != nil || task.State != domain.DCPV2TaskMergeObserving {
		return TwinSnapshot{}, apierr.Conflict("DCP_V2_EVENT_STATE_INVALID", "task is not awaiting a release proof event", nil)
	}
	command, err := s.activeCommand(ctx, taskID)
	if err != nil || command.Kind != domain.DCPV2CommandMergeObserve {
		return TwinSnapshot{}, apierr.Conflict("DCP_V2_EVENT_COMMAND_INVALID", "release observation Command is unavailable", nil)
	}
	now := s.now().UTC()
	event := domain.DCPV2ExternalEvent{DeliveryID: deliveryID, Provider: "github", TaskID: taskID,
		RevisionID: task.CurrentRevisionID, Kind: "github/release.completed", ProviderSequence: runID,
		PayloadDigest: payloadDigest, PrerequisiteDigest: command.PrerequisiteDigest, CreatedAt: now, UpdatedAt: now}
	if err := s.engine.Event(ctx, event); err != nil {
		return TwinSnapshot{}, err
	}
	return s.Snapshot(ctx, taskID)
}

func (s *TwinService) Snapshot(ctx context.Context, taskID string) (TwinSnapshot, error) {
	if !s.active {
		return TwinSnapshot{}, apierr.Invalid("DCP_V2_NOT_ACTIVATED", "the reviewed stopped Stage 5 activation is absent", nil)
	}
	task, err := s.store.GetDCPV2Task(ctx, taskID)
	if err != nil {
		return TwinSnapshot{}, err
	}
	native, found, err := s.store.GetDCPReviewLabPolicyTaskByTaskID(ctx, taskID)
	if err != nil || !found {
		return TwinSnapshot{}, errors.Join(err, errors.New("DCP v2 native task projection is unavailable"))
	}
	revisions, _ := s.store.ListDCPV2Revisions(ctx, taskID)
	commands, _ := s.store.ListDCPV2Commands(ctx, taskID)
	actions, _ := s.store.ListDCPV2Actions(ctx, taskID)
	admissions, _ := s.store.ListDCPV2Admissions(ctx, taskID)
	results, _ := s.store.ListDCPV2Results(ctx, taskID)
	incidents, _ := s.store.ListDCPV2Incidents(ctx, taskID)
	events, _ := s.store.ListDCPV2ExternalEvents(ctx, taskID)
	var command *domain.DCPV2Command
	for i := range commands {
		if commands[i].Status == domain.DCPV2CommandPending || commands[i].Status == domain.DCPV2CommandLeased {
			command = &commands[i]
		}
	}
	var action *domain.DCPV2Action
	for i := range actions {
		if actions[i].RevisionID == task.CurrentRevisionID && (actions[i].Status == domain.DCPV2ActionQueued ||
			actions[i].Status == domain.DCPV2ActionLaunching || actions[i].Status == domain.DCPV2ActionRunning) {
			action = &actions[i]
		}
	}
	var admission *domain.DCPV2Admission
	if len(admissions) > 0 {
		admission = &admissions[len(admissions)-1]
	}
	var result *domain.DCPV2Result
	if len(results) > 0 {
		result = &results[len(results)-1]
	}
	projection := domain.ProjectDCPV2Lifecycle(task, command, action, admission, result)
	return TwinSnapshot{Task: task, Native: native, Revisions: revisions, Commands: commands, Actions: actions,
		Admissions: admissions, Results: results, Incidents: incidents, Events: events, Projection: projection}, nil
}

func validateTwinActivation(a domain.DCPV2Stage5Activation) error {
	if a.ActivationID != "dcp-v2-twin-stage5" || a.AuthorityCommit != "4143982eb054a40537d963356c209bfe8447ba31" ||
		a.TargetSpecVersion != TwinTargetSpec || a.TargetPolicyDigest != domain.DCPWBCIntegrationTwinPolicyDigest() ||
		a.Repository != TwinRepository || a.RepositoryID != TwinRepositoryID || a.OwnerID != TwinOwnerID ||
		a.BaseRef != TwinBase || a.RequiredCheck != TwinRequiredCheck || a.IssuerKind != TwinIssuerKind ||
		a.IssuerActor != TwinIssuerActor || a.IssuerEvent != TwinIssuerEvent || a.IssuerEventType != TwinDispatchEvent ||
		a.WorkflowID != TwinWorkflowID || a.Environment != TwinEnvironment || a.Service != TwinServiceName ||
		a.Adapter != TwinAdapterVersion || !validV2SHA(a.SourceCommit) || !validV2SHA(a.SourceTree) ||
		len(a.InstallReceiptSHA) != 64 || a.ActivatedAt.IsZero() {
		return errors.New("immutable activation tuple is not exact")
	}
	for _, r := range a.InstallReceiptSHA {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return errors.New("install receipt identity is malformed")
		}
	}
	return nil
}

func (s *TwinService) armQueuedAction(ctx context.Context, taskID string) error {
	actions, err := s.store.ListDCPV2Actions(ctx, taskID)
	if err != nil {
		return err
	}
	for _, action := range actions {
		if action.Status != domain.DCPV2ActionQueued {
			continue
		}
		command, err := s.store.GetDCPV2Command(ctx, action.CommandID)
		if err != nil || command.Status != domain.DCPV2CommandLeased || command.EffectFence != "" {
			return errors.Join(err, errors.New("DCP v2 model Command is not an unfenced lease"))
		}
		fence := "model:" + action.ActionID
		if err := s.store.FenceDCPV2CommandEffect(ctx, command.CommandID, command.LeaseOwner, command.LeaseEpoch, command.LeaseToken, fence, s.now().UTC()); err != nil {
			return err
		}
		claimed, err := s.store.ClaimNextDCPV2Action(ctx, fence, s.now().UTC())
		if err != nil || claimed == nil || claimed.ActionID != action.ActionID {
			return errors.Join(err, errors.New("DCP v2 model Action FIFO claim drifted"))
		}
		return nil
	}
	return nil
}

func (s *TwinService) modelWake(ctx context.Context, taskID string, legacy domain.DCPModelAction, kind string) error {
	task, err := s.store.GetDCPV2Task(ctx, taskID)
	if err != nil {
		return err
	}
	command, err := s.activeCommand(ctx, taskID)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	event := domain.DCPV2ExternalEvent{DeliveryID: kind + "/" + legacy.ID, Provider: "native-runtime", TaskID: taskID,
		RevisionID: task.CurrentRevisionID, Kind: kind, ProviderSequence: legacy.Sequence,
		PayloadDigest:      digestCanonical(map[string]string{"action": legacy.ID, "launch": legacy.LaunchID, "review": legacy.ReviewRunID}),
		PrerequisiteDigest: command.PrerequisiteDigest, CreatedAt: now, UpdatedAt: now}
	return s.engine.Event(ctx, event)
}

func (s *TwinService) activeCommand(ctx context.Context, taskID string) (domain.DCPV2Command, error) {
	commands, err := s.store.ListDCPV2Commands(ctx, taskID)
	if err != nil {
		return domain.DCPV2Command{}, err
	}
	var active []domain.DCPV2Command
	for _, command := range commands {
		if command.Status == domain.DCPV2CommandPending || command.Status == domain.DCPV2CommandLeased {
			active = append(active, command)
		}
	}
	if len(active) != 1 {
		return domain.DCPV2Command{}, fmt.Errorf("DCP v2 active Command cardinality is %d", len(active))
	}
	return active[0], nil
}

func (s *TwinService) launchingAction(ctx context.Context, taskID string, kind domain.DCPModelActionKind) (domain.DCPV2Action, error) {
	return s.v2ActionByLegacy(ctx, taskID, kind, domain.DCPV2ActionLaunching)
}

func (s *TwinService) activeV2Action(ctx context.Context, taskID string, kind domain.DCPModelActionKind) (domain.DCPV2Action, error) {
	action, err := s.v2ActionByLegacy(ctx, taskID, kind, domain.DCPV2ActionRunning)
	if err == nil {
		return action, nil
	}
	return s.v2ActionByLegacy(ctx, taskID, kind, domain.DCPV2ActionLaunching)
}

func (s *TwinService) v2ActionByLegacy(ctx context.Context, taskID string, kind domain.DCPModelActionKind, status domain.DCPV2ActionStatus) (domain.DCPV2Action, error) {
	want := domain.DCPV2ActionWorker
	switch kind {
	case domain.DCPActionReviewer:
		want = domain.DCPV2ActionReviewer
	case domain.DCPActionRepairWorker:
		want = domain.DCPV2ActionRepair
	case domain.DCPActionArbiter:
		want = domain.DCPV2ActionArbiter
	}
	actions, err := s.store.ListDCPV2Actions(ctx, taskID)
	if err != nil {
		return domain.DCPV2Action{}, err
	}
	var matched []domain.DCPV2Action
	for _, action := range actions {
		if action.Role == want && action.Status == status {
			matched = append(matched, action)
		}
	}
	if len(matched) != 1 {
		return domain.DCPV2Action{}, fmt.Errorf("DCP v2 %s Action cardinality in %s is %d", want, status, len(matched))
	}
	return matched[0], nil
}
