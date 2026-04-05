package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/openarbiter/arbiter/internal/queue"
)

// WebhookHandler handles incoming GitHub webhook requests.
type WebhookHandler struct {
	webhookSecret []byte
	queue         *queue.Queue
	stats         *Stats
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(webhookSecret string, q *queue.Queue, stats *Stats) *WebhookHandler {
	return &WebhookHandler{
		webhookSecret: []byte(webhookSecret),
		queue:         q,
		stats:         stats,
	}
}

// ServeHTTP handles incoming webhook requests.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("reading webhook body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Validate signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if !h.validateSignature(body, signature) {
		slog.Warn("invalid webhook signature")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	deliveryID := r.Header.Get("X-GitHub-Delivery")

	// Attach correlation ID for end-to-end tracing
	ctx := WithCorrelationID(r.Context(), deliveryID)

	h.stats.webhooksReceived.Add(1)

	slog.InfoContext(ctx, "webhook received",
		"event", eventType,
		"delivery_id", deliveryID,
	)

	jobType, err := mapEventToJobType(eventType, body)
	if err != nil {
		h.stats.webhooksIgnored.Add(1)
		slog.DebugContext(ctx, "ignoring webhook event", "event", eventType, "reason", err.Error())
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ignored"}`)
		return
	}

	// Extract installation ID
	var payload struct {
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Warn("could not parse installation ID from webhook", "error", err)
	}

	job := queue.Job{
		ID:             deliveryID,
		Type:           jobType,
		InstallationID: payload.Installation.ID,
		Payload:        json.RawMessage(body),
		CreatedAt:      time.Now().UTC(),
	}

	if err := h.queue.Enqueue(ctx, &job); err != nil {
		slog.ErrorContext(ctx, "enqueueing webhook job", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprint(w, `{"status":"queued"}`)
}

// validateSignature checks the X-Hub-Signature-256 header against the webhook secret.
func (h *WebhookHandler) validateSignature(body []byte, signature string) bool {
	if len(h.webhookSecret) == 0 {
		return false
	}
	if signature == "" {
		return false
	}

	sig := strings.TrimPrefix(signature, "sha256=")
	expectedMAC, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, h.webhookSecret)
	mac.Write(body)
	actualMAC := mac.Sum(nil)

	return hmac.Equal(actualMAC, expectedMAC)
}

// mapEventToJobType maps a GitHub event type to a queue job type.
// Returns an error for events we don't handle.
func mapEventToJobType(eventType string, body []byte) (queue.JobType, error) {
	switch eventType {
	case "pull_request":
		var pr struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(body, &pr); err != nil {
			return "", fmt.Errorf("parsing pull_request payload: %w", err)
		}
		switch pr.Action {
		case "opened", "reopened":
			return queue.JobPROpened, nil
		case "synchronize":
			return queue.JobPRSynchronize, nil
		case "closed":
			return queue.JobPRClosed, nil
		}
		return "", fmt.Errorf("unhandled pull_request action: %s", pr.Action)

	case "check_run":
		var cr struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(body, &cr); err != nil {
			return "", fmt.Errorf("parsing check_run payload: %w", err)
		}
		if cr.Action == "completed" {
			return queue.JobCheckRunCompleted, nil
		}
		return "", fmt.Errorf("unhandled check_run action: %s", cr.Action)

	case "check_suite":
		var cs struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(body, &cs); err != nil {
			return "", fmt.Errorf("parsing check_suite payload: %w", err)
		}
		if cs.Action == "completed" {
			return queue.JobCheckSuiteCompleted, nil
		}
		return "", fmt.Errorf("unhandled check_suite action: %s", cs.Action)
	}

	return "", fmt.Errorf("unhandled event type: %s", eventType)
}
