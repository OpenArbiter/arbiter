package model

// RiskLevel indicates the risk associated with a Task.
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

func (r RiskLevel) Valid() bool {
	switch r {
	case RiskLow, RiskMedium, RiskHigh:
		return true
	}
	return false
}

// Severity indicates the severity of a Challenge.
type Severity string

const (
	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

func (s Severity) Valid() bool {
	switch s {
	case SeverityLow, SeverityMedium, SeverityHigh:
		return true
	}
	return false
}

// ProposalStatus tracks the lifecycle of a Proposal.
type ProposalStatus string

const (
	ProposalOpen      ProposalStatus = "open"
	ProposalUpdated   ProposalStatus = "updated"
	ProposalWithdrawn ProposalStatus = "withdrawn"
)

func (p ProposalStatus) Valid() bool {
	switch p {
	case ProposalOpen, ProposalUpdated, ProposalWithdrawn:
		return true
	}
	return false
}

// EvidenceResult is the outcome of an Evidence check.
type EvidenceResult string

const (
	EvidencePass EvidenceResult = "pass"
	EvidenceFail EvidenceResult = "fail"
	EvidenceWarn EvidenceResult = "warn"
	EvidenceInfo EvidenceResult = "info"
)

func (e EvidenceResult) Valid() bool {
	switch e {
	case EvidencePass, EvidenceFail, EvidenceWarn, EvidenceInfo:
		return true
	}
	return false
}

// EvidenceType categorizes what kind of check produced the Evidence.
type EvidenceType string

const (
	EvidenceBuildCheck    EvidenceType = "build_check"
	EvidenceTestSuite     EvidenceType = "test_suite"
	EvidenceScopeMatch    EvidenceType = "scope_match"
	EvidencePolicyCheck   EvidenceType = "policy_check"
	EvidenceSecurityScan  EvidenceType = "security_scan"
	EvidenceImpactAnalysis EvidenceType = "impact_analysis"
	EvidenceReviewFinding EvidenceType = "review_finding"
	EvidenceBenchmarkCheck EvidenceType = "benchmark_check"
)

func (e EvidenceType) Valid() bool {
	switch e {
	case EvidenceBuildCheck, EvidenceTestSuite, EvidenceScopeMatch,
		EvidencePolicyCheck, EvidenceSecurityScan, EvidenceImpactAnalysis,
		EvidenceReviewFinding, EvidenceBenchmarkCheck:
		return true
	}
	return false
}

// ChallengeType categorizes the nature of a Challenge.
type ChallengeType string

const (
	ChallengeHiddenBehaviorChange     ChallengeType = "hidden_behavior_change"
	ChallengeInsufficientTestCoverage ChallengeType = "insufficient_test_coverage"
	ChallengeScopeMismatch            ChallengeType = "scope_mismatch"
	ChallengePolicyViolation          ChallengeType = "policy_violation"
	ChallengeLikelyRegression         ChallengeType = "likely_regression"
	ChallengeUnsupportedAssumption    ChallengeType = "unsupported_assumption"
)

func (c ChallengeType) Valid() bool {
	switch c {
	case ChallengeHiddenBehaviorChange, ChallengeInsufficientTestCoverage,
		ChallengeScopeMismatch, ChallengePolicyViolation,
		ChallengeLikelyRegression, ChallengeUnsupportedAssumption:
		return true
	}
	return false
}

// ChallengeStatus tracks the lifecycle of a Challenge.
type ChallengeStatus string

const (
	ChallengeOpen     ChallengeStatus = "open"
	ChallengeResolved ChallengeStatus = "resolved"
	ChallengeDismissed ChallengeStatus = "dismissed"
)

func (c ChallengeStatus) Valid() bool {
	switch c {
	case ChallengeOpen, ChallengeResolved, ChallengeDismissed:
		return true
	}
	return false
}

// DecisionOutcome is the result of evaluating a Proposal.
type DecisionOutcome string

const (
	DecisionAccepted   DecisionOutcome = "accepted"
	DecisionRejected   DecisionOutcome = "rejected"
	DecisionNeedsAction DecisionOutcome = "needs_action"
	DecisionDeferred   DecisionOutcome = "deferred"
)

func (d DecisionOutcome) Valid() bool {
	switch d {
	case DecisionAccepted, DecisionRejected, DecisionNeedsAction, DecisionDeferred:
		return true
	}
	return false
}

// ReasonCode explains why a Decision was made.
type ReasonCode string

const (
	ReasonInsufficientBehavioralEvidence ReasonCode = "insufficient_behavioral_evidence"
	ReasonUnresolvedHighSeverityChallenge ReasonCode = "unresolved_high_severity_challenge"
	ReasonPolicyViolation                ReasonCode = "policy_violation"
	ReasonScopeExceeded                  ReasonCode = "scope_exceeded"
	ReasonAcceptableLowRisk              ReasonCode = "acceptable_low_risk"
	ReasonHumanReviewRequired            ReasonCode = "human_review_required"
	ReasonMechanicalCheckFailed          ReasonCode = "mechanical_check_failed"
	ReasonAllGatesPassed                 ReasonCode = "all_gates_passed"
)

func (r ReasonCode) Valid() bool {
	switch r {
	case ReasonInsufficientBehavioralEvidence, ReasonUnresolvedHighSeverityChallenge,
		ReasonPolicyViolation, ReasonScopeExceeded, ReasonAcceptableLowRisk,
		ReasonHumanReviewRequired, ReasonMechanicalCheckFailed, ReasonAllGatesPassed:
		return true
	}
	return false
}

// PrincipalType identifies what kind of actor a Principal is.
type PrincipalType string

const (
	PrincipalHuman   PrincipalType = "human"
	PrincipalAgent   PrincipalType = "agent"
	PrincipalService PrincipalType = "service"
)

func (p PrincipalType) Valid() bool {
	switch p {
	case PrincipalHuman, PrincipalAgent, PrincipalService:
		return true
	}
	return false
}

// RefType identifies what kind of external reference an ExternalRef points to.
type RefType string

const (
	RefPullRequest  RefType = "pull_request"
	RefMergeRequest RefType = "merge_request"
	RefCommit       RefType = "commit"
	RefCIRun        RefType = "ci_run"
	RefExternalTask RefType = "external_task"
)

func (r RefType) Valid() bool {
	switch r {
	case RefPullRequest, RefMergeRequest, RefCommit, RefCIRun, RefExternalTask:
		return true
	}
	return false
}

// Provider identifies the external system.
type Provider string

const (
	ProviderGitHub      Provider = "github"
	ProviderGitLab      Provider = "gitlab"
	ProviderElasticLoom Provider = "elasticloom"
)

func (p Provider) Valid() bool {
	switch p {
	case ProviderGitHub, ProviderGitLab, ProviderElasticLoom:
		return true
	}
	return false
}
