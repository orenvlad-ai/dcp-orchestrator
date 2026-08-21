package dcpv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type processModelRunner struct {
	runtime ports.Runtime
	agent   ports.Agent
	dataDir string
	runFile string
	now     func() time.Time
	exe     func() (string, error)
	command func(context.Context, string, ...string) (string, error)
}

func NewProcessModelRunner(runtime ports.Runtime, agent ports.Agent, dataDir, runFile string) ports.DCPV2ModelRunner {
	return &processModelRunner{runtime: runtime, agent: agent, dataDir: filepath.Clean(dataDir), runFile: filepath.Clean(runFile),
		now: func() time.Time { return time.Now().UTC() }, exe: os.Executable,
		command: func(ctx context.Context, dir string, args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
			out, err := cmd.Output()
			return strings.TrimSpace(string(out)), err
		}}
}

func (r *processModelRunner) Prepare(ctx context.Context, request ports.DCPV2ModelPrepareRequest) (ports.DCPV2ModelWorkspaceReceipt, error) {
	if r == nil || r.runtime == nil || r.agent == nil || request.TaskID == "" || request.ActionID == "" ||
		request.Repository != TwinRepository || !filepath.IsAbs(request.RepositoryPath) || !validV2SHA(request.BaseSHA) || !validV2SHA(request.HeadSHA) {
		return ports.DCPV2ModelWorkspaceReceipt{}, errors.New("DCP v2 direct workspace request is incomplete")
	}
	branch := request.HeadRef
	expectedOld := request.HeadSHA
	if request.Role == domain.DCPV2ActionWorker {
		branch = directWorkerBranch(request.TaskID)
		expectedOld = ""
	}
	if branch == "" || branch == request.BaseRef || strings.ContainsAny(branch, "\x00\r\n ~^:?*[\\") {
		return ports.DCPV2ModelWorkspaceReceipt{}, errors.New("DCP v2 direct branch identity is invalid")
	}
	worktree := filepath.Join(r.dataDir, "worktrees", "dcp-v2", request.TaskID)
	if info, err := os.Stat(worktree); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(worktree), 0o700); err != nil {
			return ports.DCPV2ModelWorkspaceReceipt{}, err
		}
		args := []string{"worktree", "add", "-b", branch, worktree, request.HeadSHA}
		if _, err := r.command(ctx, request.RepositoryPath, args...); err != nil {
			return ports.DCPV2ModelWorkspaceReceipt{}, fmt.Errorf("prepare DCP v2 worktree: %w", err)
		}
	} else if err != nil || !info.IsDir() {
		return ports.DCPV2ModelWorkspaceReceipt{}, errors.Join(err, errors.New("DCP v2 worktree path is not a directory"))
	}
	for _, check := range []struct {
		args []string
		want string
	}{
		{[]string{"status", "--porcelain"}, ""}, {[]string{"branch", "--show-current"}, branch}, {[]string{"rev-parse", "HEAD"}, request.HeadSHA},
	} {
		got, err := r.command(ctx, worktree, check.args...)
		if err != nil || !strings.EqualFold(got, check.want) {
			return ports.DCPV2ModelWorkspaceReceipt{}, errors.Join(err, errors.New("DCP v2 prepared worktree drifted"))
		}
	}
	digest := digestCanonical(map[string]string{"task": request.TaskID, "branch": branch, "worktree": worktree})
	return ports.DCPV2ModelWorkspaceReceipt{Branch: branch, Worktree: worktree, WorktreeDigest: digest, ExpectedOldHead: expectedOld}, nil
}

func directWorkerBranch(taskID string) string { return "codex/dcp-v2/" + taskID + "/worker" }

type directSupervisorResult struct {
	ActionID     string    `json:"actionId"`
	RuntimeID    string    `json:"runtimeId"`
	LaunchFence  string    `json:"launchFence"`
	Started      bool      `json:"started"`
	ExitCode     int       `json:"exitCode"`
	OutputJSON   string    `json:"outputJson"`
	OutputDigest string    `json:"outputDigest"`
	CompletedAt  time.Time `json:"completedAt"`
}

func (r *processModelRunner) artifactPaths(request ports.DCPV2ModelLaunchRequest) (string, string, string, string) {
	root := filepath.Join(r.dataDir, "runtime", "dcp-v2", request.ActionID, request.RuntimeID)
	return root, filepath.Join(root, "input.json"), filepath.Join(root, "last-message.json"), filepath.Join(root, "terminal.json")
}

func (r *processModelRunner) Launch(ctx context.Context, request ports.DCPV2ModelLaunchRequest) (ports.DCPV2ModelLaunchReceipt, error) {
	if err := validateDirectLaunchRequest(request); err != nil || r == nil || r.runtime == nil || r.agent == nil {
		return ports.DCPV2ModelLaunchReceipt{}, errors.Join(err, errors.New("DCP v2 direct runner is unavailable"))
	}
	root, inputPath, messagePath, resultPath := r.artifactPaths(request)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return ports.DCPV2ModelLaunchReceipt{}, err
	}
	input, _ := json.Marshal(request)
	if err := writeExactPrivateFile(inputPath, append(input, '\n')); err != nil {
		return ports.DCPV2ModelLaunchReceipt{}, err
	}
	if _, err := os.Lstat(resultPath); err == nil {
		return ports.DCPV2ModelLaunchReceipt{}, errors.New("DCP v2 terminal artifact exists before launch")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ports.DCPV2ModelLaunchReceipt{}, err
	}
	prompt := directRolePrompt(request, inputPath)
	model := request.Model
	if model == "codex/default" {
		model = ""
	}
	permissions := ports.PermissionModeAuto
	argv, err := r.agent.GetLaunchCommand(ctx, ports.LaunchConfig{Config: ports.AgentConfig{Model: model},
		DataDir: r.dataDir, Kind: domain.KindWorker, Permissions: permissions,
		Prompt: prompt, SessionID: request.RuntimeID, SystemPrompt: directRoleSystemPrompt(request.Role), WorkspacePath: request.Worktree})
	if err != nil {
		return ports.DCPV2ModelLaunchReceipt{}, err
	}
	if request.Role == domain.DCPV2ActionReviewer || request.Role == domain.DCPV2ActionArbiter {
		argv, err = directReadOnlyModelArgv(argv)
		if err != nil {
			return ports.DCPV2ModelLaunchReceipt{}, err
		}
	}
	marker := -1
	for i, arg := range argv {
		if arg == "--" {
			marker = i
			break
		}
	}
	if marker < 1 {
		return ports.DCPV2ModelLaunchReceipt{}, errors.New("DCP v2 Codex argv lacks a prompt separator")
	}
	extra := []string{"--json", "--output-last-message", messagePath, "-c", `model_reasoning_effort="` + request.Reasoning + `"`}
	if request.Role == domain.DCPV2ActionReviewer || request.Role == domain.DCPV2ActionArbiter {
		extra = append(extra, "-c", `approval_policy="never"`, "-c", `web_search="disabled"`, "--sandbox", "read-only")
	}
	argv = append(argv[:marker], append(extra, argv[marker:]...)...)
	executable, err := r.exe()
	if err != nil || !filepath.IsAbs(executable) {
		return ports.DCPV2ModelLaunchReceipt{}, errors.New("DCP v2 supervisor executable is unavailable")
	}
	wrapper := []string{executable, "dcp-v2-model", "supervise", "--action", request.ActionID, "--runtime", request.RuntimeID,
		"--fence", request.LaunchFence, "--role", string(request.Role), "--message-file", messagePath, "--result-file", resultPath,
		"--supervisor-data-dir", r.dataDir, "--supervisor-run-file", r.runFile, "--"}
	wrapper = append(wrapper, argv...)
	created, err := r.runtime.Create(ctx, ports.RuntimeConfig{SessionID: domain.SessionID(request.RuntimeID), WorkspacePath: request.Worktree,
		Argv: wrapper, Env: map[string]string{"DCP_V2_ACTION_ID": request.ActionID, "DCP_V2_RUNTIME_ID": request.RuntimeID}})
	if err != nil || created.ID != request.RuntimeID {
		return ports.DCPV2ModelLaunchReceipt{}, errors.Join(err, errors.New("DCP v2 runtime returned a foreign handle"))
	}
	started := r.now().UTC()
	providerDigest := digestCanonical(map[string]any{"action": request.ActionID, "runtime": request.RuntimeID, "fence": request.LaunchFence, "argv": wrapper})
	return ports.DCPV2ModelLaunchReceipt{ActionID: request.ActionID, LaunchFence: request.LaunchFence, RuntimeID: request.RuntimeID,
		ProviderRequestID: request.RuntimeID, ProviderRequestDigest: providerDigest, StartedAt: started}, nil
}

func (r *processModelRunner) Observe(ctx context.Context, request ports.DCPV2ModelLaunchRequest) (domain.DCPV2RuntimeObservation, error) {
	if err := validateDirectLaunchRequest(request); err != nil {
		return domain.DCPV2RuntimeObservation{}, err
	}
	inspector, ok := r.runtime.(ports.SupervisedProcessInspector)
	if !ok {
		return domain.DCPV2RuntimeObservation{}, errors.New("DCP v2 supervised-process inspection is unavailable")
	}
	alive, err := inspector.IsSupervisedProcessAlive(ctx, ports.RuntimeHandle{ID: request.RuntimeID}, ports.SupervisedProcessRef{
		SessionID: domain.SessionID(request.RuntimeID), LaunchID: request.RuntimeID})
	return domain.DCPV2RuntimeObservation{ActionID: request.ActionID, RuntimeID: request.RuntimeID, Alive: alive, ObservedAt: r.now().UTC()}, err
}

func (r *processModelRunner) Terminal(ctx context.Context, request ports.DCPV2ModelLaunchRequest) (domain.DCPV2ModelTerminalReceipt, bool, error) {
	if err := validateDirectLaunchRequest(request); err != nil {
		return domain.DCPV2ModelTerminalReceipt{}, false, err
	}
	_, _, _, resultPath := r.artifactPaths(request)
	data, err := readExactDirectResult(resultPath)
	if errors.Is(err, os.ErrNotExist) {
		return domain.DCPV2ModelTerminalReceipt{}, false, nil
	}
	if err != nil {
		return domain.DCPV2ModelTerminalReceipt{}, false, err
	}
	var result directSupervisorResult
	var outputObject map[string]any
	if decodeExactDirectJSON(data, &result) != nil || result.ActionID != request.ActionID || result.RuntimeID != request.RuntimeID ||
		result.LaunchFence != request.LaunchFence || result.CompletedAt.IsZero() || json.Unmarshal([]byte(result.OutputJSON), &outputObject) != nil || outputObject == nil ||
		result.OutputDigest != digestCanonical(json.RawMessage(result.OutputJSON)) {
		return domain.DCPV2ModelTerminalReceipt{}, false, errors.New("DCP v2 supervisor result identity drifted")
	}
	receipt := domain.DCPV2ModelTerminalReceipt{ReceiptID: stableID("terminal-receipt", request.ActionID, result.OutputDigest),
		ActionID: request.ActionID, CommandID: request.CommandID, TaskID: request.TaskID, RevisionID: request.RevisionID,
		RuntimeID: request.RuntimeID, LaunchFence: request.LaunchFence, OutputJSON: result.OutputJSON,
		OutputDigest: result.OutputDigest, WorktreePath: request.Worktree, WorktreeDigest: request.WorktreeDigest, CreatedAt: result.CompletedAt.UTC()}
	if result.ExitCode != 0 {
		receipt.Status, receipt.ErrorCode = domain.DCPV2ModelTerminalFailed, "model_process_failed"
		if !result.Started {
			receipt.ErrorCode = "model_process_not_started"
		}
		return receipt, true, nil
	}
	receipt.Status = domain.DCPV2ModelTerminalSucceeded
	if request.Role == domain.DCPV2ActionWorker || request.Role == domain.DCPV2ActionRepair {
		for _, check := range []struct {
			args   []string
			target *string
		}{
			{[]string{"status", "--porcelain"}, nil},
			{[]string{"branch", "--show-current"}, &receipt.HeadRef}, {[]string{"rev-parse", "HEAD"}, &receipt.HeadSHA},
			{[]string{"rev-parse", "HEAD^{tree}"}, &receipt.TreeSHA},
		} {
			value, err := r.command(ctx, request.Worktree, check.args...)
			if err != nil {
				return domain.DCPV2ModelTerminalReceipt{}, false, err
			}
			if check.target == nil {
				if value != "" {
					return domain.DCPV2ModelTerminalReceipt{}, false, errors.New("DCP v2 Worker left an uncommitted worktree")
				}
				continue
			}
			*check.target = strings.ToLower(value)
		}
		receipt.BaseSHA = request.HeadSHA
		if receipt.HeadRef != request.Branch || !validV2SHA(receipt.HeadSHA) || !validV2SHA(receipt.TreeSHA) || receipt.HeadSHA == request.HeadSHA {
			return domain.DCPV2ModelTerminalReceipt{}, false, errors.New("DCP v2 Worker produced no exact successor commit")
		}
		if _, err := r.command(ctx, request.Worktree, "merge-base", "--is-ancestor", request.HeadSHA, receipt.HeadSHA); err != nil {
			return domain.DCPV2ModelTerminalReceipt{}, false, errors.New("DCP v2 Worker successor ancestry drifted")
		}
	} else {
		for _, check := range []struct {
			args []string
			want string
		}{
			{[]string{"status", "--porcelain"}, ""},
			{[]string{"branch", "--show-current"}, request.Branch},
			{[]string{"rev-parse", "HEAD"}, request.HeadSHA},
		} {
			value, err := r.command(ctx, request.Worktree, check.args...)
			if err != nil || value != check.want {
				return domain.DCPV2ModelTerminalReceipt{}, false, errors.Join(err, errors.New("DCP v2 read-only model changed its worktree"))
			}
		}
		receipt.HeadRef, receipt.HeadSHA, receipt.BaseSHA = request.HeadRef, request.HeadSHA, request.BaseSHA
	}
	receipt.ResultDigest = digestCanonical(map[string]string{"output": receipt.OutputDigest, "head": receipt.HeadSHA, "tree": receipt.TreeSHA})
	return receipt, true, nil
}

func readExactDirectResult(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || info.Size() <= 0 || info.Size() > 64*1024 {
		return nil, errors.New("DCP v2 terminal artifact is not an exact owner-controlled bounded file")
	}
	return os.ReadFile(path)
}

func validateDirectLaunchRequest(r ports.DCPV2ModelLaunchRequest) error {
	if r.TaskID == "" || r.RevisionID == "" || r.CommandID == "" || r.ActionID == "" || r.Attempt != 1 ||
		r.Model == "" || r.Reasoning == "" || r.TokenBudget <= 0 || r.TimeBudgetSec <= 0 || len(r.InputDigest) != 64 ||
		len(r.PromptDigest) != 64 || len(r.TaskInputDigest) != 64 ||
		digestCanonical(json.RawMessage(r.TaskInputJSON)) != r.TaskInputDigest ||
		digestCanonical(json.RawMessage(r.CommandPayloadJSON)) != r.PromptDigest ||
		!exactJSONObject(r.TaskInputJSON) || !exactJSONObject(r.CommandPayloadJSON) ||
		r.Repository != TwinRepository || r.BaseRef != TwinBase || !validV2SHA(r.BaseSHA) ||
		!validV2SHA(r.HeadSHA) || r.HeadRef == "" || r.Branch == "" || !filepath.IsAbs(r.Worktree) ||
		filepath.Clean(r.Worktree) != r.Worktree || len(r.WorktreeDigest) != 64 || !validDirectDCPV2ID(r.ActionID) ||
		!validDirectDCPV2ID(r.RuntimeID) || r.LaunchFence != "model:"+r.ActionID+":"+r.RuntimeID || r.EffectFence != r.LaunchFence {
		return errors.New("DCP v2 direct launch identity is incomplete")
	}
	switch r.Role {
	case domain.DCPV2ActionWorker:
		if r.Branch != directWorkerBranch(r.TaskID) || r.ExpectedOldHead != "" {
			return errors.New("DCP v2 Worker branch fence is invalid")
		}
	case domain.DCPV2ActionReviewer, domain.DCPV2ActionRepair, domain.DCPV2ActionArbiter:
		if r.Branch != r.HeadRef || r.ExpectedOldHead != r.HeadSHA {
			return errors.New("DCP v2 exact-head role branch fence is invalid")
		}
	default:
		return errors.New("DCP v2 direct launch role is invalid")
	}
	return nil
}

func validDirectDCPV2ID(value string) bool {
	return strings.HasPrefix(value, "v2-") && validV2SHA(strings.TrimPrefix(value, "v2-"))
}

func decodeExactDirectJSON(data []byte, target any) error {
	if err := rejectDuplicateDirectJSONKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("DCP v2 JSON contains trailing data")
	}
	return nil
}

func rejectDuplicateDirectJSONKeys(data []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var value func() error
	value = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok || seen[key] {
					return errors.New("DCP v2 JSON contains a duplicate object key")
				}
				seen[key] = true
				if err := value(); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return errors.New("DCP v2 JSON object is unterminated")
			}
		case '[':
			for decoder.More() {
				if err := value(); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return errors.New("DCP v2 JSON array is unterminated")
			}
		default:
			return errors.New("DCP v2 JSON contains an invalid delimiter")
		}
		return nil
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("DCP v2 JSON contains trailing data")
	}
	return nil
}

func directReadOnlyModelArgv(argv []string) ([]string, error) {
	out := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--dangerously-bypass-approvals-and-sandbox":
			return nil, errors.New("DCP v2 read-only role attempted to bypass its sandbox")
		case "--ask-for-approval", "--add-dir", "--sandbox":
			if i+1 >= len(argv) {
				return nil, errors.New("DCP v2 read-only role received an incomplete option")
			}
			i++
			continue
		case "-c", "--config":
			if i+1 >= len(argv) {
				return nil, errors.New("DCP v2 read-only role received an incomplete config option")
			}
			if strings.HasPrefix(argv[i+1], "sandbox_workspace_write.network_access=") {
				return nil, errors.New("DCP v2 read-only role attempted to enable network")
			}
			if strings.HasPrefix(argv[i+1], "approval_policy=") || strings.HasPrefix(argv[i+1], "approvals_reviewer=") {
				i++
				continue
			}
		}
		out = append(out, argv[i])
	}
	return out, nil
}

func directRoleSystemPrompt(role domain.DCPV2ActionRole) string {
	return "You are one bounded stateless DCP v2 " + string(role) + ". The supplied JSON is complete authority. Do not create or update DCP tasks, sessions, cards, queues, provider refs, pull requests, reviews, merges, deployments, credentials, or retries. Stop after the one local role result."
}

func directRolePrompt(request ports.DCPV2ModelLaunchRequest, inputPath string) string {
	if request.Role == domain.DCPV2ActionWorker || request.Role == domain.DCPV2ActionRepair {
		return "Read the exact digest-bound TaskInputJSON and CommandPayloadJSON at " + inputPath + ". Modify only the authorized local worktree, create exactly one local commit on the named branch, do not push, and stop."
	}
	if request.Role == domain.DCPV2ActionReviewer {
		return "Read the exact digest-bound TaskInputJSON and CommandPayloadJSON at " + inputPath + ". Inspect the named exact head without mutation. Return only one JSON object shaped {\"verdict\":\"approved\"|\"changes_requested\",\"headSha\":\"" + request.HeadSHA + "\",\"findings\":[\"bounded actionable finding\"]}; findings must be empty for approved and non-empty for changes_requested. Then stop."
	}
	return "Read the exact digest-bound TaskInputJSON and CommandPayloadJSON at " + inputPath + ". Inspect the bounded technical incident without mutation. Return only one JSON object shaped {\"decision\":\"admit\"}, then stop."
}

func exactJSONObject(value string) bool {
	var object map[string]any
	return decodeExactDirectJSON([]byte(value), &object) == nil && object != nil
}

func writeExactPrivateFile(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) == string(data) {
			return nil
		}
		return errors.New("DCP v2 runner artifact identity conflict")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
