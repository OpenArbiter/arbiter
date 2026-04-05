//go:build integration

package queue

import (
	"context"
	"encoding/json"
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
	t.Cleanup(func() {
		q.client.Del(context.Background(), q.queueKey)
		q.Close()
	})
	// Clear any leftover jobs
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
