package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/openarbiter/arbiter/internal/config"
	"github.com/openarbiter/arbiter/internal/model"
)

// EvalContext contains all data needed to evaluate a Proposal.
// The engine is a pure function: EvalContext in, Decision out.
type EvalContext struct {
	Task       model.Task
	Proposal   model.Proposal
	Evidence   []model.Evidence
	Challenges []model.Challenge
	Config     config.Config
}

// GateResult is the outcome of a single gate evaluation.
type GateResult struct {
	Gate    string          `json:"gate"`
	Status  GateStatus      `json:"status"`
	Mode    config.GateMode `json:"mode"`
	Reasons []string        `json:"reasons,omitempty"`
}

// GateStatus is the outcome of evaluating a single gate.
type GateStatus string

const (
	GatePassed  GateStatus = "passed"
	GateFailed  GateStatus = "failed"
	GateWarned  GateStatus = "warned"
	GateSkipped GateStatus = "skipped"
)

// EvalResult contains the full evaluation output.
type EvalResult struct {
	Decision    model.Decision `json:"decision"`
	GateResults []GateResult   `json:"gate_results"`
}

// Evaluate runs all gates against the given context and produces a Decision.
// All gates run regardless of earlier failures — no short-circuiting.
func Evaluate(ctx *EvalContext) EvalResult {
	gates := []struct {
		name string
		mode config.GateMode
		fn   func(*EvalContext) GateResult
	}{
		{"mechanical", ctx.Config.Gates.Mechanical.Mode, evaluateMechanical},
		{"policy", ctx.Config.Gates.Policy.Mode, evaluatePolicy},
		{"behavioral", ctx.Config.Gates.Behavioral.Mode, evaluateBehavioral},
		{"challenges", ctx.Config.Gates.Challenges.Mode, evaluateChallenges},
		{"scope", ctx.Config.Gates.Scope.Mode, evaluateScope},
	}

	var results []GateResult
	for _, g := range gates {
		if g.mode == config.GateSkip {
			results = append(results, GateResult{
				Gate:   g.name,
				Status: GateSkipped,
				Mode:   g.mode,
			})
			continue
		}

		result := g.fn(ctx)
		result.Gate = g.name
		result.Mode = g.mode

		// If gate failed but mode is warn, downgrade to warned
		if result.Status == GateFailed && g.mode == config.GateWarn {
			result.Status = GateWarned
		}

		results = append(results, result)
	}

	decision := buildDecision(ctx, results)
	return EvalResult{
		Decision:    decision,
		GateResults: results,
	}
}

// --- Gate 1: Mechanical Checks ---

func evaluateMechanical(ctx *EvalContext) GateResult {
	requiredChecks := ctx.Config.Gates.Mechanical.Checks
	if len(requiredChecks) == 0 {
		return GateResult{Status: GatePassed}
	}

	passed := make(map[string]bool)
	failed := make(map[string]bool)
	for i := range ctx.Evidence {
		key := string(ctx.Evidence[i].EvidenceType)
		if ctx.Evidence[i].Result == model.EvidencePass {
			passed[key] = true
		} else if ctx.Evidence[i].Result == model.EvidenceFail {
			failed[key] = true
		}
	}

	var reasons []string
	for _, check := range requiredChecks {
		if failed[check] {
			reasons = append(reasons, fmt.Sprintf("%s failed", check))
		} else if !passed[check] {
			reasons = append(reasons, fmt.Sprintf("%s missing", check))
		}
	}

	if len(reasons) > 0 {
		return GateResult{Status: GateFailed, Reasons: reasons}
	}
	return GateResult{Status: GatePassed}
}

// --- Gate 2: Policy Checks ---

func evaluatePolicy(ctx *EvalContext) GateResult {
	var reasons []string
	for i := range ctx.Evidence {
		ev := &ctx.Evidence[i]
		if ev.EvidenceType == model.EvidencePolicyCheck && ev.Result == model.EvidenceFail {
			summary := "policy check failed"
			if ev.Summary != nil {
				summary = *ev.Summary
			}
			reasons = append(reasons, summary)
		}
	}

	if len(reasons) > 0 {
		return GateResult{Status: GateFailed, Reasons: reasons}
	}
	return GateResult{Status: GatePassed}
}

// --- Gate 3: Behavioral Evidence ---

func evaluateBehavioral(ctx *EvalContext) GateResult {
	minPassing := ctx.Config.Gates.Behavioral.MinPassingTests

	passingCount := 0
	for i := range ctx.Evidence {
		if ctx.Evidence[i].EvidenceType == model.EvidenceTestSuite && ctx.Evidence[i].Result == model.EvidencePass {
			passingCount++
		}
	}

	if passingCount < minPassing {
		return GateResult{
			Status:  GateFailed,
			Reasons: []string{fmt.Sprintf("need %d passing test suite(s), got %d", minPassing, passingCount)},
		}
	}
	return GateResult{Status: GatePassed}
}

// --- Gate 4: Challenges ---

func evaluateChallenges(ctx *EvalContext) GateResult {
	blockSeverity := ctx.Config.Gates.Challenges.BlockOnSeverity
	if blockSeverity == "" {
		blockSeverity = "high"
	}

	severityRank := map[string]int{"low": 1, "medium": 2, "high": 3}
	threshold := severityRank[blockSeverity]

	var reasons []string
	for i := range ctx.Challenges {
		ch := &ctx.Challenges[i]
		if ch.Status != model.ChallengeOpen {
			continue
		}
		rank := severityRank[string(ch.Severity)]
		if rank >= threshold {
			reasons = append(reasons, fmt.Sprintf("unresolved %s challenge: %s", ch.Severity, ch.Summary))
		}
	}

	if len(reasons) > 0 {
		return GateResult{Status: GateFailed, Reasons: reasons}
	}
	return GateResult{Status: GatePassed}
}

// --- Gate 5: Scope Validation ---

func evaluateScope(ctx *EvalContext) GateResult {
	for i := range ctx.Evidence {
		ev := &ctx.Evidence[i]
		if ev.EvidenceType == model.EvidenceScopeMatch {
			if ev.Result == model.EvidenceFail {
				summary := "scope mismatch detected"
				if ev.Summary != nil {
					summary = *ev.Summary
				}
				return GateResult{
					Status:  GateFailed,
					Reasons: []string{summary},
				}
			}
			return GateResult{Status: GatePassed}
		}
	}

	return GateResult{
		Status:  GatePassed,
		Reasons: []string{"no scope evidence available, skipping validation"},
	}
}

// --- Decision Builder ---

func buildDecision(ctx *EvalContext, results []GateResult) model.Decision {
	var failedGates []string
	var warnedGates []string
	var allReasons []string
	var linkedEvIDs []string

	for _, r := range results {
		switch r.Status {
		case GateFailed:
			failedGates = append(failedGates, r.Gate)
			allReasons = append(allReasons, r.Reasons...)
		case GateWarned:
			warnedGates = append(warnedGates, r.Gate)
		}
	}

	for i := range ctx.Evidence {
		linkedEvIDs = append(linkedEvIDs, ctx.Evidence[i].EvidenceID)
	}

	var linkedChIDs []string
	for i := range ctx.Challenges {
		linkedChIDs = append(linkedChIDs, ctx.Challenges[i].ChallengeID)
	}

	var outcome model.DecisionOutcome
	var reasonCode model.ReasonCode
	var summary string

	switch {
	case len(failedGates) > 0:
		outcome = model.DecisionRejected
		reasonCode = pickReasonCode(failedGates)
		summary = fmt.Sprintf("Blocked by: %s. %s", strings.Join(failedGates, ", "), strings.Join(allReasons, "; "))
	case len(warnedGates) > 0:
		outcome = model.DecisionAccepted
		reasonCode = model.ReasonAcceptableLowRisk
		summary = fmt.Sprintf("Accepted with warnings from: %s", strings.Join(warnedGates, ", "))
	default:
		outcome = model.DecisionAccepted
		reasonCode = model.ReasonAllGatesPassed
		summary = "All gates passed"
	}

	return model.Decision{
		ProposalID:         ctx.Proposal.ProposalID,
		TenantID:           ctx.Proposal.TenantID,
		Outcome:            outcome,
		ReasonCode:         reasonCode,
		Summary:            summary,
		DecidedAt:          time.Now().UTC(),
		DecidedBy:          "arbiter-engine",
		LinkedEvidenceIDs:  linkedEvIDs,
		LinkedChallengeIDs: linkedChIDs,
	}
}

func pickReasonCode(failedGates []string) model.ReasonCode {
	for _, gate := range failedGates {
		switch gate {
		case "mechanical":
			return model.ReasonMechanicalCheckFailed
		case "policy":
			return model.ReasonPolicyViolation
		case "challenges":
			return model.ReasonUnresolvedHighSeverityChallenge
		case "scope":
			return model.ReasonScopeExceeded
		case "behavioral":
			return model.ReasonInsufficientBehavioralEvidence
		}
	}
	return model.ReasonMechanicalCheckFailed
}
