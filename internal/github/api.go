package github

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/openarbiter/arbiter/internal/model"
	"github.com/openarbiter/arbiter/internal/store"
)

// API provides REST endpoints for manual operations (challenges, re-evaluation).
type API struct {
	store store.Store
}

// NewAPI creates a new API handler.
func NewAPI(s store.Store) *API {
	return &API{store: s}
}

// CreateChallengeRequest is the JSON body for creating a challenge.
type CreateChallengeRequest struct {
	ProposalID    string `json:"proposal_id"`
	ChallengeType string `json:"challenge_type"`
	Target        string `json:"target"`
	Severity      string `json:"severity"`
	Summary       string `json:"summary"`
}

// ResolveChallengeRequest is the JSON body for resolving/dismissing a challenge.
type ResolveChallengeRequest struct {
	ChallengeID string `json:"challenge_id"`
	Note        string `json:"note"`
	Action      string `json:"action"` // "resolve" or "dismiss"
}

// HandleCreateChallenge creates a new challenge on a proposal.
func (a *API) HandleCreateChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateChallengeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Verify proposal exists
	proposal, err := a.store.GetProposal(r.Context(), req.ProposalID)
	if err != nil {
		if err == store.ErrNotFound {
			http.Error(w, `{"error":"proposal not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Check caller has write access to the repo
	owner, repo := parseRepoFromProposal(proposal)
	if owner != "" && repo != "" {
		hasAccess, err := CheckRepoAccess(r.Context(), owner, repo)
		if err != nil {
			slog.Warn("repo access check failed", "error", err)
		}
		if !hasAccess {
			http.Error(w, `{"error":"you do not have write access to this repository"}`, http.StatusForbidden)
			return
		}
	}

	user := AuthUser(r.Context())
	challenge := model.Challenge{
		ChallengeID:   fmt.Sprintf("ch:%s:%d", req.ProposalID, time.Now().UnixMilli()),
		ProposalID:    req.ProposalID,
		TenantID:      proposal.TenantID,
		RaisedBy:      user,
		ChallengeType: model.ChallengeType(req.ChallengeType),
		Target:        req.Target,
		Severity:      model.Severity(req.Severity),
		Summary:       req.Summary,
		Status:        model.ChallengeOpen,
		CreatedAt:     time.Now().UTC(),
	}

	if err := a.store.CreateChallenge(r.Context(), &challenge); err != nil {
		slog.Error("creating challenge", "error", err)
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	slog.Info("challenge created via API",
		"challenge_id", challenge.ChallengeID,
		"proposal_id", req.ProposalID,
		"severity", req.Severity,
		"raised_by", user,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(challenge)
}

// HandleResolveChallenge resolves or dismisses an existing challenge.
func (a *API) HandleResolveChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ResolveChallengeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Look up the challenge to find the proposal and check repo access
	challenge, err := a.store.GetChallenge(r.Context(), req.ChallengeID)
	if err != nil {
		if err == store.ErrNotFound {
			http.Error(w, `{"error":"challenge not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	proposal, err := a.store.GetProposal(r.Context(), challenge.ProposalID)
	if err == nil {
		owner, repo := parseRepoFromProposal(proposal)
		if owner != "" && repo != "" {
			hasAccess, checkErr := CheckRepoAccess(r.Context(), owner, repo)
			if checkErr != nil {
				slog.Warn("repo access check failed", "error", checkErr)
			}
			if !hasAccess {
				http.Error(w, `{"error":"you do not have write access to this repository"}`, http.StatusForbidden)
				return
			}
		}
	}

	user := AuthUser(r.Context())

	switch req.Action {
	case "resolve":
		err = a.store.ResolveChallenge(r.Context(), req.ChallengeID, user, req.Note)
	case "dismiss":
		err = a.store.DismissChallenge(r.Context(), req.ChallengeID, user, req.Note)
	default:
		http.Error(w, `{"error":"action must be 'resolve' or 'dismiss'"}`, http.StatusBadRequest)
		return
	}

	if err != nil {
		if err == store.ErrNotFound {
			http.Error(w, `{"error":"challenge not found or already resolved"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	slog.Info("challenge updated via API",
		"challenge_id", req.ChallengeID,
		"action", req.Action,
		"resolved_by", user,
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":       req.Action + "d",
		"challenge_id": req.ChallengeID,
	})
}

// HandleListProposals lists open proposals for a tenant.
func (a *API) HandleListProposals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		http.Error(w, `{"error":"tenant_id query param required"}`, http.StatusBadRequest)
		return
	}

	proposals, err := a.store.ListOpenProposalsByTenant(r.Context(), tenantID, 100, 0)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(proposals)
}

// parseRepoFromProposal extracts owner/repo from a proposal ID like "gh:Owner/Repo:pr:1:sha:abc"
func parseRepoFromProposal(p *model.Proposal) (string, string) {
	// Proposal ID format: gh:<owner>/<repo>:pr:<num>:sha:<sha>
	id := p.ProposalID
	if !strings.HasPrefix(id, "gh:") {
		return "", ""
	}
	parts := strings.SplitN(id[3:], ":", 2)
	if len(parts) < 1 {
		return "", ""
	}
	ownerRepo := strings.SplitN(parts[0], "/", 2)
	if len(ownerRepo) != 2 {
		return "", ""
	}
	return ownerRepo[0], ownerRepo[1]
}
