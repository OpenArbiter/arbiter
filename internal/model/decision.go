package model

import (
	"errors"
	"time"
)

// Decision is the final trust outcome for a Proposal.
type Decision struct {
	DecisionID string          `json:"decision_id"`
	ProposalID string          `json:"proposal_id"`
	TenantID   string          `json:"tenant_id"`
	Outcome    DecisionOutcome `json:"outcome"`
	ReasonCode ReasonCode      `json:"reason_code"`
	Summary    string          `json:"summary"`
	DecidedAt  time.Time       `json:"decided_at"`
	DecidedBy  string          `json:"decided_by"`

	// Optional
	LinkedEvidenceIDs  []string   `json:"linked_evidence_ids,omitempty"`
	LinkedChallengeIDs []string   `json:"linked_challenge_ids,omitempty"`
	RequiredActions    []string   `json:"required_actions,omitempty"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
}

func (d *Decision) Validate() error {
	var errs []error
	if d.DecisionID == "" {
		errs = append(errs, errors.New("decision_id is required"))
	}
	if d.ProposalID == "" {
		errs = append(errs, errors.New("proposal_id is required"))
	}
	if d.TenantID == "" {
		errs = append(errs, errors.New("tenant_id is required"))
	}
	if !d.Outcome.Valid() {
		errs = append(errs, errors.New("outcome must be accepted, rejected, needs_action, or deferred"))
	}
	if !d.ReasonCode.Valid() {
		errs = append(errs, errors.New("reason_code is invalid"))
	}
	if d.Summary == "" {
		errs = append(errs, errors.New("summary is required"))
	}
	if d.DecidedAt.IsZero() {
		errs = append(errs, errors.New("decided_at is required"))
	}
	if d.DecidedBy == "" {
		errs = append(errs, errors.New("decided_by is required"))
	}
	return errors.Join(errs...)
}
