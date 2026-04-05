//go:build integration

package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/openarbiter/arbiter/internal/model"
)

func testStore(t *testing.T) *PgStore {
	t.Helper()
	dbURL := os.Getenv("ARBITER_DB_URL")
	if dbURL == "" {
		t.Skip("ARBITER_DB_URL not set, skipping integration test")
	}
	ctx := context.Background()
	s, err := NewPgStore(ctx, dbURL)
	if err != nil {
		t.Fatalf("connecting to database: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func cleanDB(t *testing.T, s *PgStore) {
	t.Helper()
	ctx := context.Background()
	// Delete in reverse FK order
	for _, table := range []string{"decisions", "challenges", "evidence", "proposals", "tasks", "principals"} {
		_, err := s.pool.Exec(ctx, "DELETE FROM "+table)
		if err != nil {
			t.Fatalf("cleaning table %s: %v", table, err)
		}
	}
}

var testTime = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

func makeTask(id string) model.Task {
	return model.Task{
		TaskID:          id,
		TenantID:        "tenant-1",
		Title:           "Test task " + id,
		Intent:          "Test intent",
		ExpectedOutcome: "Test outcome",
		NonGoals:        []string{"non-goal-1"},
		RiskLevel:       model.RiskMedium,
		ScopeHint:       model.Selector{Paths: []string{"src/"}},
		PolicyProfile:   "default",
		CreatedAt:       testTime,
	}
}

func makeProposal(id, taskID string) model.Proposal {
	return model.Proposal{
		ProposalID:      id,
		TaskID:          taskID,
		TenantID:        "tenant-1",
		SubmittedBy:     "principal-1",
		ChangeRef:       model.ExternalRef{RefType: model.RefPullRequest, Provider: model.ProviderGitHub, ExternalID: "pr-42"},
		DeclaredScope:   model.Selector{Paths: []string{"src/auth.go"}},
		BehaviorSummary: "Fix auth bug",
		Assumptions:     []string{"Token is expired"},
		Confidence:      model.ConfidenceHigh,
		Status:          model.ProposalOpen,
		CreatedAt:       testTime,
	}
}

func makeEvidence(id, proposalID string, result model.EvidenceResult) model.Evidence {
	return model.Evidence{
		EvidenceID:   id,
		ProposalID:   proposalID,
		TenantID:     "tenant-1",
		EvidenceType: model.EvidenceTestSuite,
		Subject:      "auth_test.go",
		Result:       result,
		Confidence:   model.ConfidenceHigh,
		Source:       "github-actions",
		CreatedAt:    testTime,
	}
}

func makeChallenge(id, proposalID string, severity model.Severity) model.Challenge {
	return model.Challenge{
		ChallengeID:   id,
		ProposalID:    proposalID,
		TenantID:      "tenant-1",
		RaisedBy:      "principal-2",
		ChallengeType: model.ChallengeScopeMismatch,
		Target:        "src/session.go",
		Severity:      severity,
		Summary:       "Scope mismatch detected",
		Status:        model.ChallengeOpen,
		CreatedAt:     testTime,
	}
}

func makeDecision(id, proposalID string, outcome model.DecisionOutcome) model.Decision {
	return model.Decision{
		DecisionID: id,
		ProposalID: proposalID,
		TenantID:   "tenant-1",
		Outcome:    outcome,
		ReasonCode: model.ReasonAllGatesPassed,
		Summary:    "All checks passed",
		DecidedAt:  testTime,
		DecidedBy:  "arbiter-engine",
	}
}

// --- Task Tests ---

func TestPgStore_Task_CreateAndGet(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	if err := s.CreateTask(ctx, &task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := s.GetTask(ctx, "task-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	if got.TaskID != task.TaskID {
		t.Errorf("TaskID = %q, want %q", got.TaskID, task.TaskID)
	}
	if got.TenantID != task.TenantID {
		t.Errorf("TenantID = %q, want %q", got.TenantID, task.TenantID)
	}
	if got.RiskLevel != task.RiskLevel {
		t.Errorf("RiskLevel = %q, want %q", got.RiskLevel, task.RiskLevel)
	}
	if len(got.NonGoals) != 1 || got.NonGoals[0] != "non-goal-1" {
		t.Errorf("NonGoals = %v, want [non-goal-1]", got.NonGoals)
	}
}

func TestPgStore_Task_Duplicate(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-dup")
	if err := s.CreateTask(ctx, &task); err != nil {
		t.Fatalf("first CreateTask: %v", err)
	}
	err := s.CreateTask(ctx, &task)
	if err != ErrAlreadyExists {
		t.Errorf("duplicate CreateTask error = %v, want ErrAlreadyExists", err)
	}
}

func TestPgStore_Task_NotFound(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	_, err := s.GetTask(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("GetTask error = %v, want ErrNotFound", err)
	}
}

func TestPgStore_Task_ListByTenant(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	t1 := makeTask("task-a")
	t1.CreatedAt = testTime
	t2 := makeTask("task-b")
	t2.CreatedAt = testTime.Add(time.Hour)
	t3 := makeTask("task-c")
	t3.TenantID = "tenant-other"

	for _, task := range []model.Task{t1, t2, t3} {
		if err := s.CreateTask(ctx, &task); err != nil {
			t.Fatalf("CreateTask %s: %v", task.TaskID, err)
		}
	}

	tasks, err := s.ListTasksByTenant(ctx, "tenant-1", 10, 0)
	if err != nil {
		t.Fatalf("ListTasksByTenant: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	// Should be ordered by created_at DESC
	if tasks[0].TaskID != "task-b" {
		t.Errorf("first task = %q, want task-b (most recent)", tasks[0].TaskID)
	}
}

func TestPgStore_Task_ValidationError(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	task := model.Task{} // invalid
	err := s.CreateTask(ctx, &task)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

// --- Proposal Tests ---

func TestPgStore_Proposal_CreateAndGet(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)

	prop := makeProposal("prop-1", "task-1")
	if err := s.CreateProposal(ctx, &prop); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	got, err := s.GetProposal(ctx, "prop-1")
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}

	if got.ProposalID != prop.ProposalID {
		t.Errorf("ProposalID = %q, want %q", got.ProposalID, prop.ProposalID)
	}
	if got.Status != model.ProposalOpen {
		t.Errorf("Status = %q, want open", got.Status)
	}
	if got.ChangeRef.ExternalID != "pr-42" {
		t.Errorf("ChangeRef.ExternalID = %q, want pr-42", got.ChangeRef.ExternalID)
	}
}

func TestPgStore_Proposal_UpdateStatus(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)
	prop := makeProposal("prop-1", "task-1")
	s.CreateProposal(ctx, &prop)

	if err := s.UpdateProposalStatus(ctx, "prop-1", model.ProposalWithdrawn); err != nil {
		t.Fatalf("UpdateProposalStatus: %v", err)
	}

	got, _ := s.GetProposal(ctx, "prop-1")
	if got.Status != model.ProposalWithdrawn {
		t.Errorf("Status = %q, want withdrawn", got.Status)
	}
}

func TestPgStore_Proposal_ListByTask(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)

	p1 := makeProposal("prop-1", "task-1")
	p1.CreatedAt = testTime
	p2 := makeProposal("prop-2", "task-1")
	p2.CreatedAt = testTime.Add(time.Hour)

	s.CreateProposal(ctx, &p1)
	s.CreateProposal(ctx, &p2)

	proposals, err := s.ListProposalsByTask(ctx, "task-1")
	if err != nil {
		t.Fatalf("ListProposalsByTask: %v", err)
	}
	if len(proposals) != 2 {
		t.Fatalf("got %d proposals, want 2", len(proposals))
	}
}

func TestPgStore_Proposal_ListOpen(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)

	p1 := makeProposal("prop-1", "task-1")
	p2 := makeProposal("prop-2", "task-1")
	p2.Status = model.ProposalWithdrawn

	s.CreateProposal(ctx, &p1)
	s.CreateProposal(ctx, &p2)

	open, err := s.ListOpenProposalsByTenant(ctx, "tenant-1", 10, 0)
	if err != nil {
		t.Fatalf("ListOpenProposalsByTenant: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("got %d open proposals, want 1", len(open))
	}
}

// --- Evidence Tests ---

func TestPgStore_Evidence_CreateAndGet(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)
	prop := makeProposal("prop-1", "task-1")
	s.CreateProposal(ctx, &prop)

	ev := makeEvidence("ev-1", "prop-1", model.EvidencePass)
	if err := s.CreateEvidence(ctx, &ev); err != nil {
		t.Fatalf("CreateEvidence: %v", err)
	}

	got, err := s.GetEvidence(ctx, "ev-1")
	if err != nil {
		t.Fatalf("GetEvidence: %v", err)
	}
	if got.Result != model.EvidencePass {
		t.Errorf("Result = %q, want pass", got.Result)
	}
}

func TestPgStore_Evidence_ListByProposal(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)
	prop := makeProposal("prop-1", "task-1")
	s.CreateProposal(ctx, &prop)

	ev1 := makeEvidence("ev-1", "prop-1", model.EvidencePass)
	ev2 := makeEvidence("ev-2", "prop-1", model.EvidenceFail)
	s.CreateEvidence(ctx, &ev1)
	s.CreateEvidence(ctx, &ev2)

	evidence, err := s.ListEvidenceByProposal(ctx, "prop-1")
	if err != nil {
		t.Fatalf("ListEvidenceByProposal: %v", err)
	}
	if len(evidence) != 2 {
		t.Fatalf("got %d evidence, want 2", len(evidence))
	}
}

// --- Challenge Tests ---

func TestPgStore_Challenge_CreateAndGet(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)
	prop := makeProposal("prop-1", "task-1")
	s.CreateProposal(ctx, &prop)

	ch := makeChallenge("ch-1", "prop-1", model.SeverityHigh)
	if err := s.CreateChallenge(ctx, &ch); err != nil {
		t.Fatalf("CreateChallenge: %v", err)
	}

	got, err := s.GetChallenge(ctx, "ch-1")
	if err != nil {
		t.Fatalf("GetChallenge: %v", err)
	}
	if got.Severity != model.SeverityHigh {
		t.Errorf("Severity = %q, want high", got.Severity)
	}
	if got.Status != model.ChallengeOpen {
		t.Errorf("Status = %q, want open", got.Status)
	}
}

func TestPgStore_Challenge_Resolve(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)
	prop := makeProposal("prop-1", "task-1")
	s.CreateProposal(ctx, &prop)
	ch := makeChallenge("ch-1", "prop-1", model.SeverityHigh)
	s.CreateChallenge(ctx, &ch)

	if err := s.ResolveChallenge(ctx, "ch-1", "principal-1", "Fixed the issue"); err != nil {
		t.Fatalf("ResolveChallenge: %v", err)
	}

	got, _ := s.GetChallenge(ctx, "ch-1")
	if got.Status != model.ChallengeResolved {
		t.Errorf("Status = %q, want resolved", got.Status)
	}
	if *got.ResolvedBy != "principal-1" {
		t.Errorf("ResolvedBy = %q, want principal-1", *got.ResolvedBy)
	}
}

func TestPgStore_Challenge_Dismiss(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)
	prop := makeProposal("prop-1", "task-1")
	s.CreateProposal(ctx, &prop)
	ch := makeChallenge("ch-1", "prop-1", model.SeverityLow)
	s.CreateChallenge(ctx, &ch)

	if err := s.DismissChallenge(ctx, "ch-1", "principal-1", "Not relevant"); err != nil {
		t.Fatalf("DismissChallenge: %v", err)
	}

	got, _ := s.GetChallenge(ctx, "ch-1")
	if got.Status != model.ChallengeDismissed {
		t.Errorf("Status = %q, want dismissed", got.Status)
	}
}

func TestPgStore_Challenge_ResolveAlreadyResolved(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)
	prop := makeProposal("prop-1", "task-1")
	s.CreateProposal(ctx, &prop)
	ch := makeChallenge("ch-1", "prop-1", model.SeverityHigh)
	s.CreateChallenge(ctx, &ch)

	s.ResolveChallenge(ctx, "ch-1", "principal-1", "Fixed")
	err := s.ResolveChallenge(ctx, "ch-1", "principal-1", "Fixed again")
	if err != ErrNotFound {
		t.Errorf("re-resolve error = %v, want ErrNotFound", err)
	}
}

func TestPgStore_Challenge_ListOpen(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)
	prop := makeProposal("prop-1", "task-1")
	s.CreateProposal(ctx, &prop)

	ch1 := makeChallenge("ch-1", "prop-1", model.SeverityHigh)
	ch2 := makeChallenge("ch-2", "prop-1", model.SeverityLow)
	s.CreateChallenge(ctx, &ch1)
	s.CreateChallenge(ctx, &ch2)
	s.ResolveChallenge(ctx, "ch-2", "principal-1", "Fixed")

	open, err := s.ListOpenChallengesByProposal(ctx, "prop-1")
	if err != nil {
		t.Fatalf("ListOpenChallengesByProposal: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("got %d open challenges, want 1", len(open))
	}
	if open[0].ChallengeID != "ch-1" {
		t.Errorf("open challenge = %q, want ch-1", open[0].ChallengeID)
	}
}

// --- Decision Tests ---

func TestPgStore_Decision_CreateAndGet(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)
	prop := makeProposal("prop-1", "task-1")
	s.CreateProposal(ctx, &prop)

	dec := makeDecision("dec-1", "prop-1", model.DecisionAccepted)
	if err := s.CreateDecision(ctx, &dec); err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}

	got, err := s.GetDecision(ctx, "dec-1")
	if err != nil {
		t.Fatalf("GetDecision: %v", err)
	}
	if got.Outcome != model.DecisionAccepted {
		t.Errorf("Outcome = %q, want accepted", got.Outcome)
	}
}

func TestPgStore_Decision_LatestByProposal(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	task := makeTask("task-1")
	s.CreateTask(ctx, &task)
	prop := makeProposal("prop-1", "task-1")
	s.CreateProposal(ctx, &prop)

	d1 := makeDecision("dec-1", "prop-1", model.DecisionNeedsAction)
	d1.DecidedAt = testTime
	d2 := makeDecision("dec-2", "prop-1", model.DecisionAccepted)
	d2.DecidedAt = testTime.Add(time.Hour)

	s.CreateDecision(ctx, &d1)
	s.CreateDecision(ctx, &d2)

	latest, err := s.GetLatestDecisionByProposal(ctx, "prop-1")
	if err != nil {
		t.Fatalf("GetLatestDecisionByProposal: %v", err)
	}
	if latest.DecisionID != "dec-2" {
		t.Errorf("latest decision = %q, want dec-2", latest.DecisionID)
	}
	if latest.Outcome != model.DecisionAccepted {
		t.Errorf("latest outcome = %q, want accepted", latest.Outcome)
	}
}

func TestPgStore_Decision_NotFound(t *testing.T) {
	s := testStore(t)
	cleanDB(t, s)
	ctx := context.Background()

	_, err := s.GetLatestDecisionByProposal(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// --- Ping ---

func TestPgStore_Ping(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Errorf("Ping: %v", err)
	}
}
