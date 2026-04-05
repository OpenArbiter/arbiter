package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v72/github"

	"github.com/openarbiter/arbiter/internal/config"
	"github.com/openarbiter/arbiter/internal/model"
)

// ActionContext contains all data needed to execute actions after a decision.
type ActionContext struct {
	InstallationID int64
	Owner          string
	Repo           string
	PRNumber       int
	HeadSHA        string
	Decision       model.Decision
	Confidence     float64
}

// ExecuteActions runs all configured actions for the given decision outcome.
func (c *Client) ExecuteActions(ctx context.Context, actCtx *ActionContext, cfg config.ActionsConfig) {
	var actions []config.Action

	switch actCtx.Decision.Outcome {
	case model.DecisionAccepted:
		actions = cfg.OnAccepted
	case model.DecisionRejected:
		actions = cfg.OnRejected
	case model.DecisionNeedsAction:
		actions = cfg.OnNeedsAction
	default:
		return
	}

	for i := range actions {
		action := &actions[i]
		if err := c.executeAction(ctx, actCtx, action); err != nil {
			slog.ErrorContext(ctx, "action failed",
				"action_type", action.Type,
				"error", err,
			)
		}
	}
}

func (c *Client) executeAction(ctx context.Context, actCtx *ActionContext, action *config.Action) error {
	switch action.Type {
	case config.ActionComment:
		return c.execComment(ctx, actCtx, action)
	case config.ActionLabel:
		return c.execLabel(ctx, actCtx, action)
	case config.ActionAutoMerge:
		return c.execAutoMerge(ctx, actCtx, action)
	case config.ActionClose:
		return c.execClose(ctx, actCtx)
	case config.ActionWebhook:
		return c.execWebhook(ctx, actCtx, action)
	case config.ActionAssign:
		return c.execAssign(ctx, actCtx, action)
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

func (c *Client) execComment(ctx context.Context, actCtx *ActionContext, action *config.Action) error {
	body := renderTemplate(action.Body, actCtx)

	client, err := c.ghClient(ctx, actCtx.InstallationID)
	if err != nil {
		return err
	}

	_, _, err = client.Issues.CreateComment(ctx, actCtx.Owner, actCtx.Repo, actCtx.PRNumber, &gh.IssueComment{Body: &body})
	if err != nil {
		return fmt.Errorf("creating comment: %w", err)
	}

	slog.InfoContext(ctx, "comment posted", "pr", actCtx.PRNumber)
	return nil
}

func (c *Client) execLabel(ctx context.Context, actCtx *ActionContext, action *config.Action) error {
	client, err := c.ghClient(ctx, actCtx.InstallationID)
	if err != nil {
		return err
	}

	if action.Remove != "" {
		// Ignore error — label may not exist
		_, err := client.Issues.RemoveLabelForIssue(ctx, actCtx.Owner, actCtx.Repo, actCtx.PRNumber, action.Remove)
		if err != nil {
			slog.DebugContext(ctx, "could not remove label", "label", action.Remove, "error", err)
		}
	}

	if action.Add != "" {
		_, _, err := client.Issues.AddLabelsToIssue(ctx, actCtx.Owner, actCtx.Repo, actCtx.PRNumber, []string{action.Add})
		if err != nil {
			return fmt.Errorf("adding label %q: %w", action.Add, err)
		}
		slog.InfoContext(ctx, "label added", "label", action.Add, "pr", actCtx.PRNumber)
	}

	return nil
}

func (c *Client) execAutoMerge(ctx context.Context, actCtx *ActionContext, action *config.Action) error {
	client, err := c.ghClient(ctx, actCtx.InstallationID)
	if err != nil {
		return err
	}

	method := action.Method
	if method == "" {
		method = "squash"
	}

	commitMsg := fmt.Sprintf("Arbiter: auto-merge (confidence: %.0f%%)\n\n%s", actCtx.Confidence*100, actCtx.Decision.Summary)

	_, _, err = client.PullRequests.Merge(ctx, actCtx.Owner, actCtx.Repo, actCtx.PRNumber, commitMsg, &gh.PullRequestOptions{
		MergeMethod: method,
		SHA:         actCtx.HeadSHA,
	})
	if err != nil {
		return fmt.Errorf("auto-merging PR: %w", err)
	}

	slog.InfoContext(ctx, "PR auto-merged",
		"pr", actCtx.PRNumber,
		"method", method,
		"confidence", actCtx.Confidence,
	)
	return nil
}

func (c *Client) execClose(ctx context.Context, actCtx *ActionContext) error {
	client, err := c.ghClient(ctx, actCtx.InstallationID)
	if err != nil {
		return err
	}

	state := "closed"
	_, _, err = client.PullRequests.Edit(ctx, actCtx.Owner, actCtx.Repo, actCtx.PRNumber, &gh.PullRequest{State: &state})
	if err != nil {
		return fmt.Errorf("closing PR: %w", err)
	}

	slog.InfoContext(ctx, "PR closed", "pr", actCtx.PRNumber)
	return nil
}

func (c *Client) execWebhook(ctx context.Context, actCtx *ActionContext, action *config.Action) error {
	payload := map[string]any{
		"outcome":    string(actCtx.Decision.Outcome),
		"reason":     string(actCtx.Decision.ReasonCode),
		"summary":    actCtx.Decision.Summary,
		"confidence": actCtx.Confidence,
		"proposal_id": actCtx.Decision.ProposalID,
		"pr_number":  actCtx.PRNumber,
		"repo":       actCtx.Owner + "/" + actCtx.Repo,
		"head_sha":   actCtx.HeadSHA,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, action.URL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Arbiter/1.0")
	for k, v := range action.Headers {
		req.Header.Set(k, renderTemplate(v, actCtx))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending webhook: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	slog.InfoContext(ctx, "webhook sent",
		"url", action.URL,
		"status", resp.StatusCode,
	)
	return nil
}

func (c *Client) execAssign(ctx context.Context, actCtx *ActionContext, action *config.Action) error {
	if len(action.Users) == 0 {
		return nil
	}

	client, err := c.ghClient(ctx, actCtx.InstallationID)
	if err != nil {
		return err
	}

	_, _, err = client.Issues.AddAssignees(ctx, actCtx.Owner, actCtx.Repo, actCtx.PRNumber, action.Users)
	if err != nil {
		return fmt.Errorf("assigning users: %w", err)
	}

	slog.InfoContext(ctx, "users assigned", "users", action.Users, "pr", actCtx.PRNumber)
	return nil
}

// renderTemplate replaces {{variable}} placeholders in a string.
func renderTemplate(tmpl string, actCtx *ActionContext) string {
	r := strings.NewReplacer(
		"{{outcome}}", string(actCtx.Decision.Outcome),
		"{{summary}}", actCtx.Decision.Summary,
		"{{reason}}", string(actCtx.Decision.ReasonCode),
		"{{confidence}}", fmt.Sprintf("%.0f%%", actCtx.Confidence*100),
		"{{pr_number}}", fmt.Sprintf("%d", actCtx.PRNumber),
		"{{repo}}", actCtx.Owner+"/"+actCtx.Repo,
		"{{head_sha}}", actCtx.HeadSHA,
	)
	return r.Replace(tmpl)
}
