-- Create core tables for Arbiter trust layer
-- All tables follow append-only pattern: no UPDATEs or DELETEs in normal operation
-- Idempotent: safe to run multiple times

CREATE TABLE IF NOT EXISTS principals (
    principal_id   TEXT PRIMARY KEY,
    principal_type TEXT NOT NULL CHECK (principal_type IN ('human', 'agent', 'service')),
    display_name   TEXT NOT NULL,
    origin_system  TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tasks (
    task_id          TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL,
    title            TEXT NOT NULL,
    intent           TEXT NOT NULL,
    expected_outcome TEXT NOT NULL,
    non_goals        JSONB NOT NULL DEFAULT '[]',
    risk_level       TEXT NOT NULL CHECK (risk_level IN ('low', 'medium', 'high')),
    scope_hint       JSONB NOT NULL DEFAULT '{}',
    policy_profile   TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Optional
    priority         INT,
    deadline         TIMESTAMPTZ,
    external_refs    JSONB DEFAULT '[]',
    parent_task_id   TEXT REFERENCES tasks(task_id)
);

CREATE INDEX IF NOT EXISTS idx_tasks_tenant ON tasks(tenant_id);
CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at DESC);

CREATE TABLE IF NOT EXISTS proposals (
    proposal_id          TEXT PRIMARY KEY,
    task_id              TEXT NOT NULL REFERENCES tasks(task_id),
    tenant_id            TEXT NOT NULL,
    submitted_by         TEXT NOT NULL,
    change_ref           JSONB NOT NULL,
    declared_scope       JSONB NOT NULL DEFAULT '{}',
    behavior_summary     TEXT NOT NULL,
    assumptions          JSONB NOT NULL DEFAULT '[]',
    confidence           TEXT NOT NULL CHECK (confidence IN ('low', 'medium', 'high')),
    status               TEXT NOT NULL CHECK (status IN ('open', 'updated', 'withdrawn')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Optional
    execution_ref            JSONB,
    supersedes_proposal_id   TEXT REFERENCES proposals(proposal_id),
    cost_hint                TEXT,
    strategy_hint            TEXT
);

CREATE INDEX IF NOT EXISTS idx_proposals_task ON proposals(task_id);
CREATE INDEX IF NOT EXISTS idx_proposals_tenant ON proposals(tenant_id);
CREATE INDEX IF NOT EXISTS idx_proposals_status ON proposals(status) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS idx_proposals_created ON proposals(created_at DESC);

CREATE TABLE IF NOT EXISTS evidence (
    evidence_id    TEXT PRIMARY KEY,
    proposal_id    TEXT NOT NULL REFERENCES proposals(proposal_id),
    tenant_id      TEXT NOT NULL,
    evidence_type  TEXT NOT NULL CHECK (evidence_type IN (
        'build_check', 'test_suite', 'scope_match', 'policy_check',
        'security_scan', 'impact_analysis', 'review_finding', 'benchmark_check'
    )),
    subject        TEXT NOT NULL,
    result         TEXT NOT NULL CHECK (result IN ('pass', 'fail', 'warn', 'info')),
    confidence     TEXT NOT NULL CHECK (confidence IN ('low', 'medium', 'high')),
    source         TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Optional
    artifact_id    TEXT,
    summary        TEXT,
    details        TEXT,
    expires_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_evidence_proposal ON evidence(proposal_id);
CREATE INDEX IF NOT EXISTS idx_evidence_tenant ON evidence(tenant_id);
CREATE INDEX IF NOT EXISTS idx_evidence_type ON evidence(evidence_type);
CREATE INDEX IF NOT EXISTS idx_evidence_created ON evidence(created_at DESC);

CREATE TABLE IF NOT EXISTS challenges (
    challenge_id       TEXT PRIMARY KEY,
    proposal_id        TEXT NOT NULL REFERENCES proposals(proposal_id),
    tenant_id          TEXT NOT NULL,
    raised_by          TEXT NOT NULL,
    challenge_type     TEXT NOT NULL CHECK (challenge_type IN (
        'hidden_behavior_change', 'insufficient_test_coverage', 'scope_mismatch',
        'policy_violation', 'likely_regression', 'unsupported_assumption'
    )),
    target             TEXT NOT NULL,
    severity           TEXT NOT NULL CHECK (severity IN ('low', 'medium', 'high')),
    summary            TEXT NOT NULL,
    status             TEXT NOT NULL CHECK (status IN ('open', 'resolved', 'dismissed')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Optional
    artifact_id        TEXT,
    linked_evidence_ids JSONB DEFAULT '[]',
    resolution_note    TEXT,
    resolved_by        TEXT,
    resolved_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_challenges_proposal ON challenges(proposal_id);
CREATE INDEX IF NOT EXISTS idx_challenges_tenant ON challenges(tenant_id);
CREATE INDEX IF NOT EXISTS idx_challenges_open ON challenges(proposal_id) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS idx_challenges_created ON challenges(created_at DESC);

CREATE TABLE IF NOT EXISTS decisions (
    decision_id          TEXT PRIMARY KEY,
    proposal_id          TEXT NOT NULL REFERENCES proposals(proposal_id),
    tenant_id            TEXT NOT NULL,
    outcome              TEXT NOT NULL CHECK (outcome IN ('accepted', 'rejected', 'needs_action', 'deferred')),
    reason_code          TEXT NOT NULL CHECK (reason_code IN (
        'insufficient_behavioral_evidence', 'unresolved_high_severity_challenge',
        'policy_violation', 'scope_exceeded', 'acceptable_low_risk',
        'human_review_required', 'mechanical_check_failed', 'all_gates_passed'
    )),
    summary              TEXT NOT NULL,
    decided_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_by           TEXT NOT NULL,

    -- Optional
    linked_evidence_ids  JSONB DEFAULT '[]',
    linked_challenge_ids JSONB DEFAULT '[]',
    required_actions     JSONB DEFAULT '[]',
    expires_at           TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_decisions_proposal ON decisions(proposal_id);
CREATE INDEX IF NOT EXISTS idx_decisions_tenant ON decisions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_decisions_outcome ON decisions(outcome);
CREATE INDEX IF NOT EXISTS idx_decisions_created ON decisions(decided_at DESC);
