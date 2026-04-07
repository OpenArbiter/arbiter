package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultQueueKey = "arbiter:jobs"
	deadLetterKey   = "arbiter:dead_letter"
	maxRetries      = 3
)

// JobType identifies the kind of work to be done.
type JobType string

const (
	JobPROpened            JobType = "pr_opened"
	JobPRSynchronize       JobType = "pr_synchronize"
	JobPRClosed            JobType = "pr_closed"
	JobCheckRunCompleted   JobType = "check_run_completed"
	JobCheckSuiteCompleted JobType = "check_suite_completed"
	JobPRReviewSubmitted   JobType = "pr_review_submitted"
)

// Job represents a unit of work to be processed asynchronously.
type Job struct {
	ID             string          `json:"id"`
	Type           JobType         `json:"type"`
	InstallationID int64           `json:"installation_id"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"created_at"`
	Attempts       int             `json:"attempts"`
	LastError      string          `json:"last_error,omitempty"`
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
func (q *Queue) Enqueue(ctx context.Context, job *Job) error {
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
// Returns nil, nil on timeout (no job available).
func (q *Queue) Dequeue(ctx context.Context, timeout time.Duration) (*Job, error) {
	result, err := q.client.BRPop(ctx, timeout, q.queueKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("dequeueing job: %w", err)
	}

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
		"attempts", job.Attempts,
	)
	return &job, nil
}

// Retry re-enqueues a failed job with an incremented attempt count.
// If max retries are exceeded, the job is moved to the dead letter queue.
func (q *Queue) Retry(ctx context.Context, job *Job, jobErr error) error {
	job.Attempts++
	job.LastError = jobErr.Error()

	if job.Attempts >= maxRetries {
		return q.moveToDead(ctx, job)
	}

	slog.WarnContext(ctx, "retrying job",
		"job_id", job.ID,
		"job_type", job.Type,
		"attempt", job.Attempts,
		"error", jobErr.Error(),
	)

	return q.Enqueue(ctx, job)
}

// moveToDead puts a job in the dead letter queue for manual inspection.
func (q *Queue) moveToDead(ctx context.Context, job *Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshaling dead letter job: %w", err)
	}
	if err := q.client.LPush(ctx, deadLetterKey, data).Err(); err != nil {
		return fmt.Errorf("moving job to dead letter queue: %w", err)
	}
	slog.ErrorContext(ctx, "job moved to dead letter queue",
		"job_id", job.ID,
		"job_type", job.Type,
		"attempts", job.Attempts,
		"last_error", job.LastError,
	)
	return nil
}

// DeadLetterLen returns the number of jobs in the dead letter queue.
func (q *Queue) DeadLetterLen(ctx context.Context) (int64, error) {
	return q.client.LLen(ctx, deadLetterKey).Result()
}

// DrainDeadLetter returns and removes all jobs from the dead letter queue.
func (q *Queue) DrainDeadLetter(ctx context.Context) ([]Job, error) {
	var jobs []Job
	for {
		result, err := q.client.RPop(ctx, deadLetterKey).Result()
		if err != nil {
			if err == redis.Nil {
				break
			}
			return jobs, fmt.Errorf("draining dead letter queue: %w", err)
		}
		var job Job
		if err := json.Unmarshal([]byte(result), &job); err != nil {
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// Len returns the number of jobs currently in the main queue.
func (q *Queue) Len(ctx context.Context) (int64, error) {
	return q.client.LLen(ctx, q.queueKey).Result()
}
