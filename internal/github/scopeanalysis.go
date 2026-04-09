package github

import (
	"fmt"
	"strings"
	"time"

	"github.com/openarbiter/arbiter/internal/model"
	"github.com/openarbiter/arbiter/internal/patterns"
)

// Capability represents a new power introduced by a change.
type Capability struct {
	Name        string // e.g. "process_execution"
	Description string // e.g. "Added os/exec — can execute system commands"
	Pattern     string // what matched
	File        string // which file
}

// ScopeAnalysis contains the results of analyzing a PR's scope.
type ScopeAnalysis struct {
	// Directory spread — how many unrelated directories were touched
	Directories     []string
	DirectorySpread int

	// New capabilities introduced by the diff
	NewCapabilities []Capability

	// Change ratio concerns
	TitleLength   int
	FilesChanged  int
	LinesChanged  int
	ChangeRatio   float64 // lines changed per title character — higher = more suspicious

	// Scope flags
	Flags []string
}

// PatternStats returns counts of capability categories and individual patterns.
func PatternStats() (categories int, totalPatterns int) {
	return patterns.Stats()
}

// AnalyzeScope examines the PR diff for scope concerns.
// addedLines should be the `+` lines from the diff (without the `+` prefix).
func AnalyzeScope(title, body string, files []PRFileInfo, addedLines map[string][]string) ScopeAnalysis {
	analysis := ScopeAnalysis{
		TitleLength:  len(title),
		FilesChanged: len(files),
	}

	// Count total lines changed
	for i := range files {
		analysis.LinesChanged += files[i].Additions + files[i].Deletions
	}

	// Directory spread
	dirSet := make(map[string]bool)
	for i := range files {
		dir := "root"
		if idx := strings.LastIndex(files[i].Filename, "/"); idx >= 0 {
			dir = files[i].Filename[:idx]
		}
		dirSet[dir] = true
	}
	for dir := range dirSet {
		analysis.Directories = append(analysis.Directories, dir)
	}
	analysis.DirectorySpread = len(dirSet)

	// Change ratio — how much change relative to how much description
	descriptionLen := len(title) + len(body)
	if descriptionLen > 0 {
		analysis.ChangeRatio = float64(analysis.LinesChanged) / float64(descriptionLen)
	}

	// Flag wide directory spread
	if analysis.DirectorySpread > 5 {
		analysis.Flags = append(analysis.Flags,
			fmt.Sprintf("changes span %d directories — may indicate scope creep", analysis.DirectorySpread))
	}

	// Flag high change ratio with minimal description
	if analysis.LinesChanged > 100 && descriptionLen < 50 {
		analysis.Flags = append(analysis.Flags,
			fmt.Sprintf("%d lines changed with only %d characters of description", analysis.LinesChanged, descriptionLen))
	}

	// Capability detection — scan added lines
	// Track per file+category to report each file, but not every line
	allPatterns := patterns.All()
	capSeen := make(map[string]bool) // key: "category:filename"
	for filename, lines := range addedLines {
		for _, line := range lines {
			// Normalize confusable Unicode characters so homoglyph attacks
			// (e.g., Cyrillic "е" in "os/еxec") match patterns.
			normalized := patterns.NormalizeConfusables(line)

			for _, cat := range allPatterns {
				key := cat.Name + ":" + filename
				if capSeen[key] {
					continue
				}
				for _, pattern := range cat.Patterns {
					if strings.Contains(line, pattern) || strings.Contains(normalized, pattern) {
						analysis.NewCapabilities = append(analysis.NewCapabilities, Capability{
							Name:        cat.Name,
							Description: cat.Description,
							Pattern:     pattern,
							File:        filename,
						})
						capSeen[key] = true
						break
					}
				}
			}
		}
	}

	// Hidden character detection — Trojan Source, Unicode evasion, confusables
	for filename, lines := range addedLines {
		hiddenFound := false
		confusableFound := false
		for _, line := range lines {
			for _, r := range line {
				if !hiddenFound {
					for _, hidden := range patterns.HiddenCharRunes {
						if r == hidden.Char {
							analysis.NewCapabilities = append(analysis.NewCapabilities, Capability{
								Name:        "hidden_characters",
								Description: "Contains invisible or misleading Unicode characters",
								Pattern:     hidden.Name,
								File:        filename,
							})
							hiddenFound = true
							break
						}
					}
				}
				if !confusableFound {
					for _, conf := range patterns.ConfusableChars {
						if r == conf.Char {
							analysis.NewCapabilities = append(analysis.NewCapabilities, Capability{
								Name:        "hidden_characters",
								Description: fmt.Sprintf("Contains confusable character: %s looks like '%s'", conf.Name, conf.LooksLike),
								Pattern:     conf.Name,
								File:        filename,
							})
							confusableFound = true
							break
						}
					}
				}
				if hiddenFound && confusableFound {
					break
				}
			}
			if hiddenFound && confusableFound {
				break
			}
		}
	}

	// Flag new capabilities
	for i := range analysis.NewCapabilities {
		cap := &analysis.NewCapabilities[i]
		analysis.Flags = append(analysis.Flags,
			fmt.Sprintf("new capability: %s — %s (in %s, matched: %s)", cap.Name, cap.Description, cap.File, cap.Pattern))
	}

	return analysis
}

// GenerateScopeEvidence creates Evidence records from scope analysis.
func GenerateScopeEvidence(analysis *ScopeAnalysis, proposalID, tenantID string) []model.Evidence {
	if len(analysis.Flags) == 0 {
		return nil
	}

	result := model.EvidenceWarn
	summary := strings.Join(analysis.Flags, "; ")

	// Capabilities like process execution or eval are high risk
	for i := range analysis.NewCapabilities {
		if analysis.NewCapabilities[i].Name == "process_execution" ||
			analysis.NewCapabilities[i].Name == "eval_dynamic" {
			result = model.EvidenceFail
			break
		}
	}

	return []model.Evidence{{
		EvidenceID:   fmt.Sprintf("scope:%s:%d", proposalID, len(analysis.Flags)),
		ProposalID:   proposalID,
		TenantID:     tenantID,
		EvidenceType: model.EvidenceScopeMatch,
		Subject:      "scope-analysis",
		Result:       result,
		Confidence:   model.ConfidenceMedium,
		Source:       "arbiter-scope-analysis",
		CreatedAt:    time.Now().UTC(),
		Summary:      &summary,
	}}
}
