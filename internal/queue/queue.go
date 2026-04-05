package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultQueueKey = "arbiter:jobs"

// JobType identifies the kind of work to be done.
type JobType string

const (
	JobPROpened        JobType = "pr_opened"
	JobPRSynchronize   JobType = "pr_synchronize"
	JobPRClosed        JobType = "pr_closed"
	JobCheckRunCompleted JobType = "check_run_completed"
)

// Job represents a unit of work to be processed asynchronously.
type Job struct {
	ID           string          `json:"id"`
	Type         JobType         `json:"type"`
	InstallationID int64         `json:"installation_id"`
	Payload      json.RawMessage `json:"payload"`
	CreatedAt    time.Time       `json:"created_at"`
}

// Queue manages async job processing via Redis.
type Queue struct {
	client   *redis.Client
	queueKey string
}

// New creates a new Redis-backed Queue.
func New(redisURL string) (*Queue, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parsing redis URL: %w", err)
	}
	client := redis.NewClient(opts)
	return &Queue{client: client, queueKey: defaultQueueKey}, nil
}

// Ping checks the Redis connection.
func (q *Queue) Ping(ctx context.Context) error {
	return q.client.Ping(ctx).Err()
}

// Close shuts down the Redis connection.
func (q *Queue) Close() error {
	return q.client.Close()
}

// Enqueue adds a job to the queue.
func (q *Queue) Enqueue(ctx context.Context, job Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshaling job: %w", err)
	}
	if err := q.client.LPush(ctx, q.queueKey, data).Err(); err != nil {
		return fmt.Errorf("enqueueing job: %w", err)
	}
	slog.InfoContext(ctx, "job enqueued",
		"job_id", job.ID,
		"job_type", job.Type,
		"installation_id", job.InstallationID,
	)
	return nil
}

// Dequeue blocks until a job is available, then returns it.
// Returns context error if the context is cancelled.
func (q *Queue) Dequeue(ctx context.Context, timeout time.Duration) (*Job, error) {
	result, err := q.client.BRPop(ctx, timeout, q.queueKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // timeout, no job
		}
		return nil, fmt.Errorf("dequeueing job: %w", err)
	}

	// BRPop returns [key, value]
	if len(result) < 2 {
		return nil, fmt.Errorf("unexpected BRPop result: %v", result)
	}

	var job Job
	if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
		return nil, fmt.Errorf("unmarshaling job: %w", err)
	}

	slog.InfoContext(ctx, "job dequeued",
		"job_id", job.ID,
		"job_type", job.Type,
		"installation_id", job.InstallationID,
	)
	return &job, nil
}

// Len returns the number of jobs currently in the queue.
func (q *Queue) Len(ctx context.Context) (int64, error) {
	return q.client.LLen(ctx, q.queueKey).Result()
}
