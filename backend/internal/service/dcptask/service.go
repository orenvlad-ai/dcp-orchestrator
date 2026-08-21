// Package dcptask implements the bounded model-free I11 task surface.
package dcptask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	reviewcore "github.com/aoagents/agent-orchestrator/backend/internal/review"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
	sqlitestore "github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/store"
)

const (
	ApprovedTaskSchema  = "dcp.task/v1"
	ApprovedScopeSchema = "dcp.scope/v1"
	RepositorySchema    = "dcp.repository/v1"
	EventSchema         = "dcp.event/v1"
	TargetRepository    = "dcp-lab"
	TargetProjectID     = "dcp-lab"
	SourceID            = "internal-lab"
)

const (
	maxTitleBytes       = 160
	maxDescriptionBytes = 4096
	maxScopeBytes       = 4096
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// Store is the existing SQLite authority used by the I11 service.
type Store interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	SubmitDCPTask(ctx context.Context, task domain.DCPTask, event domain.DCPTaskEvent) (sqlitestore.DCPSubmitResult, error)
	GetDCPTask(ctx context.Context, taskID domain.DCPTaskID) (domain.DCPTask, error)
	ListDCPTasks(ctx context.Context, projectID string) ([]domain.DCPTask, error)
	ListDCPTaskEvents(ctx context.Context, taskID domain.DCPTaskID) ([]domain.DCPTaskEvent, error)
	ValidateDCPTaskSchema(ctx context.Context) error
}

// RepositoryValidator proves the one exact disposable target without mutation.
type RepositoryValidator interface {
	Validate(ctx context.Context, project domain.ProjectRecord) (domain.DCPRepositoryIdentity, error)
}

// ContinuationRepositoryValidator preserves the exact repository identity
// checks for an already-reserved policy card while allowing the clean local
// main ref to be a strict ancestor of its refreshed origin/main. A cohort
// member can legitimately observe that state after an earlier FIFO owner
// merges; new submissions still require Validate's exact equality.
type ContinuationRepositoryValidator interface {
	ValidateContinuation(ctx context.Context, project domain.ProjectRecord) (domain.DCPRepositoryIdentity, error)
}

type Deps struct {
	Store              Store
	Repository         RepositoryValidator
	PolicyRepository   RepositoryValidator
	PolicyWorktreeRoot string
	Now                func() time.Time
	NewID              func(prefix string) string
}

type Service struct {
	store              Store
	repository         RepositoryValidator
	policyStore        PolicyStore
	policyRepository   RepositoryValidator
	policyWorktreeRoot string
	policyRuntime      PolicyRuntime
	policyReviewer     PolicyReviewer
	policyArbiter      PolicyArbiter
	policyMu           sync.Mutex
	now                func() time.Time
	newID              func(prefix string) string
}

func New(deps Deps) *Service {
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	newID := deps.NewID
	if newID == nil {
		newID = func(prefix string) string { return prefix + uuid.NewString() }
	}
	policyStore, _ := deps.Store.(PolicyStore)
	return &Service{store: deps.Store, repository: deps.Repository, policyStore: policyStore,
		policyRepository: deps.PolicyRepository, policyWorktreeRoot: filepath.Clean(deps.PolicyWorktreeRoot), now: now, newID: newID}
}

const (
	PolicyTarget           = "dcp-review-lab"
	PolicyProfile          = "synthetic-pr"
	PolicyRepositoryName   = "orenvlad-ai/dcp-review-lab"
	RepoOnlyTarget         = "wb-browser-extension"
	RepoOnlyProfile        = "repo-only"
	RepoOnlyRepositoryName = "orenvlad-ai/wb-browser-extension"
	WBCTarget              = "wb-core"
	WBCRepositoryName      = "orenvlad-ai/wb-core"
	WBCLiveRuntimeProfile  = "live-runtime"
)

var policyTaskIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

// PolicyStore is the additive future-task/action surface on the same SQLite
// authority already used by this service.
type PolicyStore interface {
	GetProject(context.Context, string) (domain.ProjectRecord, bool, error)
	ReserveDCPReviewLabPolicyTask(context.Context, domain.DCPReviewLabPolicyTask, domain.SessionRecord, string) (sqlitestore.DCPPolicyReserveResult, error)
	GetDCPReviewLabPolicyTaskByTaskID(context.Context, string) (domain.DCPReviewLabPolicyTask, bool, error)
	GetDCPReviewLabPolicyTaskBySession(context.Context, domain.SessionID) (domain.DCPReviewLabPolicyTask, bool, error)
	ListDCPReviewLabPolicyTasks(context.Context) ([]domain.DCPReviewLabPolicyTask, error)
	UpdateDCPReviewLabPolicyTaskCAS(context.Context, domain.DCPReviewLabPolicyTask, domain.DCPReviewLabPolicyTask) (bool, error)
	GetDCPModelActionByID(context.Context, string) (domain.DCPModelAction, bool, error)
	GetDCPModelActionByIdentity(context.Context, string, domain.DCPModelActionKind, string) (domain.DCPModelAction, bool, error)
	GetActiveDCPModelActionBySession(context.Context, domain.SessionID) (domain.DCPModelAction, bool, error)
	ListDCPModelActions(context.Context) ([]domain.DCPModelAction, error)
	ListActiveDCPModelActions(context.Context) ([]domain.DCPModelAction, error)
	ClaimNextDCPModelAction(context.Context, time.Time) (domain.DCPModelAction, bool, error)
	StartDCPModelAction(context.Context, domain.DCPModelAction, string, string, time.Time) (bool, error)
	FinishDCPModelAction(context.Context, domain.DCPModelAction, domain.DCPReviewLabPolicyTask, domain.DCPModelActionStatus, string, time.Time) (bool, error)
	FinishDCPModelActionAndQueue(context.Context, domain.DCPModelAction, domain.DCPReviewLabPolicyTask, domain.DCPModelAction, time.Time) (bool, error)
	QueueDCPModelAction(context.Context, domain.DCPReviewLabPolicyTask, domain.DCPReviewLabPolicyTask, domain.DCPModelAction) (domain.DCPModelAction, bool, error)
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	ListPRsBySession(context.Context, domain.SessionID) ([]domain.PullRequest, error)
	ListChecks(context.Context, string) ([]domain.PullRequestCheck, error)
	GetReviewRun(context.Context, string) (domain.ReviewRun, bool, error)
	GetReviewRunBySessionPRAndSHA(context.Context, domain.SessionID, string, string) (domain.ReviewRun, bool, error)
	GetReviewBySession(context.Context, domain.SessionID) (domain.Review, bool, error)
}

// PolicyRuntime is the narrow stock-session manager surface used by the
// future policy. Provision is model-free; Launch is called only after a
// durable action has claimed one of three slots.
type PolicyRuntime interface {
	ProvisionDCPReviewLabPolicySession(context.Context, domain.SessionID, ports.SpawnConfig) (domain.SessionRecord, int, int, error)
	LaunchDCPReviewLabPolicyAction(context.Context, domain.SessionID, string) (sessionmanager.RestoreResult, error)
	DCPReviewLabPolicyActionAlive(context.Context, domain.SessionID, string) (bool, error)
}

type PolicyReviewer interface {
	AutoTrigger(context.Context, domain.SessionID) (reviewcore.TriggerResult, error)
}

// PolicyArbiter is the existing terminal-merge daemon's narrow ordinary-card
// incident surface. It shares the same model-action queue and never owns a
// scheduler, registry, or background loop.
type PolicyArbiter interface {
	LaunchPolicyArbiterAction(context.Context, domain.DCPModelAction) error
	FutureArbiterRepairPrompt(context.Context, domain.DCPReviewLabPolicyTask, domain.DCPModelAction) (string, error)
}

// SetPolicyRuntime late-binds collaborators that are constructed after the
// task service during daemon startup. It adds no background loop.
func (s *Service) SetPolicyRuntime(runtime PolicyRuntime, reviewer PolicyReviewer) {
	s.policyRuntime = runtime
	s.policyReviewer = reviewer
}

// SetPolicyArbiter connects ordinary-card arbiter actions to the existing policy queue.
func (s *Service) SetPolicyArbiter(arbiter PolicyArbiter) { s.policyArbiter = arbiter }

type PolicySubmitInput struct {
	TaskID     string
	Target     string
	Profile    string
	Repository string
	Prompt     string
}

type PolicySubmitResult struct {
	Task      domain.DCPReviewLabPolicyTask
	Duplicate bool
}

type policyPayload struct {
	SchemaVersion string `json:"schemaVersion"`
	TaskID        string `json:"taskId"`
	Target        string `json:"target"`
	Profile       string `json:"profile"`
	Repository    string `json:"repository"`
	Prompt        string `json:"prompt"`
}

// SubmitPolicy reserves one native identity before any model can start,
// completes its exact worktree idempotently, then performs one model-free
// queue drain. Equal replay returns the same identity; conflicting replay is
// rejected by the atomic store transaction.
func (s *Service) SubmitPolicy(ctx context.Context, in PolicySubmitInput) (PolicySubmitResult, error) {
	if s == nil || s.policyStore == nil || s.policyRepository == nil || s.policyRuntime == nil || s.policyWorktreeRoot == "." || !filepath.IsAbs(s.policyWorktreeRoot) {
		return PolicySubmitResult{}, apierr.Internal("DCP_POLICY_UNAVAILABLE", "DCP review-lab policy is unavailable")
	}
	spec, err := validatePolicySubmit(in)
	if err != nil {
		return PolicySubmitResult{}, err
	}
	if spec.UsesDCPV2TwinRelease() {
		return PolicySubmitResult{}, apierr.Invalid("DCP_POLICY_V2_AUTHORITY_REMOVED", "DCP v2 model lifecycle is owned only by its durable direct runner", nil)
	}
	payloadBytes, err := json.Marshal(policyPayload{
		SchemaVersion: spec.PolicyVersion, TaskID: in.TaskID, Target: in.Target,
		Profile: in.Profile, Repository: in.Repository, Prompt: in.Prompt,
	})
	if err != nil {
		return PolicySubmitResult{}, apierr.Internal("DCP_POLICY_CANONICALIZATION_FAILED", "DCP task could not be canonicalized")
	}
	sum := sha256.Sum256(payloadBytes)
	now := s.now().UTC()
	task := domain.DCPReviewLabPolicyTask{
		TaskID: in.TaskID, PayloadJSON: string(payloadBytes), PayloadDigest: hex.EncodeToString(sum[:]),
		Target: spec.Target, Profile: spec.Profile, Repository: spec.Repository,
		PolicyVersion: spec.PolicyVersion, Prompt: in.Prompt,
		State: domain.DCPPolicyReserved, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	var reserved sqlitestore.DCPPolicyReserveResult
	if existing, found, getErr := s.policyStore.GetDCPReviewLabPolicyTaskByTaskID(ctx, in.TaskID); getErr != nil {
		return PolicySubmitResult{}, apierr.Internal("DCP_POLICY_RESERVE_FAILED", "DCP task identity could not be read")
	} else if found {
		if !samePolicyPayload(existing, task) {
			return PolicySubmitResult{}, apierr.Conflict("DCP_POLICY_TASK_CONFLICT", "The task id is already bound to a different canonical payload", map[string]any{"taskId": in.TaskID})
		}
		if existing.State != domain.DCPPolicyReserved {
			return PolicySubmitResult{Task: existing, Duplicate: true}, nil
		}
		reserved = sqlitestore.DCPPolicyReserveResult{Task: existing}
	} else {
		if err := s.validatePolicyTarget(ctx, spec); err != nil {
			return PolicySubmitResult{}, apierr.Invalid("DCP_POLICY_TARGET_INVALID", "The exact public policy repository identity failed validation", nil)
		}
		seed := domain.SessionRecord{
			ProjectID: domain.ProjectID(spec.Target), Kind: domain.KindWorker, Harness: domain.HarnessCodex,
			DisplayName: "DCP:" + in.TaskID, Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now},
			CreatedAt: now, UpdatedAt: now,
		}
		reserved, err = s.policyStore.ReserveDCPReviewLabPolicyTask(ctx, task, seed, s.policyWorktreeRoot)
		if errors.Is(err, sqlitestore.ErrDCPPolicyConflict) {
			return PolicySubmitResult{}, apierr.Conflict("DCP_POLICY_TASK_CONFLICT", "The task id is already bound to a different canonical payload", map[string]any{"taskId": in.TaskID})
		}
		if err != nil {
			return PolicySubmitResult{}, apierr.Internal("DCP_POLICY_RESERVE_FAILED", "DCP task identity could not be reserved")
		}
	}
	task = reserved.Task
	if task.State == domain.DCPPolicyReserved {
		if err := s.validatePolicyTarget(ctx, spec); err != nil {
			return PolicySubmitResult{}, apierr.Invalid("DCP_POLICY_TARGET_INVALID", "The exact public policy repository identity failed validation", nil)
		}
		cfg := policySpawnConfig(task)
		if _, _, _, err := s.policyRuntime.ProvisionDCPReviewLabPolicySession(ctx, task.SessionID, cfg); err != nil {
			return PolicySubmitResult{}, apierr.Internal("DCP_POLICY_PROVISION_FAILED", "The reserved native DCP card could not be provisioned safely")
		}
		next := task
		next.State = domain.DCPPolicyWorkerQueued
		next.UpdatedAt = s.now().UTC()
		updated, updateErr := s.policyStore.UpdateDCPReviewLabPolicyTaskCAS(ctx, task, next)
		if updateErr != nil {
			return PolicySubmitResult{}, apierr.Internal("DCP_POLICY_STATE_FAILED", "The reserved DCP card could not enter its durable queue")
		}
		if updated {
			next.Revision = task.Revision + 1
			task = next
		} else if fresh, found, getErr := s.policyStore.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID); getErr == nil && found {
			task = fresh
		} else {
			return PolicySubmitResult{}, apierr.Internal("DCP_POLICY_STATE_FAILED", "The reserved DCP card state became ambiguous")
		}
	}
	if err := s.DrainModelActions(ctx); err != nil {
		return PolicySubmitResult{}, apierr.Internal("DCP_POLICY_DRAIN_FAILED", "The durable DCP action queue failed closed")
	}
	if fresh, found, err := s.policyStore.GetDCPReviewLabPolicyTaskByTaskID(ctx, task.TaskID); err == nil && found {
		task = fresh
	}
	return PolicySubmitResult{Task: task, Duplicate: !reserved.Created}, nil
}

func (s *Service) validatePolicyTarget(ctx context.Context, spec domain.DCPPolicyTargetSpec) error {
	project, ok, err := s.policyStore.GetProject(ctx, spec.Target)
	if err != nil || !ok || !project.ArchivedAt.IsZero() {
		return errors.Join(err, fmt.Errorf("exact %s project is unavailable", spec.Target))
	}
	identity, err := s.policyRepository.Validate(ctx, project)
	if err != nil || identity.ProjectID != spec.Target || identity.Repository != spec.Repository {
		return errors.Join(err, errors.New("exact public policy repository identity failed validation"))
	}
	return nil
}

func (s *Service) validatePolicyContinuationTarget(ctx context.Context, spec domain.DCPPolicyTargetSpec) error {
	project, ok, err := s.policyStore.GetProject(ctx, spec.Target)
	if err != nil || !ok || !project.ArchivedAt.IsZero() {
		return errors.Join(err, fmt.Errorf("exact %s project is unavailable", spec.Target))
	}
	validator, ok := s.policyRepository.(ContinuationRepositoryValidator)
	if !ok {
		return s.validatePolicyTarget(ctx, spec)
	}
	identity, err := validator.ValidateContinuation(ctx, project)
	if err != nil || identity.ProjectID != spec.Target || identity.Repository != spec.Repository {
		return errors.Join(err, errors.New("exact public policy continuation identity failed validation"))
	}
	return nil
}

func samePolicyPayload(existing, requested domain.DCPReviewLabPolicyTask) bool {
	return existing.TaskID == requested.TaskID && existing.PayloadJSON == requested.PayloadJSON && existing.PayloadDigest == requested.PayloadDigest &&
		existing.Target == requested.Target && existing.Profile == requested.Profile && existing.Repository == requested.Repository &&
		existing.PolicyVersion == requested.PolicyVersion && existing.Prompt == requested.Prompt
}

func policySpawnConfig(task domain.DCPReviewLabPolicyTask) ports.SpawnConfig {
	envelope := domain.CanonicalDCPPolicySpawnEnvelope(task)
	return ports.SpawnConfig{
		ProjectID: envelope.ProjectID, Kind: envelope.Kind, Harness: envelope.Harness,
		Branch: envelope.Branch, DisplayName: envelope.DisplayName, Prompt: envelope.Prompt,
	}
}

func policyPrompt(task domain.DCPReviewLabPolicyTask) string {
	return domain.CanonicalDCPPolicySpawnEnvelope(task).Prompt
}

func validatePolicySubmit(in PolicySubmitInput) (domain.DCPPolicyTargetSpec, error) {
	if !policyTaskIDPattern.MatchString(in.TaskID) {
		return domain.DCPPolicyTargetSpec{}, apierr.Invalid("DCP_POLICY_TASK_ID_INVALID", "taskId must be 1-64 lowercase letters, digits, or internal hyphens", nil)
	}
	spec, ok := domain.DCPPolicyTarget(in.Target, in.Profile)
	if !ok || in.Repository != spec.Repository {
		return domain.DCPPolicyTargetSpec{}, apierr.Invalid("DCP_POLICY_IDENTITY_INVALID", "target, profile, and repository must match one exact policy allowlist entry", nil)
	}
	if strings.TrimSpace(in.Prompt) == "" || len(in.Prompt) > 512 || !utf8.ValidString(in.Prompt) || strings.ContainsAny(in.Prompt, "\x00\r\n") {
		return domain.DCPPolicyTargetSpec{}, apierr.Invalid("DCP_POLICY_PROMPT_INVALID", "prompt must be one line and at most 512 UTF-8 bytes", nil)
	}
	if len(in.TaskID) > 16 && (!spec.UsesDCPV2TwinRelease() || in.TaskID != "dcp-v2-twin-canary-v1") {
		return domain.DCPPolicyTargetSpec{}, apierr.Invalid("DCP_POLICY_TASK_ID_INVALID", "long task identity is reserved for the exact DCP v2 twin canary", nil)
	}
	return spec, nil
}

// SubmitInput is the semantic command accepted from the internal loopback lab
// surface. Target is a fixed slug, never a caller-controlled path.
type SubmitInput struct {
	IdempotencyKey string
	Target         string
	ApprovedTask   domain.DCPApprovedTask
	ApprovedScope  domain.DCPApprovedScope
}

type SubmitResult struct {
	Task      domain.DCPTask
	Duplicate bool
}

func (s *Service) Submit(ctx context.Context, in SubmitInput) (SubmitResult, error) {
	if s == nil || s.store == nil || s.repository == nil {
		return SubmitResult{}, apierr.Internal("DCP_TASK_SERVICE_UNAVAILABLE", "DCP task service is unavailable")
	}
	if err := validateSubmitInput(in); err != nil {
		return SubmitResult{}, err
	}

	project, ok, err := s.store.GetProject(ctx, TargetProjectID)
	if err != nil {
		return SubmitResult{}, apierr.Internal("DCP_TARGET_LOAD_FAILED", "DCP target could not be loaded")
	}
	if !ok || !project.ArchivedAt.IsZero() {
		return SubmitResult{}, apierr.Invalid("DCP_TARGET_INVALID", "The disposable dcp-lab target is not registered", nil)
	}
	target, err := s.repository.Validate(ctx, project)
	if err != nil {
		var validation *TargetValidationError
		if errors.As(err, &validation) {
			return SubmitResult{}, apierr.Invalid("DCP_TARGET_INVALID", validation.Error(), nil)
		}
		return SubmitResult{}, apierr.Internal("DCP_TARGET_VALIDATION_FAILED", "DCP target validation failed")
	}

	approvedDigest, err := digestJSON(struct {
		Task   domain.DCPApprovedTask  `json:"task"`
		Scope  domain.DCPApprovedScope `json:"scope"`
		Target string                  `json:"target"`
	}{Task: in.ApprovedTask, Scope: in.ApprovedScope, Target: TargetRepository})
	if err != nil {
		return SubmitResult{}, apierr.Internal("DCP_TASK_CANONICALIZATION_FAILED", "DCP task could not be canonicalized")
	}

	now := s.now().UTC()
	taskID := domain.DCPTaskID(s.newID("dcp_task_"))
	eventID := s.newID("dcp_event_")
	task := domain.DCPTask{
		ID:             taskID,
		IdempotencyKey: in.IdempotencyKey,
		ApprovedTask:   in.ApprovedTask,
		ApprovedScope:  in.ApprovedScope,
		ApprovedDigest: approvedDigest,
		Target:         target,
		State:          domain.DCPTaskSubmitted,
		Revision:       1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	payloadBytes, err := json.Marshal(struct {
		SchemaVersion        string `json:"schemaVersion"`
		ApprovedDigest       string `json:"approvedDigest"`
		TargetIdentityDigest string `json:"targetIdentityDigest"`
	}{
		SchemaVersion:        "dcp.task-submitted/v1",
		ApprovedDigest:       approvedDigest,
		TargetIdentityDigest: target.IdentityDigest,
	})
	if err != nil {
		return SubmitResult{}, apierr.Internal("DCP_EVENT_CANONICALIZATION_FAILED", "DCP event could not be canonicalized")
	}
	event := domain.DCPTaskEvent{
		TaskID:         taskID,
		Sequence:       1,
		EventID:        eventID,
		SchemaVersion:  EventSchema,
		EventType:      "task.submitted",
		SourceKind:     "daemon",
		SourceID:       SourceID,
		CorrelationID:  string(taskID),
		IdempotencyKey: in.IdempotencyKey,
		ToState:        domain.DCPTaskSubmitted,
		TaskRevision:   1,
		OccurredAt:     now,
		RecordedAt:     now,
		Payload:        string(payloadBytes),
		EvidenceDigest: target.IdentityDigest,
	}
	event.IntegrityDigest, err = eventDigest(event)
	if err != nil {
		return SubmitResult{}, apierr.Internal("DCP_EVENT_CANONICALIZATION_FAILED", "DCP event could not be canonicalized")
	}

	stored, err := s.store.SubmitDCPTask(ctx, task, event)
	if errors.Is(err, sqlitestore.ErrDCPIdempotencyConflict) {
		return SubmitResult{}, apierr.Conflict(
			"DCP_IDEMPOTENCY_CONFLICT",
			"The idempotency key is already bound to a different canonical DCP task",
			map[string]any{"idempotencyKey": in.IdempotencyKey},
		)
	}
	if err != nil {
		return SubmitResult{}, apierr.Internal("DCP_TASK_SUBMIT_FAILED", "DCP task could not be submitted")
	}
	return SubmitResult{Task: stored.Task, Duplicate: !stored.Created}, nil
}

func (s *Service) Get(ctx context.Context, taskID domain.DCPTaskID) (domain.DCPTask, error) {
	if strings.TrimSpace(string(taskID)) == "" {
		return domain.DCPTask{}, apierr.Invalid("DCP_TASK_ID_REQUIRED", "DCP task id is required", nil)
	}
	task, err := s.store.GetDCPTask(ctx, taskID)
	if errors.Is(err, sqlitestore.ErrDCPTaskNotFound) {
		return domain.DCPTask{}, apierr.NotFound("DCP_TASK_NOT_FOUND", "Unknown DCP task")
	}
	if err != nil {
		return domain.DCPTask{}, apierr.Internal("DCP_TASK_LOAD_FAILED", "DCP task could not be loaded")
	}
	return task, nil
}

func (s *Service) List(ctx context.Context, projectID string) ([]domain.DCPTask, error) {
	if projectID != "" && projectID != TargetProjectID {
		return nil, apierr.Invalid("DCP_TARGET_INVALID", "Only dcp-lab DCP tasks may be listed", nil)
	}
	tasks, err := s.store.ListDCPTasks(ctx, projectID)
	if err != nil {
		return nil, apierr.Internal("DCP_TASK_LIST_FAILED", "DCP tasks could not be loaded")
	}
	return tasks, nil
}

func (s *Service) Events(ctx context.Context, taskID domain.DCPTaskID) ([]domain.DCPTaskEvent, error) {
	if _, err := s.Get(ctx, taskID); err != nil {
		return nil, err
	}
	events, err := s.store.ListDCPTaskEvents(ctx, taskID)
	if err != nil {
		return nil, apierr.Internal("DCP_TASK_EVENTS_LOAD_FAILED", "DCP task events could not be loaded")
	}
	return events, nil
}

func (s *Service) ValidateSchema(ctx context.Context) error {
	if s == nil || s.store == nil {
		return errors.New("dcp task store is unavailable")
	}
	return s.store.ValidateDCPTaskSchema(ctx)
}

func validateSubmitInput(in SubmitInput) error {
	if !idempotencyKeyPattern.MatchString(in.IdempotencyKey) {
		return apierr.Invalid("DCP_IDEMPOTENCY_KEY_INVALID", "Idempotency key must be 1-128 safe characters", nil)
	}
	if in.Target != TargetRepository {
		return apierr.Invalid("DCP_TARGET_INVALID", "Only the disposable dcp-lab target is allowed", nil)
	}
	if in.ApprovedTask.SchemaVersion != ApprovedTaskSchema {
		return apierr.Invalid("DCP_TASK_SCHEMA_INVALID", "approvedTask.schemaVersion must be dcp.task/v1", nil)
	}
	if err := validateText("approved task title", in.ApprovedTask.Title, maxTitleBytes); err != nil {
		return apierr.Invalid("DCP_TASK_INVALID", err.Error(), nil)
	}
	if err := validateText("approved task description", in.ApprovedTask.Description, maxDescriptionBytes); err != nil {
		return apierr.Invalid("DCP_TASK_INVALID", err.Error(), nil)
	}
	if in.ApprovedScope.SchemaVersion != ApprovedScopeSchema {
		return apierr.Invalid("DCP_SCOPE_SCHEMA_INVALID", "approvedScope.schemaVersion must be dcp.scope/v1", nil)
	}
	if err := validateText("approved scope statement", in.ApprovedScope.Statement, maxScopeBytes); err != nil {
		return apierr.Invalid("DCP_SCOPE_INVALID", err.Error(), nil)
	}
	return nil
}

func validateText(label, value string, maxBytes int) error {
	if value == "" || strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", label)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains a NUL byte", label)
	}
	if len([]byte(value)) > maxBytes {
		return fmt.Errorf("%s exceeds %d UTF-8 bytes", label, maxBytes)
	}
	return nil
}

func digestJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func eventDigest(event domain.DCPTaskEvent) (string, error) {
	return digestJSON(struct {
		TaskID         domain.DCPTaskID     `json:"taskId"`
		Sequence       int64                `json:"sequence"`
		EventID        string               `json:"eventId"`
		SchemaVersion  string               `json:"schemaVersion"`
		EventType      string               `json:"eventType"`
		SourceKind     string               `json:"sourceKind"`
		SourceID       string               `json:"sourceId"`
		CorrelationID  string               `json:"correlationId"`
		CausationID    string               `json:"causationId,omitempty"`
		IdempotencyKey string               `json:"idempotencyKey"`
		FromState      *domain.DCPTaskState `json:"fromState,omitempty"`
		ToState        domain.DCPTaskState  `json:"toState"`
		TaskRevision   int64                `json:"taskRevision"`
		OccurredAt     time.Time            `json:"occurredAt"`
		RecordedAt     time.Time            `json:"recordedAt"`
		Payload        string               `json:"payload"`
		EvidenceDigest string               `json:"evidenceDigest"`
	}{
		TaskID: event.TaskID, Sequence: event.Sequence, EventID: event.EventID,
		SchemaVersion: event.SchemaVersion, EventType: event.EventType,
		SourceKind: event.SourceKind, SourceID: event.SourceID,
		CorrelationID: event.CorrelationID, CausationID: event.CausationID,
		IdempotencyKey: event.IdempotencyKey, FromState: event.FromState,
		ToState: event.ToState, TaskRevision: event.TaskRevision,
		OccurredAt: event.OccurredAt, RecordedAt: event.RecordedAt,
		Payload: event.Payload, EvidenceDigest: event.EvidenceDigest,
	})
}
