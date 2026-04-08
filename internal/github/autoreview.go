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
	arCfg *config.AutoReviewConfig,
) {
	var challenges []model.Challenge

	// Capability-based challenges
	for i := range scopeAnalysis.NewCapabilities {
		cap := &scopeAnalysis.NewCapabilities[i]

		sevStr := arCfg.SeverityFor(cap.Name, "warn")
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
		sevStr := arCfg.SeverityFor("test_deletion", "warn")
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
		sevStr := arCfg.SeverityFor("ci_modification", "warn")
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

	// Store challenges
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
