package github

import "testing"

func TestCorrelate_CapabilityScatter(t *testing.T) {
	scope := &ScopeAnalysis{
		NewCapabilities: []Capability{
			{Name: "process_execution", File: "a.py", Pattern: "subprocess"},
			{Name: "process_execution", File: "b.py", Pattern: "os.system("},
			{Name: "process_execution", File: "c.py", Pattern: "subprocess"},
		},
	}
	classes := map[string]FileClass{
		"a.py": FileClassSource,
		"b.py": FileClassSource,
		"c.py": FileClassSource,
	}
	addedLines := map[string][]string{
		"a.py": {"import subprocess"},
		"b.py": {"os.system('ls')"},
		"c.py": {"subprocess.run(['echo'])"},
	}
	insights := DiffInsights{TotalFiles: 3}

	result := Correlate(scope, classes, addedLines, insights)

	found := false
	for _, e := range result.Escalations {
		if e.Rule == "capability_scatter" {
			found = true
			if e.Severity != "high" {
				t.Errorf("capability_scatter severity = %q, want high", e.Severity)
			}
		}
	}
	if !found {
		t.Error("expected capability_scatter escalation, got none")
	}
}

func TestCorrelate_TestNetworkExec(t *testing.T) {
	scope := &ScopeAnalysis{
		NewCapabilities: []Capability{
			{Name: "network_access", File: "tests/test_exfil.py", Pattern: "socket.getaddrinfo"},
		},
	}
	classes := map[string]FileClass{
		"tests/test_exfil.py": FileClassTest,
	}
	addedLines := map[string][]string{
		"tests/test_exfil.py": {"socket.getaddrinfo(encoded, None)"},
	}
	insights := DiffInsights{TotalFiles: 1}

	result := Correlate(scope, classes, addedLines, insights)

	found := false
	for _, e := range result.Escalations {
		if e.Rule == "test_network_exec" {
			found = true
			if e.Severity != "high" {
				t.Errorf("test_network_exec severity = %q, want high", e.Severity)
			}
		}
	}
	if !found {
		t.Error("expected test_network_exec escalation, got none")
	}
}

func TestCorrelate_BuildPlusCode(t *testing.T) {
	scope := &ScopeAnalysis{
		NewCapabilities: []Capability{
			{Name: "process_execution", File: "app.py", Pattern: "subprocess"},
		},
	}
	classes := map[string]FileClass{
		"setup.py": FileClassBuild,
		"app.py":   FileClassSource,
	}
	addedLines := map[string][]string{
		"setup.py": {"cmdclass = {'install': Install}"},
		"app.py":   {"import subprocess"},
	}
	insights := DiffInsights{TotalFiles: 2}

	result := Correlate(scope, classes, addedLines, insights)

	found := false
	for _, e := range result.Escalations {
		if e.Rule == "build_plus_code" {
			found = true
		}
	}
	if !found {
		t.Error("expected build_plus_code escalation, got none")
	}
}

func TestCorrelate_DepPlusVendored(t *testing.T) {
	scope := &ScopeAnalysis{}
	classes := map[string]FileClass{
		"requirements.txt":           FileClassDependency,
		"vendor/evil/__init__.py":    FileClassVendored,
	}
	addedLines := map[string][]string{
		"requirements.txt":        {"evil-package==1.0.0"},
		"vendor/evil/__init__.py": {"import os; os.system('whoami')"},
	}
	insights := DiffInsights{DepsModified: true, TotalFiles: 2}

	result := Correlate(scope, classes, addedLines, insights)

	foundDep := false
	foundVendor := false
	for _, e := range result.Escalations {
		if e.Rule == "dep_plus_vendored" {
			foundDep = true
		}
		if e.Rule == "new_vendored_code" {
			foundVendor = true
		}
	}
	if !foundDep {
		t.Error("expected dep_plus_vendored escalation, got none")
	}
	if !foundVendor {
		t.Error("expected new_vendored_code escalation, got none")
	}
}

func TestCorrelate_CapabilityPlusTestDeletion(t *testing.T) {
	scope := &ScopeAnalysis{
		NewCapabilities: []Capability{
			{Name: "eval_dynamic", File: "app.py", Pattern: "getattr("},
		},
	}
	classes := map[string]FileClass{
		"app.py": FileClassSource,
	}
	addedLines := map[string][]string{
		"app.py": {"func = getattr(module, name)"},
	}
	insights := DiffInsights{TestsDeleted: true, TotalFiles: 2}

	result := Correlate(scope, classes, addedLines, insights)

	found := false
	for _, e := range result.Escalations {
		if e.Rule == "capability_plus_test_deletion" {
			found = true
		}
	}
	if !found {
		t.Error("expected capability_plus_test_deletion escalation, got none")
	}
}

func TestCorrelate_LargeCleanDiff(t *testing.T) {
	scope := &ScopeAnalysis{}
	classes := map[string]FileClass{
		"app.py": FileClassSource,
	}
	// 150 lines with no capabilities
	lines := make([]string, 150)
	for i := range lines {
		lines[i] = "x = 1"
	}
	addedLines := map[string][]string{
		"app.py": lines,
	}
	insights := DiffInsights{TotalFiles: 1}

	result := Correlate(scope, classes, addedLines, insights)

	found := false
	for _, e := range result.Escalations {
		if e.Rule == "large_clean_diff" {
			found = true
			if e.Severity != "info" {
				t.Errorf("large_clean_diff severity = %q, want info", e.Severity)
			}
		}
	}
	if !found {
		t.Error("expected large_clean_diff escalation, got none")
	}
}

func TestCorrelate_NoFalsePositives(t *testing.T) {
	// Normal PR: one code file, no capabilities, no special files
	scope := &ScopeAnalysis{}
	classes := map[string]FileClass{
		"app.go":      FileClassSource,
		"app_test.go": FileClassTest,
	}
	addedLines := map[string][]string{
		"app.go":      {"func Add(a, b int) int { return a + b }"},
		"app_test.go": {"func TestAdd(t *testing.T) {}"},
	}
	insights := DiffInsights{TotalFiles: 2, TestsModified: true}

	result := Correlate(scope, classes, addedLines, insights)

	if len(result.Escalations) != 0 {
		t.Errorf("expected no escalations for clean PR, got %d: %v", len(result.Escalations), result.Escalations)
	}
}
