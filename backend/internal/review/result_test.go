package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testStructuredSHA = "1111111111111111111111111111111111111111"

func expectedStructuredResult() StructuredResultExpected {
	return StructuredResultExpected{
		WorkerSessionID:  "mer-1",
		ReviewerHandleID: "review-mer-1",
		BatchID:          "batch-1",
		RunID:            "run-1",
		PRURL:            "https://github.com/o/r/pull/1",
		TargetSHA:        testStructuredSHA,
	}
}

func validStructuredResult() StructuredResult {
	expected := expectedStructuredResult()
	return StructuredResult{
		Version:          StructuredResultVersion,
		WorkerSessionID:  expected.WorkerSessionID,
		ReviewerHandleID: expected.ReviewerHandleID,
		BatchID:          expected.BatchID,
		RunID:            expected.RunID,
		PRURL:            expected.PRURL,
		TargetSHA:        expected.TargetSHA,
		Verdict:          "approved",
		Summary:          "No blocking correctness issues found.",
		Findings:         []StructuredFinding{},
	}
}

func writeStructuredResult(t *testing.T, result StructuredResult) string {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestStructuredResultSchemaBindsEveryIdentity(t *testing.T) {
	raw, err := StructuredResultSchema(expectedStructuredResult())
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	for key, want := range map[string]string{
		"workerSessionId":  "mer-1",
		"reviewerHandleId": "review-mer-1",
		"batchId":          "batch-1",
		"runId":            "run-1",
		"prUrl":            "https://github.com/o/r/pull/1",
		"targetSha":        testStructuredSHA,
	} {
		values := properties[key].(map[string]any)["enum"].([]any)
		if len(values) != 1 || values[0] != want {
			t.Fatalf("schema %s enum = %#v, want %q", key, values, want)
		}
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("schema permits extra fields: %s", raw)
	}
}

func TestReadStructuredResultAcceptsExactlyOneValidResult(t *testing.T) {
	want := validStructuredResult()
	got, err := ReadStructuredResult(writeStructuredResult(t, want), expectedStructuredResult())
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != "approved" || got.Body() != want.Summary {
		t.Fatalf("result = %+v body=%q", got, got.Body())
	}
}

func TestReadStructuredResultRejectsMissingMalformedAmbiguousAndForeign(t *testing.T) {
	expected := expectedStructuredResult()
	t.Run("missing", func(t *testing.T) {
		_, err := ReadStructuredResult(filepath.Join(t.TempDir(), "missing.json"), expected)
		if StructuredResultKind(err) != StructuredResultMissing {
			t.Fatalf("error = %v kind=%q", err, StructuredResultKind(err))
		}
	})
	t.Run("malformed", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "result.json")
		if err := os.WriteFile(file, []byte(`{"verdict":`), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := ReadStructuredResult(file, expected)
		if StructuredResultKind(err) != StructuredResultMalformed {
			t.Fatalf("error = %v kind=%q", err, StructuredResultKind(err))
		}
	})
	t.Run("ambiguous trailing object", func(t *testing.T) {
		raw, _ := json.Marshal(validStructuredResult())
		file := filepath.Join(t.TempDir(), "result.json")
		if err := os.WriteFile(file, append(append(raw, '\n'), raw...), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := ReadStructuredResult(file, expected)
		if StructuredResultKind(err) != StructuredResultMalformed {
			t.Fatalf("error = %v kind=%q", err, StructuredResultKind(err))
		}
	})
	t.Run("unknown field", func(t *testing.T) {
		raw, _ := json.Marshal(validStructuredResult())
		raw = append(raw[:len(raw)-1], []byte(`,"other":true}`)...)
		file := filepath.Join(t.TempDir(), "result.json")
		if err := os.WriteFile(file, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := ReadStructuredResult(file, expected)
		if StructuredResultKind(err) != StructuredResultMalformed {
			t.Fatalf("error = %v kind=%q", err, StructuredResultKind(err))
		}
	})
	t.Run("foreign run", func(t *testing.T) {
		result := validStructuredResult()
		result.RunID = "run-foreign"
		_, err := ReadStructuredResult(writeStructuredResult(t, result), expected)
		if StructuredResultKind(err) != StructuredResultForeign {
			t.Fatalf("error = %v kind=%q", err, StructuredResultKind(err))
		}
	})
}

func TestStructuredResultVerdictAndFindingsAreBounded(t *testing.T) {
	expected := expectedStructuredResult()
	approvedWithFinding := validStructuredResult()
	approvedWithFinding.Findings = []StructuredFinding{{Title: "Bug", Body: "Fix it", Path: "main.go", Line: 1}}
	if err := approvedWithFinding.Validate(expected); err == nil {
		t.Fatal("approved result with findings was accepted")
	}
	changesWithoutFinding := validStructuredResult()
	changesWithoutFinding.Verdict = "changes_requested"
	if err := changesWithoutFinding.Validate(expected); err == nil {
		t.Fatal("changes_requested result without findings was accepted")
	}
	changes := changesWithoutFinding
	changes.Findings = []StructuredFinding{{Title: "Nil dereference", Body: "The new path dereferences nil.", Path: "backend/main.go", Line: 42}}
	if err := changes.Validate(expected); err != nil {
		t.Fatal(err)
	}
	wantBody := "No blocking correctness issues found.\n\nFindings:\n- Nil dereference (backend/main.go:42): The new path dereferences nil."
	if got := changes.Body(); got != wantBody {
		t.Fatalf("body = %q, want %q", got, wantBody)
	}
	changes.Findings[0].Path = "../foreign"
	if err := changes.Validate(expected); err == nil || !strings.Contains(err.Error(), "repository-relative") {
		t.Fatalf("path validation error = %v", err)
	}
}
