package engine

import (
	"testing"
	"time"

	"github.com/openarbiter/arbiter/internal/config"
	"github.com/openarbiter/arbiter/internal/model"
)

var testTime = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

func defaultCtx() EvalContext {
	return EvalContext{
		Task: model.Task{
			TaskID:          "task-1",
			TenantID:        "tenant-1",
			Title:           "Fix auth",
			Intent:          "Fix mobile auth",
			ExpectedOutcome: "Users can log in",
			RiskLevel:       model.RiskMedium,
			PolicyProfile:   "default",
			CreatedAt:       testTime,
		},
		Proposal: model.Proposal{
			ProposalID:      "prop-1",
			TaskID:          "task-1",
			TenantID:        "tenant-1",
			SubmittedBy:     "agent-1",
			ChangeRef:       model.ExternalRef{RefType: model.RefPullRequest, Provider: model.ProviderGitHub, ExternalID: "42"},
			BehaviorSummary: "Fix token refresh",
			Confidence:      model.ConfidenceHigh,
			Status:          model.ProposalOpen,
			CreatedAt:       testTime,
		},
		Config: config.DefaultConfig(),
	}
}

func makeEvidence(id string, evType model.EvidenceType, result model.EvidenceResult) model.Evidence {
	return model.Evidence{
		EvidenceID:   id,
		ProposalID:   "prop-1",
		TenantID:     "tenant-1",
		EvidenceType: evType,
		Subject:      "test",
		Result:       result,
		Confidence:   model.ConfidenceHigh,
		Source:       "ci",
		CreatedAt:    testTime,
	}
}

func makeEvidenceWithSummary(id string, evType model.EvidenceType, result model.EvidenceResult, summary string) model.Evidence {
	ev := makeEvidence(id, evType, result)
	ev.Summary = &summary
	return ev
}

func makeChallenge(id string, severity model.Severity, status model.ChallengeStatus) model.Challenge {
	return model.Challenge{
		ChallengeID:   id,
		ProposalID:    "prop-1",
		TenantID:      "tenant-1",
		RaisedBy:      "reviewer-1",
		ChallengeType: model.ChallengeScopeMismatch,
		Target:        "src/auth.go",
		Severity:      severity,
		Summary:       "Scope issue for " + id,
		Status:        status,
		CreatedAt:     testTime,
	}
}

// =============================================================================
// Full pipeline tests
// =============================================================================

func TestEvaluate_AllPassing(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
	}

	result := Evaluate(&ctx)

	if result.Decision.Outcome != model.DecisionAccepted {
		t.Errorf("outcome = %q, want accepted", result.Decision.Outcome)
	}
	if result.Decision.ReasonCode != model.ReasonAllGatesPassed {
		t.Errorf("reason = %q, want all_gates_passed", result.Decision.ReasonCode)
	}
	if len(result.GateResults) != 5 {
		t.Errorf("gate results = %d, want 5", len(result.GateResults))
	}
}

func TestEvaluate_AllFailing(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidenceFail),
		makeEvidenceWithSummary("ev-2", model.EvidencePolicyCheck, model.EvidenceFail, "blocked dependency"),
	}
	ctx.Challenges = []model.Challenge{
		makeChallenge("ch-1", model.SeverityHigh, model.ChallengeOpen),
	}

	result := Evaluate(&ctx)

	if result.Decision.Outcome != model.DecisionRejected {
		t.Errorf("outcome = %q, want rejected", result.Decision.Outcome)
	}
	// Should have multiple failed gates
	failCount := 0
	for _, gr := range result.GateResults {
		if gr.Status == GateFailed {
			failCount++
		}
	}
	if failCount < 3 {
		t.Errorf("failed gates = %d, want >= 3", failCount)
	}
}

func TestEvaluate_EmptyEvidence(t *testing.T) {
	ctx := defaultCtx()
	// No evidence at all

	result := Evaluate(&ctx)

	if result.Decision.Outcome != model.DecisionRejected {
		t.Errorf("outcome = %q, want rejected (no evidence)", result.Decision.Outcome)
	}
}

func TestEvaluate_DecisionLinksEvidenceAndChallenges(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
	}
	ctx.Challenges = []model.Challenge{
		makeChallenge("ch-1", model.SeverityLow, model.ChallengeResolved),
	}

	result := Evaluate(&ctx)

	if len(result.Decision.LinkedEvidenceIDs) != 2 {
		t.Errorf("linked evidence = %d, want 2", len(result.Decision.LinkedEvidenceIDs))
	}
	if len(result.Decision.LinkedChallengeIDs) != 1 {
		t.Errorf("linked challenges = %d, want 1", len(result.Decision.LinkedChallengeIDs))
	}
}

func TestEvaluate_DecisionMetadata(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
	}

	result := Evaluate(&ctx)

	if result.Decision.ProposalID != "prop-1" {
		t.Errorf("ProposalID = %q, want prop-1", result.Decision.ProposalID)
	}
	if result.Decision.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want tenant-1", result.Decision.TenantID)
	}
	if result.Decision.DecidedBy != "arbiter-engine" {
		t.Errorf("DecidedBy = %q, want arbiter-engine", result.Decision.DecidedBy)
	}
	if result.Decision.DecidedAt.IsZero() {
		t.Error("DecidedAt should not be zero")
	}
}

// =============================================================================
// Gate 1: Mechanical Checks
// =============================================================================

func TestGate_Mechanical_AllPass(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
	}

	result := evaluateMechanical(&ctx)
	if result.Status != GatePassed {
		t.Errorf("status = %q, want passed", result.Status)
	}
}

func TestGate_Mechanical_BuildFails(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidenceFail),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
	}

	result := evaluateMechanical(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed", result.Status)
	}
	if len(result.Reasons) != 1 {
		t.Errorf("reasons = %d, want 1", len(result.Reasons))
	}
}

func TestGate_Mechanical_MissingEvidence(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		// test_suite missing
	}

	result := evaluateMechanical(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed (missing test_suite)", result.Status)
	}
}

func TestGate_Mechanical_NoChecksConfigured(t *testing.T) {
	ctx := defaultCtx()
	ctx.Config.Gates.Mechanical.Checks = nil

	result := evaluateMechanical(&ctx)
	if result.Status != GatePassed {
		t.Errorf("status = %q, want passed (no checks required)", result.Status)
	}
}

func TestGate_Mechanical_WarnAndInfoIgnored(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidenceWarn),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidenceInfo),
	}

	result := evaluateMechanical(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed (warn/info don't count as pass)", result.Status)
	}
}

func TestGate_Mechanical_MultipleEvidenceSameType(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidenceFail),
		makeEvidence("ev-2", model.EvidenceBuildCheck, model.EvidencePass), // later run passed
		makeEvidence("ev-3", model.EvidenceTestSuite, model.EvidencePass),
	}

	result := evaluateMechanical(&ctx)
	// Both pass and fail exist for build_check — fail takes priority
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed (fail evidence exists)", result.Status)
	}
}

// =============================================================================
// Gate 2: Policy Checks
// =============================================================================

func TestGate_Policy_NoPolicyEvidence(t *testing.T) {
	ctx := defaultCtx()
	result := evaluatePolicy(&ctx)
	if result.Status != GatePassed {
		t.Errorf("status = %q, want passed (no policy evidence)", result.Status)
	}
}

func TestGate_Policy_PolicyPasses(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidencePolicyCheck, model.EvidencePass),
	}

	result := evaluatePolicy(&ctx)
	if result.Status != GatePassed {
		t.Errorf("status = %q, want passed", result.Status)
	}
}

func TestGate_Policy_PolicyFails(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidenceWithSummary("ev-1", model.EvidencePolicyCheck, model.EvidenceFail, "blocked dependency detected"),
	}

	result := evaluatePolicy(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed", result.Status)
	}
	if result.Reasons[0] != "blocked dependency detected" {
		t.Errorf("reason = %q, want 'blocked dependency detected'", result.Reasons[0])
	}
}

func TestGate_Policy_MultiplePolicyFailures(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidenceWithSummary("ev-1", model.EvidencePolicyCheck, model.EvidenceFail, "issue 1"),
		makeEvidenceWithSummary("ev-2", model.EvidencePolicyCheck, model.EvidenceFail, "issue 2"),
	}

	result := evaluatePolicy(&ctx)
	if len(result.Reasons) != 2 {
		t.Errorf("reasons = %d, want 2", len(result.Reasons))
	}
}

// =============================================================================
// Gate 3: Behavioral Evidence
// =============================================================================

func TestGate_Behavioral_Passes(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceTestSuite, model.EvidencePass),
	}

	result := evaluateBehavioral(&ctx)
	if result.Status != GatePassed {
		t.Errorf("status = %q, want passed", result.Status)
	}
}

func TestGate_Behavioral_NoTests(t *testing.T) {
	ctx := defaultCtx()

	result := evaluateBehavioral(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed (no tests)", result.Status)
	}
}

func TestGate_Behavioral_FailingTestsDontCount(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceTestSuite, model.EvidenceFail),
	}

	result := evaluateBehavioral(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed (failing tests don't count)", result.Status)
	}
}

func TestGate_Behavioral_HigherThreshold(t *testing.T) {
	ctx := defaultCtx()
	ctx.Config.Gates.Behavioral.MinPassingTests = 3
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceTestSuite, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
	}

	result := evaluateBehavioral(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed (need 3, got 2)", result.Status)
	}
}

func TestGate_Behavioral_ZeroThreshold(t *testing.T) {
	ctx := defaultCtx()
	ctx.Config.Gates.Behavioral.MinPassingTests = 0

	result := evaluateBehavioral(&ctx)
	if result.Status != GatePassed {
		t.Errorf("status = %q, want passed (zero threshold)", result.Status)
	}
}

func TestGate_Behavioral_OtherEvidenceIgnored(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceSecurityScan, model.EvidencePass),
	}

	result := evaluateBehavioral(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed (non-test evidence doesn't count)", result.Status)
	}
}

// =============================================================================
// Gate 4: Challenges
// =============================================================================

func TestGate_Challenges_NoChallenges(t *testing.T) {
	ctx := defaultCtx()
	result := evaluateChallenges(&ctx)
	if result.Status != GatePassed {
		t.Errorf("status = %q, want passed", result.Status)
	}
}

func TestGate_Challenges_OpenHighBlocks(t *testing.T) {
	ctx := defaultCtx()
	ctx.Challenges = []model.Challenge{
		makeChallenge("ch-1", model.SeverityHigh, model.ChallengeOpen),
	}

	result := evaluateChallenges(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed", result.Status)
	}
}

func TestGate_Challenges_OpenMediumDoesNotBlockByDefault(t *testing.T) {
	ctx := defaultCtx()
	ctx.Challenges = []model.Challenge{
		makeChallenge("ch-1", model.SeverityMedium, model.ChallengeOpen),
	}

	result := evaluateChallenges(&ctx)
	if result.Status != GatePassed {
		t.Errorf("status = %q, want passed (default blocks on high only)", result.Status)
	}
}

func TestGate_Challenges_BlockOnMediumConfig(t *testing.T) {
	ctx := defaultCtx()
	ctx.Config.Gates.Challenges.BlockOnSeverity = "medium"
	ctx.Challenges = []model.Challenge{
		makeChallenge("ch-1", model.SeverityMedium, model.ChallengeOpen),
	}

	result := evaluateChallenges(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed (block on medium)", result.Status)
	}
}

func TestGate_Challenges_BlockOnLowConfig(t *testing.T) {
	ctx := defaultCtx()
	ctx.Config.Gates.Challenges.BlockOnSeverity = "low"
	ctx.Challenges = []model.Challenge{
		makeChallenge("ch-1", model.SeverityLow, model.ChallengeOpen),
	}

	result := evaluateChallenges(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed (block on low)", result.Status)
	}
}

func TestGate_Challenges_ResolvedIgnored(t *testing.T) {
	ctx := defaultCtx()
	ctx.Challenges = []model.Challenge{
		makeChallenge("ch-1", model.SeverityHigh, model.ChallengeResolved),
	}

	result := evaluateChallenges(&ctx)
	if result.Status != GatePassed {
		t.Errorf("status = %q, want passed (resolved challenges ignored)", result.Status)
	}
}

func TestGate_Challenges_DismissedIgnored(t *testing.T) {
	ctx := defaultCtx()
	ctx.Challenges = []model.Challenge{
		makeChallenge("ch-1", model.SeverityHigh, model.ChallengeDismissed),
	}

	result := evaluateChallenges(&ctx)
	if result.Status != GatePassed {
		t.Errorf("status = %q, want passed (dismissed challenges ignored)", result.Status)
	}
}

func TestGate_Challenges_MultipleChallenges(t *testing.T) {
	ctx := defaultCtx()
	ctx.Challenges = []model.Challenge{
		makeChallenge("ch-1", model.SeverityHigh, model.ChallengeOpen),
		makeChallenge("ch-2", model.SeverityHigh, model.ChallengeOpen),
		makeChallenge("ch-3", model.SeverityLow, model.ChallengeOpen),
	}

	result := evaluateChallenges(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed", result.Status)
	}
	if len(result.Reasons) != 2 {
		t.Errorf("reasons = %d, want 2 (only high severity)", len(result.Reasons))
	}
}

// =============================================================================
// Gate 5: Scope Validation
// =============================================================================

func TestGate_Scope_NoEvidence(t *testing.T) {
	ctx := defaultCtx()
	result := evaluateScope(&ctx)
	if result.Status != GatePassed {
		t.Errorf("status = %q, want passed (no scope evidence)", result.Status)
	}
}

func TestGate_Scope_Passes(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceScopeMatch, model.EvidencePass),
	}

	result := evaluateScope(&ctx)
	if result.Status != GatePassed {
		t.Errorf("status = %q, want passed", result.Status)
	}
}

func TestGate_Scope_Fails(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidenceWithSummary("ev-1", model.EvidenceScopeMatch, model.EvidenceFail, "changed files outside declared scope"),
	}

	result := evaluateScope(&ctx)
	if result.Status != GateFailed {
		t.Errorf("status = %q, want failed", result.Status)
	}
	if result.Reasons[0] != "changed files outside declared scope" {
		t.Errorf("reason = %q, want 'changed files outside declared scope'", result.Reasons[0])
	}
}

// =============================================================================
// Gate mode behavior (enforce/warn/skip)
// =============================================================================

func TestGateMode_Skip(t *testing.T) {
	ctx := defaultCtx()
	ctx.Config.Gates.Mechanical.Mode = config.GateSkip
	ctx.Config.Gates.Policy.Mode = config.GateSkip
	ctx.Config.Gates.Behavioral.Mode = config.GateSkip
	ctx.Config.Gates.Challenges.Mode = config.GateSkip
	ctx.Config.Gates.Scope.Mode = config.GateSkip

	result := Evaluate(&ctx)

	if result.Decision.Outcome != model.DecisionAccepted {
		t.Errorf("outcome = %q, want accepted (all skipped)", result.Decision.Outcome)
	}
	for _, gr := range result.GateResults {
		if gr.Status != GateSkipped {
			t.Errorf("gate %q status = %q, want skipped", gr.Gate, gr.Status)
		}
	}
}

func TestGateMode_WarnDoesNotBlock(t *testing.T) {
	ctx := defaultCtx()
	ctx.Config.Gates.Mechanical.Mode = config.GateWarn
	ctx.Config.Gates.Behavioral.Mode = config.GateWarn
	// No evidence — mechanical and behavioral would fail
	ctx.Evidence = nil

	result := Evaluate(&ctx)

	if result.Decision.Outcome != model.DecisionAccepted {
		t.Errorf("outcome = %q, want accepted (warn mode)", result.Decision.Outcome)
	}
	if result.Decision.ReasonCode != model.ReasonAcceptableLowRisk {
		t.Errorf("reason = %q, want acceptable_low_risk", result.Decision.ReasonCode)
	}
}

func TestGateMode_EnforceBlocks(t *testing.T) {
	ctx := defaultCtx()
	ctx.Config.Gates.Mechanical.Mode = config.GateEnforce
	// No evidence — mechanical fails
	ctx.Evidence = nil

	result := Evaluate(&ctx)

	if result.Decision.Outcome != model.DecisionRejected {
		t.Errorf("outcome = %q, want rejected (enforce mode)", result.Decision.Outcome)
	}
}

func TestGateMode_MixedEnforceAndWarn(t *testing.T) {
	ctx := defaultCtx()
	ctx.Config.Gates.Mechanical.Mode = config.GateWarn     // will warn (no evidence)
	ctx.Config.Gates.Behavioral.Mode = config.GateEnforce  // will fail (no tests)
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
	}

	result := Evaluate(&ctx)

	if result.Decision.Outcome != model.DecisionRejected {
		t.Errorf("outcome = %q, want rejected (behavioral enforced)", result.Decision.Outcome)
	}
}

// =============================================================================
// Reason code selection
// =============================================================================

func TestReasonCode_MechanicalFirst(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidenceFail),
		makeEvidenceWithSummary("ev-2", model.EvidencePolicyCheck, model.EvidenceFail, "policy issue"),
	}

	result := Evaluate(&ctx)
	if result.Decision.ReasonCode != model.ReasonMechanicalCheckFailed {
		t.Errorf("reason = %q, want mechanical_check_failed (first gate priority)", result.Decision.ReasonCode)
	}
}

func TestReasonCode_PolicyWhenOnlyPolicyFails(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
		makeEvidenceWithSummary("ev-3", model.EvidencePolicyCheck, model.EvidenceFail, "blocked"),
	}

	result := Evaluate(&ctx)
	if result.Decision.ReasonCode != model.ReasonPolicyViolation {
		t.Errorf("reason = %q, want policy_violation", result.Decision.ReasonCode)
	}
}

func TestReasonCode_ChallengeWhenOnlyChallengeBlocks(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
	}
	ctx.Challenges = []model.Challenge{
		makeChallenge("ch-1", model.SeverityHigh, model.ChallengeOpen),
	}

	result := Evaluate(&ctx)
	if result.Decision.ReasonCode != model.ReasonUnresolvedHighSeverityChallenge {
		t.Errorf("reason = %q, want unresolved_high_severity_challenge", result.Decision.ReasonCode)
	}
}

// =============================================================================
// Edge cases
// =============================================================================

func TestEdge_EmptyConfig(t *testing.T) {
	ctx := defaultCtx()
	ctx.Config = config.Config{
		Gates: config.GatesConfig{
			Mechanical: config.MechanicalGateConfig{Mode: config.GateSkip},
			Policy:     config.PolicyGateConfig{Mode: config.GateSkip},
			Behavioral: config.BehavioralGateConfig{Mode: config.GateSkip},
			Challenges: config.ChallengesGateConfig{Mode: config.GateSkip},
			Scope:      config.ScopeGateConfig{Mode: config.GateSkip},
		},
	}

	result := Evaluate(&ctx)
	if result.Decision.Outcome != model.DecisionAccepted {
		t.Errorf("outcome = %q, want accepted (all gates skipped)", result.Decision.Outcome)
	}
}

func TestEdge_ConflictingEvidence(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceTestSuite, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidenceFail),
		makeEvidence("ev-3", model.EvidenceBuildCheck, model.EvidencePass),
	}

	result := Evaluate(&ctx)
	// behavioral: 1 passing test meets threshold
	// But let's verify it still works
	if result.Decision.Outcome == "" {
		t.Error("should produce a decision")
	}
}

func TestEdge_OnlyInfoEvidence(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidenceInfo),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidenceInfo),
	}

	result := Evaluate(&ctx)
	if result.Decision.Outcome != model.DecisionRejected {
		t.Errorf("outcome = %q, want rejected (info doesn't satisfy gates)", result.Decision.Outcome)
	}
}

func TestEdge_AllGatesReturnResults(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
	}

	result := Evaluate(&ctx)

	gateNames := make(map[string]bool)
	for _, gr := range result.GateResults {
		gateNames[gr.Gate] = true
	}

	expected := []string{"mechanical", "policy", "behavioral", "challenges", "scope"}
	for _, name := range expected {
		if !gateNames[name] {
			t.Errorf("missing gate result for %q", name)
		}
	}
}

// =============================================================================
// Confidence scoring
// =============================================================================

func TestConfidence_HighWithGoodEvidence(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
		makeEvidence("ev-3", model.EvidenceSecurityScan, model.EvidencePass),
	}

	result := Evaluate(&ctx)
	if result.Confidence < 0.7 {
		t.Errorf("confidence = %.2f, want >= 0.70 (good evidence)", result.Confidence)
	}
}

func TestConfidence_ZeroWithNoEvidence(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = nil
	ctx.Challenges = nil

	result := Evaluate(&ctx)
	if result.Confidence != 0.0 {
		t.Errorf("confidence = %.2f, want 0.00 (no evidence)", result.Confidence)
	}
}

func TestConfidence_LowerWithSkippedGates(t *testing.T) {
	ctxFull := defaultCtx()
	ctxFull.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
	}

	ctxSkipped := defaultCtx()
	ctxSkipped.Evidence = ctxFull.Evidence
	ctxSkipped.Config.Gates.Policy.Mode = config.GateSkip
	ctxSkipped.Config.Gates.Challenges.Mode = config.GateSkip
	ctxSkipped.Config.Gates.Scope.Mode = config.GateSkip

	full := Evaluate(&ctxFull)
	skipped := Evaluate(&ctxSkipped)

	if skipped.Confidence >= full.Confidence {
		t.Errorf("skipped confidence (%.2f) should be less than full (%.2f)", skipped.Confidence, full.Confidence)
	}
}

func TestConfidence_ReducedByOpenChallenges(t *testing.T) {
	ctxClean := defaultCtx()
	ctxClean.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
	}

	ctxChallenged := defaultCtx()
	ctxChallenged.Evidence = ctxClean.Evidence
	ctxChallenged.Challenges = []model.Challenge{
		makeChallenge("ch-1", model.SeverityLow, model.ChallengeOpen),
	}

	clean := Evaluate(&ctxClean)
	challenged := Evaluate(&ctxChallenged)

	if challenged.Confidence >= clean.Confidence {
		t.Errorf("challenged confidence (%.2f) should be less than clean (%.2f)", challenged.Confidence, clean.Confidence)
	}
}

func TestConfidence_ResolvedChallengesRestoreConfidence(t *testing.T) {
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
	}
	ctx.Challenges = []model.Challenge{
		makeChallenge("ch-1", model.SeverityHigh, model.ChallengeResolved),
	}

	result := Evaluate(&ctx)
	if result.Confidence < 0.5 {
		t.Errorf("confidence = %.2f, want >= 0.50 (resolved challenges shouldn't hurt)", result.Confidence)
	}
}

func TestConfidence_BoundedZeroToOne(t *testing.T) {
	// Lots of evidence
	ctx := defaultCtx()
	ctx.Evidence = []model.Evidence{
		makeEvidence("ev-1", model.EvidenceBuildCheck, model.EvidencePass),
		makeEvidence("ev-2", model.EvidenceTestSuite, model.EvidencePass),
		makeEvidence("ev-3", model.EvidenceSecurityScan, model.EvidencePass),
		makeEvidence("ev-4", model.EvidencePolicyCheck, model.EvidencePass),
		makeEvidence("ev-5", model.EvidenceBenchmarkCheck, model.EvidencePass),
		makeEvidence("ev-6", model.EvidenceScopeMatch, model.EvidencePass),
	}

	result := Evaluate(&ctx)
	if result.Confidence < 0.0 || result.Confidence > 1.0 {
		t.Errorf("confidence = %.2f, should be in [0.0, 1.0]", result.Confidence)
	}
}
