package model

import (
	"errors"
	"time"
)

// Task defines intended work that a Proposal attempts to fulfill.
type Task struct {
	TaskID         string        `json:"task_id"`
	TenantID       string        `json:"tenant_id"`
	Title          string        `json:"title"`
	Intent         string        `json:"intent"`
	ExpectedOutcome string       `json:"expected_outcome"`
	NonGoals       []string      `json:"non_goals"`
	RiskLevel      RiskLevel     `json:"risk_level"`
	ScopeHint      Selector      `json:"scope_hint"`
	PolicyProfile  string        `json:"policy_profile"`
	CreatedAt      time.Time     `json:"created_at"`

	// Optional
	Priority     *int          `json:"priority,omitempty"`
	Deadline     *time.Time    `json:"deadline,omitempty"`
	ExternalRefs []ExternalRef `json:"external_refs,omitempty"`
	ParentTaskID *string       `json:"parent_task_id,omitempty"`
}

func (t *Task) Validate() error {
	var errs []error
	if t.TaskID == "" {
		errs = append(errs, errors.New("task_id is required"))
	}
	if t.TenantID == "" {
		errs = append(errs, errors.New("tenant_id is required"))
	}
	if t.Title == "" {
		errs = append(errs, errors.New("title is required"))
	}
	if t.Intent == "" {
		errs = append(errs, errors.New("intent is required"))
	}
	if t.ExpectedOutcome == "" {
		errs = append(errs, errors.New("expected_outcome is required"))
	}
	if !t.RiskLevel.Valid() {
		errs = append(errs, errors.New("risk_level must be low, medium, or high"))
	}
	if t.PolicyProfile == "" {
		errs = append(errs, errors.New("policy_profile is required"))
	}
	if t.CreatedAt.IsZero() {
		errs = append(errs, errors.New("created_at is required"))
	}
	return errors.Join(errs...)
}
