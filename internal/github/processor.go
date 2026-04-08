package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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
	case queue.JobPRReviewSubmitted:
		return p.handlePRReview(ctx, job)
	case queue.JobStatusEvent:
		return p.handleStatusEvent(ctx, job)
	case queue.JobInstallationCreated:
		return p.handleInstallation(ctx, job)
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

	// Analyze the diff and generate evidence
	if p.client != nil {
		fileDetails, err := p.client.GetPRFileDetails(ctx, installID, repo.Owner.Login, repo.Name, pr.Number)
		if err != nil {
			slog.WarnContext(ctx, "could not fetch PR file details", "error", err)
		} else {
			// Diff analysis — file-level patterns
			insights := AnalyzeDiff(fileDetails)
			diffEvidence := GenerateEvidence(insights, proposalID, tenantID)
			StoreEvidence(ctx, p.store, diffEvidence)

			// Scope analysis — capability detection from diff content
			addedLines := ExtractAddedLines(fileDetails)
			scopeResult := AnalyzeScope(pr.Title, pr.Body, fileDetails, addedLines)
			scopeEvidence := GenerateScopeEvidence(scopeResult, proposalID, tenantID)
			StoreEvidence(ctx, p.store, scopeEvidence)

			// Coverage analysis — check if code changes have test changes
			coverageResult := AnalyzeCoverage(fileDetails, config.TestingConfig{})
			coverageEvidence := GenerateCoverageEvidence(coverageResult, proposalID, tenantID)
			StoreEvidence(ctx, p.store, coverageEvidence)

			// Load config early for invariants and auto-review (best effort)
			var invariants []config.Invariant
			var arCfg config.AutoReviewConfig
			if p.client != nil {
				configData, cfgErr := p.client.GetFileContent(ctx, installID, repo.Owner.Login, repo.Name, ".arbiter.yml", pr.Base.Ref)
				if cfgErr == nil && configData != nil {
					if parsed, parseErr := config.Parse(configData); parseErr == nil {
						invariants = parsed.Invariants
						arCfg = parsed.AutoReview
					}
				}
			}
			var invariantResults []InvariantResult
			if len(invariants) > 0 {
				invariantResults = CheckInvariants(invariants, fileDetails, addedLines)
				invariantEvidence := GenerateInvariantEvidence(invariantResults, proposalID, tenantID)
				StoreEvidence(ctx, p.store, invariantEvidence)
			}

			// Deep analysis — targets, entropy, combinations
			var analysisCfg config.AnalysisConfig
			if p.client != nil {
				configData, cfgErr := p.client.GetFileContent(ctx, installID, repo.Owner.Login, repo.Name, ".arbiter.yml", pr.Base.Ref)
				if cfgErr == nil && configData != nil {
					if parsed, parseErr := config.Parse(configData); parseErr == nil {
						analysisCfg = parsed.Analysis
					}
				}
			}
			deepResult := RunDeepAnalysis(fileDetails, analysisCfg)
			deepEvidence := GenerateDeepAnalysisEvidence(deepResult, analysisCfg, proposalID, tenantID)
			StoreEvidence(ctx, p.store, deepEvidence)

			// Dependency analysis
			var depCfg config.DependencyConfig
			if p.client != nil {
				configData, cfgErr := p.client.GetFileContent(ctx, installID, repo.Owner.Login, repo.Name, ".arbiter.yml", pr.Base.Ref)
				if cfgErr == nil && configData != nil {
					if parsed, parseErr := config.Parse(configData); parseErr == nil {
						depCfg = parsed.Dependencies
					}
				}
			}
			depResult := AnalyzeDependencies(fileDetails, depCfg)
			depEvidence := GenerateDepEvidence(depResult, proposalID, tenantID)
			StoreEvidence(ctx, p.store, depEvidence)

			// Auto-review — generate challenges from analysis results
			AutoReview(ctx, p.store, proposalID, tenantID,
				insights, scopeResult, coverageResult, invariantResults, arCfg)

			slog.InfoContext(ctx, "full analysis complete",
				"files", insights.TotalFiles,
				"diff_flags", len(insights.Flags),
				"scope_flags", len(scopeResult.Flags),
				"capabilities", len(scopeResult.NewCapabilities),
				"uncovered_files", len(coverageResult.UncoveredCodeFiles),
				"suspicious_targets", len(deepResult.SuspiciousTargets),
				"high_entropy", len(deepResult.HighEntropyStrings),
				"dangerous_combos", len(deepResult.DangerousCombos),
			)
		}
	}

	// Check if diff analysis found critical issues — fail fast before CI runs
	// This is the gatekeeper: if the diff is dangerous, block immediately
	hasCriticalFindings := false
	if p.client != nil {
		challenges, chErr := p.store.ListOpenChallengesByProposal(ctx, proposalID)
		if chErr == nil {
			for i := range challenges {
				if challenges[i].RaisedBy == "arbiter-auto-review" && challenges[i].Severity == model.SeverityHigh {
					hasCriticalFindings = true
					break
				}
			}
		}
	}

	if hasCriticalFindings {
		// Post immediate failure — don't wait for CI
		slog.InfoContext(ctx, "critical findings in diff analysis — failing fast",
			"proposal_id", proposalID,
		)
		return p.evaluateProposal(ctx, proposalID, installID, repo.Owner.Login, repo.Name, pr.Head.SHA, pr.Base.Ref, pr.Number)
	}

	// No critical findings — post in_progress and wait for CI
	_, err = p.client.CreateCheckRun(ctx, installID, repo.Owner.Login, repo.Name, pr.Head.SHA, &CheckRunOpts{
		Name:       "openarbiter/trust",
		Status:     "in_progress",
		Conclusion: "",
		Title:      "Arbiter is evaluating this change",
		Summary:    "Diff analysis clean. Waiting for CI evidence.",
	})
	if err != nil {
		slog.WarnContext(ctx, "could not create check run", "error", err)
	}

	// Run initial evaluation
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

	// Auto-link challenges to relevant evidence
	AutoLinkChallenges(ctx, p.store, proposalID)

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

	checkRunSummary := buildCheckRunSummary(result, evidence)

	// Generate inline annotations from evidence
	var annotations []Annotation
	if p.client != nil && prNumber > 0 {
		fileDetails, fileErr := p.client.GetPRFileDetails(ctx, installID, owner, repo, prNumber)
		if fileErr == nil && len(fileDetails) > 0 {
			addedLines := ExtractAddedLines(fileDetails)
			scopeResult := AnalyzeScope("", "", fileDetails, addedLines)

			var invResults []InvariantResult
			if len(cfg.Invariants) > 0 {
				invResults = CheckInvariants(cfg.Invariants, fileDetails, addedLines)
			}

			annotations = GenerateAnnotations(fileDetails, scopeResult, invResults)
		}
	}

	_, err = p.client.CreateCheckRun(ctx, installID, owner, repo, headSHA, &CheckRunOpts{
		Name:        "openarbiter/trust",
		Status:      "completed",
		Conclusion:  conclusion,
		Title:       fmt.Sprintf("Arbiter: %s (confidence: %.0f%%)", decision.Outcome, result.Confidence*100),
		Summary:     checkRunSummary,
		Annotations: annotations,
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
	// Skip the very first evaluation (no previous decision, no evidence) — it's just "CI hasn't run yet"
	isFirstEval := previousDecision == nil
	hasEvidence := len(evidence) > 0
	decisionChanged := previousDecision != nil && previousDecision.Outcome != decision.Outcome
	shouldComment := (isFirstEval && hasEvidence) || decisionChanged
	if prNumber > 0 && shouldComment {
		actCtx := &ActionContext{
			InstallationID: installID,
			Owner:          owner,
			Repo:           repo,
			PRNumber:       prNumber,
			HeadSHA:        headSHA,
			Decision:       decision,
			Confidence:     result.Confidence,
			Stats:          p.stats,
			CheckRunDetail: checkRunSummary,
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

// InstallationEvent is the relevant subset of a GitHub installation webhook payload.
type InstallationEvent struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repositories []struct {
		FullName string `json:"full_name"`
		Name     string `json:"name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repositories"`
}

func (p *Processor) handleInstallation(ctx context.Context, job *queue.Job) error {
	if p.client == nil {
		return nil
	}

	var event InstallationEvent
	if err := json.Unmarshal(job.Payload, &event); err != nil {
		return permanent("parsing installation event: %w", err)
	}

	installID := event.Installation.ID
	slog.InfoContext(ctx, "processing installation",
		"installation_id", installID,
		"repos", len(event.Repositories),
	)

	for _, repo := range event.Repositories {
		owner := repo.Owner.Login
		repoName := repo.Name
		if owner == "" {
			// Some payloads have owner in full_name
			parts := strings.SplitN(repo.FullName, "/", 2)
			if len(parts) == 2 {
				owner = parts[0]
				repoName = parts[1]
			}
		}
		if owner == "" || repoName == "" {
			continue
		}

		// Check if repo has .arbiter.yml with scan_existing enabled
		configData, err := p.client.GetFileContent(ctx, installID, owner, repoName, ".arbiter.yml", "main")
		if err != nil || configData == nil {
			slog.InfoContext(ctx, "no .arbiter.yml, skipping repo",
				"repo", repo.FullName,
			)
			continue
		}

		cfg, err := config.Parse(configData)
		if err != nil {
			slog.WarnContext(ctx, "invalid .arbiter.yml, skipping repo",
				"repo", repo.FullName, "error", err,
			)
			continue
		}

		if !cfg.ScanExisting {
			slog.InfoContext(ctx, "scan_existing not enabled, skipping repo",
				"repo", repo.FullName,
			)
			continue
		}

		// Scan open PRs
		openPRs, err := p.client.ListOpenPRs(ctx, installID, owner, repoName)
		if err != nil {
			slog.WarnContext(ctx, "could not list open PRs",
				"repo", repo.FullName, "error", err,
			)
			continue
		}

		slog.InfoContext(ctx, "scanning existing PRs",
			"repo", repo.FullName,
			"open_prs", len(openPRs),
		)

		for i := range openPRs {
			pr := &openPRs[i]
			// Synthesize a PR opened event and enqueue it
			payload := map[string]any{
				"action": "opened",
				"number": pr.Number,
				"pull_request": map[string]any{
					"number":   pr.Number,
					"title":    pr.Title,
					"body":     pr.Body,
					"html_url": pr.HTMLURL,
					"head":     map[string]any{"sha": pr.HeadSHA, "ref": pr.HeadRef},
					"base":     map[string]any{"sha": "", "ref": pr.BaseRef},
					"user":     map[string]any{"login": pr.User},
				},
				"repository": map[string]any{
					"full_name": repo.FullName,
					"owner":     map[string]any{"login": owner},
					"name":      repoName,
				},
				"installation": map[string]any{"id": installID},
			}
			jobData, _ := json.Marshal(payload)
			scanJob := queue.Job{
				ID:             fmt.Sprintf("scan:%s:pr:%d", repo.FullName, pr.Number),
				Type:           queue.JobPROpened,
				InstallationID: installID,
				Payload:        jobData,
				CreatedAt:      time.Now().UTC(),
			}
			if err := p.queue.Enqueue(ctx, &scanJob); err != nil {
				slog.WarnContext(ctx, "could not enqueue scan job",
					"repo", repo.FullName, "pr", pr.Number, "error", err,
				)
			}
		}
	}

	return nil
}

// StatusEvent is the relevant subset of a GitHub status webhook payload.
type StatusEvent struct {
	SHA     string `json:"sha"`
	State   string `json:"state"` // pending, success, failure, error
	Context string `json:"context"`
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

func (p *Processor) handleStatusEvent(ctx context.Context, job *queue.Job) error {
	var event StatusEvent
	if err := json.Unmarshal(job.Payload, &event); err != nil {
		return permanent("parsing status event: %w", err)
	}

	// Ignore pending statuses
	if event.State == "pending" {
		return nil
	}

	installID := event.Installation.ID
	tenantID := fmt.Sprintf("github:%d", installID)

	slog.InfoContext(ctx, "processing status",
		"context", event.Context,
		"state", event.State,
		"sha", event.SHA,
	)

	// Find matching proposal
	proposals, err := p.store.ListOpenProposalsByTenant(ctx, tenantID, 100, 0)
	if err != nil {
		return fmt.Errorf("listing proposals: %w", err)
	}

	var matchedProposal *model.Proposal
	for i := range proposals {
		expectedSuffix := ":sha:" + event.SHA
		if len(proposals[i].ProposalID) > len(expectedSuffix) &&
			proposals[i].ProposalID[len(proposals[i].ProposalID)-len(expectedSuffix):] == expectedSuffix {
			matchedProposal = &proposals[i]
			break
		}
	}

	if matchedProposal == nil {
		slog.InfoContext(ctx, "no matching proposal for status", "sha", event.SHA)
		return nil
	}

	// Map status to evidence
	var result model.EvidenceResult
	switch event.State {
	case "success":
		result = model.EvidencePass
	case "failure", "error":
		result = model.EvidenceFail
	default:
		result = model.EvidenceInfo
	}

	evidenceType := mapCheckRunToEvidenceType(event.Context)
	evidenceID := fmt.Sprintf("gh:status:%s:%s:%s", event.Repository.FullName, event.Context, event.SHA)

	// Idempotency
	if _, err := p.store.GetEvidence(ctx, evidenceID); err == nil {
		return nil
	}

	summary := fmt.Sprintf("%s: %s", event.Context, event.State)
	evidence := model.Evidence{
		EvidenceID:   evidenceID,
		ProposalID:   matchedProposal.ProposalID,
		TenantID:     tenantID,
		EvidenceType: evidenceType,
		Subject:      event.Context,
		Result:       result,
		Confidence:   model.ConfidenceHigh,
		Source:       "github-status",
		CreatedAt:    time.Now().UTC(),
		Summary:      &summary,
	}
	if err := p.store.CreateEvidence(ctx, &evidence); err != nil {
		return fmt.Errorf("creating evidence from status: %w", err)
	}

	// Re-evaluate
	repo := event.Repository
	prNum := 0
	_, _ = fmt.Sscanf(matchedProposal.ChangeRef.ExternalID, "%d", &prNum)
	return p.evaluateProposal(ctx, matchedProposal.ProposalID, installID,
		repo.Owner.Login, repo.Name, event.SHA, "", prNum)
}

// PRReviewEvent is the relevant subset of a GitHub pull_request_review webhook payload.
type PRReviewEvent struct {
	Action string `json:"action"`
	Review struct {
		ID   int64  `json:"id"`
		State string `json:"state"` // "approved", "changes_requested", "commented"
		Body  string `json:"body"`
		User  struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"review"`
	PullRequest struct {
		Number int    `json:"number"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
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

func (p *Processor) handlePRReview(ctx context.Context, job *queue.Job) error {
	var event PRReviewEvent
	if err := json.Unmarshal(job.Payload, &event); err != nil {
		return permanent("parsing pull_request_review event: %w", err)
	}

	review := event.Review
	pr := event.PullRequest
	repo := event.Repository
	installID := event.Installation.ID
	tenantID := fmt.Sprintf("github:%d", installID)

	slog.InfoContext(ctx, "processing review",
		"reviewer", review.User.Login,
		"state", review.State,
		"pr", pr.Number,
	)

	// Find the proposal for this PR and SHA
	proposalID := fmt.Sprintf("gh:%s:pr:%d:sha:%s", repo.FullName, pr.Number, pr.Head.SHA)
	_, err := p.store.GetProposal(ctx, proposalID)
	if err != nil {
		if err == store.ErrNotFound {
			slog.InfoContext(ctx, "no proposal for review", "proposal_id", proposalID)
			return nil
		}
		return fmt.Errorf("getting proposal: %w", err)
	}

	switch review.State {
	case "changes_requested":
		// Parse severity from review body — look for "severity: high|medium|low"
		severity := parseSeverity(review.Body)

		// Determine challenge type from review body
		challengeType := model.ChallengeHiddenBehaviorChange // default
		for _, ct := range []struct {
			keyword string
			ctype   model.ChallengeType
		}{
			{"scope", model.ChallengeScopeMismatch},
			{"test", model.ChallengeInsufficientTestCoverage},
			{"policy", model.ChallengePolicyViolation},
			{"regression", model.ChallengeLikelyRegression},
		} {
			if containsAny(strings.ToLower(review.Body), ct.keyword) {
				challengeType = ct.ctype
				break
			}
		}

		summary := review.Body
		if summary == "" {
			summary = "Changes requested by reviewer"
		}

		challengeID := fmt.Sprintf("ch:review:%s:%d:%d", repo.FullName, pr.Number, review.ID)

		// Idempotency — don't duplicate if we already processed this review
		if _, err := p.store.GetChallenge(ctx, challengeID); err == nil {
			slog.InfoContext(ctx, "challenge already exists for review", "challenge_id", challengeID)
			return nil
		}

		challenge := model.Challenge{
			ChallengeID:   challengeID,
			ProposalID:    proposalID,
			TenantID:      tenantID,
			RaisedBy:      review.User.Login,
			ChallengeType: challengeType,
			Target:        fmt.Sprintf("PR #%d", pr.Number),
			Severity:      severity,
			Summary:       summary,
			Status:        model.ChallengeOpen,
			CreatedAt:     time.Now().UTC(),
		}

		if err := p.store.CreateChallenge(ctx, &challenge); err != nil {
			return fmt.Errorf("creating challenge from review: %w", err)
		}

		slog.InfoContext(ctx, "challenge created from review",
			"challenge_id", challengeID,
			"reviewer", review.User.Login,
			"severity", severity,
		)

		// Re-evaluate with the new challenge
		return p.evaluateProposal(ctx, proposalID, installID,
			repo.Owner.Login, repo.Name, pr.Head.SHA, "", pr.Number)

	case "approved":
		// Resolve all open challenges raised by this reviewer on this proposal
		challenges, err := p.store.ListOpenChallengesByProposal(ctx, proposalID)
		if err != nil {
			return fmt.Errorf("listing challenges: %w", err)
		}

		resolved := 0
		for i := range challenges {
			if challenges[i].RaisedBy == review.User.Login {
				note := "Reviewer approved the PR"
				if review.Body != "" {
					note = review.Body
				}
				if err := p.store.ResolveChallenge(ctx, challenges[i].ChallengeID, review.User.Login, note); err != nil {
					slog.WarnContext(ctx, "could not resolve challenge", "challenge_id", challenges[i].ChallengeID, "error", err)
				} else {
					resolved++
				}
			}
		}

		if resolved > 0 {
			slog.InfoContext(ctx, "challenges resolved by approval",
				"reviewer", review.User.Login,
				"resolved", resolved,
			)
			// Re-evaluate now that challenges are resolved
			return p.evaluateProposal(ctx, proposalID, installID,
				repo.Owner.Login, repo.Name, pr.Head.SHA, "", pr.Number)
		}

	case "commented":
		// Regular comments don't create challenges
		return nil
	}

	return nil
}

// parseSeverity extracts severity from review body text.
// Looks for "severity: high" or just keywords like "critical", "minor".
// Defaults to high for changes_requested reviews.
func buildCheckRunSummary(result engine.EvalResult, evidence []model.Evidence) string {
	var sb strings.Builder

	// Collect all issues by priority
	var critical []string // blocks merge
	var warnings []string // worth noting
	var info []string     // informational

	// Challenges (highest priority)
	for _, gr := range result.GateResults {
		if gr.Gate == "challenges" && gr.Status == engine.GateFailed {
			for _, r := range gr.Reasons {
				// Clean up the message
				msg := r
				msg = strings.TrimPrefix(msg, "unresolved high challenge: ")
				msg = strings.TrimPrefix(msg, "unresolved medium challenge: ")
				critical = append(critical, msg)
			}
		}
	}

	// Invariant violations
	for i := range evidence {
		if evidence[i].Source == "arbiter-invariant-checks" && evidence[i].Summary != nil &&
			evidence[i].Result == model.EvidenceFail {
			for _, part := range strings.Split(*evidence[i].Summary, "; ") {
				if part != "" {
					// Clean up: "[name] message" → just message
					if idx := strings.Index(part, "] "); idx > 0 {
						part = part[idx+2:]
					}
					critical = append(critical, part)
				}
			}
		}
	}

	// Scope/capability findings
	for i := range evidence {
		if evidence[i].Source == "arbiter-scope-analysis" && evidence[i].Summary != nil &&
			(evidence[i].Result == model.EvidenceWarn || evidence[i].Result == model.EvidenceFail) {
			for _, part := range strings.Split(*evidence[i].Summary, "; ") {
				if part == "" {
					continue
				}
				if strings.Contains(part, "process_execution") || strings.Contains(part, "eval_dynamic") {
					// Extract the readable part
					if idx := strings.Index(part, " — "); idx > 0 {
						critical = append(critical, part[idx+5:]) // skip " — "
					} else {
						critical = append(critical, part)
					}
				} else if strings.Contains(part, "new capability") {
					if idx := strings.Index(part, " — "); idx > 0 {
						warnings = append(warnings, part[idx+5:])
					} else {
						warnings = append(warnings, part)
					}
				}
			}
		}
	}

	// Coverage — summarize if many files
	var coverageFiles []string
	for i := range evidence {
		if evidence[i].Source == "arbiter-coverage-analysis" && evidence[i].Summary != nil &&
			evidence[i].Result == model.EvidenceWarn {
			for _, part := range strings.Split(*evidence[i].Summary, "; ") {
				if part != "" {
					part = strings.Replace(part, "code changed without test: ", "", 1)
					coverageFiles = append(coverageFiles, part)
				}
			}
		}
	}
	if len(coverageFiles) > 5 {
		warnings = append(warnings, fmt.Sprintf("%d code files changed with no tests", len(coverageFiles)))
	} else {
		for _, f := range coverageFiles {
			warnings = append(warnings, "No tests for "+f)
		}
	}

	// Diff analysis
	for i := range evidence {
		if evidence[i].Source == "arbiter-diff-analysis" && evidence[i].Summary != nil &&
			evidence[i].Result == model.EvidenceWarn {
			for _, part := range strings.Split(*evidence[i].Summary, "; ") {
				if part != "" && !strings.Contains(part, "code file(s) changed with no test") { // already in coverage
					info = append(info, part)
				}
			}
		}
	}

	// Deep analysis findings
	for i := range evidence {
		if evidence[i].Source == "arbiter-deep-analysis" && evidence[i].Summary != nil {
			for _, part := range strings.Split(*evidence[i].Summary, "; ") {
				if part == "" {
					continue
				}
				if evidence[i].Result == model.EvidenceFail {
					critical = append(critical, part)
				} else {
					warnings = append(warnings, part)
				}
			}
		}
	}

	// Dependency analysis
	for i := range evidence {
		if evidence[i].Source == "arbiter-dep-analysis" && evidence[i].Summary != nil {
			for _, part := range strings.Split(*evidence[i].Summary, "; ") {
				if part != "" {
					warnings = append(warnings, "📦 "+part)
				}
			}
		}
	}

	// Deduplicate critical list
	seen := make(map[string]bool)
	var dedupedCritical []string
	for _, c := range critical {
		if !seen[c] {
			seen[c] = true
			dedupedCritical = append(dedupedCritical, c)
		}
	}
	critical = dedupedCritical

	// Deduplicate — remove items that appear in both critical and warnings
	critSet := make(map[string]bool)
	for _, c := range critical {
		critSet[c] = true
	}
	var dedupedWarnings []string
	for _, w := range warnings {
		if !critSet[w] {
			dedupedWarnings = append(dedupedWarnings, w)
		}
	}
	warnings = dedupedWarnings

	// Build the output
	totalIssues := len(critical) + len(warnings)

	if result.Decision.Outcome == model.DecisionAccepted && totalIssues == 0 {
		sb.WriteString("✅ **All checks passed.** This PR is ready to merge.\n")
	} else if result.Decision.Outcome == model.DecisionAccepted && totalIssues > 0 {
		sb.WriteString(fmt.Sprintf("✅ **Approved** with %d note(s):\n\n", totalIssues))
		for _, w := range warnings {
			sb.WriteString(fmt.Sprintf("- ⚠️ %s\n", w))
		}
		for _, i := range info {
			sb.WriteString(fmt.Sprintf("- ℹ️ %s\n", i))
		}
	} else {
		sb.WriteString(fmt.Sprintf("❌ **%d issue(s) found:**\n\n", totalIssues))
		for _, c := range critical {
			sb.WriteString(fmt.Sprintf("- ❌ %s\n", c))
		}
		for _, w := range warnings {
			sb.WriteString(fmt.Sprintf("- ⚠️ %s\n", w))
		}
		if len(info) > 0 {
			sb.WriteString("\n")
			for _, i := range info {
				sb.WriteString(fmt.Sprintf("- ℹ️ %s\n", i))
			}
		}
	}

	// Gate summary as collapsed details (for people who want the raw data)
	sb.WriteString("\n<details>\n<summary>Gate details</summary>\n\n")
	sb.WriteString("| Gate | Status | Details |\n")
	sb.WriteString("|---|---|---|\n")
	for _, gr := range result.GateResults {
		icon := "✅"
		switch gr.Status {
		case engine.GateFailed:
			icon = "❌"
		case engine.GateWarned:
			icon = "⚠️"
		case engine.GateSkipped:
			icon = "⏭️"
		}
		details := ""
		if len(gr.Reasons) > 0 {
			details = strings.Join(gr.Reasons, "; ")
			if len(details) > 100 {
				details = details[:100] + "..."
			}
		}
		sb.WriteString(fmt.Sprintf("| %s %s | %s | %s |\n", icon, gr.Gate, gr.Status, details))
	}
	sb.WriteString("\n</details>\n")

	// Footer
	categories, patterns := PatternStats()
	sb.WriteString(fmt.Sprintf("\n---\n*Confidence: %.0f%% · Arbiter %s · %d patterns across %d categories*\n",
		result.Confidence*100, Version, patterns, categories))

	return sb.String()
}

func parseSeverity(body string) model.Severity {
	lower := strings.ToLower(body)
	switch {
	case containsAny(lower, "severity: low", "minor", "nit", "nitpick"):
		return model.SeverityLow
	case containsAny(lower, "severity: medium", "moderate"):
		return model.SeverityMedium
	default:
		return model.SeverityHigh // changes_requested is a strong signal
	}
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
