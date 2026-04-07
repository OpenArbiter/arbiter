package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openarbiter/arbiter/internal/model"
)

// PgStore implements Store using PostgreSQL.
type PgStore struct {
	pool *pgxpool.Pool
}

// NewPgStore creates a new PostgreSQL-backed store.
func NewPgStore(ctx context.Context, databaseURL string) (*PgStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return &PgStore{pool: pool}, nil
}

func (s *PgStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *PgStore) Close() {
	s.pool.Close()
}

// --- Tasks ---

func (s *PgStore) CreateTask(ctx context.Context, task *model.Task) error {
	if err := task.Validate(); err != nil {
		return fmt.Errorf("invalid task: %w", err)
	}

	nonGoals, _ := json.Marshal(task.NonGoals)
	scopeHint, _ := json.Marshal(task.ScopeHint)
	externalRefs, _ := json.Marshal(task.ExternalRefs)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO tasks (task_id, tenant_id, title, intent, expected_outcome, non_goals,
			risk_level, scope_hint, policy_profile, created_at, priority, deadline,
			external_refs, parent_task_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		task.TaskID, task.TenantID, task.Title, task.Intent, task.ExpectedOutcome, nonGoals,
		string(task.RiskLevel), scopeHint, task.PolicyProfile, task.CreatedAt,
		task.Priority, task.Deadline, externalRefs, task.ParentTaskID,
	)
	if err != nil {
		return wrapPgError(err)
	}
	return nil
}

func (s *PgStore) GetTask(ctx context.Context, taskID string) (*model.Task, error) {
	var t model.Task
	var nonGoals, scopeHint, externalRefs []byte
	var riskLevel string

	err := s.pool.QueryRow(ctx, `
		SELECT task_id, tenant_id, title, intent, expected_outcome, non_goals,
			risk_level, scope_hint, policy_profile, created_at, priority, deadline,
			external_refs, parent_task_id
		FROM tasks WHERE task_id = $1`, taskID).Scan(
		&t.TaskID, &t.TenantID, &t.Title, &t.Intent, &t.ExpectedOutcome, &nonGoals,
		&riskLevel, &scopeHint, &t.PolicyProfile, &t.CreatedAt,
		&t.Priority, &t.Deadline, &externalRefs, &t.ParentTaskID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying task: %w", err)
	}

	t.RiskLevel = model.RiskLevel(riskLevel)
	json.Unmarshal(nonGoals, &t.NonGoals)
	json.Unmarshal(scopeHint, &t.ScopeHint)
	json.Unmarshal(externalRefs, &t.ExternalRefs)

	return &t, nil
}

func (s *PgStore) ListTasksByTenant(ctx context.Context, tenantID string, limit, offset int) ([]model.Task, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT task_id, tenant_id, title, intent, expected_outcome, non_goals,
			risk_level, scope_hint, policy_profile, created_at, priority, deadline,
			external_refs, parent_task_id
		FROM tasks WHERE tenant_id = $1
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

func scanTasks(rows pgx.Rows) ([]model.Task, error) {
	var tasks []model.Task
	for rows.Next() {
		var t model.Task
		var nonGoals, scopeHint, externalRefs []byte
		var riskLevel string

		if err := rows.Scan(
			&t.TaskID, &t.TenantID, &t.Title, &t.Intent, &t.ExpectedOutcome, &nonGoals,
			&riskLevel, &scopeHint, &t.PolicyProfile, &t.CreatedAt,
			&t.Priority, &t.Deadline, &externalRefs, &t.ParentTaskID,
		); err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}

		t.RiskLevel = model.RiskLevel(riskLevel)
		json.Unmarshal(nonGoals, &t.NonGoals)
		json.Unmarshal(scopeHint, &t.ScopeHint)
		json.Unmarshal(externalRefs, &t.ExternalRefs)

		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// --- Proposals ---

func (s *PgStore) CreateProposal(ctx context.Context, p *model.Proposal) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("invalid proposal: %w", err)
	}

	changeRef, _ := json.Marshal(p.ChangeRef)
	declaredScope, _ := json.Marshal(p.DeclaredScope)
	assumptions, _ := json.Marshal(p.Assumptions)
	var execRef []byte
	if p.ExecutionRef != nil {
		execRef, _ = json.Marshal(p.ExecutionRef)
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO proposals (proposal_id, task_id, tenant_id, submitted_by, change_ref,
			declared_scope, behavior_summary, assumptions, confidence, status, created_at,
			execution_ref, supersedes_proposal_id, cost_hint, strategy_hint)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		p.ProposalID, p.TaskID, p.TenantID, p.SubmittedBy, changeRef,
		declaredScope, p.BehaviorSummary, assumptions, string(p.Confidence),
		string(p.Status), p.CreatedAt, execRef, p.SupersedesProposalID,
		p.CostHint, p.StrategyHint,
	)
	if err != nil {
		return wrapPgError(err)
	}
	return nil
}

func (s *PgStore) GetProposal(ctx context.Context, proposalID string) (*model.Proposal, error) {
	var p model.Proposal
	var changeRef, declaredScope, assumptions, execRef []byte
	var confidence, status string

	err := s.pool.QueryRow(ctx, `
		SELECT proposal_id, task_id, tenant_id, submitted_by, change_ref,
			declared_scope, behavior_summary, assumptions, confidence, status, created_at,
			execution_ref, supersedes_proposal_id, cost_hint, strategy_hint
		FROM proposals WHERE proposal_id = $1`, proposalID).Scan(
		&p.ProposalID, &p.TaskID, &p.TenantID, &p.SubmittedBy, &changeRef,
		&declaredScope, &p.BehaviorSummary, &assumptions, &confidence, &status, &p.CreatedAt,
		&execRef, &p.SupersedesProposalID, &p.CostHint, &p.StrategyHint,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying proposal: %w", err)
	}

	p.Confidence = model.Confidence(confidence)
	p.Status = model.ProposalStatus(status)
	json.Unmarshal(changeRef, &p.ChangeRef)
	json.Unmarshal(declaredScope, &p.DeclaredScope)
	json.Unmarshal(assumptions, &p.Assumptions)
	if execRef != nil {
		var ref model.ExternalRef
		json.Unmarshal(execRef, &ref)
		p.ExecutionRef = &ref
	}

	return &p, nil
}

func (s *PgStore) ListProposalsByTask(ctx context.Context, taskID string) ([]model.Proposal, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT proposal_id, task_id, tenant_id, submitted_by, change_ref,
			declared_scope, behavior_summary, assumptions, confidence, status, created_at,
			execution_ref, supersedes_proposal_id, cost_hint, strategy_hint
		FROM proposals WHERE task_id = $1
		ORDER BY created_at DESC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing proposals: %w", err)
	}
	defer rows.Close()
	return scanProposals(rows)
}

func (s *PgStore) ListOpenProposalsByTenant(ctx context.Context, tenantID string, limit, offset int) ([]model.Proposal, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT proposal_id, task_id, tenant_id, submitted_by, change_ref,
			declared_scope, behavior_summary, assumptions, confidence, status, created_at,
			execution_ref, supersedes_proposal_id, cost_hint, strategy_hint
		FROM proposals WHERE tenant_id = $1 AND status = 'open'
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing proposals: %w", err)
	}
	defer rows.Close()
	return scanProposals(rows)
}

func (s *PgStore) UpdateProposalStatus(ctx context.Context, proposalID string, status model.ProposalStatus) error {
	if !status.Valid() {
		return fmt.Errorf("invalid proposal status: %s", status)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE proposals SET status = $1 WHERE proposal_id = $2`,
		string(status), proposalID)
	if err != nil {
		return fmt.Errorf("updating proposal status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanProposals(rows pgx.Rows) ([]model.Proposal, error) {
	var proposals []model.Proposal
	for rows.Next() {
		var p model.Proposal
		var changeRef, declaredScope, assumptions, execRef []byte
		var confidence, status string

		if err := rows.Scan(
			&p.ProposalID, &p.TaskID, &p.TenantID, &p.SubmittedBy, &changeRef,
			&declaredScope, &p.BehaviorSummary, &assumptions, &confidence, &status, &p.CreatedAt,
			&execRef, &p.SupersedesProposalID, &p.CostHint, &p.StrategyHint,
		); err != nil {
			return nil, fmt.Errorf("scanning proposal: %w", err)
		}

		p.Confidence = model.Confidence(confidence)
		p.Status = model.ProposalStatus(status)
		json.Unmarshal(changeRef, &p.ChangeRef)
		json.Unmarshal(declaredScope, &p.DeclaredScope)
		json.Unmarshal(assumptions, &p.Assumptions)
		if execRef != nil {
			var ref model.ExternalRef
			json.Unmarshal(execRef, &ref)
			p.ExecutionRef = &ref
		}

		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

// --- Evidence ---

func (s *PgStore) CreateEvidence(ctx context.Context, e *model.Evidence) error {
	if err := e.Validate(); err != nil {
		return fmt.Errorf("invalid evidence: %w", err)
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO evidence (evidence_id, proposal_id, tenant_id, evidence_type, subject,
			result, confidence, source, created_at, artifact_id, summary, details, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		e.EvidenceID, e.ProposalID, e.TenantID, string(e.EvidenceType), e.Subject,
		string(e.Result), string(e.Confidence), e.Source, e.CreatedAt,
		e.ArtifactID, e.Summary, e.Details, e.ExpiresAt,
	)
	if err != nil {
		return wrapPgError(err)
	}
	return nil
}

func (s *PgStore) GetEvidence(ctx context.Context, evidenceID string) (*model.Evidence, error) {
	var e model.Evidence
	var evidenceType, result, confidence string

	err := s.pool.QueryRow(ctx, `
		SELECT evidence_id, proposal_id, tenant_id, evidence_type, subject,
			result, confidence, source, created_at, artifact_id, summary, details, expires_at
		FROM evidence WHERE evidence_id = $1`, evidenceID).Scan(
		&e.EvidenceID, &e.ProposalID, &e.TenantID, &evidenceType, &e.Subject,
		&result, &confidence, &e.Source, &e.CreatedAt,
		&e.ArtifactID, &e.Summary, &e.Details, &e.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying evidence: %w", err)
	}

	e.EvidenceType = model.EvidenceType(evidenceType)
	e.Result = model.EvidenceResult(result)
	e.Confidence = model.Confidence(confidence)

	return &e, nil
}

func (s *PgStore) ListEvidenceByProposal(ctx context.Context, proposalID string) ([]model.Evidence, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT evidence_id, proposal_id, tenant_id, evidence_type, subject,
			result, confidence, source, created_at, artifact_id, summary, details, expires_at
		FROM evidence WHERE proposal_id = $1
		ORDER BY created_at ASC`, proposalID)
	if err != nil {
		return nil, fmt.Errorf("listing evidence: %w", err)
	}
	defer rows.Close()

	var evidence []model.Evidence
	for rows.Next() {
		var e model.Evidence
		var evidenceType, result, confidence string

		if err := rows.Scan(
			&e.EvidenceID, &e.ProposalID, &e.TenantID, &evidenceType, &e.Subject,
			&result, &confidence, &e.Source, &e.CreatedAt,
			&e.ArtifactID, &e.Summary, &e.Details, &e.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scanning evidence: %w", err)
		}

		e.EvidenceType = model.EvidenceType(evidenceType)
		e.Result = model.EvidenceResult(result)
		e.Confidence = model.Confidence(confidence)

		evidence = append(evidence, e)
	}
	return evidence, rows.Err()
}

// --- Challenges ---

func (s *PgStore) CreateChallenge(ctx context.Context, c *model.Challenge) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("invalid challenge: %w", err)
	}

	linkedEvIDs, _ := json.Marshal(c.LinkedEvidenceIDs)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO challenges (challenge_id, proposal_id, tenant_id, raised_by, challenge_type,
			target, severity, summary, status, created_at, artifact_id, linked_evidence_ids,
			resolution_note, resolved_by, resolved_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		c.ChallengeID, c.ProposalID, c.TenantID, c.RaisedBy, string(c.ChallengeType),
		c.Target, string(c.Severity), c.Summary, string(c.Status), c.CreatedAt,
		c.ArtifactID, linkedEvIDs, c.ResolutionNote, c.ResolvedBy, c.ResolvedAt,
	)
	if err != nil {
		return wrapPgError(err)
	}
	return nil
}

func (s *PgStore) GetChallenge(ctx context.Context, challengeID string) (*model.Challenge, error) {
	var c model.Challenge
	var challengeType, severity, status string
	var linkedEvIDs []byte

	err := s.pool.QueryRow(ctx, `
		SELECT challenge_id, proposal_id, tenant_id, raised_by, challenge_type,
			target, severity, summary, status, created_at, artifact_id, linked_evidence_ids,
			resolution_note, resolved_by, resolved_at
		FROM challenges WHERE challenge_id = $1`, challengeID).Scan(
		&c.ChallengeID, &c.ProposalID, &c.TenantID, &c.RaisedBy, &challengeType,
		&c.Target, &severity, &c.Summary, &status, &c.CreatedAt,
		&c.ArtifactID, &linkedEvIDs, &c.ResolutionNote, &c.ResolvedBy, &c.ResolvedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying challenge: %w", err)
	}

	c.ChallengeType = model.ChallengeType(challengeType)
	c.Severity = model.Severity(severity)
	c.Status = model.ChallengeStatus(status)
	json.Unmarshal(linkedEvIDs, &c.LinkedEvidenceIDs)

	return &c, nil
}

func (s *PgStore) ListChallengesByProposal(ctx context.Context, proposalID string) ([]model.Challenge, error) {
	return s.listChallenges(ctx, `
		SELECT challenge_id, proposal_id, tenant_id, raised_by, challenge_type,
			target, severity, summary, status, created_at, artifact_id, linked_evidence_ids,
			resolution_note, resolved_by, resolved_at
		FROM challenges WHERE proposal_id = $1
		ORDER BY created_at ASC`, proposalID)
}

func (s *PgStore) ListOpenChallengesByProposal(ctx context.Context, proposalID string) ([]model.Challenge, error) {
	return s.listChallenges(ctx, `
		SELECT challenge_id, proposal_id, tenant_id, raised_by, challenge_type,
			target, severity, summary, status, created_at, artifact_id, linked_evidence_ids,
			resolution_note, resolved_by, resolved_at
		FROM challenges WHERE proposal_id = $1 AND status = 'open'
		ORDER BY created_at ASC`, proposalID)
}

func (s *PgStore) listChallenges(ctx context.Context, query string, args ...any) ([]model.Challenge, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing challenges: %w", err)
	}
	defer rows.Close()

	var challenges []model.Challenge
	for rows.Next() {
		var c model.Challenge
		var challengeType, severity, status string
		var linkedEvIDs []byte

		if err := rows.Scan(
			&c.ChallengeID, &c.ProposalID, &c.TenantID, &c.RaisedBy, &challengeType,
			&c.Target, &severity, &c.Summary, &status, &c.CreatedAt,
			&c.ArtifactID, &linkedEvIDs, &c.ResolutionNote, &c.ResolvedBy, &c.ResolvedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning challenge: %w", err)
		}

		c.ChallengeType = model.ChallengeType(challengeType)
		c.Severity = model.Severity(severity)
		c.Status = model.ChallengeStatus(status)
		json.Unmarshal(linkedEvIDs, &c.LinkedEvidenceIDs)

		challenges = append(challenges, c)
	}
	return challenges, rows.Err()
}

func (s *PgStore) ResolveChallenge(ctx context.Context, challengeID string, resolvedBy string, note string) error {
	return s.updateChallengeStatus(ctx, challengeID, model.ChallengeResolved, resolvedBy, note)
}

func (s *PgStore) DismissChallenge(ctx context.Context, challengeID string, resolvedBy string, note string) error {
	return s.updateChallengeStatus(ctx, challengeID, model.ChallengeDismissed, resolvedBy, note)
}

func (s *PgStore) updateChallengeStatus(ctx context.Context, challengeID string, status model.ChallengeStatus, resolvedBy string, note string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE challenges SET status = $1, resolved_by = $2, resolution_note = $3, resolved_at = $4
		WHERE challenge_id = $5 AND status = 'open'`,
		string(status), resolvedBy, note, time.Now().UTC(), challengeID)
	if err != nil {
		return fmt.Errorf("updating challenge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PgStore) UpdateChallengeLinks(ctx context.Context, challengeID string, evidenceIDs []string) error {
	linkedIDs, _ := json.Marshal(evidenceIDs)
	tag, err := s.pool.Exec(ctx, `
		UPDATE challenges SET linked_evidence_ids = $1 WHERE challenge_id = $2`,
		linkedIDs, challengeID)
	if err != nil {
		return fmt.Errorf("updating challenge links: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Decisions ---

func (s *PgStore) CreateDecision(ctx context.Context, d *model.Decision) error {
	if err := d.Validate(); err != nil {
		return fmt.Errorf("invalid decision: %w", err)
	}

	linkedEvIDs, _ := json.Marshal(d.LinkedEvidenceIDs)
	linkedChIDs, _ := json.Marshal(d.LinkedChallengeIDs)
	requiredActions, _ := json.Marshal(d.RequiredActions)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO decisions (decision_id, proposal_id, tenant_id, outcome, reason_code,
			summary, decided_at, decided_by, linked_evidence_ids, linked_challenge_ids,
			required_actions, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		d.DecisionID, d.ProposalID, d.TenantID, string(d.Outcome), string(d.ReasonCode),
		d.Summary, d.DecidedAt, d.DecidedBy, linkedEvIDs, linkedChIDs,
		requiredActions, d.ExpiresAt,
	)
	if err != nil {
		return wrapPgError(err)
	}
	return nil
}

func (s *PgStore) GetDecision(ctx context.Context, decisionID string) (*model.Decision, error) {
	return s.scanDecision(ctx, `
		SELECT decision_id, proposal_id, tenant_id, outcome, reason_code,
			summary, decided_at, decided_by, linked_evidence_ids, linked_challenge_ids,
			required_actions, expires_at
		FROM decisions WHERE decision_id = $1`, decisionID)
}

func (s *PgStore) GetLatestDecisionByProposal(ctx context.Context, proposalID string) (*model.Decision, error) {
	return s.scanDecision(ctx, `
		SELECT decision_id, proposal_id, tenant_id, outcome, reason_code,
			summary, decided_at, decided_by, linked_evidence_ids, linked_challenge_ids,
			required_actions, expires_at
		FROM decisions WHERE proposal_id = $1
		ORDER BY decided_at DESC LIMIT 1`, proposalID)
}

func (s *PgStore) scanDecision(ctx context.Context, query string, args ...any) (*model.Decision, error) {
	var d model.Decision
	var outcome, reasonCode string
	var linkedEvIDs, linkedChIDs, requiredActions []byte

	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&d.DecisionID, &d.ProposalID, &d.TenantID, &outcome, &reasonCode,
		&d.Summary, &d.DecidedAt, &d.DecidedBy, &linkedEvIDs, &linkedChIDs,
		&requiredActions, &d.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying decision: %w", err)
	}

	d.Outcome = model.DecisionOutcome(outcome)
	d.ReasonCode = model.ReasonCode(reasonCode)
	json.Unmarshal(linkedEvIDs, &d.LinkedEvidenceIDs)
	json.Unmarshal(linkedChIDs, &d.LinkedChallengeIDs)
	json.Unmarshal(requiredActions, &d.RequiredActions)

	return &d, nil
}

// --- Helpers ---

func wrapPgError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrAlreadyExists
	}
	return fmt.Errorf("database error: %w", err)
}
