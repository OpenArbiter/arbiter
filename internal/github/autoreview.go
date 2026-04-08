package github

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/openarbiter/arbiter/internal/model"
	"github.com/openarbiter/arbiter/internal/store"
)

// AutoReview analyzes diff insights, scope analysis, and coverage results
// to automatically generate challenges. No human needed.
func AutoReview(
	ctx context.Context,
	s store.Store,
	proposalID, tenantID string,
	diffInsights DiffInsights,
	scopeAnalysis ScopeAnalysis,
	coverageAnalysis CoverageAnalysis,
	invariantResults []InvariantResult,
) {
	var challenges []model.Challenge

	// Process execution or dynamic eval → automatic high challenge
	for i := range scopeAnalysis.NewCapabilities {
		cap := &scopeAnalysis.NewCapabilities[i]
		if cap.Name == "process_execution" || cap.Name == "eval_dynamic" {
			challenges = append(challenges, model.Challenge{
				ChallengeID:   fmt.Sprintf("ch:auto:%s:%s:%d", proposalID, cap.Name, time.Now().UnixNano()),
				ProposalID:    proposalID,
				TenantID:      tenantID,
				RaisedBy:      "arbiter-auto-review",
				ChallengeType: model.ChallengeHiddenBehaviorChange,
				Target:        cap.File,
				Severity:      model.SeverityHigh,
				Summary:       fmt.Sprintf("New %s capability introduced: %s (matched: %s)", cap.Name, cap.Description, cap.Pattern),
				Status:        model.ChallengeOpen,
				CreatedAt:     time.Now().UTC(),
			})
		}
	}

	// Tests deleted → automatic medium challenge
	if diffInsights.TestsDeleted {
		challenges = append(challenges, model.Challenge{
			ChallengeID:   fmt.Sprintf("ch:auto:%s:tests-deleted:%d", proposalID, time.Now().UnixNano()),
			ProposalID:    proposalID,
			TenantID:      tenantID,
			RaisedBy:      "arbiter-auto-review",
			ChallengeType: model.ChallengeInsufficientTestCoverage,
			Target:        "test files",
			Severity:      model.SeverityMedium,
			Summary:       "Test files were deleted in this PR — verify this is intentional and coverage is not reduced",
			Status:        model.ChallengeOpen,
			CreatedAt:     time.Now().UTC(),
		})
	}

	// Many uncovered code files → automatic low challenge
	if len(coverageAnalysis.UncoveredCodeFiles) >= 3 {
		challenges = append(challenges, model.Challenge{
			ChallengeID:   fmt.Sprintf("ch:auto:%s:low-coverage:%d", proposalID, time.Now().UnixNano()),
			ProposalID:    proposalID,
			TenantID:      tenantID,
			RaisedBy:      "arbiter-auto-review",
			ChallengeType: model.ChallengeInsufficientTestCoverage,
			Target:        "multiple files",
			Severity:      model.SeverityLow,
			Summary:       fmt.Sprintf("%d code files changed with no corresponding test changes", len(coverageAnalysis.UncoveredCodeFiles)),
			Status:        model.ChallengeOpen,
			CreatedAt:     time.Now().UTC(),
		})
	}

	// CI config modified → automatic medium challenge
	if diffInsights.CIModified {
		challenges = append(challenges, model.Challenge{
			ChallengeID:   fmt.Sprintf("ch:auto:%s:ci-modified:%d", proposalID, time.Now().UnixNano()),
			ProposalID:    proposalID,
			TenantID:      tenantID,
			RaisedBy:      "arbiter-auto-review",
			ChallengeType: model.ChallengePolicyViolation,
			Target:        "CI configuration",
			Severity:      model.SeverityMedium,
			Summary:       "CI/workflow configuration was modified — verify the changes don't weaken the build pipeline",
			Status:        model.ChallengeOpen,
			CreatedAt:     time.Now().UTC(),
		})
	}

	// Wide directory spread → scope creep warning
	if scopeAnalysis.DirectorySpread > 5 {
		challenges = append(challenges, model.Challenge{
			ChallengeID:   fmt.Sprintf("ch:auto:%s:scope-creep:%d", proposalID, time.Now().UnixNano()),
			ProposalID:    proposalID,
			TenantID:      tenantID,
			RaisedBy:      "arbiter-auto-review",
			ChallengeType: model.ChallengeScopeMismatch,
			Target:        "multiple directories",
			Severity:      model.SeverityLow,
			Summary:       fmt.Sprintf("Changes span %d directories — consider splitting this PR", scopeAnalysis.DirectorySpread),
			Status:        model.ChallengeOpen,
			CreatedAt:     time.Now().UTC(),
		})
	}

	// High severity invariant failures → automatic challenge
	for i := range invariantResults {
		if invariantResults[i].Passed {
			continue
		}
		if invariantResults[i].Severity != "high" {
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
		// Idempotency — check if a similar auto-challenge already exists
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
