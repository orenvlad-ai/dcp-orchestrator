package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var _ ports.SCMReleaseTrain = (*Provider)(nil)

type releasePull struct {
	HTMLURL        string  `json:"html_url"`
	Number         int     `json:"number"`
	State          string  `json:"state"`
	Draft          bool    `json:"draft"`
	Merged         bool    `json:"merged"`
	MergeCommitSHA *string `json:"merge_commit_sha"`
	MergedBy       *struct {
		Login string `json:"login"`
	} `json:"merged_by"`
	Body *string `json:"body"`
	Head struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"base"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

type releaseComment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

type releaseGitRef struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

func (p *Provider) ApplyReleaseReady(ctx context.Context, request ports.SCMReleaseReadyRequest) error {
	observation, err := p.observeRelease(ctx, request.PR, false)
	if err != nil {
		return err
	}
	if observation.Number != request.PR.Number || observation.URL != request.PR.URL || observation.State != "open" ||
		observation.Draft || observation.Merged || observation.HeadRepository != request.PR.Repo.Repo ||
		observation.HeadBranch == "" || !strings.EqualFold(observation.HeadSHA, request.ExpectedHeadSHA) ||
		observation.BaseBranch != request.ExpectedBaseBranch {
		return ports.ErrSCMReleaseIdentityChanged
	}
	if err := validateReleaseLabels(observation.Labels, request.RequiredTaskLabel, request.RequiredScopeLabel); err != nil {
		return err
	}
	if hasLabel(observation.Labels, "release:ready") || hasLabel(observation.Labels, "release:running") {
		return nil
	}
	payload := struct {
		Labels []string `json:"labels"`
	}{Labels: []string{"release:ready"}}
	_, err = p.client.doREST(ctx, http.MethodPost,
		repoPath(request.PR.Repo.Owner, request.PR.Repo.Name, "issues", strconv.Itoa(request.PR.Number), "labels"), nil, payload)
	if err != nil {
		return fmt.Errorf("github scm: apply release:ready: %w", err)
	}
	return nil
}

func (p *Provider) ObserveRelease(ctx context.Context, ref ports.SCMPRRef) (ports.SCMReleaseObservation, error) {
	return p.observeRelease(ctx, ref, true)
}

func (p *Provider) observeRelease(ctx context.Context, ref ports.SCMPRRef, includeComments bool) (ports.SCMReleaseObservation, error) {
	if p == nil || p.client == nil {
		return ports.SCMReleaseObservation{}, fmt.Errorf("github scm: release provider is not configured")
	}
	if ref.Number <= 0 || strings.TrimSpace(ref.Repo.Owner) == "" || strings.TrimSpace(ref.Repo.Name) == "" || strings.TrimSpace(ref.Repo.Repo) == "" {
		return ports.SCMReleaseObservation{}, fmt.Errorf("github scm: invalid release pull request reference")
	}
	resp, err := p.client.doREST(ctx, http.MethodGet,
		repoPath(ref.Repo.Owner, ref.Repo.Name, "pulls", strconv.Itoa(ref.Number)), nil, nil)
	if err != nil {
		if resp.StatusCode == http.StatusNotFound {
			return ports.SCMReleaseObservation{}, fmt.Errorf("%w: %w", ports.ErrSCMNotFound, err)
		}
		return ports.SCMReleaseObservation{}, err
	}
	var pull releasePull
	if err := json.Unmarshal(resp.Body, &pull); err != nil {
		return ports.SCMReleaseObservation{}, fmt.Errorf("github scm: decode release pull request: %w", err)
	}
	labels := make([]string, 0, len(pull.Labels))
	for _, label := range pull.Labels {
		name := strings.TrimSpace(label.Name)
		if name == "" {
			return ports.SCMReleaseObservation{}, ports.ErrSCMReleaseStateInvalid
		}
		labels = append(labels, name)
	}
	sort.Strings(labels)
	mergeSHA := ""
	if pull.MergeCommitSHA != nil {
		mergeSHA = strings.ToLower(strings.TrimSpace(*pull.MergeCommitSHA))
	}
	body := ""
	if pull.Body != nil {
		body = *pull.Body
	}
	mergedBy := ""
	if pull.MergedBy != nil {
		mergedBy = pull.MergedBy.Login
	}
	var comments []ports.SCMReleaseComment
	providerMainSHA := ""
	if includeComments {
		const maxCommentPages = 10
		for page := 1; page <= maxCommentPages; page++ {
			commentResp, commentErr := p.client.doREST(ctx, http.MethodGet,
				repoPath(ref.Repo.Owner, ref.Repo.Name, "issues", strconv.Itoa(ref.Number), "comments"),
				map[string][]string{"per_page": {"100"}, "page": {strconv.Itoa(page)}}, nil)
			if commentErr != nil {
				return ports.SCMReleaseObservation{}, commentErr
			}
			var providerComments []releaseComment
			if err := json.Unmarshal(commentResp.Body, &providerComments); err != nil {
				return ports.SCMReleaseObservation{}, fmt.Errorf("github scm: decode release comments: %w", err)
			}
			for _, comment := range providerComments {
				created, createdErr := time.Parse(time.RFC3339, comment.CreatedAt)
				updated, updatedErr := time.Parse(time.RFC3339, comment.UpdatedAt)
				if comment.ID <= 0 || comment.User.Login == "" || createdErr != nil || updatedErr != nil {
					return ports.SCMReleaseObservation{}, ports.ErrSCMReleaseStateInvalid
				}
				comments = append(comments, ports.SCMReleaseComment{ID: comment.ID, Author: comment.User.Login, Body: comment.Body, CreatedAt: created, UpdatedAt: updated})
			}
			if len(providerComments) < 100 {
				break
			}
			if page == maxCommentPages {
				return ports.SCMReleaseObservation{}, ports.ErrSCMReleaseStateInvalid
			}
		}
		sort.Slice(comments, func(i, j int) bool { return comments[i].ID < comments[j].ID })
		refResp, refErr := p.client.doREST(ctx, http.MethodGet,
			repoPath(ref.Repo.Owner, ref.Repo.Name, "git", "ref", "heads", pull.Base.Ref), nil, nil)
		if refErr != nil {
			return ports.SCMReleaseObservation{}, refErr
		}
		var providerRef releaseGitRef
		if err := json.Unmarshal(refResp.Body, &providerRef); err != nil || providerRef.Object.SHA == "" {
			return ports.SCMReleaseObservation{}, ports.ErrSCMReleaseStateInvalid
		}
		providerMainSHA = strings.ToLower(providerRef.Object.SHA)
	}
	return ports.SCMReleaseObservation{
		Number: pull.Number, URL: pull.HTMLURL, State: strings.ToLower(pull.State), Draft: pull.Draft, Merged: pull.Merged,
		HeadRepository: pull.Head.Repo.FullName, HeadBranch: pull.Head.Ref, HeadSHA: strings.ToLower(pull.Head.SHA),
		BaseBranch: pull.Base.Ref, BaseSHA: strings.ToLower(pull.Base.SHA), ProviderMainSHA: providerMainSHA, Author: pull.User.Login, MergedBy: mergedBy,
		MergeCommitSHA: mergeSHA, Labels: labels, Body: body, Comments: comments,
	}, nil
}

func validateReleaseLabels(labels []string, requiredTask, requiredScope string) error {
	taskLabels, scopeLabels, readyLabels, runningLabels := 0, 0, 0, 0
	for _, label := range labels {
		switch {
		case strings.HasPrefix(label, "task:"):
			taskLabels++
			if label != requiredTask {
				return ports.ErrSCMReleaseIdentityChanged
			}
		case strings.HasPrefix(label, "scope:"):
			scopeLabels++
			if label != requiredScope {
				return ports.ErrSCMReleaseIdentityChanged
			}
		case label == "release:ready":
			readyLabels++
		case label == "release:running":
			runningLabels++
		case strings.HasPrefix(label, "release:"):
			return ports.ErrSCMReleaseStateInvalid
		}
	}
	if taskLabels != 1 || scopeLabels != 1 {
		return ports.ErrSCMReleaseIdentityChanged
	}
	if readyLabels > 1 || runningLabels > 1 || (readyLabels == 1 && runningLabels == 1) {
		return ports.ErrSCMReleaseStateInvalid
	}
	return nil
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
