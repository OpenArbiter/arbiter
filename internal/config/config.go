package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// GateMode controls how a gate's failure affects the decision.
type GateMode string

const (
	GateEnforce GateMode = "enforce" // failure blocks merge
	GateWarn    GateMode = "warn"    // failure produces warning but doesn't block
	GateSkip    GateMode = "skip"    // gate is not evaluated
)

func (m GateMode) Valid() bool {
	switch m {
	case GateEnforce, GateWarn, GateSkip:
		return true
	}
	return false
}

// ActionType identifies what an action does.
type ActionType string

const (
	ActionComment   ActionType = "comment"
	ActionLabel     ActionType = "label"
	ActionAutoMerge ActionType = "auto_merge"
	ActionClose     ActionType = "close"
	ActionWebhook   ActionType = "webhook"
	ActionAssign    ActionType = "assign"
)

func (a ActionType) Valid() bool {
	switch a {
	case ActionComment, ActionLabel, ActionAutoMerge, ActionClose, ActionWebhook, ActionAssign:
		return true
	}
	return false
}

// Action defines a single action to execute when a decision is made.
type Action struct {
	Type    ActionType        `yaml:"type"`
	Body    string            `yaml:"body,omitempty"`    // comment body (supports {{outcome}}, {{summary}}, {{reason}})
	Add     string            `yaml:"add,omitempty"`     // label to add
	Remove  string            `yaml:"remove,omitempty"`  // label to remove
	Method  string            `yaml:"method,omitempty"`  // merge method: squash, rebase, merge
	URL     string            `yaml:"url,omitempty"`     // webhook URL
	Headers map[string]string `yaml:"headers,omitempty"` // webhook headers
	Users   []string          `yaml:"users,omitempty"`   // users to assign
}

// ActionsConfig defines actions triggered by decision outcomes.
type ActionsConfig struct {
	OnAccepted    []Action `yaml:"on_accepted"`
	OnRejected    []Action `yaml:"on_rejected"`
	OnNeedsAction []Action `yaml:"on_needs_action"`
}

// Config represents the parsed .arbiter.yml configuration.
type Config struct {
	Gates    GatesConfig    `yaml:"gates"`
	Evidence EvidenceConfig `yaml:"evidence"`
	Actions  ActionsConfig  `yaml:"actions"`
}

// GatesConfig controls behavior of each evaluation gate.
type GatesConfig struct {
	Mechanical MechanicalGateConfig `yaml:"mechanical"`
	Policy     PolicyGateConfig     `yaml:"policy"`
	Behavioral BehavioralGateConfig `yaml:"behavioral"`
	Challenges ChallengesGateConfig `yaml:"challenges"`
	Scope      ScopeGateConfig      `yaml:"scope"`
}

// MechanicalGateConfig controls Gate 1: build/lint/test checks.
type MechanicalGateConfig struct {
	Mode   GateMode `yaml:"mode"`
	Checks []string `yaml:"checks"` // required evidence types (e.g. "build_check", "test_suite")
}

// PolicyGateConfig controls Gate 2: policy enforcement.
type PolicyGateConfig struct {
	Mode  GateMode `yaml:"mode"`
	Rules []string `yaml:"rules"` // policy rule names to enforce
}

// BehavioralGateConfig controls Gate 3: behavioral evidence.
type BehavioralGateConfig struct {
	Mode             GateMode `yaml:"mode"`
	MinPassingTests  int      `yaml:"min_passing_tests"`
}

// ChallengesGateConfig controls Gate 4: unresolved challenges.
type ChallengesGateConfig struct {
	Mode            GateMode `yaml:"mode"`
	BlockOnSeverity string   `yaml:"block_on_severity"` // "low", "medium", or "high"
}

// ScopeGateConfig controls Gate 5: scope validation.
type ScopeGateConfig struct {
	Mode GateMode `yaml:"mode"`
}

// EvidenceConfig controls what evidence is required vs optional.
type EvidenceConfig struct {
	RequiredTypes []string `yaml:"required_types"`
	OptionalTypes []string `yaml:"optional_types"`
}

// DefaultConfig returns the default configuration when no .arbiter.yml is present.
func DefaultConfig() Config {
	return Config{
		Gates: GatesConfig{
			Mechanical: MechanicalGateConfig{
				Mode:   GateEnforce,
				Checks: []string{"build_check", "test_suite"},
			},
			Policy: PolicyGateConfig{
				Mode:  GateEnforce,
				Rules: nil,
			},
			Behavioral: BehavioralGateConfig{
				Mode:            GateEnforce,
				MinPassingTests: 1,
			},
			Challenges: ChallengesGateConfig{
				Mode:            GateEnforce,
				BlockOnSeverity: "high",
			},
			Scope: ScopeGateConfig{
				Mode: GateWarn,
			},
		},
		Evidence: EvidenceConfig{
			RequiredTypes: []string{"build_check", "test_suite"},
			OptionalTypes: []string{"security_scan", "benchmark_check"},
		},
	}
}

// Load reads and parses a .arbiter.yml file. Returns default config if file doesn't exist.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return Config{}, fmt.Errorf("reading config: %w", err)
	}
	return Parse(data)
}

// Parse parses YAML bytes into a Config, applying defaults for missing fields.
func Parse(data []byte) (Config, error) {
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks that all config values are valid.
func (c *Config) Validate() error {
	modes := []struct {
		name string
		mode GateMode
	}{
		{"gates.mechanical.mode", c.Gates.Mechanical.Mode},
		{"gates.policy.mode", c.Gates.Policy.Mode},
		{"gates.behavioral.mode", c.Gates.Behavioral.Mode},
		{"gates.challenges.mode", c.Gates.Challenges.Mode},
		{"gates.scope.mode", c.Gates.Scope.Mode},
	}
	for _, m := range modes {
		if !m.mode.Valid() {
			return fmt.Errorf("invalid %s: %q (must be enforce, warn, or skip)", m.name, m.mode)
		}
	}

	if c.Gates.Behavioral.MinPassingTests < 0 {
		return fmt.Errorf("gates.behavioral.min_passing_tests must be >= 0")
	}

	sev := c.Gates.Challenges.BlockOnSeverity
	if sev != "" && sev != "low" && sev != "medium" && sev != "high" {
		return fmt.Errorf("gates.challenges.block_on_severity must be low, medium, or high")
	}

	allActions := []struct {
		name    string
		actions []Action
	}{
		{"actions.on_accepted", c.Actions.OnAccepted},
		{"actions.on_rejected", c.Actions.OnRejected},
		{"actions.on_needs_action", c.Actions.OnNeedsAction},
	}
	for _, group := range allActions {
		for i := range group.actions {
			a := &group.actions[i]
			if !a.Type.Valid() {
				return fmt.Errorf("invalid %s[%d].type: %q", group.name, i, a.Type)
			}
			if a.Type == ActionAutoMerge && a.Method != "" {
				if a.Method != "squash" && a.Method != "rebase" && a.Method != "merge" {
					return fmt.Errorf("%s[%d].method must be squash, rebase, or merge", group.name, i)
				}
			}
			if a.Type == ActionWebhook && a.URL == "" {
				return fmt.Errorf("%s[%d].url is required for webhook actions", group.name, i)
			}
			if a.Type == ActionComment && a.Body == "" {
				return fmt.Errorf("%s[%d].body is required for comment actions", group.name, i)
			}
		}
	}

	return nil
}
