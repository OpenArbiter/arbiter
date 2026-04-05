package github

import (
	"sync/atomic"
	"time"
)

// Stats tracks operational metrics for the Arbiter instance.
type Stats struct {
	startedAt        time.Time
	webhooksReceived atomic.Int64
	webhooksIgnored  atomic.Int64
	jobsProcessed    atomic.Int64
	jobsFailed       atomic.Int64
	jobsRetried      atomic.Int64
	decisionsAccepted atomic.Int64
	decisionsRejected atomic.Int64
	decisionsNeedsAction atomic.Int64
	actionsExecuted  atomic.Int64
	actionsFailed    atomic.Int64
}

// NewStats creates a new stats tracker.
func NewStats() *Stats {
	return &Stats{startedAt: time.Now().UTC()}
}

// Snapshot returns a point-in-time copy of all stats.
func (s *Stats) Snapshot() StatsSnapshot {
	return StatsSnapshot{
		StartedAt:          s.startedAt,
		Uptime:             time.Since(s.startedAt).Truncate(time.Second).String(),
		WebhooksReceived:   s.webhooksReceived.Load(),
		WebhooksIgnored:    s.webhooksIgnored.Load(),
		JobsProcessed:      s.jobsProcessed.Load(),
		JobsFailed:         s.jobsFailed.Load(),
		JobsRetried:        s.jobsRetried.Load(),
		DecisionsAccepted:  s.decisionsAccepted.Load(),
		DecisionsRejected:  s.decisionsRejected.Load(),
		DecisionsNeedsAction: s.decisionsNeedsAction.Load(),
		ActionsExecuted:    s.actionsExecuted.Load(),
		ActionsFailed:      s.actionsFailed.Load(),
	}
}

// StatsSnapshot is a JSON-serializable point-in-time stats report.
type StatsSnapshot struct {
	StartedAt          time.Time `json:"started_at"`
	Uptime             string    `json:"uptime"`
	WebhooksReceived   int64     `json:"webhooks_received"`
	WebhooksIgnored    int64     `json:"webhooks_ignored"`
	JobsProcessed      int64     `json:"jobs_processed"`
	JobsFailed         int64     `json:"jobs_failed"`
	JobsRetried        int64     `json:"jobs_retried"`
	DecisionsAccepted  int64     `json:"decisions_accepted"`
	DecisionsRejected  int64     `json:"decisions_rejected"`
	DecisionsNeedsAction int64   `json:"decisions_needs_action"`
	ActionsExecuted    int64     `json:"actions_executed"`
	ActionsFailed      int64     `json:"actions_failed"`
}
