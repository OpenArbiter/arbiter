package github

import (
	"testing"

	"github.com/openarbiter/arbiter/internal/model"
)

func TestMapCheckRunToEvidenceType(t *testing.T) {
	tests := []struct {
		name     string
		checkRun string
		want     model.EvidenceType
	}{
		{"build", "build", model.EvidenceBuildCheck},
		{"compile", "compile-project", model.EvidenceBuildCheck},
		{"test", "run-tests", model.EvidenceTestSuite},
		{"jest", "jest-unit", model.EvidenceTestSuite},
		{"pytest", "pytest-integration", model.EvidenceTestSuite},
		{"go test", "go test", model.EvidenceTestSuite},
		{"spec", "rspec-suite", model.EvidenceTestSuite},
		{"lint", "golangci-lint", model.EvidenceBuildCheck},
		{"eslint", "eslint-check", model.EvidenceBuildCheck},
		{"security", "security-audit", model.EvidenceSecurityScan},
		{"snyk", "snyk-test", model.EvidenceSecurityScan},
		{"codeql", "codeql-analysis", model.EvidenceSecurityScan},
		{"dependabot", "dependabot-check", model.EvidenceSecurityScan},
		{"benchmark", "benchmark-perf", model.EvidenceBenchmarkCheck},
		{"unknown", "deploy-staging", model.EvidenceBuildCheck},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapCheckRunToEvidenceType(tt.checkRun)
			if got != tt.want {
				t.Errorf("mapCheckRunToEvidenceType(%q) = %q, want %q", tt.checkRun, got, tt.want)
			}
		})
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		s       string
		substrs []string
		want    bool
	}{
		{"hello world", []string{"world"}, true},
		{"hello world", []string{"foo"}, false},
		{"hello world", []string{"foo", "hello"}, true},
		{"", []string{"foo"}, false},
		{"abc", []string{""}, true},
		{"golangci-lint", []string{"lint"}, true},
	}
	for _, tt := range tests {
		got := containsAny(tt.s, tt.substrs...)
		if got != tt.want {
			t.Errorf("containsAny(%q, %v) = %v, want %v", tt.s, tt.substrs, got, tt.want)
		}
	}
}
