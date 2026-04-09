package github

import (
	"fmt"
	"strings"
	"time"

	"github.com/openarbiter/arbiter/internal/config"
	"github.com/openarbiter/arbiter/internal/model"
	"github.com/openarbiter/arbiter/internal/patterns"
)

// InvariantResult is the outcome of checking one invariant.
type InvariantResult struct {
	Name     string
	Passed   bool
	Message  string
	Severity string
}

// CheckInvariants evaluates all configured invariants against the PR files.
func CheckInvariants(invariants []config.Invariant, files []PRFileInfo, addedLines map[string][]string) []InvariantResult {
	var results []InvariantResult

	for i := range invariants {
		inv := &invariants[i]
		result := checkInvariant(inv, files, addedLines)
		results = append(results, result)
	}

	return results
}

func checkInvariant(inv *config.Invariant, files []PRFileInfo, addedLines map[string][]string) InvariantResult {
	switch inv.Rule {
	case "max_lines_changed":
		return checkMaxLines(inv, files)
	case "max_files_changed":
		return checkMaxFiles(inv, files)
	case "no_new_files_in":
		return checkNoNewFilesIn(inv, files)
	case "require_together":
		return checkRequireTogether(inv, files)
	case "require_file":
		return checkRequireFile(inv, files)
	case "forbidden_pattern":
		return checkForbiddenPattern(inv, addedLines)
	default:
		return InvariantResult{
			Name: inv.Name, Passed: true,
			Message: fmt.Sprintf("unknown rule: %s", inv.Rule),
		}
	}
}

func checkMaxLines(inv *config.Invariant, files []PRFileInfo) InvariantResult {
	total := 0
	for i := range files {
		total += files[i].Additions + files[i].Deletions
	}
	if total > inv.Value {
		return InvariantResult{
			Name: inv.Name, Passed: false, Severity: inv.Severity,
			Message: fmt.Sprintf("PR has %d changed lines (max: %d)", total, inv.Value),
		}
	}
	return InvariantResult{Name: inv.Name, Passed: true}
}

func checkMaxFiles(inv *config.Invariant, files []PRFileInfo) InvariantResult {
	if len(files) > inv.Value {
		return InvariantResult{
			Name: inv.Name, Passed: false, Severity: inv.Severity,
			Message: fmt.Sprintf("PR has %d files (max: %d)", len(files), inv.Value),
		}
	}
	return InvariantResult{Name: inv.Name, Passed: true}
}

func checkNoNewFilesIn(inv *config.Invariant, files []PRFileInfo) InvariantResult {
	for i := range files {
		if files[i].Status != "added" {
			continue
		}
		dir := "."
		if idx := strings.LastIndex(files[i].Filename, "/"); idx >= 0 {
			dir = files[i].Filename[:idx]
		}
		if dir == inv.Path || (inv.Path == "." && !strings.Contains(files[i].Filename, "/")) {
			return InvariantResult{
				Name: inv.Name, Passed: false, Severity: inv.Severity,
				Message: fmt.Sprintf("new file in restricted path %q: %s", inv.Path, files[i].Filename),
			}
		}
	}
	return InvariantResult{Name: inv.Name, Passed: true}
}

func checkRequireTogether(inv *config.Invariant, files []PRFileInfo) InvariantResult {
	if len(inv.Files) < 2 {
		return InvariantResult{Name: inv.Name, Passed: true}
	}

	changed := make(map[string]bool)
	for i := range files {
		base := files[i].Filename
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
		changed[base] = true
		changed[files[i].Filename] = true
	}

	// If any of the required files changed, all must change
	anyChanged := false
	allChanged := true
	var missing []string
	for _, f := range inv.Files {
		if changed[f] {
			anyChanged = true
		} else {
			allChanged = false
			missing = append(missing, f)
		}
	}

	if anyChanged && !allChanged {
		return InvariantResult{
			Name: inv.Name, Passed: false, Severity: inv.Severity,
			Message: fmt.Sprintf("files must change together: missing %s", strings.Join(missing, ", ")),
		}
	}
	return InvariantResult{Name: inv.Name, Passed: true}
}

func checkRequireFile(inv *config.Invariant, files []PRFileInfo) InvariantResult {
	for i := range files {
		if files[i].Filename == inv.Path {
			return InvariantResult{Name: inv.Name, Passed: true}
		}
	}
	return InvariantResult{
		Name: inv.Name, Passed: false, Severity: inv.Severity,
		Message: fmt.Sprintf("required file not modified: %s", inv.Path),
	}
}

func checkForbiddenPattern(inv *config.Invariant, addedLines map[string][]string) InvariantResult {
	if inv.Pattern == "" {
		return InvariantResult{Name: inv.Name, Passed: true}
	}

	for filename, lines := range addedLines {
		for _, line := range lines {
			if strings.Contains(line, inv.Pattern) {
				return InvariantResult{
					Name: inv.Name, Passed: false, Severity: inv.Severity,
					Message: fmt.Sprintf("forbidden pattern %q found in %s", inv.Pattern, filename),
				}
			}
			// Also match against homoglyph-normalized line
			if normalized := patterns.NormalizeConfusables(line); normalized != line && strings.Contains(normalized, inv.Pattern) {
				return InvariantResult{
					Name: inv.Name, Passed: false, Severity: inv.Severity,
					Message: fmt.Sprintf("forbidden pattern %q found in %s (homoglyph-normalized)", inv.Pattern, filename),
				}
			}
		}
	}
	return InvariantResult{Name: inv.Name, Passed: true}
}

// GenerateInvariantEvidence creates Evidence from invariant check results.
func GenerateInvariantEvidence(results []InvariantResult, proposalID, tenantID string) []model.Evidence {
	var flags []string
	worstResult := model.EvidencePass

	for i := range results {
		if results[i].Passed {
			continue
		}
		flags = append(flags, fmt.Sprintf("[%s] %s", results[i].Name, results[i].Message))
		switch {
		case results[i].Severity == "high":
			worstResult = model.EvidenceFail
		case results[i].Severity == "medium" && worstResult != model.EvidenceFail:
			worstResult = model.EvidenceWarn
		case worstResult == model.EvidencePass:
			worstResult = model.EvidenceWarn
		}
	}

	if len(flags) == 0 {
		return nil
	}

	summary := strings.Join(flags, "; ")
	return []model.Evidence{{
		EvidenceID:   fmt.Sprintf("invariant:%s:%d", proposalID, time.Now().UnixNano()),
		ProposalID:   proposalID,
		TenantID:     tenantID,
		EvidenceType: model.EvidencePolicyCheck,
		Subject:      "invariant-checks",
		Result:       worstResult,
		Confidence:   model.ConfidenceHigh,
		Source:       "arbiter-invariant-checks",
		CreatedAt:    time.Now().UTC(),
		Summary:      &summary,
	}}
}
