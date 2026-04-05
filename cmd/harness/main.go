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
			fmt.Println("Usage: harness scenario <name|all>")
			fmt.Println("\nRun 'harness scenario all' to run all scenarios.")
			fmt.Println("Or specify a scenario name. Available scenarios:")
			for i := range allScenarios() {
				s := &allScenarios()[i]
				fmt.Printf("  %s\n", s.name)
			}
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

	allSkipCfg := config.DefaultConfig()
	allSkipCfg.Gates.Mechanical.Mode = config.GateSkip
	allSkipCfg.Gates.Policy.Mode = config.GateSkip
	allSkipCfg.Gates.Behavioral.Mode = config.GateSkip
	allSkipCfg.Gates.Challenges.Mode = config.GateSkip
	allSkipCfg.Gates.Scope.Mode = config.GateSkip

	allWarnCfg := config.DefaultConfig()
	allWarnCfg.Gates.Mechanical.Mode = config.GateWarn
	allWarnCfg.Gates.Policy.Mode = config.GateWarn
	allWarnCfg.Gates.Behavioral.Mode = config.GateWarn
	allWarnCfg.Gates.Challenges.Mode = config.GateWarn
	allWarnCfg.Gates.Scope.Mode = config.GateWarn

	scopeEnforceCfg := config.DefaultConfig()
	scopeEnforceCfg.Gates.Scope.Mode = config.GateEnforce

	blockMediumCfg := config.DefaultConfig()
	blockMediumCfg.Gates.Challenges.BlockOnSeverity = "medium"

	blockLowCfg := config.DefaultConfig()
	blockLowCfg.Gates.Challenges.BlockOnSeverity = "low"

	threeTestsCfg := config.DefaultConfig()
	threeTestsCfg.Gates.Behavioral.MinPassingTests = 3

	noChecksCfg := config.DefaultConfig()
	noChecksCfg.Gates.Mechanical.Checks = nil

	return []scenario{
		// =====================================================================
		// Basic happy/sad paths
		// =====================================================================
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

		// =====================================================================
		// Challenge scenarios
		// =====================================================================
		{
			name: "challenge-open-high-blocks",
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
			name: "challenge-open-medium-passes-default",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
			},
			challenges: []model.Challenge{
				ch("ch-1", model.SeverityMedium, model.ChallengeOpen, "Could use better error handling"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionAccepted, // default blocks on high only
		},
		{
			name: "challenge-open-medium-blocks-when-configured",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
			},
			challenges: []model.Challenge{
				ch("ch-1", model.SeverityMedium, model.ChallengeOpen, "Missing error handling"),
			},
			config:      blockMediumCfg,
			wantOutcome: model.DecisionRejected,
		},
		{
			name: "challenge-open-low-blocks-when-configured",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
			},
			challenges: []model.Challenge{
				ch("ch-1", model.SeverityLow, model.ChallengeOpen, "Minor style issue"),
			},
			config:      blockLowCfg,
			wantOutcome: model.DecisionRejected,
		},
		{
			name: "challenge-resolved-passes",
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
			name: "challenge-dismissed-passes",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
			},
			challenges: []model.Challenge{
				ch("ch-1", model.SeverityHigh, model.ChallengeDismissed, "Not relevant to this change"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionAccepted,
		},
		{
			name: "multiple-challenges-one-open",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
			},
			challenges: []model.Challenge{
				ch("ch-1", model.SeverityHigh, model.ChallengeResolved, "First issue fixed"),
				ch("ch-2", model.SeverityHigh, model.ChallengeOpen, "Second issue still open"),
				ch("ch-3", model.SeverityLow, model.ChallengeDismissed, "Not relevant"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionRejected,
		},

		// =====================================================================
		// Gate mode behavior
		// =====================================================================
		{
			name:        "all-gates-skipped",
			evidence:    nil,
			config:      allSkipCfg,
			wantOutcome: model.DecisionAccepted,
		},
		{
			name:        "all-gates-warn-no-evidence",
			evidence:    nil,
			config:      allWarnCfg,
			wantOutcome: model.DecisionAccepted,
		},
		{
			name: "warn-mode-with-failures",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidenceFail, "Build failed"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidenceFail, "Tests failed"),
				ev("ev-3", model.EvidencePolicyCheck, model.EvidenceFail, "Policy violated"),
				ev("ev-4", model.EvidenceScopeMatch, model.EvidenceFail, "Scope exceeded"),
			},
			challenges: []model.Challenge{
				ch("ch-1", model.SeverityHigh, model.ChallengeOpen, "Everything is wrong"),
			},
			config:      allWarnCfg,
			wantOutcome: model.DecisionAccepted, // all warn mode — nothing blocks
		},
		{
			name: "one-enforce-gate-fails-rest-warn",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidenceFail, "Build failed"),
			},
			config: func() config.Config {
				c := allWarnCfg
				c.Gates.Mechanical.Mode = config.GateEnforce // only this one enforces
				return c
			}(),
			wantOutcome: model.DecisionRejected,
		},

		// =====================================================================
		// Scope validation
		// =====================================================================
		{
			name: "scope-mismatch-enforced",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
				ev("ev-3", model.EvidenceScopeMatch, model.EvidenceFail, "Changed db/migrations.go outside declared scope"),
			},
			config:      scopeEnforceCfg,
			wantOutcome: model.DecisionRejected,
		},
		{
			name: "scope-mismatch-warn-only",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
				ev("ev-3", model.EvidenceScopeMatch, model.EvidenceFail, "Changed db/migrations.go outside declared scope"),
			},
			config:      defaultCfg, // scope is warn by default
			wantOutcome: model.DecisionAccepted,
		},
		{
			name: "scope-passes-enforced",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
				ev("ev-3", model.EvidenceScopeMatch, model.EvidencePass, "All changes within declared scope"),
			},
			config:      scopeEnforceCfg,
			wantOutcome: model.DecisionAccepted,
		},
		{
			name: "no-scope-evidence-enforced",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
			},
			config:      scopeEnforceCfg,
			wantOutcome: model.DecisionAccepted, // no scope evidence = pass (can't validate)
		},

		// =====================================================================
		// Policy scenarios
		// =====================================================================
		{
			name: "policy-violation",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
				ev("ev-3", model.EvidencePolicyCheck, model.EvidenceFail, "Blocked dependency: lodash@3.x has known CVE"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionRejected,
		},
		{
			name: "multiple-policy-violations",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
				ev("ev-3", model.EvidencePolicyCheck, model.EvidenceFail, "Blocked dependency"),
				ev("ev-4", model.EvidencePolicyCheck, model.EvidenceFail, "Direct push to protected path"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionRejected,
		},
		{
			name: "policy-passes",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
				ev("ev-3", model.EvidencePolicyCheck, model.EvidencePass, "All policies satisfied"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionAccepted,
		},

		// =====================================================================
		// Behavioral evidence thresholds
		// =====================================================================
		{
			name: "high-test-threshold-not-met",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Unit tests passed"),
				ev("ev-3", model.EvidenceTestSuite, model.EvidencePass, "Integration tests passed"),
			},
			config:      threeTestsCfg, // needs 3, only has 2
			wantOutcome: model.DecisionRejected,
		},
		{
			name: "high-test-threshold-met",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Unit tests passed"),
				ev("ev-3", model.EvidenceTestSuite, model.EvidencePass, "Integration tests passed"),
				ev("ev-4", model.EvidenceTestSuite, model.EvidencePass, "E2E tests passed"),
			},
			config:      threeTestsCfg,
			wantOutcome: model.DecisionAccepted,
		},
		{
			name: "failing-tests-dont-count-toward-threshold",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Unit tests passed"),
				ev("ev-3", model.EvidenceTestSuite, model.EvidenceFail, "Integration tests failed"),
				ev("ev-4", model.EvidenceTestSuite, model.EvidencePass, "E2E tests passed"),
				ev("ev-5", model.EvidenceTestSuite, model.EvidenceFail, "Smoke tests failed"),
			},
			config:      threeTestsCfg, // needs 3 passing, has 2 pass + 2 fail
			wantOutcome: model.DecisionRejected,
		},

		// =====================================================================
		// Mixed/complex scenarios
		// =====================================================================
		{
			name: "everything-passes-with-rich-evidence",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "All 142 tests passed"),
				ev("ev-3", model.EvidencePolicyCheck, model.EvidencePass, "All policies satisfied"),
				ev("ev-4", model.EvidenceSecurityScan, model.EvidencePass, "No vulnerabilities found"),
				ev("ev-5", model.EvidenceScopeMatch, model.EvidencePass, "All changes within scope"),
				ev("ev-6", model.EvidenceBenchmarkCheck, model.EvidencePass, "No performance regression"),
			},
			config:      scopeEnforceCfg,
			wantOutcome: model.DecisionAccepted,
		},
		{
			name: "mixed-signals-build-pass-tests-mixed",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Unit tests passed"),
				ev("ev-3", model.EvidenceTestSuite, model.EvidenceFail, "Integration tests failed"),
				ev("ev-4", model.EvidenceSecurityScan, model.EvidenceWarn, "1 low-severity finding"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionRejected, // test_suite has a fail → mechanical gate fails
		},
		{
			name: "only-info-and-warn-evidence",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidenceInfo, "Build info only"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidenceWarn, "Tests warn only"),
				ev("ev-3", model.EvidenceSecurityScan, model.EvidenceInfo, "Scan info"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionRejected, // info/warn don't satisfy gates
		},
		{
			name: "no-mechanical-checks-configured",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
			},
			config:      noChecksCfg,
			wantOutcome: model.DecisionAccepted, // no checks required → mechanical passes
		},
		{
			name: "security-scan-fails-but-not-gated",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests passed"),
				ev("ev-3", model.EvidenceSecurityScan, model.EvidenceFail, "3 critical vulnerabilities"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionAccepted, // security scan isn't a gate (yet)
		},
		{
			name: "everything-wrong-all-gates-fail",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidenceFail, "Build failed"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidenceFail, "All tests failed"),
				ev("ev-3", model.EvidencePolicyCheck, model.EvidenceFail, "Policy violated"),
				ev("ev-4", model.EvidenceScopeMatch, model.EvidenceFail, "Scope exceeded"),
			},
			challenges: []model.Challenge{
				ch("ch-1", model.SeverityHigh, model.ChallengeOpen, "Complete rewrite not requested"),
				ch("ch-2", model.SeverityHigh, model.ChallengeOpen, "Tests deleted"),
			},
			config:      scopeEnforceCfg,
			wantOutcome: model.DecisionRejected,
		},

		// =====================================================================
		// Agent-specific scenarios
		// =====================================================================
		{
			name: "agent-submits-passing-work",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Agent-written tests pass"),
				ev("ev-3", model.EvidenceTestSuite, model.EvidencePass, "Existing tests still pass"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionAccepted,
		},
		{
			name: "agent-submits-but-human-challenges",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests pass"),
			},
			challenges: []model.Challenge{
				ch("ch-1", model.SeverityHigh, model.ChallengeOpen, "Agent deleted error handling to make tests pass"),
			},
			config:      defaultCfg,
			wantOutcome: model.DecisionRejected,
		},
		{
			name: "agent-submits-with-scope-creep",
			evidence: []model.Evidence{
				ev("ev-1", model.EvidenceBuildCheck, model.EvidencePass, "Build succeeded"),
				ev("ev-2", model.EvidenceTestSuite, model.EvidencePass, "Tests pass"),
				ev("ev-3", model.EvidenceScopeMatch, model.EvidenceFail, "Agent modified 47 files outside task scope"),
			},
			config:      scopeEnforceCfg,
			wantOutcome: model.DecisionRejected,
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
		fmt.Printf("  %s Decision: %s (confidence: %.0f%%)\n", outcomeIcon, result.Decision.Outcome, result.Confidence*100)
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
	fmt.Printf("  Outcome: %s (confidence: %.0f%%)\n", decision.Outcome, result.Confidence*100)
	fmt.Printf("  Reason:  %s\n", decision.ReasonCode)
	fmt.Printf("  Summary: %s\n", decision.Summary)

	// Print full decision as JSON
	fmt.Println("\n━━━ Full Decision (JSON) ━━━")
	decJSON, _ := json.MarshalIndent(decision, "  ", "  ")
	fmt.Printf("  %s\n", decJSON)

	fmt.Printf("\nDecision stored in database: %s\n", decision.DecisionID)
}
