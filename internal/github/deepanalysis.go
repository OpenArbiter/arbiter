package github

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/openarbiter/arbiter/internal/config"
	"github.com/openarbiter/arbiter/internal/model"
)

// DeepAnalysisResult contains findings from target, entropy, and combination analysis.
type DeepAnalysisResult struct {
	SuspiciousTargets []TargetFinding
	HighEntropyStrings []EntropyFinding
	DangerousCombos   []ComboFinding
}

// TargetFinding is a suspicious destination found in code.
type TargetFinding struct {
	File    string
	Line    int
	Target  string
	Reason  string
}

// EntropyFinding is a suspiciously encoded string.
type EntropyFinding struct {
	File    string
	Line    int
	Preview string
	Entropy float64
	Length  int
}

// ComboFinding is a dangerous combination of operations in the same file.
type ComboFinding struct {
	File    string
	Combo   string
	Details string
}

// Default suspicious targets
var metadataIPs = []string{
	"169.254.169.254",    // AWS/GCP metadata
	"169.254.170.2",      // AWS ECS metadata
	"metadata.google",    // GCP metadata
	"metadata.internal",  // generic cloud metadata
}

var privateIPPrefixes = []string{
	"10.", "172.16.", "172.17.", "172.18.", "172.19.",
	"172.20.", "172.21.", "172.22.", "172.23.", "172.24.",
	"172.25.", "172.26.", "172.27.", "172.28.", "172.29.",
	"172.30.", "172.31.", "192.168.", "127.0.0.1", "localhost",
}

var defaultBlockedDomains = []string{
	"pastebin.com", "paste.ee", "hastebin.com",
	"ngrok.io", "ngrok.app",
	"requestbin.com", "webhook.site",
	"transfer.sh", "file.io",
}

var suspiciousPaths = []string{
	"/etc/shadow", "/etc/passwd", "/etc/hosts",
	"/.ssh/", "/.aws/", "/.gnupg/",
	"/proc/self/", "/proc/1/",
	"/.env", "/credentials", "/secret",
}

// RunDeepAnalysis performs target, entropy, and combination analysis on added lines.
func RunDeepAnalysis(files []PRFileInfo, cfg *config.AnalysisConfig) DeepAnalysisResult {
	var result DeepAnalysisResult
	addedWithLines := ExtractAddedLinesWithNumbers(files)

	// Target analysis
	if cfg.SuspiciousTargets.Mode != "off" {
		blockMeta := cfg.SuspiciousTargets.BlockMetadataIPs
		if cfg.SuspiciousTargets.Mode == "" {
			blockMeta = true // default
		}
		blockPrivate := cfg.SuspiciousTargets.BlockPrivateIPs

		allowed := make(map[string]bool)
		for _, d := range cfg.SuspiciousTargets.AllowedDomains {
			allowed[d] = true
		}

		blocked := append([]string{}, defaultBlockedDomains...)
		blocked = append(blocked, cfg.SuspiciousTargets.BlockedDomains...)

		for filename, lines := range addedWithLines {
			for _, line := range lines {
				content := line.Content

				// Check metadata IPs
				if blockMeta {
					for _, ip := range metadataIPs {
						if strings.Contains(content, ip) {
							result.SuspiciousTargets = append(result.SuspiciousTargets, TargetFinding{
								File: filename, Line: line.Line,
								Target: ip, Reason: "cloud metadata endpoint",
							})
						}
					}
				}

				// Check private IPs
				if blockPrivate {
					for _, prefix := range privateIPPrefixes {
						if strings.Contains(content, prefix) {
							result.SuspiciousTargets = append(result.SuspiciousTargets, TargetFinding{
								File: filename, Line: line.Line,
								Target: prefix, Reason: "private/internal IP",
							})
							break
						}
					}
				}

				// Check blocked domains
				for _, domain := range blocked {
					if strings.Contains(content, domain) && !allowed[domain] {
						result.SuspiciousTargets = append(result.SuspiciousTargets, TargetFinding{
							File: filename, Line: line.Line,
							Target: domain, Reason: "suspicious domain",
						})
					}
				}

				// Check suspicious paths
				for _, path := range suspiciousPaths {
					if strings.Contains(content, path) {
						result.SuspiciousTargets = append(result.SuspiciousTargets, TargetFinding{
							File: filename, Line: line.Line,
							Target: path, Reason: "sensitive system path",
						})
						break // one per line
					}
				}
			}
		}
	}

	// Entropy analysis — default off (high false positive rate)
	// Enable with: analysis.entropy.mode: warn
	if cfg.Entropy.Mode == "warn" || cfg.Entropy.Mode == "enforce" {
		minLen := cfg.Entropy.MinLength
		if minLen == 0 {
			minLen = 50
		}
		threshold := cfg.Entropy.Threshold
		if threshold == 0 {
			threshold = 4.5
		}

		for filename, lines := range addedWithLines {
			for _, line := range lines {
				// Find string literals in the line
				for _, str := range extractStringLiterals(line.Content) {
					if len(str) < minLen {
						continue
					}
					entropy := shannonEntropy(str)
					if entropy >= threshold {
						preview := str
						if len(preview) > 40 {
							preview = preview[:40] + "..."
						}
						result.HighEntropyStrings = append(result.HighEntropyStrings, EntropyFinding{
							File: filename, Line: line.Line,
							Preview: preview, Entropy: entropy, Length: len(str),
						})
					}
				}
			}
		}
	}

	// Combination analysis
	if cfg.Combinations.Mode != "off" {
		rules := cfg.Combinations.Rules
		if len(rules) == 0 {
			rules = []config.CombinationRule{
				config.CombDecodeAndWrite,
				config.CombDecodeAndExecute,
				config.CombFetchAndExecute,
				config.CombSocketDNS,
				config.CombEnvAndLeak,
			}
		}

		for filename, lines := range addedWithLines {
			allContent := ""
			for _, line := range lines {
				allContent += line.Content + "\n"
			}

			for _, rule := range rules {
				if combo := checkCombination(rule, allContent); combo != "" {
					result.DangerousCombos = append(result.DangerousCombos, ComboFinding{
						File: filename, Combo: string(rule), Details: combo,
					})
				}
			}
		}
	}

	return result
}

// checkCombination checks if a file contains a dangerous combination of operations.
func checkCombination(rule config.CombinationRule, content string) string {
	switch rule {
	case config.CombDecodeAndWrite:
		hasDecode := containsAny(content, "base64", "Base64", "b64decode", "atob(", "decode(")
		hasWrite := containsAny(content, "WriteFile", "write(", "fwrite", "file_put_contents",
			"createFile", "FileOutputStream", "open(", "0755", "0777")
		if hasDecode && hasWrite {
			return "base64 decode + file write — possible embedded payload extraction"
		}
	case config.CombDecodeAndExecute:
		hasDecode := containsAny(content, "base64", "Base64", "b64decode", "atob(", "decode(")
		hasExec := containsAny(content, "exec", "system(", "subprocess", "child_process",
			"Process(", "Command(", "popen")
		if hasDecode && hasExec {
			return "base64 decode + execution — possible encoded command execution"
		}
	case config.CombFetchAndExecute:
		hasFetch := containsAny(content, "http.Get", "requests.get", "fetch(", "curl",
			"wget", "urllib", "HttpClient", "URL(")
		hasExec := containsAny(content, "exec", "system(", "subprocess", "child_process",
			"| sh", "| bash", "Process(", "Command(")
		if hasFetch && hasExec {
			return "network fetch + execution — possible remote code execution"
		}
	case config.CombSocketDNS:
		hasSocket := containsAny(content, "socket.socket", "SOCK_DGRAM", "SOCK_RAW",
			"socket(AF_INET", "sendto(", "recvfrom(")
		hasDNS := containsAny(content, "port 53", ":53", "53)", "\"53\"",
			"dns", "DNS", "nameserver")
		if hasSocket && hasDNS {
			return "raw socket + DNS port — possible DNS exfiltration via raw UDP"
		}
	case config.CombEnvAndLeak:
		hasEnv := containsAny(content, "os.environ", "os.getenv", "process.env",
			"os.Getenv", "System.getenv", "GITHUB_TOKEN", "AWS_SECRET")
		hasLeak := containsAny(content, "print(", "println(", "console.log",
			"logging.", "logger.", "sys.stdout", "stderr")
		if hasEnv && hasLeak {
			return "environment access + output — possible credential leak via stdout/logs"
		}
	}
	return ""
}

// extractStringLiterals pulls quoted strings from a line of code.
func extractStringLiterals(line string) []string {
	var literals []string
	for _, delim := range []byte{'"', '\'', '`'} {
		parts := strings.Split(line, string(delim))
		// Every other part is inside quotes
		for i := 1; i < len(parts); i += 2 {
			if len(parts[i]) > 0 {
				literals = append(literals, parts[i])
			}
		}
	}
	return literals
}

// shannonEntropy calculates the Shannon entropy of a string in bits per character.
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	freq := make(map[rune]int)
	for _, c := range s {
		freq[c]++
	}
	entropy := 0.0
	length := float64(len(s))
	for _, count := range freq {
		p := float64(count) / length
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}
	return entropy
}

// GenerateDeepAnalysisEvidence creates Evidence from deep analysis results.
func GenerateDeepAnalysisEvidence(result DeepAnalysisResult, cfg *config.AnalysisConfig, proposalID, tenantID string) []model.Evidence {
	var flags []string
	worstResult := model.EvidencePass

	for i := range result.SuspiciousTargets {
		t := &result.SuspiciousTargets[i]
		flags = append(flags, fmt.Sprintf("suspicious target in %s:%d — %s (%s)", t.File, t.Line, t.Target, t.Reason))
		if cfg.SuspiciousTargets.Mode == "enforce" {
			worstResult = model.EvidenceFail
		} else if worstResult != model.EvidenceFail {
			worstResult = model.EvidenceWarn
		}
	}

	for i := range result.HighEntropyStrings {
		e := &result.HighEntropyStrings[i]
		flags = append(flags, fmt.Sprintf("high-entropy string in %s:%d — %d chars, %.1f bits/char: %s", e.File, e.Line, e.Length, e.Entropy, e.Preview))
		if cfg.Entropy.Mode == "enforce" {
			worstResult = model.EvidenceFail
		} else if worstResult != model.EvidenceFail {
			worstResult = model.EvidenceWarn
		}
	}

	for i := range result.DangerousCombos {
		c := &result.DangerousCombos[i]
		flags = append(flags, fmt.Sprintf("dangerous combination in %s — %s", c.File, c.Details))
		if cfg.Combinations.Mode == "enforce" {
			worstResult = model.EvidenceFail
		} else if worstResult != model.EvidenceFail {
			worstResult = model.EvidenceWarn
		}
	}

	if len(flags) == 0 {
		return nil
	}

	summary := strings.Join(flags, "; ")
	return []model.Evidence{{
		EvidenceID:   fmt.Sprintf("deep:%s:%d", proposalID, time.Now().UnixNano()),
		ProposalID:   proposalID,
		TenantID:     tenantID,
		EvidenceType: model.EvidenceSecurityScan,
		Subject:      "deep-analysis",
		Result:       worstResult,
		Confidence:   model.ConfidenceMedium,
		Source:       "arbiter-deep-analysis",
		CreatedAt:    time.Now().UTC(),
		Summary:      &summary,
	}}
}
