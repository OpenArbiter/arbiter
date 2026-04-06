package github

import (
	"context"
	"crypto/rsa"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	gh "github.com/google/go-github/v72/github"
)

// Client wraps the GitHub API for Arbiter operations.
type Client struct {
	appID      int64
	privateKey *rsa.PrivateKey

	mu          sync.Mutex
	tokenCache  map[int64]*installationToken
}

type installationToken struct {
	token     string
	expiresAt time.Time
}

// NewClient creates a new GitHub App client.
func NewClient(appID int64, privateKeyPEM []byte) (*Client, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}
	return &Client{
		appID:      appID,
		privateKey: key,
		tokenCache: make(map[int64]*installationToken),
	}, nil
}

// createJWT creates a short-lived JWT for authenticating as the GitHub App.
func (c *Client) createJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
		Issuer:    fmt.Sprintf("%d", c.appID),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(c.privateKey)
}

// ghClient returns a go-github client authenticated for a specific installation.
func (c *Client) ghClient(ctx context.Context, installationID int64) (*gh.Client, error) {
	token, err := c.getInstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	return gh.NewClient(nil).WithAuthToken(token), nil
}

// getInstallationToken returns a cached or fresh installation access token.
func (c *Client) getInstallationToken(ctx context.Context, installationID int64) (string, error) {
	c.mu.Lock()
	cached, ok := c.tokenCache[installationID]
	c.mu.Unlock()

	if ok && time.Now().Before(cached.expiresAt.Add(-5*time.Minute)) {
		return cached.token, nil
	}

	// Create a JWT-authenticated client to request an installation token
	jwtToken, err := c.createJWT()
	if err != nil {
		return "", fmt.Errorf("creating JWT: %w", err)
	}

	appClient := gh.NewClient(nil).WithAuthToken(jwtToken)
	token, _, err := appClient.Apps.CreateInstallationToken(ctx, installationID, nil)
	if err != nil {
		return "", fmt.Errorf("creating installation token: %w", err)
	}

	c.mu.Lock()
	c.tokenCache[installationID] = &installationToken{
		token:     token.GetToken(),
		expiresAt: token.GetExpiresAt().Time,
	}
	c.mu.Unlock()

	slog.InfoContext(ctx, "installation token refreshed",
		"installation_id", installationID,
		"expires_at", token.GetExpiresAt().Time,
	)

	return token.GetToken(), nil
}

// GetPRFiles returns the list of changed file paths for a pull request.
func (c *Client) GetPRFiles(ctx context.Context, installationID int64, owner, repo string, prNumber int) ([]string, error) {
	client, err := c.ghClient(ctx, installationID)
	if err != nil {
		return nil, err
	}

	var allFiles []string
	opts := &gh.ListOptions{PerPage: 100}

	for {
		files, resp, err := client.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("listing PR files: %w", err)
		}
		for _, f := range files {
			allFiles = append(allFiles, f.GetFilename())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allFiles, nil
}

// GetFileContent reads a file from a repo at a given ref (branch/SHA).
func (c *Client) GetFileContent(ctx context.Context, installationID int64, owner, repo, path, ref string) ([]byte, error) {
	client, err := c.ghClient(ctx, installationID)
	if err != nil {
		return nil, err
	}

	content, _, resp, err := client.Repositories.GetContents(ctx, owner, repo, path, &gh.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, nil // file doesn't exist
		}
		return nil, fmt.Errorf("getting file content: %w", err)
	}

	if content == nil {
		return nil, nil
	}

	raw, err := content.GetContent()
	if err != nil {
		return nil, fmt.Errorf("decoding file content: %w", err)
	}
	return []byte(raw), nil
}

// CreateCheckRun creates a new check run on a commit.
func (c *Client) CreateCheckRun(ctx context.Context, installationID int64, owner, repo, headSHA string, opts *CheckRunOpts) (int64, error) {
	client, err := c.ghClient(ctx, installationID)
	if err != nil {
		return 0, err
	}

	createOpts := gh.CreateCheckRunOptions{
		Name:    opts.Name,
		HeadSHA: headSHA,
		Status:  gh.Ptr(opts.Status),
		Output: &gh.CheckRunOutput{
			Title:   gh.Ptr(opts.Title),
			Summary: gh.Ptr(opts.Summary),
		},
	}
	// Only set conclusion when status is completed — GitHub rejects empty conclusion
	if opts.Conclusion != "" {
		createOpts.Conclusion = gh.Ptr(opts.Conclusion)
	}

	checkRun, _, err := client.Checks.CreateCheckRun(ctx, owner, repo, createOpts)
	if err != nil {
		return 0, fmt.Errorf("creating check run: %w", err)
	}

	slog.InfoContext(ctx, "check run created",
		"check_run_id", checkRun.GetID(),
		"name", opts.Name,
		"conclusion", opts.Conclusion,
	)

	return checkRun.GetID(), nil
}

// CheckRunOpts configures a check run to create.
type CheckRunOpts struct {
	Name       string
	Status     string // "completed"
	Conclusion string // "success", "failure", "neutral", "action_required"
	Title      string
	Summary    string
}
