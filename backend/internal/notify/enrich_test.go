package notify

import (
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestEnrichReadyToMergePrioritizesPRContext(t *testing.T) {
	t.Parallel()

	rec, err := enrich(Intent{
		Type:               domain.NotificationReadyToMerge,
		SessionID:          "sess-1",
		ProjectID:          "proj-1",
		PRURL:              "https://github.com/acme/app/pull/67",
		SessionDisplayName: "Checkout flow",
		PRNumber:           67,
		PRTitle:            "Fix checkout totals",
		CreatedAt:          time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("enrich ready notification: %v", err)
	}

	if want := "Fix checkout totals · PR #67"; rec.Title != want {
		t.Fatalf("title = %q, want %q", rec.Title, want)
	}
	if want := "PR from session Checkout flow is ready to merge. CI passed with no blocking review feedback."; rec.Body != want {
		t.Fatalf("body = %q, want %q", rec.Body, want)
	}
}

func TestEnrichReadyToMergeFallsBackWithoutPRTitle(t *testing.T) {
	t.Parallel()

	rec, err := enrich(Intent{
		Type:      domain.NotificationReadyToMerge,
		SessionID: "sess-1",
		ProjectID: "proj-1",
		PRURL:     "https://github.com/acme/app/pull/67",
		PRNumber:  67,
		CreatedAt: time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("enrich ready notification: %v", err)
	}

	if want := "PR #67 is ready to merge"; rec.Title != want {
		t.Fatalf("title = %q, want %q", rec.Title, want)
	}
}

func TestEnrichReadyToMergeUsesReleaseTrainTruthForWBCPolicy(t *testing.T) {
	rec, err := enrich(Intent{
		Type: domain.NotificationReadyToMerge, SessionID: "wb-core-1", ProjectID: "wb-core",
		PRURL: "https://github.com/orenvlad-ai/wb-core/pull/987", PRNumber: 987,
		SessionDisplayName: "DCP:wbc-canary-v1", ReadyDestination: "wbc_release_train", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Title != "PR #987 is ready for Release Train" ||
		rec.Body != "PR from session DCP:wbc-canary-v1 completed DCP review and FIFO admission and is waiting for the WBC Release Train." {
		t.Fatalf("release-train notification=%+v", rec)
	}
}

func TestEnrichWBCCompletionUsesProfileTerminalTruth(t *testing.T) {
	for _, tc := range []struct {
		destination string
		title       string
		body        string
	}{
		{
			destination: "wbc_repo_release", title: "PR #987 released",
			body: "WBC Release Train merged the exact admitted head and published release:done; no deploy applies.",
		},
		{
			destination: "wbc_production", title: "PR #987 deployed to production",
			body: "WBC Release Train merged, deployed and verified the exact release SHA in production.",
		},
	} {
		rec, err := enrich(Intent{
			Type: domain.NotificationPRMerged, SessionID: "wb-core-1", ProjectID: "wb-core",
			PRURL: "https://github.com/orenvlad-ai/wb-core/pull/987", PRNumber: 987,
			CompletionDestination: tc.destination, CreatedAt: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if rec.Title != tc.title || rec.Body != tc.body {
			t.Fatalf("completion notification=%+v", rec)
		}
	}
}
