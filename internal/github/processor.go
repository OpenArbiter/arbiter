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
}

// NewProcessor creates a new webhook event processor.
func NewProcessor(s store.Store, q *queue.Queue, c *Client) *Processor {
	return &Processor{store: s, queue: q, client: c}
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
			slog.ErrorContext(ctx, "processing job failed",
				"job_id", job.ID,
				"job_type", job.Type,
				"error", err,
			)
		}
	}
}

func (p *Processor) processJob(ctx context.Context, job *queue.Job) error {
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
		return fmt.Errorf("parsing PR event: %w", err)
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
	_, err = p.store.GetProposal(ctx, proposalID)
	if err == nil {
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

	// Create initial check run (in_progress)
	_, err = p.client.CreateCheckRun(ctx, installID, repo.Owner.Login, repo.Name, pr.Head.SHA, CheckRunOpts{
		Name:       "arbiter/trust",
		Status:     "in_progress",
		Conclusion: "",
		Title:      "Arbiter is evaluating this change",
		Summary:    "Waiting for CI evidence before making a decision.",
	})
	if err != nil {
		slog.WarnContext(ctx, "could not create check run", "error", err)
	}

	// Run initial evaluation (likely insufficient evidence at this point)
	return p.evaluateProposal(ctx, proposalID, installID, repo.Owner.Login, repo.Name, pr.Head.SHA, pr.Base.Ref)
}

func (p *Processor) handlePRClosed(ctx context.Context, job *queue.Job) error {
	var event PREvent
	if err := json.Unmarshal(job.Payload, &event); err != nil {
		return fmt.Errorf("parsing PR event: %w", err)
	}

	// Find and withdraw the latest proposal
	taskID := fmt.Sprintf("gh:%s:pr:%d", event.Repository.FullName, event.PullRequest.Number)
	proposals, err := p.store.ListProposalsByTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("listing proposals: %w", err)
	}

	for _, prop := range proposals {
		if prop.Status == model.ProposalOpen {
			if err := p.store.UpdateProposalStatus(ctx, prop.ProposalID, model.ProposalWithdrawn); err != nil {
				slog.WarnContext(ctx, "could not withdraw proposal", "proposal_id", prop.ProposalID, "error", err)
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
		return fmt.Errorf("parsing check_run event: %w", err)
	}

	cr := event.CheckRun

	// Ignore our own check runs
	if cr.Name == "arbiter/trust" {
		return nil
	}

	installID := event.Installation.ID
	tenantID := fmt.Sprintf("github:%d", installID)

	// Find the proposal for this SHA
	// We need to search open proposals for this tenant
	proposals, err := p.store.ListOpenProposalsByTenant(ctx, tenantID, 100, 0)
	if err != nil {
		return fmt.Errorf("listing proposals: %w", err)
	}

	var matchedProposal *model.Proposal
	for _, prop := range proposals {
		if prop.ChangeRef.ExternalID != "" {
			// Check if the proposal's SHA matches this check run
			// The proposal ID encodes the SHA
			expectedSuffix := ":sha:" + cr.HeadSHA
			if len(prop.ProposalID) > len(expectedSuffix) &&
				prop.ProposalID[len(prop.ProposalID)-len(expectedSuffix):] == expectedSuffix {
				matchedProposal = &prop
				break
			}
		}
	}

	if matchedProposal == nil {
		slog.DebugContext(ctx, "no matching proposal for check run", "head_sha", cr.HeadSHA)
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
	// Extract repo info from the proposal's context
	repo := event.Repository
	return p.evaluateProposal(ctx, matchedProposal.ProposalID, installID,
		repo.Owner.Login, repo.Name, cr.HeadSHA, "")
}

func (p *Processor) evaluateProposal(ctx context.Context, proposalID string, installID int64, owner, repo, headSHA, baseRef string) error {
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

	// Load config from the base branch
	cfg := config.DefaultConfig()
	if baseRef != "" {
		configData, err := p.client.GetFileContent(ctx, installID, owner, repo, ".arbiter.yml", baseRef)
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
	result := engine.Evaluate(evalCtx)

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

	_, err = p.client.CreateCheckRun(ctx, installID, owner, repo, headSHA, CheckRunOpts{
		Name:       "arbiter/trust",
		Status:     "completed",
		Conclusion: conclusion,
		Title:      fmt.Sprintf("Arbiter: %s", decision.Outcome),
		Summary:    decision.Summary,
	})
	if err != nil {
		slog.WarnContext(ctx, "could not update check run", "error", err)
	}

	slog.InfoContext(ctx, "evaluation complete",
		"proposal_id", proposalID,
		"outcome", decision.Outcome,
		"reason", decision.ReasonCode,
	)

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
