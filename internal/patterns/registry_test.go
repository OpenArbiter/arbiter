package patterns

import "testing"

func TestAllCategoriesRegistered(t *testing.T) {
	all := All()
	if len(all) < 10 {
		t.Errorf("expected at least 10 categories, got %d", len(all))
	}

	// Check known categories exist
	names := make(map[string]bool)
	for _, cat := range all {
		names[cat.Name] = true
	}

	// hidden_characters uses custom rune detection, not pattern strings, so it's not in All()
	required := []string{
		"process_execution", "network_access", "eval_dynamic",
		"file_system_write", "environment_access",
		"build_time_execution", "container_escape",
		"prompt_injection",
	}
	for _, name := range required {
		if !names[name] {
			t.Errorf("missing required category: %s", name)
		}
	}
}

func TestPythonIndirectExecutionPatterns(t *testing.T) {
	// Verify the new Python indirect execution patterns are in eval_dynamic
	patterns := EvalDynamic.Patterns

	required := []string{
		"getattr(", "setattr(", "globals()", "__builtins__",
		"__import__(", "importlib", "compile(",
		"ctypes.",
	}

	patternSet := make(map[string]bool)
	for _, p := range patterns {
		patternSet[p] = true
	}

	for _, r := range required {
		if !patternSet[r] {
			t.Errorf("eval_dynamic missing Python indirect execution pattern: %q", r)
		}
	}
}

func TestDNSExfiltrationPatterns(t *testing.T) {
	patterns := NetworkAccess.Patterns

	required := []string{
		"socket.getaddrinfo", "dns.resolver",
		"net.LookupHost", "net.LookupTXT",
		"getaddrinfo(",
	}

	patternSet := make(map[string]bool)
	for _, p := range patterns {
		patternSet[p] = true
	}

	for _, r := range required {
		if !patternSet[r] {
			t.Errorf("network_access missing DNS exfiltration pattern: %q", r)
		}
	}
}

func TestBuildTimePatterns(t *testing.T) {
	patterns := BuildTimeExecution.Patterns

	required := []string{
		"cmdclass", "entry_points", "console_scripts", "build-backend",
	}

	patternSet := make(map[string]bool)
	for _, p := range patterns {
		patternSet[p] = true
	}

	for _, r := range required {
		if !patternSet[r] {
			t.Errorf("build_time_execution missing Python build pattern: %q", r)
		}
	}
}

func TestStats(t *testing.T) {
	categories, total := Stats()
	if categories < 10 {
		t.Errorf("expected at least 10 categories, got %d", categories)
	}
	// After adding Python indirect exec, DNS, and build-time patterns
	if total < 200 {
		t.Errorf("expected at least 200 total patterns, got %d", total)
	}
	t.Logf("pattern stats: %d categories, %d total patterns", categories, total)
}
