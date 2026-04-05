package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/openarbiter/arbiter/internal/config"
	"github.com/openarbiter/arbiter/internal/engine"
	"github.com/openarbiter/arbiter/internal/model"
	"github.com/openarbiter/arbiter/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	ctx := context.Background()
	cmd := os.Args[1]

	switch cmd {
	case "scenario":
		if len(os.Args) < 3 {
			fmt.Println("Usage: harness scenario <name>")
			fmt.Println("\nAvailable scenarios:")
			fmt.Println("  all-pass        — all evidence passes, no challenges")
			fmt.Println("  build-fails     — build check fails")
			fmt.Println("  test-fails      — test suite fails")
			fmt.Println("  no-evidence     — no evidence submitted")
			fmt.Println("  challenge       — open high-severity challenge blocks")
			fmt.Println("  challenge-resolved — challenge resolved, passes")
			fmt.Println("  warn-mode       — failing gate in warn mode still passes")
			fmt.Println("  scope-mismatch  — scope validation fails")
			fmt.Println("  mixed-signals   — some pass, some fail")
			fmt.Println("  all             — run all scenarios")
			os.Exit(1)
		}
		runScenarios(ctx, os.Args[2])

	case "live":
		runLive(ctx)

	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Arbiter Local Testing Harness")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  harness scenario <name>  — run a predefined scenario (no DB required)")
	fmt.Println("  harness live             — run against real Postgres (ARBITER_DB_URL required)")
}

// =============================================================================
// In-memory scenarios (no DB required)
// =============================================================================

type scenario struct {
	name       string
	evidence   []model.Evidence
	challenges []model.Challenge
	config     config.Config
	wantOutcome model.DecisionOutcome
}

var now = time.Now().UTC()

func baseTask() model.Task {
	return model.Task{
		TaskID:          "task-demo",
		TenantID:        "tenant-demo",
		Title:           "Fix authentication on mobile",
		Intent:          "Users on mobile cannot log in due to expired token handling",
		ExpectedOutcome: "Mobile users can log in successfully",
		NonGoals:        []string{"Do not change desktop login flow"},
		RiskLevel:       model.RiskMedium,
		ScopeHint:       model.Selector{Paths: []string{"auth/"}},
		PolicyProfile:   "default",
		CreatedAt:       now,
	}
}

func baseProposal() model.Proposal {
	return model.Proposal{
		ProposalID:  "prop-demo",
		TaskID:      "task-demo",
		TenantID:    "tenant-demo",
		SubmittedBy: "developer-1",
		ChangeRef: model.ExternalRef{
			RefType:    model.RefPullRequest,
			Provider:   model.ProviderGitHub,
			ExternalID: "42",
			URL:        "https://github.com/example/repo/pull/42",
		},
		DeclaredScope:   model.Selector{Paths: []string{"auth/login.go", "auth/token.go"}},
		BehaviorSummary: "Fix token refresh logic for mobile clients",
		Assumptions:     []string{"Token expiry is the root cause"},
		Confidence:      model.ConfidenceHigh,
		Status:          model.ProposalOpen,
		CreatedAt:       now,
	}
}

func ev(id string, evType model.EvidenceType, result model.EvidenceResult, summary string) model.Evidence {
	return model.Evidence{
		EvidenceID:   id,
		ProposalID:   "prop-demo",
		TenantID:     "tenant-demo",
		EvidenceType: evType,
		Subject:      string(evType),
		Result:       result,
		Confidence:   model.ConfidenceHigh,
		Source:       "github-actions",
		CreatedAt:    now,
		Summary:      &summary,
	}
}

func ch(id string, severity model.Severity, status model.ChallengeStatus, summary string) model.Challenge {
	return model.Challenge{
		ChallengeID:   id,
		ProposalID:    "prop-demo",
		TenantID:      "tenant-demo",
		RaisedBy:      "reviewer-1",
		ChallengeType: model.ChallengeScopeMismatch,
		Target:        "auth/session.go",
		Severity:      severity,
		Summary:       summary,
		Status:        status,
		CreatedAt:     now,
	}
}

func allScenarios() []scenario {
	defaultCfg := config.DefaultConfig()

	warnCfg := config.DefaultConfig()
	warnCfg.Gates.Mechanical.Mode = config.GateWarn
	warnCfg.Gates.Behavioral.Mode = config.GateWarn

	return []scenario{
		{
			name: "all-pass",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "All 47 tests passed"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionAccepted,
		},
		{
			name: "build-fails",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidenceFail, "Compilation error in auth/token.go:42"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionRejected,
		},
		{
			name: "test-fails",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidenceFail, "3 tests failed in auth/login_test.go"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionRejected,
		},
		{
			name:        "no-evidence",
			evidence:    nil,
			config:      defaultCfg,
			wantOutcome: model.DecisionRejected,
		},
		{
			name: "challenge",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
			},
			challenges: []model.Challenge{
				ch("ch-1", model.SeverityHigh, model.ChallengeOpen, "Changes modify session handling outside declared scope"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionRejected,
		},
		{
			name: "challenge-resolved",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
			},
			challenges: []model.Challenge{
				ch("ch-1", model.SeverityHigh, model.ChallengeResolved, "Scope issue addressed in latest push"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionAccepted,
		},
		{
			name:        "warn-mode",
			evidence:    nil, // no evidence — gates would fail
			config:      warnCfg,
			wantOutcome: model.DecisionAccepted,
		},
		{
			name: "scope-mismatch",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
				ev("ev-3", model.EvidenceScopeMatch, model.EvidenceFail, "Changed db/migrations.go outside declared scope"),
			},
			config:      func() config.Config { c := defaultCfg; c.Gates.Scope.Mode = config.GateEnforce; return c }(),
			wantOutcome: model.DecisionRejected,
		},
		{
			name: "mixed-signals",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Unit tests passed"),
				ev("ev-3", model.EvidenceTestSuite, model.EvidenceFail, "Integration tests failed"),
				ev("ev-4", model.EvidenceSecurityScan, model.EvidenceWarn, "1 low-severity finding"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionRejected, // build_check failed due to test_suite fail
		},
	}
}

func runScenarios(_ context.Context, name string) {
	scenarios := allScenarios()

	if name != "all" {
		found := false
		for i := range scenarios {
			if scenarios[i].name == name {
				scenarios = []scenario{scenarios[i]}
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("Unknown scenario: %s\n", name)
			os.Exit(1)
		}
	}

	passed := 0
	failed := 0

	for i := range scenarios {
		s := &scenarios[i]
		fmt.Printf("\n━━━ Scenario: %s ━━━\n", s.name)

		evalCtx := engine.EvalContext{
			Task:       baseTask(),
			Proposal:   baseProposal(),
			Evidence:   s.evidence,
			Challenges: s.challenges,
			Config:     s.config,
		}

		result := engine.Evaluate(&evalCtx)

		// Print gate results
		for _, gr := range result.GateResults {
			icon := "✓"
			switch gr.Status {
			case engine.GateFailed:
				icon = "✗"
			case engine.GateWarned:
				icon = "⚠"
			case engine.GateSkipped:
				icon = "−"
			}
			fmt.Printf("  %s %-12s [%s] %s\n", icon, gr.Gate, gr.Mode, gr.Status)
			for _, r := range gr.Reasons {
				fmt.Printf("    → %s\n", r)
			}
		}

		// Print decision
		fmt.Println()
		outcomeIcon := "✓"
		if result.Decision.Outcome == model.DecisionRejected {
			outcomeIcon = "✗"
		} else if result.Decision.Outcome == model.DecisionNeedsAction {
			outcomeIcon = "⚠"
		}
		fmt.Printf("  %s Decision: %s\n", outcomeIcon, result.Decision.Outcome)
		fmt.Printf("    Reason:  %s\n", result.Decision.ReasonCode)
		fmt.Printf("    Summary: %s\n", result.Decision.Summary)

		// Verify expected outcome
		if result.Decision.Outcome == s.wantOutcome {
			fmt.Printf("    Result:  PASS (expected %s)\n", s.wantOutcome)
			passed++
		} else {
			fmt.Printf("    Result:  FAIL (expected %s, got %s)\n", s.wantOutcome, result.Decision.Outcome)
			failed++
		}
	}

	fmt.Printf("\n━━━ Results: %d passed, %d failed ━━━\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// =============================================================================
// Live mode (against real Postgres)
// =============================================================================

func runLive(ctx context.Context) {
	dbURL := os.Getenv("ARBITER_DB_URL")
	if dbURL == "" {
		fmt.Println("ARBITER_DB_URL is required for live mode")
		os.Exit(1)
	}

	s, err := store.NewPgStore(ctx, dbURL)
	if err != nil {
		fmt.Printf("connecting to database: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	fmt.Println("Connected to database. Running live scenario...")

	// Create task
	task := baseTask()
	task.TaskID = fmt.Sprintf("task-live-%d", time.Now().UnixMilli())
	if err := s.CreateTask(ctx, &task); err != nil {
		fmt.Printf("creating task: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Created task: %s\n", task.TaskID)

	// Create proposal
	proposal := baseProposal()
	proposal.ProposalID = fmt.Sprintf("prop-live-%d", time.Now().UnixMilli())
	proposal.TaskID = task.TaskID
	if err := s.CreateProposal(ctx, &proposal); err != nil {
		fmt.Printf("creating proposal: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Created proposal: %s\n", proposal.ProposalID)

	// Add evidence
	evidence := []model.Evidence{
		ev("ev-live-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
		ev("ev-live-2", model.EvidenceTestSuite, model.EvidencePass, "All tests passed"),
	}
	for i := range evidence {
		evidence[i].EvidenceID = fmt.Sprintf("ev-live-%d-%d", time.Now().UnixMilli(), i)
		evidence[i].ProposalID = proposal.ProposalID
		if err := s.CreateEvidence(ctx, &evidence[i]); err != nil {
			fmt.Printf("creating evidence: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Added evidence: %s (%s: %s)\n", evidence[i].EvidenceID, evidence[i].EvidenceType, evidence[i].Result)
	}

	// Add a challenge and resolve it
	challenge := ch("ch-live-1", model.SeverityHigh, model.ChallengeOpen, "Scope concern")
	challenge.ChallengeID = fmt.Sprintf("ch-live-%d", time.Now().UnixMilli())
	challenge.ProposalID = proposal.ProposalID
	if err := s.CreateChallenge(ctx, &challenge); err != nil {
		fmt.Printf("creating challenge: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Created challenge: %s (severity: %s)\n", challenge.ChallengeID, challenge.Severity)

	if err := s.ResolveChallenge(ctx, challenge.ChallengeID, "developer-1", "Addressed in latest commit"); err != nil {
		fmt.Printf("resolving challenge: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Resolved challenge: %s\n", challenge.ChallengeID)

	// Gather data for evaluation
	storedEvidence, _ := s.ListEvidenceByProposal(ctx, proposal.ProposalID)
	storedChallenges, _ := s.ListChallengesByProposal(ctx, proposal.ProposalID)

	// Evaluate
	fmt.Println("\nRunning evaluation...")
	evalCtx := engine.EvalContext{
		Task:       task,
		Proposal:   proposal,
		Evidence:   storedEvidence,
		Challenges: storedChallenges,
		Config:     config.DefaultConfig(),
	}
	result := engine.Evaluate(&evalCtx)

	// Store decision
	decision := result.Decision
	decision.DecisionID = fmt.Sprintf("dec-live-%d", time.Now().UnixMilli())
	if err := s.CreateDecision(ctx, &decision); err != nil {
		fmt.Printf("creating decision: %v\n", err)
		os.Exit(1)
	}

	// Print results
	fmt.Println("\n━━━ Gate Results ━━━")
	for _, gr := range result.GateResults {
		icon := "✓"
		switch gr.Status {
		case engine.GateFailed:
			icon = "✗"
		case engine.GateWarned:
			icon = "⚠"
		case engine.GateSkipped:
			icon = "−"
		}
		fmt.Printf("  %s %-12s [%s] %s\n", icon, gr.Gate, gr.Mode, gr.Status)
		for _, r := range gr.Reasons {
			fmt.Printf("    → %s\n", r)
		}
	}

	fmt.Println("\n━━━ Decision ━━━")
	fmt.Printf("  ID:      %s\n", decision.DecisionID)
	fmt.Printf("  Outcome: %s\n", decision.Outcome)
	fmt.Printf("  Reason:  %s\n", decision.ReasonCode)
	fmt.Printf("  Summary: %s\n", decision.Summary)

	// Print full decision as JSON
	fmt.Println("\n━━━ Full Decision (JSON) ━━━")
	decJSON, _ := json.MarshalIndent(decision, "  ", "  ")
	fmt.Printf("  %s\n", decJSON)

	fmt.Printf("\nDecision stored in database: %s\n", decision.DecisionID)
}
