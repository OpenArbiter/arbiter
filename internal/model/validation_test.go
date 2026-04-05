package model

import (
	"strings"
	"testing"
	"time"
)

var testTime = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// validTask returns a fully populated, valid Task for testing.
func validTask() Task {
	return Task{
		TaskID:          "task-1",
		TenantID:        "tenant-1",
		Title:           "Fix login bug",
		Intent:          "Resolve the authentication failure on mobile",
		ExpectedOutcome: "Users can log in from mobile devices",
		NonGoals:        []string{"Do not change desktop login flow"},
		RiskLevel:       RiskMedium,
		ScopeHint:       Selector{Paths: []string{"auth/"}},
		PolicyProfile:   "default",
		CreatedAt:       testTime,
	}
}

// validProposal returns a fully populated, valid Proposal for testing.
func validProposal() Proposal {
	return Proposal{
		ProposalID:      "prop-1",
		TaskID:          "task-1",
		TenantID:        "tenant-1",
		SubmittedBy:     "principal-1",
		ChangeRef:       ExternalRef{RefType: RefPullRequest, Provider: ProviderGitHub, ExternalID: "123"},
		DeclaredScope:   Selector{Paths: []string{"auth/login.go"}},
		BehaviorSummary: "Fix mobile auth token refresh",
		Assumptions:     []string{"Token expiry is the root cause"},
		Confidence:      ConfidenceHigh,
		Status:          ProposalOpen,
		CreatedAt:       testTime,
	}
}

// validEvidence returns a fully populated, valid Evidence for testing.
func validEvidence() Evidence {
	return Evidence{
		EvidenceID:   "ev-1",
		ProposalID:   "prop-1",
		TenantID:     "tenant-1",
		EvidenceType: EvidenceTestSuite,
		Subject:      "auth/login_test.go",
		Result:       EvidencePass,
		Confidence:   ConfidenceHigh,
		Source:        "github-actions",
		CreatedAt:    testTime,
	}
}

// validChallenge returns a fully populated, valid Challenge for testing.
func validChallenge() Challenge {
	return Challenge{
		ChallengeID:   "ch-1",
		ProposalID:    "prop-1",
		TenantID:      "tenant-1",
		RaisedBy:      "principal-2",
		ChallengeType: ChallengeScopeMismatch,
		Target:        "auth/session.go",
		Severity:      SeverityHigh,
		Summary:       "Change modifies session handling outside declared scope",
		Status:        ChallengeOpen,
		CreatedAt:     testTime,
	}
}

// validDecision returns a fully populated, valid Decision for testing.
func validDecision() Decision {
	return Decision{
		DecisionID: "dec-1",
		ProposalID: "prop-1",
		TenantID:   "tenant-1",
		Outcome:    DecisionAccepted,
		ReasonCode: ReasonAllGatesPassed,
		Summary:    "All evidence passes, no unresolved challenges",
		DecidedAt:  testTime,
		DecidedBy:  "arbiter-engine",
	}
}

// --- Task Validation ---

func TestTask_Validate_Valid(t *testing.T) {
	task := validTask()
	if err := task.Validate(); err != nil {
		t.Errorf("valid task should not error, got: %v", err)
	}
}

func TestTask_Validate_MissingFields(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Task)
		wantErr string
	}{
		{"missing task_id", func(t *Task) { t.TaskID = "" }, "task_id is required"},
		{"missing tenant_id", func(t *Task) { t.TenantID = "" }, "tenant_id is required"},
		{"missing title", func(t *Task) { t.Title = "" }, "title is required"},
		{"missing intent", func(t *Task) { t.Intent = "" }, "intent is required"},
		{"missing expected_outcome", func(t *Task) { t.ExpectedOutcome = "" }, "expected_outcome is required"},
		{"invalid risk_level", func(t *Task) { t.RiskLevel = "critical" }, "risk_level must be"},
		{"missing policy_profile", func(t *Task) { t.PolicyProfile = "" }, "policy_profile is required"},
		{"zero created_at", func(t *Task) { t.CreatedAt = time.Time{} }, "created_at is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := validTask()
			tt.modify(&task)
			err := task.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestTask_Validate_MultipleErrors(t *testing.T) {
	task := Task{} // all fields missing
	err := task.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Should report multiple errors
	errStr := err.Error()
	if !strings.Contains(errStr, "task_id") {
		t.Error("should report task_id error")
	}
	if !strings.Contains(errStr, "tenant_id") {
		t.Error("should report tenant_id error")
	}
}

// --- Proposal Validation ---

func TestProposal_Validate_Valid(t *testing.T) {
	p := validProposal()
	if err := p.Validate(); err != nil {
		t.Errorf("valid proposal should not error, got: %v", err)
	}
}

func TestProposal_Validate_MissingFields(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Proposal)
		wantErr string
	}{
		{"missing proposal_id", func(p *Proposal) { p.ProposalID = "" }, "proposal_id is required"},
		{"missing task_id", func(p *Proposal) { p.TaskID = "" }, "task_id is required"},
		{"missing tenant_id", func(p *Proposal) { p.TenantID = "" }, "tenant_id is required"},
		{"missing submitted_by", func(p *Proposal) { p.SubmittedBy = "" }, "submitted_by is required"},
		{"missing change_ref", func(p *Proposal) { p.ChangeRef = ExternalRef{} }, "change_ref.external_id is required"},
		{"missing behavior_summary", func(p *Proposal) { p.BehaviorSummary = "" }, "behavior_summary is required"},
		{"invalid confidence", func(p *Proposal) { p.Confidence = "very_high" }, "confidence must be"},
		{"invalid status", func(p *Proposal) { p.Status = "closed" }, "status must be"},
		{"zero created_at", func(p *Proposal) { p.CreatedAt = time.Time{} }, "created_at is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validProposal()
			tt.modify(&p)
			err := p.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// --- Evidence Validation ---

func TestEvidence_Validate_Valid(t *testing.T) {
	e := validEvidence()
	if err := e.Validate(); err != nil {
		t.Errorf("valid evidence should not error, got: %v", err)
	}
}

func TestEvidence_Validate_MissingFields(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Evidence)
		wantErr string
	}{
		{"missing evidence_id", func(e *Evidence) { e.EvidenceID = "" }, "evidence_id is required"},
		{"missing proposal_id", func(e *Evidence) { e.ProposalID = "" }, "proposal_id is required"},
		{"missing tenant_id", func(e *Evidence) { e.TenantID = "" }, "tenant_id is required"},
		{"invalid evidence_type", func(e *Evidence) { e.EvidenceType = "custom" }, "evidence_type is invalid"},
		{"missing subject", func(e *Evidence) { e.Subject = "" }, "subject is required"},
		{"invalid result", func(e *Evidence) { e.Result = "error" }, "result must be"},
		{"invalid confidence", func(e *Evidence) { e.Confidence = "none" }, "confidence must be"},
		{"missing source", func(e *Evidence) { e.Source = "" }, "source is required"},
		{"zero created_at", func(e *Evidence) { e.CreatedAt = time.Time{} }, "created_at is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validEvidence()
			tt.modify(&e)
			err := e.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// --- Challenge Validation ---

func TestChallenge_Validate_Valid(t *testing.T) {
	c := validChallenge()
	if err := c.Validate(); err != nil {
		t.Errorf("valid challenge should not error, got: %v", err)
	}
}

func TestChallenge_Validate_MissingFields(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Challenge)
		wantErr string
	}{
		{"missing challenge_id", func(c *Challenge) { c.ChallengeID = "" }, "challenge_id is required"},
		{"missing proposal_id", func(c *Challenge) { c.ProposalID = "" }, "proposal_id is required"},
		{"missing tenant_id", func(c *Challenge) { c.TenantID = "" }, "tenant_id is required"},
		{"missing raised_by", func(c *Challenge) { c.RaisedBy = "" }, "raised_by is required"},
		{"invalid challenge_type", func(c *Challenge) { c.ChallengeType = "other" }, "challenge_type is invalid"},
		{"missing target", func(c *Challenge) { c.Target = "" }, "target is required"},
		{"invalid severity", func(c *Challenge) { c.Severity = "critical" }, "severity must be"},
		{"missing summary", func(c *Challenge) { c.Summary = "" }, "summary is required"},
		{"invalid status", func(c *Challenge) { c.Status = "closed" }, "status must be"},
		{"zero created_at", func(c *Challenge) { c.CreatedAt = time.Time{} }, "created_at is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validChallenge()
			tt.modify(&c)
			err := c.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// --- Decision Validation ---

func TestDecision_Validate_Valid(t *testing.T) {
	d := validDecision()
	if err := d.Validate(); err != nil {
		t.Errorf("valid decision should not error, got: %v", err)
	}
}

func TestDecision_Validate_MissingFields(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Decision)
		wantErr string
	}{
		{"missing decision_id", func(d *Decision) { d.DecisionID = "" }, "decision_id is required"},
		{"missing proposal_id", func(d *Decision) { d.ProposalID = "" }, "proposal_id is required"},
		{"missing tenant_id", func(d *Decision) { d.TenantID = "" }, "tenant_id is required"},
		{"invalid outcome", func(d *Decision) { d.Outcome = "pending" }, "outcome must be"},
		{"invalid reason_code", func(d *Decision) { d.ReasonCode = "unknown" }, "reason_code is invalid"},
		{"missing summary", func(d *Decision) { d.Summary = "" }, "summary is required"},
		{"zero decided_at", func(d *Decision) { d.DecidedAt = time.Time{} }, "decided_at is required"},
		{"missing decided_by", func(d *Decision) { d.DecidedBy = "" }, "decided_by is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := validDecision()
			tt.modify(&d)
			err := d.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestDecision_Validate_MultipleErrors(t *testing.T) {
	d := Decision{} // all fields missing
	err := d.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "decision_id") {
		t.Error("should report decision_id error")
	}
	if !strings.Contains(errStr, "outcome") {
		t.Error("should report outcome error")
	}
}
