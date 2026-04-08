package github

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/openarbiter/arbiter/internal/config"
	"github.com/openarbiter/arbiter/internal/model"
	"github.com/openarbiter/arbiter/internal/store"
)

func severityFromString(s string) model.Severity {
	switch s {
	case "low":
		return model.SeverityLow
	case "medium":
		return model.SeverityMedium
	case "high":
		return model.SeverityHigh
	default:
		return model.SeverityLow
	}
}

// defaultSeverityFor returns the default severity for a capability when no
// .arbiter.yml is present. Dangerous capabilities default to "high" (blocking)
// so that Arbiter is secure by default. Repos can lower severity in .arbiter.yml.
func defaultSeverityFor(capability string) string {
	switch capability {
	case "process_execution", "eval_dynamic", "container_escape",
		"build_time_execution", "prompt_injection", "hidden_characters":
		return "high"
	case "network_access", "file_system_write", "environment_access":
		return "medium"
	case "crypto_operations", "linter_suppression":
		return "warn"
	default:
		return "high" // unknown capabilities block by default
	}
}

// AutoReview analyzes diff insights, scope analysis, and coverage results
// to automatically generate challenges. Severity is configurable via .arbiter.yml.
// Default: warn (low severity). Repo owners can escalate to blocking.
func AutoReview(
	ctx context.Context,
	s store.Store,
	proposalID, tenantID string,
	diffInsights DiffInsights,
	scopeAnalysis *ScopeAnalysis,
	coverageAnalysis *CoverageAnalysis,
	invariantResults []InvariantResult,
	deepResult *DeepAnalysisResult,
	arCfg *config.AutoReviewConfig,
) {
	var challenges []model.Challenge

	// Capability-based challenges
	for i := range scopeAnalysis.NewCapabilities {
		cap := &scopeAnalysis.NewCapabilities[i]

		sevStr := arCfg.SeverityFor(cap.Name, defaultSeverityFor(cap.Name))
		if sevStr == "off" {
			continue
		}

		severity := severityFromString(sevStr)

		challenges = append(challenges, model.Challenge{
			ChallengeID:   fmt.Sprintf("ch:auto:%s:%s:%d", proposalID, cap.Name, time.Now().UnixNano()),
			ProposalID:    proposalID,
			TenantID:      tenantID,
			RaisedBy:      "arbiter-auto-review",
			ChallengeType: model.ChallengeHiddenBehaviorChange,
			Target:        cap.File,
			Severity:      severity,
			Summary:       fmt.Sprintf("New %s capability introduced: %s (matched: %s)", cap.Name, cap.Description, cap.Pattern),
			Status:        model.ChallengeOpen,
			CreatedAt:     time.Now().UTC(),
		})
	}

	// Tests deleted
	if diffInsights.TestsDeleted {
		sevStr := arCfg.SeverityFor("test_deletion", "high")
		if sevStr != "off" {
			challenges = append(challenges, model.Challenge{
				ChallengeID:   fmt.Sprintf("ch:auto:%s:tests-deleted:%d", proposalID, time.Now().UnixNano()),
				ProposalID:    proposalID,
				TenantID:      tenantID,
				RaisedBy:      "arbiter-auto-review",
				ChallengeType: model.ChallengeInsufficientTestCoverage,
				Target:        "test files",
				Severity:      severityFromString(sevStr),
				Summary:       "Test files were deleted in this PR — verify this is intentional and coverage is not reduced",
				Status:        model.ChallengeOpen,
				CreatedAt:     time.Now().UTC(),
			})
		}
	}

	// Many uncovered code files
	if len(coverageAnalysis.UncoveredCodeFiles) >= 3 {
		sevStr := arCfg.SeverityFor("low_coverage", "warn")
		if sevStr != "off" {
			challenges = append(challenges, model.Challenge{
				ChallengeID:   fmt.Sprintf("ch:auto:%s:low-coverage:%d", proposalID, time.Now().UnixNano()),
				ProposalID:    proposalID,
				TenantID:      tenantID,
				RaisedBy:      "arbiter-auto-review",
				ChallengeType: model.ChallengeInsufficientTestCoverage,
				Target:        "multiple files",
				Severity:      severityFromString(sevStr),
				Summary:       fmt.Sprintf("%d code files changed with no corresponding test changes", len(coverageAnalysis.UncoveredCodeFiles)),
				Status:        model.ChallengeOpen,
				CreatedAt:     time.Now().UTC(),
			})
		}
	}

	// CI config modified
	if diffInsights.CIModified {
		sevStr := arCfg.SeverityFor("ci_modification", "high")
		if sevStr != "off" {
			challenges = append(challenges, model.Challenge{
				ChallengeID:   fmt.Sprintf("ch:auto:%s:ci-modified:%d", proposalID, time.Now().UnixNano()),
				ProposalID:    proposalID,
				TenantID:      tenantID,
				RaisedBy:      "arbiter-auto-review",
				ChallengeType: model.ChallengePolicyViolation,
				Target:        "CI configuration",
				Severity:      severityFromString(sevStr),
				Summary:       "CI/workflow configuration was modified — verify the changes don't weaken the build pipeline",
				Status:        model.ChallengeOpen,
				CreatedAt:     time.Now().UTC(),
			})
		}
	}

	// Wide directory spread
	if scopeAnalysis.DirectorySpread > 5 {
		sevStr := arCfg.SeverityFor("scope_creep", "warn")
		if sevStr != "off" {
			challenges = append(challenges, model.Challenge{
				ChallengeID:   fmt.Sprintf("ch:auto:%s:scope-creep:%d", proposalID, time.Now().UnixNano()),
				ProposalID:    proposalID,
				TenantID:      tenantID,
				RaisedBy:      "arbiter-auto-review",
				ChallengeType: model.ChallengeScopeMismatch,
				Target:        "multiple directories",
				Severity:      severityFromString(sevStr),
				Summary:       fmt.Sprintf("Changes span %d directories — consider splitting this PR", scopeAnalysis.DirectorySpread),
				Status:        model.ChallengeOpen,
				CreatedAt:     time.Now().UTC(),
			})
		}
	}

	// High severity invariant failures
	for i := range invariantResults {
		if invariantResults[i].Passed || invariantResults[i].Severity != "high" {
			continue
		}
		challenges = append(challenges, model.Challenge{
			ChallengeID:   fmt.Sprintf("ch:auto:%s:invariant:%s:%d", proposalID, invariantResults[i].Name, time.Now().UnixNano()),
			ProposalID:    proposalID,
			TenantID:      tenantID,
			RaisedBy:      "arbiter-auto-review",
			ChallengeType: model.ChallengePolicyViolation,
			Target:        invariantResults[i].Name,
			Severity:      model.SeverityHigh,
			Summary:       invariantResults[i].Message,
			Status:        model.ChallengeOpen,
			CreatedAt:     time.Now().UTC(),
		})
	}

	// Deep analysis findings — suspicious targets, dangerous combos
	if deepResult != nil {
		for i := range deepResult.SuspiciousTargets {
			t := &deepResult.SuspiciousTargets[i]
			challenges = append(challenges, model.Challenge{
				ChallengeID:   fmt.Sprintf("ch:auto:%s:target:%d", proposalID, time.Now().UnixNano()),
				ProposalID:    proposalID,
				TenantID:      tenantID,
				RaisedBy:      "arbiter-auto-review",
				ChallengeType: model.ChallengeHiddenBehaviorChange,
				Target:        t.File,
				Severity:      model.SeverityHigh,
				Summary:       fmt.Sprintf("Suspicious target in %s:%d — %s (%s)", t.File, t.Line, t.Target, t.Reason),
				Status:        model.ChallengeOpen,
				CreatedAt:     time.Now().UTC(),
			})
		}
		for i := range deepResult.DangerousCombos {
			c := &deepResult.DangerousCombos[i]
			challenges = append(challenges, model.Challenge{
				ChallengeID:   fmt.Sprintf("ch:auto:%s:combo:%d", proposalID, time.Now().UnixNano()),
				ProposalID:    proposalID,
				TenantID:      tenantID,
				RaisedBy:      "arbiter-auto-review",
				ChallengeType: model.ChallengeHiddenBehaviorChange,
				Target:        c.File,
				Severity:      model.SeverityHigh,
				Summary:       fmt.Sprintf("Dangerous combination in %s — %s", c.File, c.Details),
				Status:        model.ChallengeOpen,
				CreatedAt:     time.Now().UTC(),
			})
		}
	}

	// Store challenges
	storeAutoReviewChallenges(ctx, s, proposalID, challenges)
}

// AutoReviewCorrelation generates challenges from cross-file correlation results.
func AutoReviewCorrelation(
	ctx context.Context,
	s store.Store,
	proposalID, tenantID string,
	corrResult CorrelationResult,
	arCfg *config.AutoReviewConfig,
) {
	var challenges []model.Challenge

	for i := range corrResult.Escalations {
		e := &corrResult.Escalations[i]
		if e.Severity == "info" {
			continue // don't create challenges for info-level findings
		}

		chalType := model.ChallengeHiddenBehaviorChange
		switch e.Rule {
		case "capability_plus_test_deletion":
			chalType = model.ChallengeInsufficientTestCoverage
		case "dep_plus_vendored":
			chalType = model.ChallengePolicyViolation
		}

		target := "cross-file"
		if len(e.Files) > 0 {
			target = e.Files[0]
		}

		challenges = append(challenges, model.Challenge{
			ChallengeID:   fmt.Sprintf("ch:corr:%s:%s:%d", proposalID, e.Rule, time.Now().UnixNano()),
			ProposalID:    proposalID,
			TenantID:      tenantID,
			RaisedBy:      "arbiter-auto-review",
			ChallengeType: chalType,
			Target:        target,
			Severity:      severityFromString(e.Severity),
			Summary:       fmt.Sprintf("[cross-file] %s", e.Message),
			Status:        model.ChallengeOpen,
			CreatedAt:     time.Now().UTC(),
		})
	}

	storeAutoReviewChallenges(ctx, s, proposalID, challenges)
}

func storeAutoReviewChallenges(ctx context.Context, s store.Store, proposalID string, challenges []model.Challenge) {
	for i := range challenges {
		existing, _ := s.ListOpenChallengesByProposal(ctx, proposalID)
		duplicate := false
		for j := range existing {
			if existing[j].RaisedBy == "arbiter-auto-review" && existing[j].Summary == challenges[i].Summary {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}

		if err := s.CreateChallenge(ctx, &challenges[i]); err != nil {
			slog.WarnContext(ctx, "auto-review: could not create challenge", "error", err)
		} else {
			slog.InfoContext(ctx, "auto-review: challenge created",
				"challenge_id", challenges[i].ChallengeID,
				"type", challenges[i].ChallengeType,
				"severity", challenges[i].Severity,
				"summary", challenges[i].Summary,
			)
		}
	}
}
