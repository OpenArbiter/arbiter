package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// APIAuth wraps an http.Handler with GitHub token authentication.
// Callers pass their GitHub token as a Bearer token. We verify
// they have access to the repository by calling the GitHub API.
type APIAuth struct {
	next http.Handler
}

// NewAPIAuth creates middleware that validates GitHub tokens.
func NewAPIAuth(next http.Handler) *APIAuth {
	return &APIAuth{next: next}
}

func (a *APIAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		http.Error(w, `{"error":"authorization required — pass a GitHub token as Bearer"}`, http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(auth, "Bearer ")
	if token == auth {
		http.Error(w, `{"error":"invalid authorization format — use Bearer <github-token>"}`, http.StatusUnauthorized)
		return
	}

	// Validate the token against GitHub
	user, err := validateGitHubToken(r.Context(), token)
	if err != nil {
		slog.Warn("API auth failed", "error", err, "remote_addr", r.RemoteAddr)
		http.Error(w, `{"error":"invalid or expired GitHub token"}`, http.StatusUnauthorized)
		return
	}

	// Attach the authenticated user to the context
	ctx := context.WithValue(r.Context(), authUserKey, user)
	ctx = context.WithValue(ctx, authTokenKey, token)
	a.next.ServeHTTP(w, r.WithContext(ctx))
}

type contextKeyType string

const (
	authUserKey  contextKeyType = "auth_user"
	authTokenKey contextKeyType = "auth_token"
)

// AuthUser returns the authenticated GitHub username from the context.
func AuthUser(ctx context.Context) string {
	if v, ok := ctx.Value(authUserKey).(string); ok {
		return v
	}
	return ""
}

// AuthToken returns the GitHub token from the context.
func AuthToken(ctx context.Context) string {
	if v, ok := ctx.Value(authTokenKey).(string); ok {
		return v
	}
	return ""
}

// validateGitHubToken calls GitHub's /user endpoint to verify the token is valid.
func validateGitHubToken(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %d", resp.StatusCode)
	}

	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", fmt.Errorf("parsing GitHub response: %w", err)
	}

	if user.Login == "" {
		return "", fmt.Errorf("no login in GitHub response")
	}

	return user.Login, nil
}

// CheckRepoAccess verifies the authenticated user has write access to a repo.
func CheckRepoAccess(ctx context.Context, owner, repo string) (bool, error) {
	token := AuthToken(ctx)
	if token == "" {
		return false, fmt.Errorf("no auth token in context")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("calling GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, nil // can't access repo
	}

	var repoInfo struct {
		Permissions struct {
			Push  bool `json:"push"`
			Admin bool `json:"admin"`
		} `json:"permissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repoInfo); err != nil {
		return false, fmt.Errorf("parsing repo response: %w", err)
	}

	return repoInfo.Permissions.Push || repoInfo.Permissions.Admin, nil
}
