package store

import (
	"context"

	"github.com/openarbiter/arbiter/internal/model"
)

// Store defines the persistence interface for all core Arbiter objects.
// All write operations are append-only.
type Store interface {
	// Tasks
	CreateTask(ctx context.Context, task *model.Task) error
	GetTask(ctx context.Context, taskID string) (*model.Task, error)
	ListTasksByTenant(ctx context.Context, tenantID string, limit, offset int) ([]model.Task, error)

	// Proposals
	CreateProposal(ctx context.Context, proposal *model.Proposal) error
	GetProposal(ctx context.Context, proposalID string) (*model.Proposal, error)
	ListProposalsByTask(ctx context.Context, taskID string) ([]model.Proposal, error)
	ListOpenProposalsByTenant(ctx context.Context, tenantID string, limit, offset int) ([]model.Proposal, error)
	UpdateProposalStatus(ctx context.Context, proposalID string, status model.ProposalStatus) error

	// Evidence
	CreateEvidence(ctx context.Context, evidence *model.Evidence) error
	GetEvidence(ctx context.Context, evidenceID string) (*model.Evidence, error)
	ListEvidenceByProposal(ctx context.Context, proposalID string) ([]model.Evidence, error)

	// Challenges
	CreateChallenge(ctx context.Context, challenge *model.Challenge) error
	GetChallenge(ctx context.Context, challengeID string) (*model.Challenge, error)
	ListChallengesByProposal(ctx context.Context, proposalID string) ([]model.Challenge, error)
	ListOpenChallengesByProposal(ctx context.Context, proposalID string) ([]model.Challenge, error)
	ResolveChallenge(ctx context.Context, challengeID string, resolvedBy string, note string) error
	DismissChallenge(ctx context.Context, challengeID string, resolvedBy string, note string) error
	UpdateChallengeLinks(ctx context.Context, challengeID string, evidenceIDs []string) error

	// Decisions
	CreateDecision(ctx context.Context, decision *model.Decision) error
	GetDecision(ctx context.Context, decisionID string) (*model.Decision, error)
	GetLatestDecisionByProposal(ctx context.Context, proposalID string) (*model.Decision, error)

	// Health
	Ping(ctx context.Context) error
	Close()
}
