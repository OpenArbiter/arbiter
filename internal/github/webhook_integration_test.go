//go:build integration

package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openarbiter/arbiter/internal/queue"
)

const integrationWebhookSecret = "integration-test-secret"

func testWebhookEnv(t *testing.T) (*WebhookHandler, *queue.Queue) {
	t.Helper()
	redisURL := os.Getenv("ARBITER_REDIS_URL")
	if redisURL == "" {
		t.Skip("ARBITER_REDIS_URL not set")
	}
	q, err := queue.New(redisURL)
	if err != nil {
		t.Fatalf("creating queue: %v", err)
	}
	t.Cleanup(func() { q.Close() })

	stats := NewStats()
	handler := NewWebhookHandler(integrationWebhookSecret, q, stats)
	return handler, q
}

// dequeueExpected keeps dequeueing until it finds a job of the expected type or times out.
func dequeueExpected(t *testing.T, q *queue.Queue, expected queue.JobType) *queue.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := q.Dequeue(t.Context(), 1*time.Second)
		if err != nil {
			continue
		}
		if job == nil {
			continue
		}
		if job.Type == expected {
			return job
		}
		// Wrong type — keep looking
	}
	t.Fatalf("timed out waiting for job type %q", expected)
	return nil
}

func signPayloadIntegration(body []byte) string {
	mac := hmac.New(sha256.New, []byte(integrationWebhookSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func sendWebhookIntegration(t *testing.T, handler *WebhookHandler, event string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", signPayloadIntegration(body))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", "integration-"+event+"-"+time.Now().Format("150405.000"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestIntegration_WebhookToQueue_PROpened(t *testing.T) {
	handler, q := testWebhookEnv(t)

	payload := map[string]any{
		"action":       "opened",
		"installation": map[string]any{"id": 88888},
		"pull_request": map[string]any{
			"number": 1, "title": "Test",
			"head": map[string]any{"sha": "abc"},
		},
	}

	w := sendWebhookIntegration(t, handler, "pull_request", payload)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}

	job := dequeueExpected(t, q, queue.JobPROpened)
	if job.Type != queue.JobPROpened {
		t.Errorf("type = %q, want pr_opened", job.Type)
	}
	if job.InstallationID != 88888 {
		t.Errorf("installation = %d, want 88888", job.InstallationID)
	}
}

func TestIntegration_WebhookToQueue_PRSynchronize(t *testing.T) {
	handler, q := testWebhookEnv(t)

	payload := map[string]any{
		"action":       "synchronize",
		"installation": map[string]any{"id": 88888},
		"pull_request": map[string]any{
			"number": 1, "title": "Test",
			"head": map[string]any{"sha": "def"},
		},
	}

	w := sendWebhookIntegration(t, handler, "pull_request", payload)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}

	job := dequeueExpected(t, q, queue.JobPRSynchronize)
	if job.Type != queue.JobPRSynchronize {
		t.Errorf("type = %q, want pr_synchronize", job.Type)
	}
}

func TestIntegration_WebhookToQueue_CheckRunCompleted(t *testing.T) {
	handler, q := testWebhookEnv(t)

	payload := map[string]any{
		"action": "completed",
		"check_run": map[string]any{
			"name": "test", "status": "completed", "conclusion": "success",
			"head_sha": "abc", "app": map[string]any{"slug": "ci"},
		},
		"installation": map[string]any{"id": 88888},
		"repository":   map[string]any{"full_name": "test/repo"},
	}

	w := sendWebhookIntegration(t, handler, "check_run", payload)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}

	job := dequeueExpected(t, q, queue.JobCheckRunCompleted)
	if job.Type != queue.JobCheckRunCompleted {
		t.Errorf("type = %q, want check_run_completed", job.Type)
	}
}

func TestIntegration_WebhookToQueue_ReviewSubmitted(t *testing.T) {
	handler, q := testWebhookEnv(t)

	payload := map[string]any{
		"action": "submitted",
		"review": map[string]any{
			"id": 1, "state": "changes_requested", "body": "needs work",
			"user": map[string]any{"login": "reviewer"},
		},
		"pull_request": map[string]any{
			"number": 5, "head": map[string]any{"sha": "xyz"},
		},
		"installation": map[string]any{"id": 88888},
		"repository":   map[string]any{"full_name": "test/repo"},
	}

	w := sendWebhookIntegration(t, handler, "pull_request_review", payload)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}

	job := dequeueExpected(t, q, queue.JobPRReviewSubmitted)
	if job.Type != queue.JobPRReviewSubmitted {
		t.Errorf("type = %q, want pr_review_submitted", job.Type)
	}
}

func TestIntegration_WebhookToQueue_StatusEvent(t *testing.T) {
	handler, q := testWebhookEnv(t)

	payload := map[string]any{
		"sha": "abc123", "state": "success", "context": "ci/test",
		"installation": map[string]any{"id": 88888},
		"repository":   map[string]any{"full_name": "test/repo"},
	}

	w := sendWebhookIntegration(t, handler, "status", payload)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}

	job := dequeueExpected(t, q, queue.JobStatusEvent)
	if job.Type != queue.JobStatusEvent {
		t.Errorf("type = %q, want status", job.Type)
	}
}

func TestIntegration_WebhookToQueue_IgnoredEvent(t *testing.T) {
	handler, _ := testWebhookEnv(t)

	payload := map[string]any{"action": "created"}
	w := sendWebhookIntegration(t, handler, "issues", payload)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (ignored)", w.Code)
	}
}

func TestIntegration_WebhookToQueue_BadSignature(t *testing.T) {
	handler, _ := testWebhookEnv(t)

	body := []byte(`{"action":"opened"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", "sha256=0000000000000000000000000000000000000000000000000000000000000000")
	req.Header.Set("X-GitHub-Event", "pull_request")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestIntegration_WebhookToQueue_StatsUpdated(t *testing.T) {
	handler, _ := testWebhookEnv(t)

	// Valid webhook
	payload := map[string]any{
		"action":       "opened",
		"installation": map[string]any{"id": 88888},
		"pull_request": map[string]any{"number": 1, "head": map[string]any{"sha": "a"}},
	}
	sendWebhookIntegration(t, handler, "pull_request", payload)

	// Ignored event
	sendWebhookIntegration(t, handler, "issues", map[string]any{"action": "created"})

	if handler.stats.webhooksReceived.Load() < 2 {
		t.Errorf("webhooks_received = %d, want >= 2", handler.stats.webhooksReceived.Load())
	}
	if handler.stats.webhooksIgnored.Load() < 1 {
		t.Errorf("webhooks_ignored = %d, want >= 1", handler.stats.webhooksIgnored.Load())
	}
}
