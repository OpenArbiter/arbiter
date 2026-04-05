package model

import "testing"

func TestRiskLevel_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value RiskLevel
		want  bool
	}{
		{"low", RiskLow, true},
		{"medium", RiskMedium, true},
		{"high", RiskHigh, true},
		{"empty", RiskLevel(""), false},
		{"invalid", RiskLevel("critical"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("RiskLevel(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestSeverity_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value Severity
		want  bool
	}{
		{"low", SeverityLow, true},
		{"medium", SeverityMedium, true},
		{"high", SeverityHigh, true},
		{"empty", Severity(""), false},
		{"invalid", Severity("critical"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("Severity(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestProposalStatus_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value ProposalStatus
		want  bool
	}{
		{"open", ProposalOpen, true},
		{"updated", ProposalUpdated, true},
		{"withdrawn", ProposalWithdrawn, true},
		{"empty", ProposalStatus(""), false},
		{"invalid", ProposalStatus("closed"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("ProposalStatus(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestEvidenceResult_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value EvidenceResult
		want  bool
	}{
		{"pass", EvidencePass, true},
		{"fail", EvidenceFail, true},
		{"warn", EvidenceWarn, true},
		{"info", EvidenceInfo, true},
		{"empty", EvidenceResult(""), false},
		{"invalid", EvidenceResult("error"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("EvidenceResult(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestEvidenceType_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value EvidenceType
		want  bool
	}{
		{"build_check", EvidenceBuildCheck, true},
		{"test_suite", EvidenceTestSuite, true},
		{"scope_match", EvidenceScopeMatch, true},
		{"policy_check", EvidencePolicyCheck, true},
		{"security_scan", EvidenceSecurityScan, true},
		{"impact_analysis", EvidenceImpactAnalysis, true},
		{"review_finding", EvidenceReviewFinding, true},
		{"benchmark_check", EvidenceBenchmarkCheck, true},
		{"empty", EvidenceType(""), false},
		{"invalid", EvidenceType("custom_check"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("EvidenceType(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestChallengeType_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value ChallengeType
		want  bool
	}{
		{"hidden_behavior_change", ChallengeHiddenBehaviorChange, true},
		{"insufficient_test_coverage", ChallengeInsufficientTestCoverage, true},
		{"scope_mismatch", ChallengeScopeMismatch, true},
		{"policy_violation", ChallengePolicyViolation, true},
		{"likely_regression", ChallengeLikelyRegression, true},
		{"unsupported_assumption", ChallengeUnsupportedAssumption, true},
		{"empty", ChallengeType(""), false},
		{"invalid", ChallengeType("other"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("ChallengeType(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestChallengeStatus_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value ChallengeStatus
		want  bool
	}{
		{"open", ChallengeOpen, true},
		{"resolved", ChallengeResolved, true},
		{"dismissed", ChallengeDismissed, true},
		{"empty", ChallengeStatus(""), false},
		{"invalid", ChallengeStatus("closed"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("ChallengeStatus(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestDecisionOutcome_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value DecisionOutcome
		want  bool
	}{
		{"accepted", DecisionAccepted, true},
		{"rejected", DecisionRejected, true},
		{"needs_action", DecisionNeedsAction, true},
		{"deferred", DecisionDeferred, true},
		{"empty", DecisionOutcome(""), false},
		{"invalid", DecisionOutcome("pending"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("DecisionOutcome(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestReasonCode_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value ReasonCode
		want  bool
	}{
		{"insufficient_behavioral_evidence", ReasonInsufficientBehavioralEvidence, true},
		{"unresolved_high_severity_challenge", ReasonUnresolvedHighSeverityChallenge, true},
		{"policy_violation", ReasonPolicyViolation, true},
		{"scope_exceeded", ReasonScopeExceeded, true},
		{"acceptable_low_risk", ReasonAcceptableLowRisk, true},
		{"human_review_required", ReasonHumanReviewRequired, true},
		{"mechanical_check_failed", ReasonMechanicalCheckFailed, true},
		{"all_gates_passed", ReasonAllGatesPassed, true},
		{"empty", ReasonCode(""), false},
		{"invalid", ReasonCode("unknown"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("ReasonCode(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestPrincipalType_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value PrincipalType
		want  bool
	}{
		{"human", PrincipalHuman, true},
		{"agent", PrincipalAgent, true},
		{"service", PrincipalService, true},
		{"empty", PrincipalType(""), false},
		{"invalid", PrincipalType("bot"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("PrincipalType(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestRefType_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value RefType
		want  bool
	}{
		{"pull_request", RefPullRequest, true},
		{"merge_request", RefMergeRequest, true},
		{"commit", RefCommit, true},
		{"ci_run", RefCIRun, true},
		{"external_task", RefExternalTask, true},
		{"empty", RefType(""), false},
		{"invalid", RefType("branch"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("RefType(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestProvider_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value Provider
		want  bool
	}{
		{"github", ProviderGitHub, true},
		{"gitlab", ProviderGitLab, true},
		{"elasticloom", ProviderElasticLoom, true},
		{"empty", Provider(""), false},
		{"invalid", Provider("bitbucket"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("Provider(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestConfidence_Valid(t *testing.T) {
	tests := []struct {
		name  string
		value Confidence
		want  bool
	}{
		{"low", ConfidenceLow, true},
		{"medium", ConfidenceMedium, true},
		{"high", ConfidenceHigh, true},
		{"empty", Confidence(""), false},
		{"invalid", Confidence("very_high"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Valid(); got != tt.want {
				t.Errorf("Confidence(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
