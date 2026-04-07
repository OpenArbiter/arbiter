package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/openarbiter/arbiter/internal/config"
	"github.com/openarbiter/arbiter/internal/engine"
	"github.com/openarbiter/arbiter/internal/model"
	"github.com/openarbiter/arbiter/internal/queue"
	"github.com/openarbiter/arbiter/internal/store"
)

// Processor consumes jobs from the queue and runs the evaluation pipeline.
type Processor struct {
	store  store.Store
	queue  *queue.Queue
	client *Client
	stats  *Stats
}

// NewProcessor creates a new webhook event processor.
func NewProcessor(s store.Store, q *queue.Queue, c *Client, stats *Stats) *Processor {
	return &Processor{store: s, queue: q, client: c, stats: stats}
}

// Run starts the worker loop, processing jobs until the context is cancelled.
func (p *Processor) Run(ctx context.Context) error {
	slog.InfoContext(ctx, "processor started")
	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "processor shutting down")
			return ctx.Err()
		default:
		}

		job, err := p.queue.Dequeue(ctx, 5*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.ErrorContext(ctx, "dequeue error", "error", err)
			continue
		}
		if job == nil {
			continue // timeout, loop back
		}

		if err := p.processJob(ctx, job); err != nil {
			p.stats.jobsFailed.Add(1)
			slog.ErrorContext(ctx, "processing job failed",
				"job_id", job.ID,
				"job_type", job.Type,
				"permanent", IsPermanent(err),
				"error", err,
			)
			if IsPermanent(err) {
				slog.WarnContext(ctx, "permanent error, skipping retry", "job_id", job.ID)
			} else {
				p.stats.jobsRetried.Add(1)
				if retryErr := p.queue.Retry(ctx, job, err); retryErr != nil {
					slog.ErrorContext(ctx, "retry failed", "job_id", job.ID, "error", retryErr)
				}
			}
		} else {
			p.stats.jobsProcessed.Add(1)
		}
	}
}

func (p *Processor) processJob(ctx context.Context, job *queue.Job) error {
	// Carry the webhook delivery ID as correlation ID through the pipeline
	ctx = WithCorrelationID(ctx, job.ID)

	slog.InfoContext(ctx, "processing job",
		"job_id", job.ID,
		"job_type", job.Type,
		"installation_id", job.InstallationID,
	)

	switch job.Type {
	case queue.JobPROpened, queue.JobPRSynchronize:
		return p.handlePREvent(ctx, job)
	case queue.JobPRClosed:
		return p.handlePRClosed(ctx, job)
	case queue.JobCheckRunCompleted:
		return p.handleCheckRunCompleted(ctx, job)
	case queue.JobCheckSuiteCompleted:
		return p.handleCheckSuiteCompleted(ctx, job)
	default:
		slog.WarnContext(ctx, "unknown job type", "job_type", job.Type)
		return nil
	}
}

// PREvent is the relevant subset of a GitHub pull_request webhook payload.
type PREvent struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Head    struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"base"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func (p *Processor) handlePREvent(ctx context.Context, job *queue.Job) error {
	var event PREvent
	if err := json.Unmarshal(job.Payload, &event); err != nil {
		return permanent("parsing PR event: %w", err)
	}

	pr := event.PullRequest
	repo := event.Repository
	installID := event.Installation.ID
	tenantID := fmt.Sprintf("github:%d", installID)

	// Create or find Task for this PR
	taskID := fmt.Sprintf("gh:%s:pr:%d", repo.FullName, pr.Number)
	_, err := p.store.GetTask(ctx, taskID)
	if err == store.ErrNotFound {
		task := model.Task{
			TaskID:          taskID,
			TenantID:        tenantID,
			Title:           pr.Title,
			Intent:          pr.Body,
			ExpectedOutcome: pr.Title,
			RiskLevel:       model.RiskMedium,
			ScopeHint:       model.Selector{},
			PolicyProfile:   "default",
			CreatedAt:       time.Now().UTC(),
			ExternalRefs: []model.ExternalRef{{
				RefType:    model.RefPullRequest,
				Provider:   model.ProviderGitHub,
				ExternalID: fmt.Sprintf("%d", pr.Number),
				URL:        pr.HTMLURL,
			}},
		}
		if err := p.store.CreateTask(ctx, &task); err != nil {
			return fmt.Errorf("creating task: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("getting task: %w", err)
	}

	// Create Proposal for this version of the PR
	proposalID := fmt.Sprintf("gh:%s:pr:%d:sha:%s", repo.FullName, pr.Number, pr.Head.SHA)

	// Check if we already processed this exact SHA (idempotency)
	existing, err := p.store.GetProposal(ctx, proposalID)
	if err == nil {
		// Proposal exists — if it was withdrawn (PR closed then reopened), reopen it
		if existing.Status == model.ProposalWithdrawn {
			slog.InfoContext(ctx, "reopening withdrawn proposal", "proposal_id", proposalID)
			if err := p.store.UpdateProposalStatus(ctx, proposalID, model.ProposalOpen); err != nil {
				return fmt.Errorf("reopening proposal: %w", err)
			}
			return p.evaluateProposal(ctx, proposalID, installID, repo.Owner.Login, repo.Name, pr.Head.SHA, pr.Base.Ref, pr.Number)
		}
		slog.InfoContext(ctx, "proposal already exists, skipping", "proposal_id", proposalID)
		return nil
	}
	if err != store.ErrNotFound {
		return fmt.Errorf("checking proposal: %w", err)
	}

	// Get changed files for scope
	files, err := p.client.GetPRFiles(ctx, installID, repo.Owner.Login, repo.Name, pr.Number)
	if err != nil {
		slog.WarnContext(ctx, "could not fetch PR files", "error", err)
	}

	proposal := model.Proposal{
		ProposalID:  proposalID,
		TaskID:      taskID,
		TenantID:    tenantID,
		SubmittedBy: pr.User.Login,
		ChangeRef: model.ExternalRef{
			RefType:    model.RefPullRequest,
			Provider:   model.ProviderGitHub,
			ExternalID: fmt.Sprintf("%d", pr.Number),
			URL:        pr.HTMLURL,
		},
		DeclaredScope:   model.Selector{Paths: files},
		BehaviorSummary: pr.Title,
		Confidence:      model.ConfidenceMedium,
		Status:          model.ProposalOpen,
		CreatedAt:       time.Now().UTC(),
	}
	if err := p.store.CreateProposal(ctx, &proposal); err != nil {
		return fmt.Errorf("creating proposal: %w", err)
	}

	// Carry forward unresolved challenges from previous proposals on this PR.
	// Without this, an attacker can push an empty commit to escape challenges.
	previousProposals, err := p.store.ListProposalsByTask(ctx, taskID)
	if err != nil {
		slog.WarnContext(ctx, "could not list previous proposals", "error", err)
	}
	for i := range previousProposals {
		if previousProposals[i].ProposalID == proposalID {
			continue // skip the one we just created
		}
		oldChallenges, err := p.store.ListOpenChallengesByProposal(ctx, previousProposals[i].ProposalID)
		if err != nil {
			slog.WarnContext(ctx, "could not list challenges", "error", err)
			continue
		}
		for j := range oldChallenges {
			carried := model.Challenge{
				ChallengeID:   fmt.Sprintf("ch:%s:carry:%d", proposalID, time.Now().UnixNano()),
				ProposalID:    proposalID,
				TenantID:      tenantID,
				RaisedBy:      oldChallenges[j].RaisedBy,
				ChallengeType: oldChallenges[j].ChallengeType,
				Target:        oldChallenges[j].Target,
				Severity:      oldChallenges[j].Severity,
				Summary:       fmt.Sprintf("[carried from previous commit] %s", oldChallenges[j].Summary),
				Status:        model.ChallengeOpen,
				CreatedAt:     time.Now().UTC(),
			}
			if err := p.store.CreateChallenge(ctx, &carried); err != nil {
				slog.WarnContext(ctx, "could not carry forward challenge", "error", err)
			} else {
				slog.InfoContext(ctx, "challenge carried forward",
					"old_challenge", oldChallenges[j].ChallengeID,
					"new_challenge", carried.ChallengeID,
					"severity", carried.Severity,
				)
			}
		}
	}

	// Create initial check run (in_progress)
	_, err = p.client.CreateCheckRun(ctx, installID, repo.Owner.Login, repo.Name, pr.Head.SHA, &CheckRunOpts{
		Name:       "openarbiter/trust",
		Status:     "in_progress",
		Conclusion: "",
		Title:      "Arbiter is evaluating this change",
		Summary:    "Waiting for CI evidence before making a decision.",
	})
	if err != nil {
		slog.WarnContext(ctx, "could not create check run", "error", err)
	}

	// Run initial evaluation (likely insufficient evidence at this point)
	return p.evaluateProposal(ctx, proposalID, installID, repo.Owner.Login, repo.Name, pr.Head.SHA, pr.Base.Ref, pr.Number)
}

func (p *Processor) handlePRClosed(ctx context.Context, job *queue.Job) error {
	var event PREvent
	if err := json.Unmarshal(job.Payload, &event); err != nil {
		return permanent("parsing PR event: %w", err)
	}

	// Find and withdraw the latest proposal
	taskID := fmt.Sprintf("gh:%s:pr:%d", event.Repository.FullName, event.PullRequest.Number)
	proposals, err := p.store.ListProposalsByTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("listing proposals: %w", err)
	}

	for i := range proposals {
		if proposals[i].Status == model.ProposalOpen {
			if err := p.store.UpdateProposalStatus(ctx, proposals[i].ProposalID, model.ProposalWithdrawn); err != nil {
				slog.WarnContext(ctx, "could not withdraw proposal", "proposal_id", proposals[i].ProposalID, "error", err)
			}
		}
	}
	return nil
}

// CheckRunEvent is the relevant subset of a GitHub check_run webhook payload.
type CheckRunEvent struct {
	Action   string `json:"action"`
	CheckRun struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadSHA    string `json:"head_sha"`
		App        struct {
			Slug string `json:"slug"`
		} `json:"app"`
	} `json:"check_run"`
	Repository struct {
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func (p *Processor) handleCheckRunCompleted(ctx context.Context, job *queue.Job) error {
	var event CheckRunEvent
	if err := json.Unmarshal(job.Payload, &event); err != nil {
		return permanent("parsing check_run event: %w", err)
	}

	cr := event.CheckRun

	// Ignore our own check runs
	if cr.Name == "openarbiter/trust" {
		return nil
	}

	installID := event.Installation.ID
	tenantID := fmt.Sprintf("github:%d", installID)

	slog.InfoContext(ctx, "processing check_run",
		"name", cr.Name,
		"conclusion", cr.Conclusion,
		"head_sha", cr.HeadSHA,
	)

	// Find the proposal for this SHA
	proposals, err := p.store.ListOpenProposalsByTenant(ctx, tenantID, 100, 0)
	if err != nil {
		return fmt.Errorf("listing proposals: %w", err)
	}

	var matchedProposal *model.Proposal
	for i := range proposals {
		if proposals[i].ChangeRef.ExternalID != "" {
			expectedSuffix := ":sha:" + cr.HeadSHA
			if len(proposals[i].ProposalID) > len(expectedSuffix) &&
				proposals[i].ProposalID[len(proposals[i].ProposalID)-len(expectedSuffix):] == expectedSuffix {
				matchedProposal = &proposals[i]
				break
			}
		}
	}

	if matchedProposal == nil {
		slog.InfoContext(ctx, "no matching proposal for check run",
			"head_sha", cr.HeadSHA,
			"tenant_id", tenantID,
			"open_proposals", len(proposals),
		)
		return nil
	}

	// Map check run result to Evidence
	var result model.EvidenceResult
	switch cr.Conclusion {
	case "success":
		result = model.EvidencePass
	case "failure":
		result = model.EvidenceFail
	case "neutral", "skipped":
		result = model.EvidenceInfo
	default:
		result = model.EvidenceWarn
	}

	evidenceType := mapCheckRunToEvidenceType(cr.Name)
	evidenceID := fmt.Sprintf("gh:cr:%s:%s:%s", event.Repository.FullName, cr.Name, cr.HeadSHA)

	// Idempotency check
	_, err = p.store.GetEvidence(ctx, evidenceID)
	if err == nil {
		slog.DebugContext(ctx, "evidence already exists", "evidence_id", evidenceID)
		return nil
	}

	summary := fmt.Sprintf("%s: %s", cr.Name, cr.Conclusion)
	evidence := model.Evidence{
		EvidenceID:   evidenceID,
		ProposalID:   matchedProposal.ProposalID,
		TenantID:     tenantID,
		EvidenceType: evidenceType,
		Subject:      cr.Name,
		Result:       result,
		Confidence:   model.ConfidenceHigh,
		Source:       cr.App.Slug,
		CreatedAt:    time.Now().UTC(),
		Summary:      &summary,
	}
	if err := p.store.CreateEvidence(ctx, &evidence); err != nil {
		return fmt.Errorf("creating evidence: %w", err)
	}

	// Re-evaluate the proposal with new evidence
	repo := event.Repository
	prNum := 0
	_, _ = fmt.Sscanf(matchedProposal.ChangeRef.ExternalID, "%d", &prNum)
	slog.InfoContext(ctx, "re-evaluating after check_run",
		"proposal_id", matchedProposal.ProposalID,
		"pr_number", prNum,
		"evidence_id", evidenceID,
	)
	return p.evaluateProposal(ctx, matchedProposal.ProposalID, installID,
		repo.Owner.Login, repo.Name, cr.HeadSHA, "", prNum)
}

func (p *Processor) evaluateProposal(ctx context.Context, proposalID string, installID int64, owner, repo, headSHA, baseRef string, prNumber int) error {
	proposal, err := p.store.GetProposal(ctx, proposalID)
	if err != nil {
		return fmt.Errorf("getting proposal: %w", err)
	}

	task, err := p.store.GetTask(ctx, proposal.TaskID)
	if err != nil {
		return fmt.Errorf("getting task: %w", err)
	}

	evidence, err := p.store.ListEvidenceByProposal(ctx, proposalID)
	if err != nil {
		return fmt.Errorf("listing evidence: %w", err)
	}

	challenges, err := p.store.ListChallengesByProposal(ctx, proposalID)
	if err != nil {
		return fmt.Errorf("listing challenges: %w", err)
	}

	// Load config from the base branch (fall back to default branch)
	cfg := config.DefaultConfig()
	configRef := baseRef
	if configRef == "" {
		configRef = "main"
	}
	{
		configData, err := p.client.GetFileContent(ctx, installID, owner, repo, ".arbiter.yml", configRef)
		if err != nil {
			slog.WarnContext(ctx, "could not read .arbiter.yml", "error", err)
		} else if configData != nil {
			parsed, err := config.Parse(configData)
			if err != nil {
				slog.WarnContext(ctx, "invalid .arbiter.yml, using defaults", "error", err)
			} else {
				cfg = parsed
			}
		}
	}

	// Run the engine
	evalCtx := engine.EvalContext{
		Task:       *task,
		Proposal:   *proposal,
		Evidence:   evidence,
		Challenges: challenges,
		Config:     cfg,
	}
	result := engine.Evaluate(&evalCtx)

	// Check previous decision to determine if outcome changed
	previousDecision, _ := p.store.GetLatestDecisionByProposal(ctx, proposalID)

	// Store the decision
	decision := result.Decision
	decision.DecisionID = fmt.Sprintf("dec:%s:%d", proposalID, time.Now().UnixMilli())
	if err := p.store.CreateDecision(ctx, &decision); err != nil {
		return fmt.Errorf("creating decision: %w", err)
	}

	// Publish check run result
	conclusion := "neutral"
	switch decision.Outcome {
	case model.DecisionAccepted:
		conclusion = "success"
	case model.DecisionRejected:
		conclusion = "failure"
	case model.DecisionNeedsAction:
		conclusion = "action_required"
	}

	_, err = p.client.CreateCheckRun(ctx, installID, owner, repo, headSHA, &CheckRunOpts{
		Name:       "openarbiter/trust",
		Status:     "completed",
		Conclusion: conclusion,
		Title:      fmt.Sprintf("Arbiter: %s", decision.Outcome),
		Summary:    decision.Summary,
	})
	if err != nil {
		slog.WarnContext(ctx, "could not update check run", "error", err)
	}

	switch decision.Outcome {
	case model.DecisionAccepted:
		p.stats.decisionsAccepted.Add(1)
	case model.DecisionRejected:
		p.stats.decisionsRejected.Add(1)
	case model.DecisionNeedsAction:
		p.stats.decisionsNeedsAction.Add(1)
	}

	slog.InfoContext(ctx, "evaluation complete",
		"proposal_id", proposalID,
		"outcome", decision.Outcome,
		"reason", decision.ReasonCode,
		"confidence", result.Confidence,
	)

	// Execute configured actions — only when decision outcome changes
	decisionChanged := previousDecision == nil || previousDecision.Outcome != decision.Outcome
	if prNumber > 0 && decisionChanged {
		actCtx := &ActionContext{
			InstallationID: installID,
			Owner:          owner,
			Repo:           repo,
			PRNumber:       prNumber,
			HeadSHA:        headSHA,
			Decision:       decision,
			Confidence:     result.Confidence,
			Stats:          p.stats,
		}
		p.client.ExecuteActions(ctx, actCtx, cfg.Actions)
	}

	return nil
}

// CheckSuiteEvent is the relevant subset of a GitHub check_suite webhook payload.
type CheckSuiteEvent struct {
	Action     string `json:"action"`
	CheckSuite struct {
		HeadSHA    string `json:"head_sha"`
		Conclusion string `json:"conclusion"`
		PullRequests []struct {
			Number int `json:"number"`
		} `json:"pull_requests"`
	} `json:"check_suite"`
	Repository struct {
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func (p *Processor) handleCheckSuiteCompleted(ctx context.Context, job *queue.Job) error {
	var event CheckSuiteEvent
	if err := json.Unmarshal(job.Payload, &event); err != nil {
		return permanent("parsing check_suite event: %w", err)
	}

	cs := event.CheckSuite
	installID := event.Installation.ID
	tenantID := fmt.Sprintf("github:%d", installID)
	repo := event.Repository

	slog.InfoContext(ctx, "check suite completed",
		"head_sha", cs.HeadSHA,
		"conclusion", cs.Conclusion,
		"pr_count", len(cs.PullRequests),
	)

	// Re-evaluate each PR associated with this check suite
	for _, pr := range cs.PullRequests {
		proposalID := fmt.Sprintf("gh:%s:pr:%d:sha:%s", repo.FullName, pr.Number, cs.HeadSHA)

		// Verify proposal exists
		if _, err := p.store.GetProposal(ctx, proposalID); err != nil {
			if err == store.ErrNotFound {
				slog.DebugContext(ctx, "no proposal for check suite PR",
					"pr", pr.Number, "head_sha", cs.HeadSHA)
				continue
			}
			return fmt.Errorf("getting proposal: %w", err)
		}

		// Find base ref from the task's external refs
		taskID := fmt.Sprintf("gh:%s:pr:%d", repo.FullName, pr.Number)
		proposals, err := p.store.ListProposalsByTask(ctx, taskID)
		if err != nil {
			return fmt.Errorf("listing proposals: %w", err)
		}

		// Use empty baseRef — config was loaded on initial PR event
		_ = proposals
		_ = tenantID
		if err := p.evaluateProposal(ctx, proposalID, installID,
			repo.Owner.Login, repo.Name, cs.HeadSHA, "", pr.Number); err != nil {
			slog.ErrorContext(ctx, "re-evaluation failed on check suite complete",
				"proposal_id", proposalID, "error", err)
		}
	}

	return nil
}

// mapCheckRunToEvidenceType maps a GitHub check run name to an evidence type.
func mapCheckRunToEvidenceType(name string) model.EvidenceType {
	// Check more specific patterns first to avoid false matches
	// (e.g. "snyk-test" should match security, not test)
	switch {
	case containsAny(name, "security", "snyk", "dependabot", "codeql"):
		return model.EvidenceSecurityScan
	case containsAny(name, "benchmark", "perf"):
		return model.EvidenceBenchmarkCheck
	case containsAny(name, "lint", "eslint", "golangci", "rubocop", "flake8"):
		return model.EvidenceBuildCheck
	case containsAny(name, "test", "spec", "jest", "pytest", "go test"):
		return model.EvidenceTestSuite
	case containsAny(name, "build", "compile"):
		return model.EvidenceBuildCheck
	default:
		return model.EvidenceBuildCheck
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
