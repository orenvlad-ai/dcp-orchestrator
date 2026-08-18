package github

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func releaseRef() ports.SCMPRRef {
	return ports.SCMPRRef{
		Repo:   ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "orenvlad-ai", Name: "wb-core", Repo: "orenvlad-ai/wb-core"},
		Number: 7, URL: "https://github.com/orenvlad-ai/wb-core/pull/7",
	}
}

func TestObserveReleasePaginatesAllImmutableComments(t *testing.T) {
	f := newFakeGH(t)
	f.on(http.MethodGet, "/repos/orenvlad-ai/wb-core/pulls/7", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(releasePullPayload("release:ready", "scope:repo-only", "task:standard"))
	})
	f.on(http.MethodGet, "/repos/orenvlad-ai/wb-core/issues/7/comments", func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil || r.URL.Query().Get("per_page") != "100" {
			t.Fatalf("comment pagination query = %q", r.URL.RawQuery)
		}
		count := 100
		if page == 2 {
			count = 1
		} else if page != 1 {
			t.Fatalf("unexpected comments page %d", page)
		}
		comments := make([]map[string]any, 0, count)
		for i := 0; i < count; i++ {
			id := int64((page-1)*100 + i + 1)
			comments = append(comments, map[string]any{
				"id": id, "body": "proof", "created_at": "2026-08-18T01:02:03Z", "updated_at": "2026-08-18T01:02:03Z",
				"user": map[string]string{"login": "github-actions[bot]"},
			})
		}
		_ = json.NewEncoder(w).Encode(comments)
	})
	f.on(http.MethodGet, "/repos/orenvlad-ai/wb-core/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": "dddddddddddddddddddddddddddddddddddddddd"}})
	})
	got, err := newProviderForTest(t, f).ObserveRelease(ctx(), releaseRef())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Comments) != 101 || got.Comments[0].ID != 1 || got.Comments[100].ID != 101 {
		t.Fatalf("paginated comments = %d (%+v .. %+v)", len(got.Comments), got.Comments[0], got.Comments[len(got.Comments)-1])
	}
}

func releasePullPayload(labels ...string) map[string]any {
	items := make([]map[string]string, 0, len(labels))
	for _, label := range labels {
		items = append(items, map[string]string{"name": label})
	}
	return map[string]any{
		"html_url": "https://github.com/orenvlad-ai/wb-core/pull/7", "number": 7,
		"state": "open", "draft": false, "merged": false, "merge_commit_sha": nil, "body": "",
		"head": map[string]any{"ref": "ao/wb-core-1/root", "sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "repo": map[string]string{"full_name": "orenvlad-ai/wb-core"}},
		"base": map[string]string{"ref": "main", "sha": "cccccccccccccccccccccccccccccccccccccccc"}, "user": map[string]string{"login": "orenvlad-ai"}, "labels": items,
	}
}

func releaseReadyRequest() ports.SCMReleaseReadyRequest {
	return ports.SCMReleaseReadyRequest{
		PR: releaseRef(), ExpectedHeadSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ExpectedBaseBranch: "main",
		RequiredTaskLabel: "task:standard", RequiredScopeLabel: "scope:repo-only",
	}
}

func TestApplyReleaseReadyAddsOnlyExactLabel(t *testing.T) {
	f := newFakeGH(t)
	f.on(http.MethodGet, "/repos/orenvlad-ai/wb-core/pulls/7", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(releasePullPayload("task:standard", "scope:repo-only"))
	})
	f.on(http.MethodPost, "/repos/orenvlad-ai/wb-core/issues/7/labels", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Labels []string `json:"labels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Labels) != 1 || payload.Labels[0] != "release:ready" {
			t.Fatalf("labels payload = %#v", payload)
		}
		_ = json.NewEncoder(w).Encode([]map[string]string{{"name": "release:ready"}})
	})
	if err := newProviderForTest(t, f).ApplyReleaseReady(ctx(), releaseReadyRequest()); err != nil {
		t.Fatal(err)
	}
}

func TestApplyReleaseReadyIsIdempotentAndRejectsCrossedScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		labels  []string
		wantErr bool
	}{
		{name: "already ready", labels: []string{"task:standard", "scope:repo-only", "release:ready"}},
		{name: "Release Train already running", labels: []string{"task:standard", "scope:repo-only", "release:running"}},
		{name: "crossed scope", labels: []string{"task:standard", "scope:repo-only", "scope:live-runtime"}, wantErr: true},
		{name: "terminal label before admission", labels: []string{"task:standard", "scope:repo-only", "release:done"}, wantErr: true},
		{name: "ambiguous ready and running", labels: []string{"task:standard", "scope:repo-only", "release:ready", "release:running"}, wantErr: true},
		{name: "unknown release label", labels: []string{"task:standard", "scope:repo-only", "release:unknown"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeGH(t)
			f.on(http.MethodGet, "/repos/orenvlad-ai/wb-core/pulls/7", func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(releasePullPayload(tc.labels...))
			})
			err := newProviderForTest(t, f).ApplyReleaseReady(ctx(), releaseReadyRequest())
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestObserveReleaseCarriesExactTerminalFacts(t *testing.T) {
	f := newFakeGH(t)
	f.on(http.MethodGet, "/repos/orenvlad-ai/wb-core/pulls/7", func(w http.ResponseWriter, _ *http.Request) {
		payload := releasePullPayload("release:done", "scope:repo-only", "task:standard")
		payload["state"], payload["merged"], payload["merge_commit_sha"] = "closed", true, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		payload["body"] = "<!-- wb-core-release-completion-proof contour=repo-only merge=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb pr=7 -->"
		_ = json.NewEncoder(w).Encode(payload)
	})
	f.on(http.MethodGet, "/repos/orenvlad-ai/wb-core/issues/7/comments", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": 91, "body": "proof", "created_at": "2026-08-18T01:02:03Z", "updated_at": "2026-08-18T01:02:03Z",
			"user": map[string]string{"login": "github-actions[bot]"},
		}})
	})
	f.on(http.MethodGet, "/repos/orenvlad-ai/wb-core/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": "dddddddddddddddddddddddddddddddddddddddd"}})
	})
	got, err := newProviderForTest(t, f).ObserveRelease(ctx(), releaseRef())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Merged || got.State != "closed" || got.MergeCommitSHA != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ||
		got.BaseSHA != "cccccccccccccccccccccccccccccccccccccccc" || got.ProviderMainSHA != "dddddddddddddddddddddddddddddddddddddddd" || len(got.Comments) != 1 || got.Comments[0].ID != 91 ||
		len(got.Labels) != 3 || got.Labels[0] != "release:done" || got.Labels[1] != "scope:repo-only" || got.Labels[2] != "task:standard" {
		t.Fatalf("terminal observation = %+v", got)
	}
}
