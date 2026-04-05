//go:build integration

package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

func testQueue(t *testing.T) *Queue {
	t.Helper()
	redisURL := os.Getenv("ARBITER_REDIS_URL")
	if redisURL == "" {
		t.Skip("ARBITER_REDIS_URL not set, skipping integration test")
	}
	q, err := New(redisURL)
	if err != nil {
		t.Fatalf("creating queue: %v", err)
	}
	// Use a unique queue key per test to avoid cross-test interference
	q.queueKey = "arbiter:test:" + t.Name()
	t.Cleanup(func() {
		q.client.Del(context.Background(), q.queueKey)
		q.client.Del(context.Background(), deadLetterKey+":"+t.Name())
		q.Close()
	})
	q.client.Del(context.Background(), q.queueKey)
	return q
}

func TestQueue_Ping(t *testing.T) {
	q := testQueue(t)
	if err := q.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestQueue_EnqueueDequeue(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	job := Job{
		ID:             "job-1",
		Type:           JobPROpened,
		InstallationID: 12345,
		Payload:        json.RawMessage(`{"pr_number": 42}`),
		CreatedAt:      time.Now().UTC(),
	}

	if err := q.Enqueue(ctx, &job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	got, err := q.Dequeue(ctx, 1*time.Second)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got == nil {
		t.Fatal("expected job, got nil")
	}
	if got.ID != "job-1" {
		t.Errorf("ID = %q, want job-1", got.ID)
	}
	if got.Type != JobPROpened {
		t.Errorf("Type = %q, want pr_opened", got.Type)
	}
	if got.InstallationID != 12345 {
		t.Errorf("InstallationID = %d, want 12345", got.InstallationID)
	}
}

func TestQueue_FIFO(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	for i, id := range []string{"first", "second", "third"} {
		job := Job{
			ID:             id,
			Type:           JobPROpened,
			InstallationID: int64(i),
			Payload:        json.RawMessage(`{}`),
			CreatedAt:      time.Now().UTC(),
		}
		q.Enqueue(ctx, &job)
	}

	for _, expected := range []string{"first", "second", "third"} {
		got, err := q.Dequeue(ctx, 1*time.Second)
		if err != nil {
			t.Fatalf("Dequeue: %v", err)
		}
		if got.ID != expected {
			t.Errorf("got %q, want %q", got.ID, expected)
		}
	}
}

func TestQueue_DequeueTimeout(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	got, err := q.Dequeue(ctx, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil on timeout, got %+v", got)
	}
}

func TestQueue_Len(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	n, _ := q.Len(ctx)
	if n != 0 {
		t.Errorf("initial len = %d, want 0", n)
	}

	q.Enqueue(ctx, &Job{ID: "a", Type: JobPROpened, Payload: json.RawMessage(`{}`), CreatedAt: time.Now()})
	q.Enqueue(ctx, &Job{ID: "b", Type: JobPROpened, Payload: json.RawMessage(`{}`), CreatedAt: time.Now()})

	n, _ = q.Len(ctx)
	if n != 2 {
		t.Errorf("len = %d, want 2", n)
	}
}

func TestQueue_DequeueContextCancelled(t *testing.T) {
	q := testQueue(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := q.Dequeue(ctx, 5*time.Second)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestQueue_Retry(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	job := &Job{
		ID:        "retry-job",
		Type:      JobPROpened,
		Payload:   json.RawMessage(`{}`),
		CreatedAt: time.Now().UTC(),
		Attempts:  0,
	}

	// Retry should re-enqueue with incremented attempt count
	if err := q.Retry(ctx, job, fmt.Errorf("temporary failure")); err != nil {
		t.Fatalf("Retry: %v", err)
	}

	got, err := q.Dequeue(ctx, 1*time.Second)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", got.Attempts)
	}
	if got.LastError != "temporary failure" {
		t.Errorf("LastError = %q, want 'temporary failure'", got.LastError)
	}
}

func TestQueue_RetryExceedsMax_MovesToDeadLetter(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	// Also clean dead letter
	q.client.Del(ctx, deadLetterKey)

	job := &Job{
		ID:        "dead-job",
		Type:      JobPROpened,
		Payload:   json.RawMessage(`{}`),
		CreatedAt: time.Now().UTC(),
		Attempts:  maxRetries - 1, // one more retry will exceed
	}

	if err := q.Retry(ctx, job, fmt.Errorf("permanent failure")); err != nil {
		t.Fatalf("Retry: %v", err)
	}

	// Should NOT be in main queue
	mainLen, _ := q.Len(ctx)
	if mainLen != 0 {
		t.Errorf("main queue len = %d, want 0", mainLen)
	}

	// Should be in dead letter queue
	dlLen, _ := q.DeadLetterLen(ctx)
	if dlLen != 1 {
		t.Errorf("dead letter len = %d, want 1", dlLen)
	}
}

func TestQueue_DrainDeadLetter(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	q.client.Del(ctx, deadLetterKey)

	// Put a job in dead letter
	job := &Job{
		ID:        "drain-job",
		Type:      JobPROpened,
		Payload:   json.RawMessage(`{}`),
		CreatedAt: time.Now().UTC(),
		Attempts:  maxRetries,
	}
	q.Retry(ctx, job, fmt.Errorf("final failure"))

	jobs, err := q.DrainDeadLetter(ctx)
	if err != nil {
		t.Fatalf("DrainDeadLetter: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}
	if jobs[0].ID != "drain-job" {
		t.Errorf("ID = %q, want drain-job", jobs[0].ID)
	}

	// Dead letter should be empty now
	dlLen, _ := q.DeadLetterLen(ctx)
	if dlLen != 0 {
		t.Errorf("dead letter len = %d, want 0 after drain", dlLen)
	}
}
