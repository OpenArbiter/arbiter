package github

import (
	"fmt"
	"strings"
	"time"

	"github.com/openarbiter/arbiter/internal/model"
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

// capabilityPatterns maps diff line patterns to capabilities.
// These are searched in added lines (+ lines) only.
var capabilityPatterns = []struct {
	name        string
	description string
	patterns    []string
}{
	{
		name:        "process_execution",
		description: "Can execute system commands",
		patterns: []string{
			"os/exec", "os.exec", "subprocess", "child_process",
			"exec.Command", "exec.Run", "os.system(", "system(",
			"popen(", "Runtime.exec", "ProcessBuilder",
			"syscall.Exec", "syscall.ForkExec", "\"syscall\"",
			"std::process::Command", "Command::new(",
			"shell_exec(", "passthru(", "proc_open(",
			"Process.Start", "ProcessStartInfo",
			"popen(", "execvp(", "execve(",
		},
	},
	{
		name:        "network_access",
		description: "Can make network requests",
		patterns: []string{
			"net/http", "net.Dial", "http.Get", "http.Post", "http.NewRequest",
			"requests.get", "requests.post", "urllib", "fetch(",
			"axios", "XMLHttpRequest", "WebSocket",
			"TcpStream", "UdpSocket", "hyper::Client",
			"curl_init(", "curl_exec(", "file_get_contents(\"http",
			"HttpClient", "HttpURLConnection", "socket(",
			"WebClient", "HttpWebRequest",
		},
	},
	{
		name:        "file_system_write",
		description: "Can write to the file system",
		patterns: []string{
			"os.Create", "os.WriteFile", "os.Remove", "os.RemoveAll",
			"os.MkdirAll", "ioutil.WriteFile",
			"open(", "fs.writeFile", "fs.unlink", "shutil.rmtree",
			"file_put_contents(", "fwrite(", "chmod(",
			"FileOutputStream", "File.WriteAll", "File.Create",
			"fopen(", "fprintf(",
		},
	},
	{
		name:        "environment_access",
		description: "Reads environment variables (potential secret access)",
		patterns: []string{
			"os.Getenv", "os.Environ", "process.env",
			"os.environ", "getenv(",
		},
	},
	{
		name:        "eval_dynamic",
		description: "Dynamic code execution",
		patterns: []string{
			"eval(", "exec(", "Function(", "reflect.Value",
			"unsafe.Pointer", "//go:linkname",
			"plugin.Open", "\"plugin\"",
			"unsafe {", "unsafe fn",
			"unserialize(", "call_user_func",
			"Assembly.Load", "Activator.CreateInstance",
			"BinaryFormatter", "ObjectInputStream",
			"dlopen(", "dlsym(",
			"Class.forName(",
		},
	},
	{
		name:        "crypto_operations",
		description: "Cryptographic operations",
		patterns: []string{
			"crypto/", "hashlib", "bcrypt", "jwt.",
			"PrivateKey", "PublicKey", "x509",
		},
	},
	{
		name:        "linter_suppression",
		description: "Suppressing code quality checks",
		patterns: []string{
			"//nolint", "# noqa", "eslint-disable", "// nosec",
			"@SuppressWarnings", "rubocop:disable",
		},
	},
	{
		name:        "build_time_execution",
		description: "Executes commands at build time",
		patterns: []string{
			"//go:generate", "go:generate",
			"pre-commit", "post-commit", "pre-push",
			"Makefile:", "$(shell",
		},
	},
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
	capSeen := make(map[string]bool)
	for filename, lines := range addedLines {
		for _, line := range lines {
			for _, cap := range capabilityPatterns {
				if capSeen[cap.name] {
					continue
				}
				for _, pattern := range cap.patterns {
					if strings.Contains(line, pattern) {
						analysis.NewCapabilities = append(analysis.NewCapabilities, Capability{
							Name:        cap.name,
							Description: cap.description,
							Pattern:     pattern,
							File:        filename,
						})
						capSeen[cap.name] = true
						break
					}
				}
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
func GenerateScopeEvidence(analysis ScopeAnalysis, proposalID, tenantID string) []model.Evidence {
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
