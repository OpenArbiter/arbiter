package github

import (
	"fmt"
	"strings"
	"time"

	"github.com/openarbiter/arbiter/internal/config"
	"github.com/openarbiter/arbiter/internal/model"
)

// DepChange represents a dependency addition, removal, or version change.
type DepChange struct {
	Package    string
	Version    string
	OldVersion string // empty for additions
	Action     string // "added", "removed", "changed", "downgraded"
	File       string
	Line       int
}

// DepAnalysisResult contains dependency change findings.
type DepAnalysisResult struct {
	Changes []DepChange
	Flags   []string
}

// AnalyzeDependencies parses added/removed lines in dependency files
// and flags new packages, version changes, and downgrades.
func AnalyzeDependencies(files []PRFileInfo, cfg config.DependencyConfig) DepAnalysisResult {
	var result DepAnalysisResult

	flagNew := cfg.FlagNew
	if !cfg.FlagNew && len(cfg.AllowedPrefixes) == 0 && !cfg.FlagDowngrades {
		flagNew = true // default: flag new deps
	}

	addedWithLines := ExtractAddedLinesWithNumbers(files)

	for i := range files {
		f := &files[i]
		lower := strings.ToLower(f.Filename)
		base := f.Filename
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
		baseLower := strings.ToLower(base)

		lines, hasAdded := addedWithLines[f.Filename]
		if !hasAdded {
			continue
		}

		switch {
		case baseLower == "go.mod":
			result.Changes = append(result.Changes, parseGoMod(lines, f.Filename)...)
		case baseLower == "package.json":
			result.Changes = append(result.Changes, parsePackageJSON(lines, f.Filename)...)
		case baseLower == "requirements.txt" || baseLower == "constraints.txt":
			result.Changes = append(result.Changes, parsePipRequirements(lines, f.Filename)...)
		case baseLower == "gemfile":
			result.Changes = append(result.Changes, parseGemfile(lines, f.Filename)...)
		case baseLower == "cargo.toml":
			result.Changes = append(result.Changes, parseCargoToml(lines, f.Filename)...)
		case strings.Contains(lower, "pom.xml") || strings.Contains(lower, "build.gradle"):
			// Java — just flag the file change, parsing XML/Gradle is complex
			result.Flags = append(result.Flags, fmt.Sprintf("Java build config modified: %s", f.Filename))
		}
	}

	// Generate flags from changes
	for i := range result.Changes {
		dep := &result.Changes[i]

		// Check if allowed
		if isAllowed(dep.Package, cfg.AllowedPrefixes) {
			continue
		}

		switch dep.Action {
		case "added":
			if flagNew {
				result.Flags = append(result.Flags, fmt.Sprintf("new dependency: %s %s (in %s)", dep.Package, dep.Version, dep.File))
			}
		case "removed":
			result.Flags = append(result.Flags, fmt.Sprintf("dependency removed: %s (in %s)", dep.Package, dep.File))
		case "downgraded":
			if cfg.FlagDowngrades {
				result.Flags = append(result.Flags, fmt.Sprintf("dependency downgraded: %s %s → %s (in %s)", dep.Package, dep.OldVersion, dep.Version, dep.File))
			}
		}
	}

	return result
}

func isAllowed(pkg string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(pkg, prefix) {
			return true
		}
	}
	return false
}

// parseGoMod extracts dependency additions from go.mod added lines.
func parseGoMod(lines []AddedLine, filename string) []DepChange {
	var changes []DepChange
	for _, line := range lines {
		content := strings.TrimSpace(line.Content)
		// Skip comments and empty
		if content == "" || strings.HasPrefix(content, "//") {
			continue
		}
		// Match: require github.com/foo/bar v1.2.3
		// Or inside require block: github.com/foo/bar v1.2.3
		parts := strings.Fields(content)
		if len(parts) >= 2 {
			pkg := parts[0]
			if pkg == "require" && len(parts) >= 3 {
				pkg = parts[1]
				changes = append(changes, DepChange{
					Package: pkg, Version: parts[2],
					Action: "added", File: filename, Line: line.Line,
				})
			} else if strings.Contains(pkg, "/") && !strings.HasPrefix(pkg, "module") && !strings.HasPrefix(pkg, "go") {
				version := parts[1]
				changes = append(changes, DepChange{
					Package: pkg, Version: version,
					Action: "added", File: filename, Line: line.Line,
				})
			}
		}
	}
	return changes
}

// parsePackageJSON extracts dependency additions from package.json added lines.
func parsePackageJSON(lines []AddedLine, filename string) []DepChange {
	var changes []DepChange
	for _, line := range lines {
		content := strings.TrimSpace(line.Content)
		// Match: "package-name": "^1.2.3"
		if strings.Contains(content, ":") && strings.Contains(content, "\"") {
			parts := strings.SplitN(content, ":", 2)
			if len(parts) != 2 {
				continue
			}
			pkg := strings.Trim(strings.TrimSpace(parts[0]), "\"")
			ver := strings.Trim(strings.TrimSpace(parts[1]), "\",")

			// Skip non-dependency keys
			if pkg == "name" || pkg == "version" || pkg == "description" ||
				pkg == "main" || pkg == "scripts" || pkg == "license" ||
				pkg == "start" || pkg == "test" || pkg == "build" ||
				pkg == "repository" || pkg == "author" || pkg == "type" {
				continue
			}

			if pkg != "" && ver != "" && !strings.HasPrefix(pkg, "@") || strings.Contains(pkg, "/") {
				changes = append(changes, DepChange{
					Package: pkg, Version: ver,
					Action: "added", File: filename, Line: line.Line,
				})
			}
		}
	}
	return changes
}

// suspiciousPipDirectives are pip options that change where packages are fetched from.
var suspiciousPipDirectives = []string{
	"--index-url",
	"--extra-index-url",
	"--trusted-host",
	"--no-verify",
	"--find-links",
}

// parsePipRequirements extracts dependency additions from requirements.txt.
func parsePipRequirements(lines []AddedLine, filename string) []DepChange {
	var changes []DepChange
	for _, line := range lines {
		content := strings.TrimSpace(line.Content)
		if content == "" || strings.HasPrefix(content, "#") {
			continue
		}

		// Flag suspicious pip directives that change package sources
		if strings.HasPrefix(content, "-") {
			for _, directive := range suspiciousPipDirectives {
				if strings.Contains(content, directive) {
					changes = append(changes, DepChange{
						Package: content, Version: "",
						Action: "added", File: filename, Line: line.Line,
					})
					break
				}
			}
			continue
		}

		// Flag git+http / git+ssh URL dependencies (potential repo swap)
		if strings.HasPrefix(content, "git+") || strings.Contains(content, "@ git+") {
			changes = append(changes, DepChange{
				Package: content, Version: "",
				Action: "added", File: filename, Line: line.Line,
			})
			continue
		}

		// Match: package==1.2.3 or package>=1.2.3 or just package
		var pkg, version string
		for _, sep := range []string{"==", ">=", "<=", "~=", "!="} {
			if idx := strings.Index(content, sep); idx > 0 {
				pkg = strings.TrimSpace(content[:idx])
				version = strings.TrimSpace(content[idx:])
				break
			}
		}
		if pkg == "" {
			pkg = content
		}
		if pkg != "" {
			changes = append(changes, DepChange{
				Package: pkg, Version: version,
				Action: "added", File: filename, Line: line.Line,
			})
		}
	}
	return changes
}

// parseGemfile extracts dependency additions from Gemfile.
func parseGemfile(lines []AddedLine, filename string) []DepChange {
	var changes []DepChange
	for _, line := range lines {
		content := strings.TrimSpace(line.Content)
		if strings.HasPrefix(content, "gem ") || strings.HasPrefix(content, "gem(") {
			// gem 'name', '~> 1.0'
			parts := strings.FieldsFunc(content, func(r rune) bool {
				return r == '\'' || r == '"' || r == ',' || r == ' '
			})
			if len(parts) >= 2 {
				pkg := parts[1]
				version := ""
				if len(parts) >= 3 {
					version = parts[2]
				}
				changes = append(changes, DepChange{
					Package: pkg, Version: version,
					Action: "added", File: filename, Line: line.Line,
				})
			}
		}
	}
	return changes
}

// parseCargoToml extracts dependency additions from Cargo.toml.
func parseCargoToml(lines []AddedLine, filename string) []DepChange {
	var changes []DepChange
	for _, line := range lines {
		content := strings.TrimSpace(line.Content)
		if strings.Contains(content, "=") && !strings.HasPrefix(content, "[") && !strings.HasPrefix(content, "#") {
			parts := strings.SplitN(content, "=", 2)
			if len(parts) == 2 {
				pkg := strings.TrimSpace(parts[0])
				ver := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
				if pkg != "name" && pkg != "version" && pkg != "edition" && pkg != "authors" {
					changes = append(changes, DepChange{
						Package: pkg, Version: ver,
						Action: "added", File: filename, Line: line.Line,
					})
				}
			}
		}
	}
	return changes
}

// GenerateDepEvidence creates Evidence from dependency analysis.
func GenerateDepEvidence(result DepAnalysisResult, proposalID, tenantID string) []model.Evidence {
	if len(result.Flags) == 0 {
		return nil
	}

	summary := strings.Join(result.Flags, "; ")
	return []model.Evidence{{
		EvidenceID:   fmt.Sprintf("deps:%s:%d", proposalID, time.Now().UnixNano()),
		ProposalID:   proposalID,
		TenantID:     tenantID,
		EvidenceType: model.EvidencePolicyCheck,
		Subject:      "dependency-analysis",
		Result:       model.EvidenceWarn,
		Confidence:   model.ConfidenceHigh,
		Source:       "arbiter-dep-analysis",
		CreatedAt:    time.Now().UTC(),
		Summary:      &summary,
	}}
}
