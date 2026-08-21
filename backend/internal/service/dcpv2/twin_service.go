package dcpv2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
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
	Projection domain.DCPV2LifecycleProjection `json:"projection"`
	Duplicate  bool                            `json:"duplicate"`
}

type TwinSnapshot struct {
	Task       domain.DCPV2Task                `json:"task"`
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

// TwinService composes the DCP-owned lifecycle with stateless model transport
// and the exact GitHub/Release Train adapter. It has no legacy policy/session
// lifecycle collaborator.
type TwinService struct {
	store     *sqlite.Store
	runner    ports.DCPV2ModelRunner
	adapter   *TwinGitHubAdapter
	processor *twinProcessor
	engine    *Engine
	active    bool
	now       func() time.Time
}

func NewTwinService(store *sqlite.Store, runner ports.DCPV2ModelRunner, adapter *TwinGitHubAdapter, epoch string, now func() time.Time) (*TwinService, error) {
	if store == nil || runner == nil || adapter == nil || epoch == "" {
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
	return &TwinService{store: store, runner: runner, adapter: adapter, processor: processor, engine: engine, active: active, now: now}, nil
}

func (s *TwinService) Startup(ctx context.Context) error {
	if !s.active {
		return nil
	}
	if err := s.reconcileDirectModels(ctx); err != nil {
		return err
	}
	if err := s.engine.Startup(ctx); err != nil {
		return err
	}
	return s.driveDirectModels(ctx)
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
		if err := s.driveDirectModels(ctx); err != nil {
			return TwinSubmitResult{}, apierr.Internal("DCP_V2_DIRECT_RUNNER_FAILED", "the initial v2 Action could not be launched through the direct runner")
		}
	}
	snapshot, err := s.Snapshot(ctx, in.TaskID)
	if err != nil {
		return TwinSubmitResult{}, err
	}
	return TwinSubmitResult{Task: snapshot.Task, Projection: snapshot.Projection, Duplicate: !created}, nil
}

func (s *TwinService) WakeRelease(ctx context.Context, taskID, deliveryID string, runID int64, payloadDigest string) (TwinSnapshot, error) {
	if !s.active {
		return TwinSnapshot{}, apierr.Invalid("DCP_V2_NOT_ACTIVATED", "the reviewed stopped Stage 5 activation is absent", nil)
	}
	if taskID != TwinCanaryTaskID || deliveryID == "" || runID < 1 || len(payloadDigest) != 64 {
		return TwinSnapshot{}, apierr.Invalid("DCP_V2_EVENT_INVALID", "release event identity is incomplete", nil)
	}
	task, err := s.store.GetDCPV2Task(ctx, taskID)
	if err != nil || (task.State != domain.DCPV2TaskMergeObserving && task.State != domain.DCPV2TaskReleaseWaiting) {
		return TwinSnapshot{}, apierr.Conflict("DCP_V2_EVENT_STATE_INVALID", "task is not awaiting a release proof event", nil)
	}
	command, err := s.activeCommand(ctx, taskID)
	if err != nil || (command.Kind != domain.DCPV2CommandMergeObserve &&
		(command.Kind != domain.DCPV2CommandReleaseDispatch || command.Status != domain.DCPV2CommandLeased || command.EffectFence == "")) {
		return TwinSnapshot{}, apierr.Conflict("DCP_V2_EVENT_COMMAND_INVALID", "release observation Command is unavailable", nil)
	}
	prerequisite := command.PrerequisiteDigest
	if command.Kind == domain.DCPV2CommandReleaseDispatch {
		admissions, listErr := s.store.ListDCPV2Admissions(ctx, taskID)
		if listErr != nil || len(admissions) == 0 || admissions[len(admissions)-1].RevisionID != task.CurrentRevisionID ||
			command.EffectFence != "release:"+admissions[len(admissions)-1].ManifestDigest {
			return TwinSnapshot{}, apierr.Conflict("DCP_V2_EVENT_COMMAND_INVALID", "fenced release Admission is unavailable", nil)
		}
		prerequisite = admissions[len(admissions)-1].ManifestDigest
	}
	now := s.now().UTC()
	event := domain.DCPV2ExternalEvent{DeliveryID: deliveryID, Provider: "github", TaskID: taskID,
		RevisionID: task.CurrentRevisionID, Kind: "github/release.completed", ProviderSequence: runID,
		PayloadDigest: payloadDigest, PrerequisiteDigest: prerequisite, CreatedAt: now, UpdatedAt: now}
	if err := s.engine.Event(ctx, event); err != nil {
		return TwinSnapshot{}, err
	}
	return s.Snapshot(ctx, taskID)
}

func (s *TwinService) WakeChecks(ctx context.Context, taskID, deliveryID string, runID int64, payloadDigest string) (TwinSnapshot, error) {
	if !s.active {
		return TwinSnapshot{}, apierr.Invalid("DCP_V2_NOT_ACTIVATED", "the reviewed stopped Stage 5 activation is absent", nil)
	}
	if taskID != TwinCanaryTaskID || deliveryID == "" || runID < 1 || !validV2Digest(payloadDigest) {
		return TwinSnapshot{}, apierr.Invalid("DCP_V2_EVENT_INVALID", "check event identity is incomplete", nil)
	}
	task, err := s.store.GetDCPV2Task(ctx, taskID)
	if err != nil || task.State != domain.DCPV2TaskChecksWaiting {
		return TwinSnapshot{}, apierr.Conflict("DCP_V2_EVENT_STATE_INVALID", "task is not awaiting an exact check event", nil)
	}
	command, err := s.activeCommand(ctx, taskID)
	if err != nil || command.Kind != domain.DCPV2CommandChecksObserve || command.RevisionID != task.CurrentRevisionID ||
		(command.Status != domain.DCPV2CommandPending && command.Status != domain.DCPV2CommandLeased) {
		return TwinSnapshot{}, apierr.Conflict("DCP_V2_EVENT_COMMAND_INVALID", "check observation Command is unavailable", nil)
	}
	now := s.now().UTC()
	event := domain.DCPV2ExternalEvent{DeliveryID: deliveryID, Provider: "github", TaskID: taskID,
		RevisionID: task.CurrentRevisionID, Kind: "github/check.completed", ProviderSequence: runID,
		PayloadDigest: payloadDigest, PrerequisiteDigest: command.PrerequisiteDigest, CreatedAt: now, UpdatedAt: now}
	if err := s.engine.Event(ctx, event); err != nil {
		return TwinSnapshot{}, err
	}
	if err := s.driveDirectModels(ctx); err != nil {
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
	revisions, revisionErr := s.store.ListDCPV2Revisions(ctx, taskID)
	commands, commandErr := s.store.ListDCPV2Commands(ctx, taskID)
	actions, actionErr := s.store.ListDCPV2Actions(ctx, taskID)
	admissions, admissionErr := s.store.ListDCPV2Admissions(ctx, taskID)
	results, resultErr := s.store.ListDCPV2Results(ctx, taskID)
	incidents, incidentErr := s.store.ListDCPV2Incidents(ctx, taskID)
	events, eventErr := s.store.ListDCPV2ExternalEvents(ctx, taskID)
	activeRuntimes, runtimeErr := s.store.ListActiveDCPV2ModelRuntimes(ctx)
	if err := errors.Join(revisionErr, commandErr, actionErr, admissionErr, resultErr, incidentErr, eventErr, runtimeErr); err != nil {
		return TwinSnapshot{}, fmt.Errorf("DCP v2 durable projection is incomplete: %w", err)
	}
	var command *domain.DCPV2Command
	for i := range commands {
		if commands[i].RevisionID == task.CurrentRevisionID && (commands[i].Status == domain.DCPV2CommandPending || commands[i].Status == domain.DCPV2CommandLeased) {
			if command != nil {
				return TwinSnapshot{}, errors.New("DCP v2 projection has multiple active Commands")
			}
			command = &commands[i]
		}
	}
	var action *domain.DCPV2Action
	var runtimeObservation *domain.DCPV2RuntimeObservation
	for i := range actions {
		if actions[i].RevisionID == task.CurrentRevisionID && (actions[i].Status == domain.DCPV2ActionQueued ||
			actions[i].Status == domain.DCPV2ActionLaunching || actions[i].Status == domain.DCPV2ActionRunning) {
			if action != nil {
				return TwinSnapshot{}, errors.New("DCP v2 projection has multiple active Actions")
			}
			action = &actions[i]
		}
	}
	var activeRuntime *domain.DCPV2ModelRuntime
	for i := range activeRuntimes {
		if activeRuntimes[i].TaskID != taskID {
			continue
		}
		if activeRuntime != nil {
			return TwinSnapshot{}, errors.New("DCP v2 projection has multiple active runtimes")
		}
		activeRuntime = &activeRuntimes[i]
	}
	if action == nil || action.Status == domain.DCPV2ActionQueued {
		if activeRuntime != nil {
			return TwinSnapshot{}, errors.New("DCP v2 inactive Action has an active runtime")
		}
	} else {
		if activeRuntime == nil || activeRuntime.ActionID != action.ActionID || activeRuntime.RevisionID != action.RevisionID ||
			activeRuntime.LaunchFence != action.LaunchFence ||
			(action.Status == domain.DCPV2ActionLaunching && (action.RuntimeID != "" || activeRuntime.State != domain.DCPV2ModelRuntimeReserved)) ||
			(action.Status == domain.DCPV2ActionRunning && (activeRuntime.RuntimeID != action.RuntimeID || activeRuntime.State != domain.DCPV2ModelRuntimeRunning)) {
			return TwinSnapshot{}, errors.New("DCP v2 active Action runtime identity is incomplete")
		}
		request, err := s.directRequestForRuntime(ctx, *activeRuntime)
		if err != nil {
			return TwinSnapshot{}, err
		}
		observation, err := s.runner.Observe(ctx, request)
		if err != nil || observation.ActionID != action.ActionID || observation.RuntimeID != activeRuntime.RuntimeID || observation.ObservedAt.IsZero() {
			return TwinSnapshot{}, errors.Join(err, errors.New("DCP v2 runtime observation identity drifted"))
		}
		runtimeObservation = &observation
	}
	var admission *domain.DCPV2Admission
	for i := range admissions {
		if admissions[i].RevisionID == task.CurrentRevisionID {
			if admission != nil {
				return TwinSnapshot{}, errors.New("DCP v2 projection has multiple current Admissions")
			}
			admission = &admissions[i]
		}
	}
	var result *domain.DCPV2Result
	if len(results) > 0 {
		result = &results[len(results)-1]
	}
	projection := domain.ProjectDCPV2LifecycleWithRuntime(task, command, action, runtimeObservation, admission, result)
	return TwinSnapshot{Task: task, Revisions: revisions, Commands: commands, Actions: actions,
		Admissions: admissions, Results: results, Incidents: incidents, Events: events, Projection: projection}, nil
}

func (s *TwinService) Projection(ctx context.Context, taskID string) (domain.DCPV2LifecycleProjection, error) {
	snapshot, err := s.Snapshot(ctx, taskID)
	return snapshot.Projection, err
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
