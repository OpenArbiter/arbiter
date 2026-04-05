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

// Config represents the parsed .arbiter.yml configuration.
type Config struct {
	Gates    GatesConfig    `yaml:"gates"`
	Evidence EvidenceConfig `yaml:"evidence"`
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

	return nil
}
