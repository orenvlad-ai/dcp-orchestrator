package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestGovernedStartupQuarantineAllowsOrdinaryDatabaseAndFailsUnknownGovernedState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	quarantine, err := s.EstablishDCPGovernedStartupQuarantine(ctx, now)
	if err != nil || len(quarantine) != 0 {
		t.Fatalf("ordinary database quarantine = %v, %v", quarantine, err)
	}
	seedProject(t, s, "dcp-review-lab")
	for i := 1; i <= 12; i++ {
		id := domain.SessionID(fmt.Sprintf("dcp-review-lab-%d", i))
		branch := "ao/ordinary/root"
		handle := "ordinary"
		workspace := "/tmp/ordinary"
		if i == 11 || i == 12 {
			branch = "ao/" + string(id) + "/root"
			handle = string(id)
			workspace = "/tmp/" + string(id)
		}
		rec, createErr := s.CreateSession(ctx, domain.SessionRecord{
			ProjectID: "dcp-review-lab", Kind: domain.KindWorker, Harness: domain.HarnessCodex,
			Activity:  domain.Activity{State: domain.ActivityIdle, LastActivityAt: now},
			Metadata:  domain.SessionMetadata{Branch: branch, WorkspacePath: workspace, RuntimeHandleID: handle},
			CreatedAt: now, UpdatedAt: now,
		})
		if createErr != nil {
			t.Fatalf("seed session %d: %v", i, createErr)
		}
		if i >= 11 && rec.ID != id {
			t.Fatalf("session %d id = %s, want %s", i, rec.ID, id)
		}
	}
	if _, err := s.EstablishDCPGovernedStartupQuarantine(ctx, now.Add(time.Second)); err == nil {
		t.Fatal("unknown governed card-11/card-12 state must fail startup closed")
	}
}
