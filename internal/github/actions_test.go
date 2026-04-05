package github

import (
	"testing"

	"github.com/openarbiter/arbiter/internal/config"
	"github.com/openarbiter/arbiter/internal/model"
)

func testActionContext() *ActionContext {
	return &ActionContext{
		InstallationID: 12345,
		Owner:          "example",
		Repo:           "myrepo",
		PRNumber:       42,
		HeadSHA:        "abc123def",
		Decision: model.Decision{
			ProposalID: "prop-1",
			TenantID:   "tenant-1",
			Outcome:    model.DecisionAccepted,
			ReasonCode: model.ReasonAllGatesPassed,
			Summary:    "All gates passed",
			DecidedBy:  "arbiter-engine",
		},
		Confidence: 0.85,
	}
}

func TestRenderTemplate_AllVariables(t *testing.T) {
	actCtx := testActionContext()

	tests := []struct {
		tmpl string
		want string
	}{
		{"{{outcome}}", "accepted"},
		{"{{summary}}", "All gates passed"},
		{"{{reason}}", "all_gates_passed"},
		{"{{confidence}}", "85%"},
		{"{{pr_number}}", "42"},
		{"{{repo}}", "example/myrepo"},
		{"{{head_sha}}", "abc123def"},
		{"Arbiter: {{outcome}} ({{confidence}})", "Arbiter: accepted (85%)"},
		{"no variables here", "no variables here"},
		{"", ""},
	}
	for _, tt := range tests {
		got := renderTemplate(tt.tmpl, actCtx)
		if got != tt.want {
			t.Errorf("renderTemplate(%q) = %q, want %q", tt.tmpl, got, tt.want)
		}
	}
}

func TestRenderTemplate_RejectedContext(t *testing.T) {
	actCtx := testActionContext()
	actCtx.Decision.Outcome = model.DecisionRejected
	actCtx.Decision.Summary = "Blocked by: mechanical. build_check failed"
	actCtx.Decision.ReasonCode = model.ReasonMechanicalCheckFailed
	actCtx.Confidence = 0.3

	got := renderTemplate("Arbiter: {{outcome}} — {{summary}} (confidence: {{confidence}})", actCtx)
	want := "Arbiter: rejected — Blocked by: mechanical. build_check failed (confidence: 30%)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestActionConfigParsing_FullConfig(t *testing.T) {
	yaml := `
gates:
  mechanical:
    mode: enforce
    checks: [build_check, test_suite]
  scope:
    mode: warn
actions:
  on_accepted:
    - type: label
      add: arbiter-approved
    - type: comment
      body: "Arbiter: approved ✓"
    - type: auto_merge
      method: squash
  on_rejected:
    - type: label
      add: arbiter-blocked
      remove: arbiter-approved
    - type: comment
      body: "Arbiter: blocked — {{summary}}"
    - type: webhook
      url: https://example.com/hook
      headers:
        Authorization: "Bearer token123"
  on_needs_action:
    - type: assign
      users: [reviewer1, reviewer2]
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(cfg.Actions.OnAccepted) != 3 {
		t.Errorf("on_accepted actions = %d, want 3", len(cfg.Actions.OnAccepted))
	}
	if len(cfg.Actions.OnRejected) != 3 {
		t.Errorf("on_rejected actions = %d, want 3", len(cfg.Actions.OnRejected))
	}
	if len(cfg.Actions.OnNeedsAction) != 1 {
		t.Errorf("on_needs_action actions = %d, want 1", len(cfg.Actions.OnNeedsAction))
	}

	// Check auto_merge config
	autoMerge := cfg.Actions.OnAccepted[2]
	if autoMerge.Type != config.ActionAutoMerge {
		t.Errorf("type = %q, want auto_merge", autoMerge.Type)
	}
	if autoMerge.Method != "squash" {
		t.Errorf("method = %q, want squash", autoMerge.Method)
	}

	// Check webhook config
	webhook := cfg.Actions.OnRejected[2]
	if webhook.URL != "https://example.com/hook" {
		t.Errorf("url = %q, want https://example.com/hook", webhook.URL)
	}
	if webhook.Headers["Authorization"] != "Bearer token123" {
		t.Errorf("auth header = %q, want 'Bearer token123'", webhook.Headers["Authorization"])
	}

	// Check assign config
	assign := cfg.Actions.OnNeedsAction[0]
	if len(assign.Users) != 2 {
		t.Errorf("users = %d, want 2", len(assign.Users))
	}
}

func TestActionConfigParsing_NoActions(t *testing.T) {
	yaml := `
gates:
  mechanical:
    mode: enforce
    checks: [build_check]
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Actions.OnAccepted) != 0 {
		t.Errorf("on_accepted should be empty, got %d", len(cfg.Actions.OnAccepted))
	}
}

func TestActionConfigValidation_InvalidType(t *testing.T) {
	yaml := `
actions:
  on_accepted:
    - type: invalid_action
`
	_, err := config.Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid action type")
	}
}

func TestActionConfigValidation_WebhookMissingURL(t *testing.T) {
	yaml := `
actions:
  on_rejected:
    - type: webhook
`
	_, err := config.Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for webhook without URL")
	}
}

func TestActionConfigValidation_CommentMissingBody(t *testing.T) {
	yaml := `
actions:
  on_accepted:
    - type: comment
`
	_, err := config.Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for comment without body")
	}
}

func TestActionConfigValidation_InvalidMergeMethod(t *testing.T) {
	yaml := `
actions:
  on_accepted:
    - type: auto_merge
      method: fast_forward
`
	_, err := config.Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid merge method")
	}
}

func TestActionConfigValidation_ValidAutoMergeNoMethod(t *testing.T) {
	yaml := `
actions:
  on_accepted:
    - type: auto_merge
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("auto_merge without method should be valid (defaults to squash): %v", err)
	}
	if cfg.Actions.OnAccepted[0].Type != config.ActionAutoMerge {
		t.Error("should parse auto_merge action")
	}
}

func TestActionType_Valid(t *testing.T) {
	tests := []struct {
		action config.ActionType
		want   bool
	}{
		{config.ActionComment, true},
		{config.ActionLabel, true},
		{config.ActionAutoMerge, true},
		{config.ActionClose, true},
		{config.ActionWebhook, true},
		{config.ActionAssign, true},
		{config.ActionType(""), false},
		{config.ActionType("invalid"), false},
	}
	for _, tt := range tests {
		if got := tt.action.Valid(); got != tt.want {
			t.Errorf("ActionType(%q).Valid() = %v, want %v", tt.action, got, tt.want)
		}
	}
}
