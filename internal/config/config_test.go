package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
	if cfg.Gates.Mechanical.Mode != GateEnforce {
		t.Errorf("mechanical mode = %q, want enforce", cfg.Gates.Mechanical.Mode)
	}
	if cfg.Gates.Scope.Mode != GateWarn {
		t.Errorf("scope mode = %q, want warn", cfg.Gates.Scope.Mode)
	}
	if cfg.Gates.Behavioral.MinPassingTests != 1 {
		t.Errorf("min_passing_tests = %d, want 1", cfg.Gates.Behavioral.MinPassingTests)
	}
	if cfg.Gates.Challenges.BlockOnSeverity != "high" {
		t.Errorf("block_on_severity = %q, want high", cfg.Gates.Challenges.BlockOnSeverity)
	}
}

func TestParse_FullConfig(t *testing.T) {
	yaml := `
gates:
  mechanical:
    mode: enforce
    checks:
      - build_check
      - test_suite
      - lint_check
  policy:
    mode: warn
    rules:
      - no-direct-push
  behavioral:
    mode: enforce
    min_passing_tests: 3
  challenges:
    mode: enforce
    block_on_severity: medium
  scope:
    mode: skip
evidence:
  required_types:
    - build_check
  optional_types:
    - security_scan
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.Gates.Mechanical.Mode != GateEnforce {
		t.Errorf("mechanical mode = %q, want enforce", cfg.Gates.Mechanical.Mode)
	}
	if len(cfg.Gates.Mechanical.Checks) != 3 {
		t.Errorf("mechanical checks = %d, want 3", len(cfg.Gates.Mechanical.Checks))
	}
	if cfg.Gates.Policy.Mode != GateWarn {
		t.Errorf("policy mode = %q, want warn", cfg.Gates.Policy.Mode)
	}
	if cfg.Gates.Behavioral.MinPassingTests != 3 {
		t.Errorf("min_passing_tests = %d, want 3", cfg.Gates.Behavioral.MinPassingTests)
	}
	if cfg.Gates.Challenges.BlockOnSeverity != "medium" {
		t.Errorf("block_on_severity = %q, want medium", cfg.Gates.Challenges.BlockOnSeverity)
	}
	if cfg.Gates.Scope.Mode != GateSkip {
		t.Errorf("scope mode = %q, want skip", cfg.Gates.Scope.Mode)
	}
}

func TestParse_PartialConfig_DefaultsFilled(t *testing.T) {
	yaml := `
gates:
  scope:
    mode: enforce
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Scope was overridden
	if cfg.Gates.Scope.Mode != GateEnforce {
		t.Errorf("scope mode = %q, want enforce", cfg.Gates.Scope.Mode)
	}
	// Others should keep defaults
	if cfg.Gates.Mechanical.Mode != GateEnforce {
		t.Errorf("mechanical mode = %q, want enforce (default)", cfg.Gates.Mechanical.Mode)
	}
	if cfg.Gates.Behavioral.MinPassingTests != 1 {
		t.Errorf("min_passing_tests = %d, want 1 (default)", cfg.Gates.Behavioral.MinPassingTests)
	}
}

func TestParse_EmptyConfig_ReturnsDefaults(t *testing.T) {
	cfg, err := Parse([]byte(""))
	if err != nil {
		t.Fatalf("Parse empty: %v", err)
	}
	if cfg.Gates.Mechanical.Mode != GateEnforce {
		t.Errorf("mechanical mode = %q, want enforce (default)", cfg.Gates.Mechanical.Mode)
	}
}

func TestParse_InvalidMode(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			"invalid mechanical mode",
			`gates:
  mechanical:
    mode: turbo`,
			"gates.mechanical.mode",
		},
		{
			"invalid policy mode",
			`gates:
  policy:
    mode: maybe`,
			"gates.policy.mode",
		},
		{
			"invalid behavioral mode",
			`gates:
  behavioral:
    mode: sometimes`,
			"gates.behavioral.mode",
		},
		{
			"invalid challenges mode",
			`gates:
  challenges:
    mode: nope`,
			"gates.challenges.mode",
		},
		{
			"invalid scope mode",
			`gates:
  scope:
    mode: auto`,
			"gates.scope.mode",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q should contain %q", err.Error(), tt.want)
			}
		})
	}
}

func TestParse_InvalidBlockOnSeverity(t *testing.T) {
	yaml := `
gates:
  challenges:
    block_on_severity: critical
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "block_on_severity") {
		t.Errorf("error %q should mention block_on_severity", err.Error())
	}
}

func TestParse_NegativeMinPassingTests(t *testing.T) {
	yaml := `
gates:
  behavioral:
    min_passing_tests: -1
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "min_passing_tests") {
		t.Errorf("error %q should mention min_passing_tests", err.Error())
	}
}

func TestParse_MalformedYAML(t *testing.T) {
	_, err := Parse([]byte("{{{{not yaml"))
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestLoad_FileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".arbiter.yml")
	os.WriteFile(path, []byte(`
gates:
  scope:
    mode: enforce
`), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Gates.Scope.Mode != GateEnforce {
		t.Errorf("scope mode = %q, want enforce", cfg.Gates.Scope.Mode)
	}
}

func TestLoad_FileNotFound_ReturnsDefaults(t *testing.T) {
	cfg, err := Load("/nonexistent/.arbiter.yml")
	if err != nil {
		t.Fatalf("Load should not error for missing file: %v", err)
	}
	if cfg.Gates.Mechanical.Mode != GateEnforce {
		t.Errorf("should return defaults when file missing")
	}
}

func TestLoad_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".arbiter.yml")
	os.WriteFile(path, []byte(`gates:
  mechanical:
    mode: invalid_mode
`), 0644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestGateMode_Valid(t *testing.T) {
	tests := []struct {
		mode GateMode
		want bool
	}{
		{GateEnforce, true},
		{GateWarn, true},
		{GateSkip, true},
		{GateMode(""), false},
		{GateMode("turbo"), false},
	}
	for _, tt := range tests {
		if got := tt.mode.Valid(); got != tt.want {
			t.Errorf("GateMode(%q).Valid() = %v, want %v", tt.mode, got, tt.want)
		}
	}
}
