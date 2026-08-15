package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDCPPolicySubmitResponsePreservesImmutablePolicyIdentity(t *testing.T) {
	const serverResponse = `{"task":{"taskId":"price-arch-v1","target":"wb-price-extension","profile":"repo-only","repository":"orenvlad-ai/wb-price-extension","sessionId":"wb-price-extension-1","cardNumber":1,"worktreePath":"/tmp/wb-price-extension-1","sourceBranch":"ao/wb-price-extension-1/root","state":"worker_running","revision":3},"duplicate":false}`

	var response dcpPolicySubmitResponse
	if err := json.Unmarshal([]byte(serverResponse), &response); err != nil {
		t.Fatal(err)
	}
	if response.Task.TaskID != "price-arch-v1" || response.Task.Target != "wb-price-extension" ||
		response.Task.Profile != "repo-only" || response.Task.Repository != "orenvlad-ai/wb-price-extension" {
		t.Fatalf("immutable response identity was dropped: %+v", response.Task)
	}

	var output bytes.Buffer
	if err := writeJSON(&output, response); err != nil {
		t.Fatal(err)
	}
	var projected map[string]any
	if err := json.Unmarshal(output.Bytes(), &projected); err != nil {
		t.Fatal(err)
	}
	task, ok := projected["task"].(map[string]any)
	if !ok || task["taskId"] != "price-arch-v1" || task["target"] != "wb-price-extension" ||
		task["profile"] != "repo-only" || task["repository"] != "orenvlad-ai/wb-price-extension" {
		t.Fatalf("CLI JSON drifted from the immutable server tuple: %s", output.String())
	}
}
