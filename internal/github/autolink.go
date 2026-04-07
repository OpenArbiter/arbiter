package github

import (
	"context"
	"log/slog"
	"strings"

	"github.com/openarbiter/arbiter/internal/model"
	"github.com/openarbiter/arbiter/internal/store"
)

// AutoLinkChallenges finds relevant evidence for each challenge and updates
// the challenge's LinkedEvidenceIDs. Called after evidence is created or
// after challenges are created.
func AutoLinkChallenges(ctx context.Context, s store.Store, proposalID string) {
	challenges, err := s.ListOpenChallengesByProposal(ctx, proposalID)
	if err != nil {
		slog.WarnContext(ctx, "autolink: could not list challenges", "error", err)
		return
	}
	if len(challenges) == 0 {
		return
	}

	evidence, err := s.ListEvidenceByProposal(ctx, proposalID)
	if err != nil {
		slog.WarnContext(ctx, "autolink: could not list evidence", "error", err)
		return
	}
	if len(evidence) == 0 {
		return
	}

	for i := range challenges {
		ch := &challenges[i]
		if len(ch.LinkedEvidenceIDs) > 0 {
			continue // already linked
		}

		linked := findRelevantEvidence(ch, evidence)
		if len(linked) > 0 {
			ch.LinkedEvidenceIDs = linked
			if err := s.UpdateChallengeLinks(ctx, ch.ChallengeID, linked); err != nil {
				slog.WarnContext(ctx, "autolink: could not update challenge",
					"challenge_id", ch.ChallengeID, "error", err)
			} else {
				slog.InfoContext(ctx, "autolinked challenge to evidence",
					"challenge_id", ch.ChallengeID,
					"evidence_count", len(linked),
				)
			}
		}
	}
}

// findRelevantEvidence matches a challenge to evidence by type and content.
func findRelevantEvidence(ch *model.Challenge, evidence []model.Evidence) []string {
	var linked []string

	for i := range evidence {
		ev := &evidence[i]
		if matchesChallengeToEvidence(ch, ev) {
			linked = append(linked, ev.EvidenceID)
		}
	}

	return linked
}

// matchesChallengeToEvidence determines if evidence is relevant to a challenge.
func matchesChallengeToEvidence(ch *model.Challenge, ev *model.Evidence) bool {
	// Match by challenge type → evidence type
	switch ch.ChallengeType {
	case model.ChallengeInsufficientTestCoverage:
		if ev.EvidenceType == model.EvidenceTestSuite || ev.Source == "arbiter-coverage-analysis" {
			return true
		}
	case model.ChallengeScopeMismatch:
		if ev.EvidenceType == model.EvidenceScopeMatch || ev.Source == "arbiter-scope-analysis" {
			return true
		}
	case model.ChallengePolicyViolation:
		if ev.EvidenceType == model.EvidencePolicyCheck || ev.Source == "arbiter-invariant-checks" {
			return true
		}
	case model.ChallengeLikelyRegression:
		if ev.EvidenceType == model.EvidenceTestSuite && ev.Result == model.EvidenceFail {
			return true
		}
	}

	// Match by target file — if the challenge targets a specific file,
	// link evidence that mentions that file
	if ch.Target != "" && !strings.HasPrefix(ch.Target, "PR #") {
		if ev.Summary != nil && strings.Contains(*ev.Summary, ch.Target) {
			return true
		}
		if ev.Subject == ch.Target {
			return true
		}
	}

	// Match by summary keyword overlap
	if ev.Summary != nil && ev.Result != model.EvidencePass {
		chWords := significantWords(ch.Summary)
		evWords := significantWords(*ev.Summary)
		if wordOverlap(chWords, evWords) >= 2 {
			return true
		}
	}

	return false
}

// significantWords extracts meaningful words from text (>3 chars, lowercase).
func significantWords(text string) map[string]bool {
	words := make(map[string]bool)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		// Strip punctuation
		word = strings.Trim(word, ".,;:!?\"'()[]{}*")
		if len(word) > 3 {
			words[word] = true
		}
	}
	return words
}

// wordOverlap counts how many words appear in both sets.
func wordOverlap(a, b map[string]bool) int {
	count := 0
	for word := range a {
		if b[word] {
			count++
		}
	}
	return count
}
