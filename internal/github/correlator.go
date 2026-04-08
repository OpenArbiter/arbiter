package github

import (
	"fmt"
	"strings"
	"time"

	"github.com/openarbiter/arbiter/internal/model"
)

// CorrelationResult contains findings from cross-file signal analysis.
type CorrelationResult struct {
	Escalations []Escalation
}

// Escalation is a cross-file finding that elevates the severity of per-file signals.
type Escalation struct {
	Rule     string
	Severity string
	Message  string
	Files    []string
}

// Correlate examines per-file signals across the entire PR and produces
// escalated findings for suspicious combinations.
func Correlate(
	scopeResult *ScopeAnalysis,
	fileClasses map[string]FileClass,
	addedLines map[string][]string,
	insights DiffInsights,
) CorrelationResult {
	var result CorrelationResult

	// Rule 1: Capability scatter — same capability in 3+ files
	capFileCount := make(map[string][]string)
	for i := range scopeResult.NewCapabilities {
		cap := &scopeResult.NewCapabilities[i]
		capFileCount[cap.Name] = appendUnique(capFileCount[cap.Name], cap.File)
	}
	for cap, files := range capFileCount {
		if len(files) >= 3 {
			result.Escalations = append(result.Escalations, Escalation{
				Rule:     "capability_scatter",
				Severity: "high",
				Message:  fmt.Sprintf("%s capability introduced in %d files — unusual spread", cap, len(files)),
				Files:    files,
			})
		}
	}

	// Rule 2: Test file with network operations
	for i := range scopeResult.NewCapabilities {
		cap := &scopeResult.NewCapabilities[i]
		if fileClasses[cap.File] == FileClassTest &&
			(cap.Name == "network_access" || cap.Name == "process_execution") {
			result.Escalations = append(result.Escalations, Escalation{
				Rule:     "test_network_exec",
				Severity: "high",
				Message:  fmt.Sprintf("test file has %s capability: %s (matched: %s)", cap.Name, cap.File, cap.Pattern),
				Files:    []string{cap.File},
			})
		}
	}

	// Rule 3: Build file + code in same PR with signals
	hasBuildFile := false
	hasCodeSignal := false
	var buildFiles, codeFiles []string
	for file, class := range fileClasses {
		if class == FileClassBuild || class == FileClassCI {
			hasBuildFile = true
			buildFiles = append(buildFiles, file)
		}
	}
	for i := range scopeResult.NewCapabilities {
		cap := &scopeResult.NewCapabilities[i]
		if fileClasses[cap.File] == FileClassSource {
			hasCodeSignal = true
			codeFiles = appendUnique(codeFiles, cap.File)
		}
	}
	if hasBuildFile && hasCodeSignal {
		result.Escalations = append(result.Escalations, Escalation{
			Rule:     "build_plus_code",
			Severity: "high",
			Message:  fmt.Sprintf("PR modifies build files (%s) AND introduces capabilities in code — elevated risk", strings.Join(buildFiles, ", ")),
			Files:    append(buildFiles, codeFiles...),
		})
	}

	// Rule 4: Dependency change + vendored code in same PR
	hasDepChange := insights.DepsModified
	hasVendored := false
	var vendoredFiles []string
	for file, class := range fileClasses {
		if class == FileClassVendored {
			hasVendored = true
			vendoredFiles = append(vendoredFiles, file)
		}
	}
	if hasDepChange && hasVendored {
		result.Escalations = append(result.Escalations, Escalation{
			Rule:     "dep_plus_vendored",
			Severity: "high",
			Message:  fmt.Sprintf("PR adds dependencies AND vendored code — vendored code may not match declared dependency"),
			Files:    vendoredFiles,
		})
	}

	// Rule 5: Capability signal + test deletion in same PR
	hasCapabilities := len(scopeResult.NewCapabilities) > 0
	if hasCapabilities && insights.TestsDeleted {
		result.Escalations = append(result.Escalations, Escalation{
			Rule:     "capability_plus_test_deletion",
			Severity: "high",
			Message:  "PR introduces new capabilities AND deletes tests — adding dangerous APIs while removing safety checks",
			Files:    nil, // applies to whole PR
		})
	}

	// Rule 6: Large diff with zero signals — potential evasion
	totalAdded := 0
	for _, lines := range addedLines {
		totalAdded += len(lines)
	}
	hasCodeFiles := false
	for _, class := range fileClasses {
		if class == FileClassSource {
			hasCodeFiles = true
			break
		}
	}
	if totalAdded > 100 && len(scopeResult.NewCapabilities) == 0 && hasCodeFiles {
		result.Escalations = append(result.Escalations, Escalation{
			Rule:     "large_clean_diff",
			Severity: "info",
			Message:  fmt.Sprintf("%d lines added to code files with zero capability signals — may be clean or may be evasive", totalAdded),
			Files:    nil,
		})
	}

	// Rule 7: New vendored code (any new files in vendor dirs)
	for file, class := range fileClasses {
		if class == FileClassVendored {
			if lines, ok := addedLines[file]; ok && len(lines) > 0 {
				result.Escalations = append(result.Escalations, Escalation{
					Rule:     "new_vendored_code",
					Severity: "high",
					Message:  fmt.Sprintf("new vendored file: %s — third-party code added outside package manager", file),
					Files:    []string{file},
				})
			}
		}
	}

	return result
}

// GenerateCorrelationEvidence creates Evidence from correlation results.
func GenerateCorrelationEvidence(result CorrelationResult, proposalID, tenantID string) []model.Evidence {
	if len(result.Escalations) == 0 {
		return nil
	}

	var flags []string
	worstResult := model.EvidenceWarn
	for i := range result.Escalations {
		e := &result.Escalations[i]
		flags = append(flags, fmt.Sprintf("[%s] %s", e.Rule, e.Message))
		if e.Severity == "high" {
			worstResult = model.EvidenceFail
		}
	}

	summary := strings.Join(flags, "; ")
	return []model.Evidence{{
		EvidenceID:   fmt.Sprintf("corr:%s:%d", proposalID, time.Now().UnixNano()),
		ProposalID:   proposalID,
		TenantID:     tenantID,
		EvidenceType: model.EvidenceSecurityScan,
		Subject:      "cross-file-correlation",
		Result:       worstResult,
		Confidence:   model.ConfidenceMedium,
		Source:       "arbiter-correlator",
		CreatedAt:    time.Now().UTC(),
		Summary:      &summary,
	}}
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}
