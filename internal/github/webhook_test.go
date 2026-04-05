package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openarbiter/arbiter/internal/queue"
)

const testSecret = "test-webhook-secret"

func signPayload(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// --- Signature Validation ---

func TestValidateSignature_Valid(t *testing.T) {
	h := &WebhookHandler{webhookSecret: []byte(testSecret)}
	body := []byte(`{"action":"opened"}`)
	sig := signPayload(testSecret, string(body))

	if !h.validateSignature(body, sig) {
		t.Error("valid signature should pass")
	}
}

func TestValidateSignature_Invalid(t *testing.T) {
	h := &WebhookHandler{webhookSecret: []byte(testSecret)}
	body := []byte(`{"action":"opened"}`)

	if h.validateSignature(body, "sha256=deadbeef") {
		t.Error("invalid signature should fail")
	}
}

func TestValidateSignature_Empty(t *testing.T) {
	h := &WebhookHandler{webhookSecret: []byte(testSecret)}
	body := []byte(`{"action":"opened"}`)

	if h.validateSignature(body, "") {
		t.Error("empty signature should fail")
	}
}

func TestValidateSignature_NoSecret(t *testing.T) {
	h := &WebhookHandler{webhookSecret: nil}
	body := []byte(`{"action":"opened"}`)

	if h.validateSignature(body, signPayload(testSecret, string(body))) {
		t.Error("no secret configured should fail")
	}
}

func TestValidateSignature_BadHex(t *testing.T) {
	h := &WebhookHandler{webhookSecret: []byte(testSecret)}
	body := []byte(`{"action":"opened"}`)

	if h.validateSignature(body, "sha256=not-hex") {
		t.Error("bad hex should fail")
	}
}

func TestValidateSignature_WrongSecret(t *testing.T) {
	h := &WebhookHandler{webhookSecret: []byte(testSecret)}
	body := []byte(`{"action":"opened"}`)
	sig := signPayload("wrong-secret", string(body))

	if h.validateSignature(body, sig) {
		t.Error("wrong secret should fail")
	}
}

// --- Event Mapping ---

func TestMapEventToJobType(t *testing.T) {
	tests := []struct {
		name      string
		event     string
		body      string
		wantType  queue.JobType
		wantError bool
	}{
		{"pr opened", "pull_request", `{"action":"opened"}`, queue.JobPROpened, false},
		{"pr reopened", "pull_request", `{"action":"reopened"}`, queue.JobPROpened, false},
		{"pr synchronize", "pull_request", `{"action":"synchronize"}`, queue.JobPRSynchronize, false},
		{"pr closed", "pull_request", `{"action":"closed"}`, queue.JobPRClosed, false},
		{"pr labeled (ignored)", "pull_request", `{"action":"labeled"}`, "", true},
		{"check_run completed", "check_run", `{"action":"completed"}`, queue.JobCheckRunCompleted, false},
		{"check_run created (ignored)", "check_run", `{"action":"created"}`, "", true},
		{"push (ignored)", "push", `{}`, "", true},
		{"issues (ignored)", "issues", `{}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobType, err := mapEventToJobType(tt.event, []byte(tt.body))
			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if jobType != tt.wantType {
				t.Errorf("jobType = %q, want %q", jobType, tt.wantType)
			}
		})
	}
}

// --- HTTP Handler ---

func TestWebhookHandler_MethodNotAllowed(t *testing.T) {
	h := NewWebhookHandler(testSecret, nil)
	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestWebhookHandler_InvalidSignature(t *testing.T) {
	h := NewWebhookHandler(testSecret, nil)
	body := `{"action":"opened"}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")
	req.Header.Set("X-GitHub-Event", "pull_request")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestWebhookHandler_IgnoredEvent(t *testing.T) {
	h := NewWebhookHandler(testSecret, nil)
	body := `{"action":"labeled"}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signPayload(testSecret, body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (ignored events still acknowledged)", w.Code)
	}
}

func TestWebhookHandler_MissingSignature(t *testing.T) {
	h := NewWebhookHandler(testSecret, nil)
	body := `{"action":"opened"}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	// No signature header
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}
