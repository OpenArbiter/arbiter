package github

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gh "github.com/google/go-github/v72/github"

	"github.com/openarbiter/arbiter/internal/model"
	"github.com/openarbiter/arbiter/internal/store"
)

// PRFileInfo is enriched file data from the GitHub API.
type PRFileInfo struct {
	Filename  string
	Status    string // added, removed, modified, renamed
	Additions int
	Deletions int
	Changes   int
	Patch     string // the actual diff content
}

// GetPRFileDetails returns detailed file info for a pull request.
func (c *Client) GetPRFileDetails(ctx context.Context, installationID int64, owner, repo string, prNumber int) ([]PRFileInfo, error) {
	client, err := c.ghClient(ctx, installationID)
	if err != nil {
		return nil, err
	}

	var allFiles []PRFileInfo
	opts := &gh.ListOptions{PerPage: 100}

	for {
		files, resp, err := client.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("listing PR files: %w", err)
		}
		for _, f := range files {
			allFiles = append(allFiles, PRFileInfo{
				Filename:  f.GetFilename(),
				Status:    f.GetStatus(),
				Additions: f.GetAdditions(),
				Deletions: f.GetDeletions(),
				Changes:   f.GetChanges(),
				Patch:     f.GetPatch(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allFiles, nil
}

// DiffInsights contains the results of analyzing a PR's diff.
type DiffInsights struct {
	TotalFiles      int
	TotalAdditions  int
	TotalDeletions  int
	TestsModified   bool
	TestsDeleted    bool
	CIModified      bool
	DepsModified    bool
	ConfigModified  bool
	SecurityFiles   bool
	BinaryFiles     bool
	LargePR         bool
	Flags           []string // human-readable flags
}

// AnalyzeDiff examines PR files and produces insights.
func AnalyzeDiff(files []PRFileInfo) DiffInsights {
	insights := DiffInsights{
		TotalFiles: len(files),
	}

	var codeFilesChanged int
	var testFilesChanged int

	for i := range files {
		f := &files[i]
		insights.TotalAdditions += f.Additions
		insights.TotalDeletions += f.Deletions
		lower := strings.ToLower(f.Filename)

		// Test files
		if isTestFile(lower) {
			testFilesChanged++
			insights.TestsModified = true
			if f.Status == "removed" {
				insights.TestsDeleted = true
				insights.Flags = append(insights.Flags, fmt.Sprintf("test file deleted: %s", f.Filename))
			}
		} else if isCodeFile(lower) {
			codeFilesChanged++
		}

		// CI/workflow files
		if isCIFile(lower) {
			insights.CIModified = true
			insights.Flags = append(insights.Flags, fmt.Sprintf("CI config modified: %s", f.Filename))
		}

		// Dependency files
		if isDepsFile(lower) {
			insights.DepsModified = true
			insights.Flags = append(insights.Flags, fmt.Sprintf("dependency file modified: %s", f.Filename))
		}

		// Config/infra files
		if isConfigFile(lower) {
			insights.ConfigModified = true
		}

		// Security-sensitive files
		if isSecurityFile(lower) {
			insights.SecurityFiles = true
			insights.Flags = append(insights.Flags, fmt.Sprintf("security-sensitive file modified: %s", f.Filename))
		}

		// Binary files (no additions/deletions but has changes, or common extensions)
		if isBinaryFile(lower) {
			insights.BinaryFiles = true
			insights.Flags = append(insights.Flags, fmt.Sprintf("binary file: %s", f.Filename))
		}
	}

	// Large PR detection
	if insights.TotalFiles > 20 || (insights.TotalAdditions+insights.TotalDeletions) > 500 {
		insights.LargePR = true
		insights.Flags = append(insights.Flags,
			fmt.Sprintf("large PR: %d files, +%d/-%d lines", insights.TotalFiles, insights.TotalAdditions, insights.TotalDeletions))
	}

	// Code changed but no tests modified
	if codeFilesChanged > 0 && testFilesChanged == 0 {
		insights.Flags = append(insights.Flags,
			fmt.Sprintf("%d code file(s) changed with no test changes", codeFilesChanged))
	}

	return insights
}

// GenerateEvidence creates Evidence records from diff insights.
func GenerateEvidence(insights DiffInsights, proposalID, tenantID string) []model.Evidence {
	var evidence []model.Evidence
	now := time.Now().UTC()

	// Scope match evidence
	scopeResult := model.EvidencePass
	scopeSummary := fmt.Sprintf("%d files changed, +%d/-%d lines", insights.TotalFiles, insights.TotalAdditions, insights.TotalDeletions)
	if len(insights.Flags) > 0 {
		scopeResult = model.EvidenceWarn
		scopeSummary = strings.Join(insights.Flags, "; ")
	}
	evidence = append(evidence, model.Evidence{
		EvidenceID:   fmt.Sprintf("diff:%s:%d", proposalID, now.UnixNano()),
		ProposalID:   proposalID,
		TenantID:     tenantID,
		EvidenceType: model.EvidenceImpactAnalysis,
		Subject:      "diff-analysis",
		Result:       scopeResult,
		Confidence:   model.ConfidenceHigh,
		Source:       "arbiter-diff-analysis",
		CreatedAt:    now,
		Summary:      &scopeSummary,
	})

	// Policy evidence for specific concerns
	if insights.TestsDeleted {
		summary := "Tests were deleted in this PR"
		evidence = append(evidence, model.Evidence{
			EvidenceID:   fmt.Sprintf("diff:tests-deleted:%s:%d", proposalID, now.UnixNano()),
			ProposalID:   proposalID,
			TenantID:     tenantID,
			EvidenceType: model.EvidencePolicyCheck,
			Subject:      "test-deletion",
			Result:       model.EvidenceFail,
			Confidence:   model.ConfidenceHigh,
			Source:       "arbiter-diff-analysis",
			CreatedAt:    now,
			Summary:      &summary,
		})
	}

	if insights.CIModified {
		summary := "CI/workflow configuration was modified"
		evidence = append(evidence, model.Evidence{
			EvidenceID:   fmt.Sprintf("diff:ci-modified:%s:%d", proposalID, now.UnixNano()),
			ProposalID:   proposalID,
			TenantID:     tenantID,
			EvidenceType: model.EvidencePolicyCheck,
			Subject:      "ci-modification",
			Result:       model.EvidenceWarn,
			Confidence:   model.ConfidenceHigh,
			Source:       "arbiter-diff-analysis",
			CreatedAt:    now,
			Summary:      &summary,
		})
	}

	return evidence
}

// ExtractAddedLines parses the patch content and returns only added lines per file.
func ExtractAddedLines(files []PRFileInfo) map[string][]string {
	result := make(map[string][]string)
	for i := range files {
		if files[i].Patch == "" {
			continue
		}
		var added []string
		for _, line := range strings.Split(files[i].Patch, "\n") {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				added = append(added, line[1:]) // strip the + prefix
			}
		}
		if len(added) > 0 {
			result[files[i].Filename] = added
		}
	}
	return result
}

// File classification helpers

func isTestFile(path string) bool {
	return containsAny(path, "_test.", ".test.", ".spec.", "test/", "tests/", "__tests__/", "spec/")
}

func isCodeFile(path string) bool {
	exts := []string{".go", ".py", ".js", ".ts", ".java", ".rb", ".rs", ".c", ".cpp", ".cs", ".php", ".swift", ".kt"}
	for _, ext := range exts {
		if strings.HasSuffix(path, ext) && !isTestFile(path) {
			return true
		}
	}
	return false
}

func isCIFile(path string) bool {
	return containsAny(path, ".github/workflows/", ".gitlab-ci", "jenkinsfile", ".circleci/",
		".travis.yml", "azure-pipelines", ".buildkite/")
}

func isDepsFile(path string) bool {
	deps := []string{"go.mod", "go.sum", "package.json", "package-lock.json", "yarn.lock",
		"requirements.txt", "pipfile", "gemfile", "cargo.toml", "cargo.lock",
		"pom.xml", "build.gradle", "composer.json"}
	base := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		base = path[idx+1:]
	}
	for _, d := range deps {
		if base == d {
			return true
		}
	}
	return false
}

func isConfigFile(path string) bool {
	return containsAny(path, "dockerfile", "docker-compose", ".env", "makefile",
		".arbiter.yml", "terraform", ".tf", "helm/", "k8s/", "kubernetes/")
}

func isSecurityFile(path string) bool {
	return containsAny(path, "auth", "crypto", "security", "password", "secret",
		"token", "credential", "permission", "acl", "rbac", "oauth", "jwt", "session")
}

func isBinaryFile(path string) bool {
	return containsAny(path, ".exe", ".dll", ".so", ".dylib", ".bin", ".dat",
		".zip", ".tar", ".gz", ".jar", ".war", ".whl", ".pyc")
}

// StoreEvidence saves evidence records, skipping duplicates.
func StoreEvidence(ctx context.Context, s store.Store, evidence []model.Evidence) {
	for i := range evidence {
		if err := s.CreateEvidence(ctx, &evidence[i]); err != nil {
			if err == store.ErrAlreadyExists {
				continue
			}
			slog.WarnContext(ctx, "could not store diff evidence", "evidence_id", evidence[i].EvidenceID, "error", err)
		}
	}
}
