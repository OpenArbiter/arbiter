package model

import (
	"errors"
	"time"
)

// Evidence is a normalized result or observation about a Proposal.
type Evidence struct {
	EvidenceID   string         `json:"evidence_id"`
	ProposalID   string         `json:"proposal_id"`
	TenantID     string         `json:"tenant_id"`
	EvidenceType EvidenceType   `json:"evidence_type"`
	Subject      string         `json:"subject"`
	Result       EvidenceResult `json:"result"`
	Confidence   Confidence     `json:"confidence"`
	Source       string         `json:"source"`
	CreatedAt    time.Time      `json:"created_at"`

	// Optional
	ArtifactID *string    `json:"artifact_id,omitempty"`
	Summary    *string    `json:"summary,omitempty"`
	Details    *string    `json:"details,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

func (e *Evidence) Validate() error {
	var errs []error
	if e.EvidenceID == "" {
		errs = append(errs, errors.New("evidence_id is required"))
	}
	if e.ProposalID == "" {
		errs = append(errs, errors.New("proposal_id is required"))
	}
	if e.TenantID == "" {
		errs = append(errs, errors.New("tenant_id is required"))
	}
	if !e.EvidenceType.Valid() {
		errs = append(errs, errors.New("evidence_type is invalid"))
	}
	if e.Subject == "" {
		errs = append(errs, errors.New("subject is required"))
	}
	if !e.Result.Valid() {
		errs = append(errs, errors.New("result must be pass, fail, warn, or info"))
	}
	if !e.Confidence.Valid() {
		errs = append(errs, errors.New("confidence must be low, medium, or high"))
	}
	if e.Source == "" {
		errs = append(errs, errors.New("source is required"))
	}
	if e.CreatedAt.IsZero() {
		errs = append(errs, errors.New("created_at is required"))
	}
	return errors.Join(errs...)
}
