package model

import (
	"errors"
	"time"
)

// Challenge is a structured objection to a Proposal.
type Challenge struct {
	ChallengeID   string          `json:"challenge_id"`
	ProposalID    string          `json:"proposal_id"`
	TenantID      string          `json:"tenant_id"`
	RaisedBy      string          `json:"raised_by"`
	ChallengeType ChallengeType   `json:"challenge_type"`
	Target        string          `json:"target"`
	Severity      Severity        `json:"severity"`
	Summary       string          `json:"summary"`
	Status        ChallengeStatus `json:"status"`
	CreatedAt     time.Time       `json:"created_at"`

	// Optional
	ArtifactID        *string    `json:"artifact_id,omitempty"`
	LinkedEvidenceIDs []string   `json:"linked_evidence_ids,omitempty"`
	ResolutionNote    *string    `json:"resolution_note,omitempty"`
	ResolvedBy        *string    `json:"resolved_by,omitempty"`
	ResolvedAt        *time.Time `json:"resolved_at,omitempty"`
}

func (c *Challenge) Validate() error {
	var errs []error
	if c.ChallengeID == "" {
		errs = append(errs, errors.New("challenge_id is required"))
	}
	if c.ProposalID == "" {
		errs = append(errs, errors.New("proposal_id is required"))
	}
	if c.TenantID == "" {
		errs = append(errs, errors.New("tenant_id is required"))
	}
	if c.RaisedBy == "" {
		errs = append(errs, errors.New("raised_by is required"))
	}
	if !c.ChallengeType.Valid() {
		errs = append(errs, errors.New("challenge_type is invalid"))
	}
	if c.Target == "" {
		errs = append(errs, errors.New("target is required"))
	}
	if !c.Severity.Valid() {
		errs = append(errs, errors.New("severity must be low, medium, or high"))
	}
	if c.Summary == "" {
		errs = append(errs, errors.New("summary is required"))
	}
	if !c.Status.Valid() {
		errs = append(errs, errors.New("status must be open, resolved, or dismissed"))
	}
	if c.CreatedAt.IsZero() {
		errs = append(errs, errors.New("created_at is required"))
	}
	return errors.Join(errs...)
}
