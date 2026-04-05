package model

import (
	"errors"
	"time"
)

// Proposal represents a candidate change submitted for a Task.
type Proposal struct {
	ProposalID      string         `json:"proposal_id"`
	TaskID          string         `json:"task_id"`
	TenantID        string         `json:"tenant_id"`
	SubmittedBy     string         `json:"submitted_by"`
	ChangeRef       ExternalRef    `json:"change_ref"`
	DeclaredScope   Selector       `json:"declared_scope"`
	BehaviorSummary string         `json:"behavior_summary"`
	Assumptions     []string       `json:"assumptions"`
	Confidence      Confidence     `json:"confidence"`
	Status          ProposalStatus `json:"status"`
	CreatedAt       time.Time      `json:"created_at"`

	// Optional
	ExecutionRef        *ExternalRef `json:"execution_ref,omitempty"`
	SupersedesProposalID *string     `json:"supersedes_proposal_id,omitempty"`
	CostHint            *string      `json:"cost_hint,omitempty"`
	StrategyHint        *string      `json:"strategy_hint,omitempty"`
}

// Confidence represents a level of certainty, expressed as low/medium/high.
type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

func (c Confidence) Valid() bool {
	switch c {
	case ConfidenceLow, ConfidenceMedium, ConfidenceHigh:
		return true
	}
	return false
}

func (p *Proposal) Validate() error {
	var errs []error
	if p.ProposalID == "" {
		errs = append(errs, errors.New("proposal_id is required"))
	}
	if p.TaskID == "" {
		errs = append(errs, errors.New("task_id is required"))
	}
	if p.TenantID == "" {
		errs = append(errs, errors.New("tenant_id is required"))
	}
	if p.SubmittedBy == "" {
		errs = append(errs, errors.New("submitted_by is required"))
	}
	if p.ChangeRef.ExternalID == "" {
		errs = append(errs, errors.New("change_ref.external_id is required"))
	}
	if p.BehaviorSummary == "" {
		errs = append(errs, errors.New("behavior_summary is required"))
	}
	if !p.Confidence.Valid() {
		errs = append(errs, errors.New("confidence must be low, medium, or high"))
	}
	if !p.Status.Valid() {
		errs = append(errs, errors.New("status must be open, updated, or withdrawn"))
	}
	if p.CreatedAt.IsZero() {
		errs = append(errs, errors.New("created_at is required"))
	}
	return errors.Join(errs...)
}
