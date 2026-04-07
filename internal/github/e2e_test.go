//go:build e2e

package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openarbiter/arbiter/internal/config"
	"github.com/openarbiter/arbiter/internal/engine"
	"github.com/openarbiter/arbiter/internal/model"
	"github.com/openarbiter/arbiter/internal/queue"
	"github.com/openarbiter/arbiter/internal/store"
)

const testWebhookSecret = "e2e-test-secret"
const testTenantID = "github:99999"

type e2eEnv struct {
	store   store.Store
	queue   *queue.Queue
	handler *WebhookHandler
	stats   *Stats
}

func setupE2E(t *testing.T) *e2eEnv {
	t.Helper()
	dbURL := os.Getenv("ARBITER_DB_URL")
	if dbURL == "" {
		t.Skip("ARBITER_DB_URL not set")
	}
	redisURL := os.Getenv("ARBITER_REDIS_URL")
	if redisURL == "" {
		t.Skip("ARBITER_REDIS_URL not set")
	}

	ctx := context.Background()
	s, err := store.NewPgStore(ctx, dbURL)
	if err != nil {
		t.Fatalf("connecting to DB: %v", err)
	}

	q, err := queue.New(redisURL)
	if err != nil {
		t.Fatalf("connecting to Redis: %v", err)
	}

	stats := NewStats()
	handler := NewWebhookHandler(testWebhookSecret, q, stats)

	t.Cleanup(func() {
		s.Close()
		q.Close()
	})

	return &e2eEnv{store: s, queue: q, handler: handler, stats: stats}
}

func (e *e2eEnv) sendWebhook(t *testing.T, eventType string, payload any) int {
	t.Helper()
	body, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(testWebhookSecret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", eventType)
	req.Header.Set("X-GitHub-Delivery", fmt.Sprintf("e2e-%d", time.Now().UnixNano()))

	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, req)
	return w.Code
}

func (e *e2eEnv) drainQueue(t *testing.T) []*queue.Job {
	t.Helper()
	ctx := context.Background()
	var jobs []*queue.Job
	for {
		job, err := e.queue.Dequeue(ctx, 500*time.Millisecond)
		if err != nil || job == nil {
			break
		}
		jobs = append(jobs, job)
	}
	return jobs
}

func (e *e2eEnv) createProposal(t *testing.T, prNum int, sha string) string {
	t.Helper()
	ctx := context.Background()
	taskID := fmt.Sprintf("gh:e2e/repo:pr:%d", prNum)
	proposalID := fmt.Sprintf("gh:e2e/repo:pr:%d:sha:%s", prNum, sha)

	task := model.Task{
		TaskID: taskID, TenantID: testTenantID, Title: "E2E test",
		Intent: "test", ExpectedOutcome: "test", RiskLevel: model.RiskMedium,
		PolicyProfile: "default", CreatedAt: time.Now().UTC(),
	}
	e.store.CreateTask(ctx, &task)

	proposal := model.Proposal{
		ProposalID: proposalID, TaskID: taskID, TenantID: testTenantID,
		SubmittedBy: "e2e-user", BehaviorSummary: "E2E test",
		ChangeRef: model.ExternalRef{
			RefType: model.RefPullRequest, Provider: model.ProviderGitHub,
			ExternalID: fmt.Sprintf("%d", prNum),
		},
		Confidence: model.ConfidenceMedium, Status: model.ProposalOpen,
		CreatedAt: time.Now().UTC(),
	}
	e.store.CreateProposal(ctx, &proposal)
	return proposalID
}

func (e *e2eEnv) addEvidence(t *testing.T, proposalID string, evType model.EvidenceType, result model.EvidenceResult) {
	t.Helper()
	summary := fmt.Sprintf("%s: %s", evType, result)
	ev := model.Evidence{
		EvidenceID: fmt.Sprintf("e2e:%s:%d", evType, time.Now().UnixNano()),
		ProposalID: proposalID, TenantID: testTenantID,
		EvidenceType: evType, Subject: string(evType), Result: result,
		Confidence: model.ConfidenceHigh, Source: "e2e",
		CreatedAt: time.Now().UTC(), Summary: &summary,
	}
	if err := e.store.CreateEvidence(context.Background(), &ev); err != nil {
		t.Fatalf("creating evidence: %v", err)
	}
}

func (e *e2eEnv) evaluate(t *testing.T, proposalID string, cfg config.Config) engine.EvalResult {
	t.Helper()
	ctx := context.Background()
	proposal, err := e.store.GetProposal(ctx, proposalID)
	if err != nil {
		t.Fatalf("getting proposal: %v", err)
	}
	task, err := e.store.GetTask(ctx, proposal.TaskID)
	if err != nil {
		t.Fatalf("getting task: %v", err)
	}
	evidence, _ := e.store.ListEvidenceByProposal(ctx, proposalID)
	challenges, _ := e.store.ListChallengesByProposal(ctx, proposalID)

	evalCtx := engine.EvalContext{
		Task: *task, Proposal: *proposal,
		Evidence: evidence, Challenges: challenges, Config: cfg,
	}
	return engine.Evaluate(&evalCtx)
}

// =============================================================================
// Tests
// =============================================================================

func TestE2E_WebhookAcceptsValidSignature(t *testing.T) {
	env := setupE2E(t)
	code := env.sendWebhook(t, "pull_request", map[string]any{
		"action": "opened", "installation": map[string]any{"id": 99999},
		"pull_request": map[string]any{"number": 1, "head": map[string]any{"sha": "abc"}},
	})
	if code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", code)
	}
}

func TestE2E_WebhookRejectsInvalidSignature(t *testing.T) {
	env := setupE2E(t)
	body := []byte(`{"action":"opened"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	req.Header.Set("X-GitHub-Event", "pull_request")
	w := httptest.NewRecorder()
	env.handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestE2E_WebhookEnqueuesJob(t *testing.T) {
	env := setupE2E(t)
	env.sendWebhook(t, "pull_request", map[string]any{
		"action": "opened", "installation": map[string]any{"id": 99999},
		"pull_request": map[string]any{"number": 1, "head": map[string]any{"sha": "abc"}},
	})
	jobs := env.drainQueue(t)
	if len(jobs) == 0 {
		t.Fatal("no jobs in queue")
	}
	if jobs[0].Type != queue.JobPROpened {
		t.Errorf("job type = %q, want pr_opened", jobs[0].Type)
	}
}

func TestE2E_FullPipeline_AllPass(t *testing.T) {
	env := setupE2E(t)
	proposalID := env.createProposal(t, 200, "sha200")
	env.addEvidence(t, proposalID, model.EvidenceBuildCheck, model.EvidencePass)
	env.addEvidence(t, proposalID, model.EvidenceTestSuite, model.EvidencePass)

	result := env.evaluate(t, proposalID, config.DefaultConfig())
	if result.Decision.Outcome != model.DecisionAccepted {
		t.Errorf("outcome = %q, want accepted", result.Decision.Outcome)
	}
	if result.Decision.ReasonCode != model.ReasonAllGatesPassed {
		t.Errorf("reason = %q, want all_gates_passed", result.Decision.ReasonCode)
	}
}

func TestE2E_FullPipeline_BuildFails(t *testing.T) {
	env := setupE2E(t)
	proposalID := env.createProposal(t, 201, "sha201")
	env.addEvidence(t, proposalID, model.EvidenceBuildCheck, model.EvidenceFail)
	env.addEvidence(t, proposalID, model.EvidenceTestSuite, model.EvidencePass)

	result := env.evaluate(t, proposalID, config.DefaultConfig())
	if result.Decision.Outcome != model.DecisionRejected {
		t.Errorf("outcome = %q, want rejected", result.Decision.Outcome)
	}
}

func TestE2E_FullPipeline_NoEvidence(t *testing.T) {
	env := setupE2E(t)
	proposalID := env.createProposal(t, 202, "sha202")

	result := env.evaluate(t, proposalID, config.DefaultConfig())
	if result.Decision.Outcome != model.DecisionRejected {
		t.Errorf("outcome = %q, want rejected (no evidence)", result.Decision.Outcome)
	}
}

func TestE2E_ChallengeBlocksDespiteGreenCI(t *testing.T) {
	env := setupE2E(t)
	ctx := context.Background()
	proposalID := env.createProposal(t, 203, "sha203")
	env.addEvidence(t, proposalID, model.EvidenceBuildCheck, model.EvidencePass)
	env.addEvidence(t, proposalID, model.EvidenceTestSuite, model.EvidencePass)

	challenge := model.Challenge{
		ChallengeID: fmt.Sprintf("ch:e2e:203:%d", time.Now().UnixNano()),
		ProposalID: proposalID, TenantID: testTenantID,
		RaisedBy: "e2e-reviewer", ChallengeType: model.ChallengeHiddenBehaviorChange,
		Target: "test.go", Severity: model.SeverityHigh,
		Summary: "Security concern", Status: model.ChallengeOpen,
		CreatedAt: time.Now().UTC(),
	}
	env.store.CreateChallenge(ctx, &challenge)

	result := env.evaluate(t, proposalID, config.DefaultConfig())
	if result.Decision.Outcome != model.DecisionRejected {
		t.Errorf("outcome = %q, want rejected (open challenge)", result.Decision.Outcome)
	}
	if result.Decision.ReasonCode != model.ReasonUnresolvedHighSeverityChallenge {
		t.Errorf("reason = %q, want unresolved_high_severity_challenge", result.Decision.ReasonCode)
	}
}

func TestE2E_ChallengeResolvedAccepts(t *testing.T) {
	env := setupE2E(t)
	ctx := context.Background()
	proposalID := env.createProposal(t, 204, "sha204")
	env.addEvidence(t, proposalID, model.EvidenceBuildCheck, model.EvidencePass)
	env.addEvidence(t, proposalID, model.EvidenceTestSuite, model.EvidencePass)

	chID := fmt.Sprintf("ch:e2e:204:%d", time.Now().UnixNano())
	challenge := model.Challenge{
		ChallengeID: chID, ProposalID: proposalID, TenantID: testTenantID,
		RaisedBy: "e2e-reviewer", ChallengeType: model.ChallengeScopeMismatch,
		Target: "test.go", Severity: model.SeverityHigh,
		Summary: "Scope issue", Status: model.ChallengeOpen,
		CreatedAt: time.Now().UTC(),
	}
	env.store.CreateChallenge(ctx, &challenge)
	env.store.ResolveChallenge(ctx, chID, "e2e-user", "Fixed")

	result := env.evaluate(t, proposalID, config.DefaultConfig())
	if result.Decision.Outcome != model.DecisionAccepted {
		t.Errorf("outcome = %q, want accepted (challenge resolved)", result.Decision.Outcome)
	}
}

func TestE2E_WarnModeAccepts(t *testing.T) {
	env := setupE2E(t)
	proposalID := env.createProposal(t, 205, "sha205")
	// No evidence — would fail in enforce mode

	cfg := config.DefaultConfig()
	cfg.Gates.Mechanical.Mode = config.GateWarn
	cfg.Gates.Behavioral.Mode = config.GateWarn

	result := env.evaluate(t, proposalID, cfg)
	if result.Decision.Outcome != model.DecisionAccepted {
		t.Errorf("outcome = %q, want accepted (warn mode)", result.Decision.Outcome)
	}
}

func TestE2E_DecisionStored(t *testing.T) {
	env := setupE2E(t)
	ctx := context.Background()
	proposalID := env.createProposal(t, 206, "sha206")
	env.addEvidence(t, proposalID, model.EvidenceBuildCheck, model.EvidencePass)
	env.addEvidence(t, proposalID, model.EvidenceTestSuite, model.EvidencePass)

	result := env.evaluate(t, proposalID, config.DefaultConfig())

	// Store the decision
	decision := result.Decision
	decision.DecisionID = fmt.Sprintf("dec:e2e:206:%d", time.Now().UnixNano())
	if err := env.store.CreateDecision(ctx, &decision); err != nil {
		t.Fatalf("storing decision: %v", err)
	}

	// Retrieve and verify
	got, err := env.store.GetLatestDecisionByProposal(ctx, proposalID)
	if err != nil {
		t.Fatalf("getting decision: %v", err)
	}
	if got.Outcome != model.DecisionAccepted {
		t.Errorf("stored outcome = %q, want accepted", got.Outcome)
	}
	if len(got.LinkedEvidenceIDs) != 2 {
		t.Errorf("linked evidence = %d, want 2", len(got.LinkedEvidenceIDs))
	}
}
