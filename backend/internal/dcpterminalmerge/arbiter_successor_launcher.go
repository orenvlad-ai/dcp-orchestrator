package dcpterminalmerge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type ArbiterSuccessorLauncher interface {
	PreflightSuccessor(context.Context, domain.DCPReleaseArbiterIncident, domain.DCPReleaseArbiterSuccessorAttempt) error
	LaunchSuccessor(context.Context, domain.DCPReleaseArbiterIncident, domain.DCPReleaseArbiterSuccessorAttempt) error
	SuccessorProcessAlive(context.Context, domain.DCPReleaseArbiterSuccessorAttempt) (bool, error)
	SuccessorResultPath(domain.DCPReleaseArbiterSuccessorAttempt) (string, error)
}

func (l *arbiterLauncher) successorArtifacts(attempt domain.DCPReleaseArbiterSuccessorAttempt) (arbiterArtifacts, error) {
	if l == nil || !filepath.IsAbs(l.dataDir) || filepath.Clean(l.dataDir) != l.dataDir ||
		!filepath.IsAbs(l.runFile) || filepath.Clean(l.runFile) != l.runFile ||
		attempt.AttemptID != ArbiterSuccessorAttemptID || attempt.AttemptIdentityDigest != ArbiterSuccessorAttemptDigest ||
		attempt.RuntimeHandleID != ArbiterSuccessorRuntimeHandle || attempt.LaunchID != attempt.AttemptID {
		return arbiterArtifacts{}, errors.New("dcp arbiter successor: launcher identity is invalid")
	}
	directory := filepath.Join(l.dataDir, "runtime", "dcp-arbiter-successor", attempt.AttemptID)
	return arbiterArtifacts{
		directory: directory,
		input:     filepath.Join(directory, "input.json"),
		schema:    filepath.Join(directory, "schema.json"),
		result:    filepath.Join(directory, "result.json"),
	}, nil
}

func (l *arbiterLauncher) PreflightSuccessor(ctx context.Context, incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt) error {
	if l.runtime == nil || l.lookPath == nil || l.command == nil || l.executable == nil {
		return errors.New("dcp arbiter successor: launcher dependencies are unavailable")
	}
	if !exactSuccessorAuthorization(attempt, incident) || attempt.Status != domain.DCPArbiterSuccessorRequested ||
		attempt.ModelCallCount != 0 || !validDigest(attempt.InputDigest) || attempt.InputJSON == "" {
		return errors.New("dcp arbiter successor: requested action identity is invalid")
	}
	if err := verifyOriginalArbiterArtifacts(l.dataDir, incident, attempt); err != nil {
		return err
	}
	artifacts, err := l.successorArtifacts(attempt)
	if err != nil {
		return err
	}
	if err := ensureExactDirectory(artifacts.directory); err != nil {
		return err
	}
	schema, err := ArbiterSuccessorDecisionJSONSchema(incident, attempt)
	if err != nil {
		return err
	}
	if err := ensureExactArtifact(artifacts.input, append([]byte(attempt.InputJSON), '\n')); err != nil {
		return fmt.Errorf("dcp arbiter successor: input artifact: %w", err)
	}
	if err := ensureExactArtifact(artifacts.schema, append(schema, '\n')); err != nil {
		return fmt.Errorf("dcp arbiter successor: schema artifact: %w", err)
	}
	if _, err := os.Lstat(artifacts.result); err == nil {
		return errors.New("dcp arbiter successor: result exists before the call fence")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("dcp arbiter successor: inspect result artifact: %w", err)
	}
	codex, err := l.lookPath("codex")
	if err != nil || !filepath.IsAbs(codex) {
		return errors.New("dcp arbiter successor: exact installed Codex binary is unavailable")
	}
	version, err := l.command(ctx, codex, "--version")
	if err != nil || strings.TrimSpace(string(version)) != arbiterCodexVersion {
		return errors.New("dcp arbiter successor: installed Codex version is not exact")
	}
	missingSchema := filepath.Join(artifacts.directory, "preflight-schema-must-not-exist.json")
	if _, err := os.Lstat(missingSchema); err == nil {
		return errors.New("dcp arbiter successor: strict-config preflight sentinel already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("dcp arbiter successor: inspect strict-config preflight sentinel: %w", err)
	}
	probe := append(arbiterCodexBaseArgs(), "--output-schema", missingSchema, "--", "DCP model-free strict successor configuration preflight")
	output, probeErr := l.command(ctx, codex, probe...)
	expected := "Failed to read output schema file " + missingSchema + ":"
	if probeErr == nil || !strings.Contains(string(output), expected) {
		return fmt.Errorf("dcp arbiter successor: installed Codex cannot strictly parse exact argv: %w: %s", probeErr, strings.TrimSpace(string(output)))
	}
	return nil
}

func (l *arbiterLauncher) LaunchSuccessor(ctx context.Context, incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt) error {
	if !exactSuccessorAuthorization(attempt, incident) || attempt.Status != domain.DCPArbiterSuccessorRunning || attempt.ModelCallCount != 1 {
		return errors.New("dcp arbiter successor: fenced launch identity is invalid")
	}
	artifacts, err := l.successorArtifacts(attempt)
	if err != nil {
		return err
	}
	codex, err := l.lookPath("codex")
	if err != nil || !filepath.IsAbs(codex) {
		return errors.New("dcp arbiter successor: exact installed Codex binary disappeared after preflight")
	}
	executable, err := l.executable()
	if err != nil || !filepath.IsAbs(executable) {
		return errors.New("dcp arbiter successor: exact supervisor executable is unavailable")
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("dcp arbiter successor: supervisor executable is not an executable regular file")
	}
	prompt := "You are the bounded DCP global release arbiter successor. The following JSON is the complete current authoritative incident envelope. Return exactly one JSON object matching the supplied schema. Select assign_recovery only when the evidence proves the sole same-worker conflict-repair path remains inside the approved task; otherwise select one truthful safe_stop. Worker/reviewer call limits are trusted daemon policy and are not your decision. Do not propose or perform mutations, merges, labels, scope changes, risk acceptance, HumanGate, or owner acceptance. Current input:\n" + attempt.InputJSON
	child := append(arbiterCodexBaseArgs(),
		"--cd", artifacts.directory, "--output-schema", artifacts.schema,
		"--output-last-message", artifacts.result, "--", prompt)
	argv := []string{executable, "arbiter", "supervise",
		"--handle", ArbiterSuccessorRuntimeHandle,
		"--incident", attempt.AttemptID, "--identity-digest", attempt.AttemptIdentityDigest,
		"--input-digest", attempt.InputDigest, "--result-file", artifacts.result,
		"--result-schema", artifacts.schema, "--supervisor-data-dir", l.dataDir,
		"--supervisor-run-file", l.runFile, "--"}
	argv = append(argv, codex)
	argv = append(argv, child...)
	handle := ports.RuntimeHandle{ID: ArbiterSuccessorRuntimeHandle}
	if err := l.runtime.Destroy(ctx, handle); err != nil {
		return fmt.Errorf("dcp arbiter successor: replace foreign or stale terminal: %w", err)
	}
	created, err := l.runtime.Create(ctx, ports.RuntimeConfig{
		SessionID: domain.SessionID(ArbiterSuccessorRuntimeHandle), WorkspacePath: artifacts.directory, Argv: argv,
		Env: map[string]string{"DCP_ARBITER_ATTEMPT_ID": attempt.AttemptID},
	})
	if err != nil {
		return fmt.Errorf("dcp arbiter successor: runtime create: %w", err)
	}
	if created.ID != ArbiterSuccessorRuntimeHandle {
		return errors.New("dcp arbiter successor: runtime returned a foreign stable handle")
	}
	return nil
}

func (l *arbiterLauncher) SuccessorProcessAlive(ctx context.Context, attempt domain.DCPReleaseArbiterSuccessorAttempt) (bool, error) {
	inspector, ok := l.runtime.(ports.SupervisedProcessInspector)
	if !ok {
		return false, errors.New("dcp arbiter successor: exact process inspection is unavailable")
	}
	return inspector.IsSupervisedProcessAlive(ctx, ports.RuntimeHandle{ID: attempt.RuntimeHandleID}, ports.SupervisedProcessRef{
		SessionID: domain.SessionID(attempt.RuntimeHandleID), LaunchID: attempt.LaunchID,
	})
}

func (l *arbiterLauncher) SuccessorResultPath(attempt domain.DCPReleaseArbiterSuccessorAttempt) (string, error) {
	artifacts, err := l.successorArtifacts(attempt)
	if err != nil {
		return "", err
	}
	return artifacts.result, nil
}

func verifyOriginalArbiterArtifacts(dataDir string, incident domain.DCPReleaseArbiterIncident, attempt domain.DCPReleaseArbiterSuccessorAttempt) error {
	if !exactSuccessorAuthorization(attempt, incident) {
		return errors.New("dcp arbiter successor: original attempt identity is invalid")
	}
	root := filepath.Join(dataDir, "runtime", "dcp-arbiter", incident.IncidentID)
	for path, expected := range map[string]string{
		filepath.Join(root, "input.json"):  attempt.OriginalInputArtifactDigest,
		filepath.Join(root, "schema.json"): attempt.OriginalSchemaArtifactDigest,
		filepath.Join(root, "result.json"): attempt.OriginalResultArtifactDigest,
	} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > arbiterMaxInputBytes || info.Mode().Perm()&0o022 != 0 {
			return errors.New("dcp arbiter successor: original artifact identity is unsafe")
		}
		data, err := os.ReadFile(path)
		if err != nil || digestBytes(data) != expected {
			return errors.New("dcp arbiter successor: original artifact bytes drifted")
		}
	}
	return nil
}
