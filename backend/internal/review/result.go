package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	StructuredResultVersion  = 1
	maxStructuredResultBytes = 32 * 1024
	maxSummaryRunes          = 1200
	maxFindingTitleRunes     = 160
	maxFindingBodyRunes      = 1600
	maxFindingPathRunes      = 512
	maxStructuredFindings    = 8
)

var (
	structuredIdentityPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	structuredSHA             = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// StructuredResultErrorKind is the bounded failure category recorded when a
// successful reviewer process does not leave one usable AO-owned result.
type StructuredResultErrorKind string

const (
	StructuredResultMissing   StructuredResultErrorKind = "missing_result"
	StructuredResultMalformed StructuredResultErrorKind = "malformed_result"
	StructuredResultForeign   StructuredResultErrorKind = "foreign_result"
)

// StructuredResultError keeps untrusted result details out of durable review
// state while still letting the supervisor report a deterministic category.
type StructuredResultError struct {
	Kind StructuredResultErrorKind
	Err  error
}

func (e *StructuredResultError) Error() string {
	if e == nil || e.Err == nil {
		return "structured review result is invalid"
	}
	return e.Err.Error()
}

func (e *StructuredResultError) Unwrap() error { return e.Err }

// StructuredResultKind returns the fail-closed category for a result error.
func StructuredResultKind(err error) StructuredResultErrorKind {
	var resultErr *StructuredResultError
	if errors.As(err, &resultErr) {
		return resultErr.Kind
	}
	return StructuredResultMalformed
}

func resultError(kind StructuredResultErrorKind, format string, args ...any) error {
	return &StructuredResultError{Kind: kind, Err: fmt.Errorf(format, args...)}
}

// StructuredResultExpected is the trusted identity authored by the daemon for
// one reviewer process. Every field is repeated in the model result and bound
// again by the SQLite update before the verdict can become authoritative.
type StructuredResultExpected struct {
	WorkerSessionID  string
	ReviewerHandleID string
	BatchID          string
	RunID            string
	PRURL            string
	TargetSHA        string
}

// StructuredFinding is one bounded, local-only review finding. Line zero means
// that the finding applies to the file or change generally.
type StructuredFinding struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Path  string `json:"path"`
	Line  int    `json:"line"`
}

// StructuredResult is the only model-authored reviewer output accepted by the
// deterministic DCP result channel.
type StructuredResult struct {
	Version          int                 `json:"version"`
	WorkerSessionID  string              `json:"workerSessionId"`
	ReviewerHandleID string              `json:"reviewerHandleId"`
	BatchID          string              `json:"batchId"`
	RunID            string              `json:"runId"`
	PRURL            string              `json:"prUrl"`
	TargetSHA        string              `json:"targetSha"`
	Verdict          string              `json:"verdict"`
	Summary          string              `json:"summary"`
	Findings         []StructuredFinding `json:"findings"`
}

func (e StructuredResultExpected) validate() error {
	for name, value := range map[string]string{
		"worker session id":  e.WorkerSessionID,
		"reviewer handle id": e.ReviewerHandleID,
		"batch id":           e.BatchID,
		"run id":             e.RunID,
	} {
		if !structuredIdentityPattern.MatchString(value) {
			return fmt.Errorf("%s is invalid", name)
		}
	}
	if e.ReviewerHandleID != reviewerHandleIDForResult(e.WorkerSessionID) {
		return fmt.Errorf("reviewer handle does not belong to worker session")
	}
	parsed, err := url.Parse(e.PRURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return fmt.Errorf("PR URL is invalid")
	}
	if !structuredSHA.MatchString(e.TargetSHA) {
		return fmt.Errorf("target SHA is not an exact lowercase 40-character commit id")
	}
	return nil
}

func reviewerHandleIDForResult(workerID string) string { return "review-" + workerID }

// Validate proves schema semantics and exact trusted identity. The JSON Schema
// narrows model generation; this independent check remains authoritative.
func (r StructuredResult) Validate(expected StructuredResultExpected) error {
	if err := expected.validate(); err != nil {
		return resultError(StructuredResultForeign, "invalid trusted review identity: %v", err)
	}
	if r.Version != StructuredResultVersion {
		return resultError(StructuredResultMalformed, "structured review result version %d is unsupported", r.Version)
	}
	for _, identity := range []struct {
		name string
		got  string
		want string
	}{
		{"worker session id", r.WorkerSessionID, expected.WorkerSessionID},
		{"reviewer handle id", r.ReviewerHandleID, expected.ReviewerHandleID},
		{"batch id", r.BatchID, expected.BatchID},
		{"run id", r.RunID, expected.RunID},
		{"PR URL", r.PRURL, expected.PRURL},
		{"target SHA", r.TargetSHA, expected.TargetSHA},
	} {
		if identity.got != identity.want {
			return resultError(StructuredResultForeign, "structured review result %s mismatch", identity.name)
		}
	}
	if r.Verdict != "approved" && r.Verdict != "changes_requested" {
		return resultError(StructuredResultMalformed, "structured review verdict is invalid")
	}
	if strings.TrimSpace(r.Summary) == "" || utf8.RuneCountInString(r.Summary) > maxSummaryRunes {
		return resultError(StructuredResultMalformed, "structured review summary is empty or too long")
	}
	if len(r.Findings) > maxStructuredFindings {
		return resultError(StructuredResultMalformed, "structured review has too many findings")
	}
	if r.Verdict == "approved" && len(r.Findings) != 0 {
		return resultError(StructuredResultMalformed, "approved structured review must not contain findings")
	}
	if r.Verdict == "changes_requested" && len(r.Findings) == 0 {
		return resultError(StructuredResultMalformed, "changes_requested structured review requires findings")
	}
	for i, finding := range r.Findings {
		if strings.TrimSpace(finding.Title) == "" || utf8.RuneCountInString(finding.Title) > maxFindingTitleRunes {
			return resultError(StructuredResultMalformed, "structured review finding %d has an empty or long title", i+1)
		}
		if strings.TrimSpace(finding.Body) == "" || utf8.RuneCountInString(finding.Body) > maxFindingBodyRunes {
			return resultError(StructuredResultMalformed, "structured review finding %d has an empty or long body", i+1)
		}
		if utf8.RuneCountInString(finding.Path) > maxFindingPathRunes || strings.ContainsAny(finding.Path, "\r\n") {
			return resultError(StructuredResultMalformed, "structured review finding %d has an invalid path", i+1)
		}
		if finding.Path != "" {
			clean := path.Clean(finding.Path)
			if strings.HasPrefix(clean, "/") || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
				return resultError(StructuredResultMalformed, "structured review finding %d path is not repository-relative", i+1)
			}
		}
		if finding.Line < 0 || finding.Line > 10_000_000 {
			return resultError(StructuredResultMalformed, "structured review finding %d has an invalid line", i+1)
		}
	}
	return nil
}

// Body renders the bounded result into the existing ReviewRun body field. It
// adds no parallel finding store or transcript authority.
func (r StructuredResult) Body() string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(r.Summary))
	if len(r.Findings) == 0 {
		return b.String()
	}
	b.WriteString("\n\nFindings:\n")
	for _, finding := range r.Findings {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(finding.Title))
		if finding.Path != "" {
			b.WriteString(" (")
			b.WriteString(finding.Path)
			if finding.Line > 0 {
				fmt.Fprintf(&b, ":%d", finding.Line)
			}
			b.WriteString(")")
		}
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(finding.Body))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// StructuredResultSchema returns the exact per-run JSON Schema supplied to
// codex exec --output-schema. Identity enums reduce generation ambiguity; the
// supervisor and SQLite path still validate every field independently.
func StructuredResultSchema(expected StructuredResultExpected) ([]byte, error) {
	if err := expected.validate(); err != nil {
		return nil, err
	}
	stringField := func(max int) map[string]any {
		return map[string]any{"type": "string", "maxLength": max}
	}
	exactString := func(value string) map[string]any {
		return map[string]any{"type": "string", "enum": []string{value}}
	}
	finding := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{"type": "string", "minLength": 1, "maxLength": maxFindingTitleRunes},
			"body":  map[string]any{"type": "string", "minLength": 1, "maxLength": maxFindingBodyRunes},
			"path":  stringField(maxFindingPathRunes),
			"line":  map[string]any{"type": "integer", "minimum": 0, "maximum": 10_000_000},
		},
		"required":             []string{"title", "body", "path", "line"},
		"additionalProperties": false,
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"version":          map[string]any{"type": "integer", "enum": []int{StructuredResultVersion}},
			"workerSessionId":  exactString(expected.WorkerSessionID),
			"reviewerHandleId": exactString(expected.ReviewerHandleID),
			"batchId":          exactString(expected.BatchID),
			"runId":            exactString(expected.RunID),
			"prUrl":            exactString(expected.PRURL),
			"targetSha":        exactString(expected.TargetSHA),
			"verdict":          map[string]any{"type": "string", "enum": []string{"approved", "changes_requested"}},
			"summary":          map[string]any{"type": "string", "minLength": 1, "maxLength": maxSummaryRunes},
			"findings": map[string]any{
				"type": "array", "items": finding, "maxItems": maxStructuredFindings,
			},
		},
		"required": []string{
			"version", "workerSessionId", "reviewerHandleId", "batchId", "runId", "prUrl", "targetSha", "verdict", "summary", "findings",
		},
		"additionalProperties": false,
	}
	return json.MarshalIndent(schema, "", "  ")
}

// ReadStructuredResult reads exactly one bounded regular file and rejects
// trailing JSON, unknown fields, foreign identity, and invalid verdict shapes.
func ReadStructuredResult(resultPath string, expected StructuredResultExpected) (StructuredResult, error) {
	info, err := os.Lstat(resultPath)
	if errors.Is(err, os.ErrNotExist) {
		return StructuredResult{}, resultError(StructuredResultMissing, "structured review result is missing")
	}
	if err != nil {
		return StructuredResult{}, resultError(StructuredResultMalformed, "inspect structured review result: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return StructuredResult{}, resultError(StructuredResultForeign, "structured review result is not an owner-controlled regular file")
	}
	if info.Size() <= 0 || info.Size() > maxStructuredResultBytes {
		return StructuredResult{}, resultError(StructuredResultMalformed, "structured review result size is invalid")
	}
	f, err := os.Open(resultPath)
	if err != nil {
		return StructuredResult{}, resultError(StructuredResultMalformed, "open structured review result: %v", err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return StructuredResult{}, resultError(StructuredResultForeign, "structured review result changed while opening")
	}
	decoder := json.NewDecoder(io.LimitReader(f, maxStructuredResultBytes+1))
	decoder.DisallowUnknownFields()
	var result StructuredResult
	if err := decoder.Decode(&result); err != nil {
		return StructuredResult{}, resultError(StructuredResultMalformed, "decode structured review result: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return StructuredResult{}, resultError(StructuredResultMalformed, "structured review result contains trailing data")
	}
	if err := result.Validate(expected); err != nil {
		return StructuredResult{}, err
	}
	return result, nil
}
