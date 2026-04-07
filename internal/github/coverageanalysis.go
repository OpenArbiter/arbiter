package github

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/openarbiter/arbiter/internal/config"
	"github.com/openarbiter/arbiter/internal/model"
)

// CoverageAnalysis results from checking if changed code has test coverage.
type CoverageAnalysis struct {
	CodeFilesChanged    int
	TestFilesChanged    int
	UncoveredCodeFiles  []string // code files with no matching test change
	SensitiveFilesHit   []string // files in sensitive_paths that were modified
	Flags               []string
}

// DefaultTestPatterns provides sensible defaults when no patterns are configured.
var DefaultTestPatterns = []config.TestMapping{
	// Go
	{Code: "**/*.go", Test: "**/*_test.go"},
	// JavaScript/TypeScript
	{Code: "**/*.js", Test: "**/*.test.js"},
	{Code: "**/*.ts", Test: "**/*.test.ts"},
	{Code: "**/*.jsx", Test: "**/*.test.jsx"},
	{Code: "**/*.tsx", Test: "**/*.test.tsx"},
	// Also check spec pattern
	{Code: "**/*.js", Test: "**/*.spec.js"},
	{Code: "**/*.ts", Test: "**/*.spec.ts"},
	// Python
	{Code: "**/*.py", Test: "**/test_*.py"},
	{Code: "**/*.py", Test: "**/*_test.py"},
	// Ruby
	{Code: "**/*.rb", Test: "**/*_spec.rb"},
	// Rust
	{Code: "**/*.rs", Test: "**/*_test.rs"},
	// Java
	{Code: "src/main/**/*.java", Test: "src/test/**/*.java"},
}

// AnalyzeCoverage checks which code files have corresponding test changes.
func AnalyzeCoverage(files []PRFileInfo, testingCfg config.TestingConfig) CoverageAnalysis {
	analysis := CoverageAnalysis{}

	patterns := testingCfg.Patterns
	if len(patterns) == 0 {
		patterns = DefaultTestPatterns
	}

	// Separate code and test files
	codeFiles := make(map[string]bool)
	testFiles := make(map[string]bool)

	for i := range files {
		f := files[i].Filename
		if isTestFileByPatterns(f, patterns) {
			testFiles[f] = true
			analysis.TestFilesChanged++
		} else if isCodeFileByExtension(f) {
			codeFiles[f] = true
			analysis.CodeFilesChanged++
		}
	}

	// For each code file, check if a matching test file was also changed
	for codeFile := range codeFiles {
		if !hasMatchingTestChange(codeFile, testFiles, patterns) {
			analysis.UncoveredCodeFiles = append(analysis.UncoveredCodeFiles, codeFile)
		}
	}

	// Check sensitive paths
	for i := range files {
		for _, sensitivePath := range testingCfg.SensitivePaths {
			matched, _ := filepath.Match(sensitivePath+"*", files[i].Filename)
			if !matched {
				matched = strings.HasPrefix(files[i].Filename, sensitivePath)
			}
			if matched {
				analysis.SensitiveFilesHit = append(analysis.SensitiveFilesHit, files[i].Filename)
				break
			}
		}
	}

	// Generate flags
	if len(analysis.UncoveredCodeFiles) > 0 {
		for _, f := range analysis.UncoveredCodeFiles {
			analysis.Flags = append(analysis.Flags, fmt.Sprintf("code changed without test: %s", f))
		}
	}
	for _, f := range analysis.SensitiveFilesHit {
		analysis.Flags = append(analysis.Flags, fmt.Sprintf("sensitive path modified: %s", f))
	}

	return analysis
}

// isTestFileByPatterns checks if a file matches any test pattern.
func isTestFileByPatterns(filename string, patterns []config.TestMapping) bool {
	for _, p := range patterns {
		matched, _ := matchGlob(p.Test, filename)
		if matched {
			return true
		}
	}
	return false
}

// isCodeFileByExtension checks if a file is a code file by its extension.
func isCodeFileByExtension(filename string) bool {
	exts := []string{".go", ".py", ".js", ".ts", ".jsx", ".tsx", ".java", ".rb",
		".rs", ".c", ".cpp", ".cs", ".php", ".swift", ".kt"}
	for _, ext := range exts {
		if strings.HasSuffix(filename, ext) {
			return true
		}
	}
	return false
}

// hasMatchingTestChange checks if any modified test file corresponds to a code file.
func hasMatchingTestChange(codeFile string, testFiles map[string]bool, patterns []config.TestMapping) bool {
	for _, p := range patterns {
		codeMatched, _ := matchGlob(p.Code, codeFile)
		if !codeMatched {
			continue
		}
		// Derive expected test file from the code file
		expectedTests := deriveTestPaths(codeFile, p)
		for _, expected := range expectedTests {
			if testFiles[expected] {
				return true
			}
		}
	}

	// Fallback: check if ANY test file in the same directory was modified
	dir := filepath.Dir(codeFile)
	for tf := range testFiles {
		if filepath.Dir(tf) == dir {
			return true
		}
	}

	return false
}

// deriveTestPaths generates possible test file paths from a code file and pattern.
func deriveTestPaths(codeFile string, mapping config.TestMapping) []string {
	ext := filepath.Ext(codeFile)
	base := strings.TrimSuffix(filepath.Base(codeFile), ext)
	dir := filepath.Dir(codeFile)

	var paths []string

	// Common naming conventions
	// Go: foo.go → foo_test.go
	// JS/TS: foo.js → foo.test.js, foo.spec.js
	// Python: foo.py → test_foo.py, foo_test.py
	// Ruby: foo.rb → foo_spec.rb

	if strings.Contains(mapping.Test, "_test.") {
		paths = append(paths, filepath.Join(dir, base+"_test"+ext))
	}
	if strings.Contains(mapping.Test, ".test.") {
		paths = append(paths, filepath.Join(dir, base+".test"+ext))
	}
	if strings.Contains(mapping.Test, ".spec.") {
		paths = append(paths, filepath.Join(dir, base+".spec"+ext))
	}
	if strings.Contains(mapping.Test, "test_") {
		paths = append(paths, filepath.Join(dir, "test_"+base+ext))
	}
	if strings.Contains(mapping.Test, "_spec.") {
		paths = append(paths, filepath.Join(dir, base+"_spec"+ext))
	}

	// Java src/main → src/test mapping
	if strings.Contains(mapping.Code, "src/main") && strings.Contains(mapping.Test, "src/test") {
		testPath := strings.Replace(codeFile, "src/main", "src/test", 1)
		paths = append(paths, testPath)
	}

	return paths
}

// matchGlob does simple glob matching supporting ** and *.
func matchGlob(pattern, name string) (bool, error) {
	// Handle ** by trying to match with any number of directory levels
	if strings.Contains(pattern, "**") {
		// Split pattern at **
		parts := strings.SplitN(pattern, "**", 2)
		prefix := parts[0]
		suffix := ""
		if len(parts) > 1 {
			suffix = parts[1]
			if strings.HasPrefix(suffix, "/") {
				suffix = suffix[1:]
			}
		}

		// Check if the name has the right prefix
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			// Also try without prefix (** at start matches everything)
			if prefix != "" {
				return false, nil
			}
		}

		// Check if the name ends with the suffix pattern
		if suffix != "" {
			return filepath.Match(suffix, filepath.Base(name))
		}
		return true, nil
	}

	return filepath.Match(pattern, name)
}

// GenerateCoverageEvidence creates Evidence from coverage analysis.
func GenerateCoverageEvidence(analysis CoverageAnalysis, proposalID, tenantID string) []model.Evidence {
	if len(analysis.Flags) == 0 {
		return nil
	}

	result := model.EvidenceWarn
	summary := strings.Join(analysis.Flags, "; ")

	return []model.Evidence{{
		EvidenceID:   fmt.Sprintf("coverage:%s:%d", proposalID, time.Now().UnixNano()),
		ProposalID:   proposalID,
		TenantID:     tenantID,
		EvidenceType: model.EvidenceReviewFinding,
		Subject:      "coverage-analysis",
		Result:       result,
		Confidence:   model.ConfidenceMedium,
		Source:       "arbiter-coverage-analysis",
		CreatedAt:    time.Now().UTC(),
		Summary:      &summary,
	}}
}
