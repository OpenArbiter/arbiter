package patterns

import "testing"

func TestNormalizeConfusables(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no confusables",
			input: "os/exec",
			want:  "os/exec",
		},
		{
			name:  "cyrillic e in exec",
			input: "os/\u0435xec", // Cyrillic е (U+0435) instead of Latin e
			want:  "os/exec",
		},
		{
			name:  "cyrillic o in open",
			input: "\u043Epen(", // Cyrillic о (U+043E) instead of Latin o
			want:  "open(",
		},
		{
			name:  "multiple confusables",
			input: "\u0435v\u0430l(", // Cyrillic е and а
			want:  "eval(",
		},
		{
			name:  "fullwidth parentheses",
			input: "eval\uFF08\uFF09", // fullwidth ( and )
			want:  "eval()",
		},
		{
			name:  "mixed cyrillic in subprocess",
			input: "sub\u0440ro\u0441ess", // Cyrillic р and с
			want:  "subprocess",
		},
		{
			name:  "fullwidth slash in import path",
			input: "os\uFF0Fexec", // fullwidth /
			want:  "os/exec",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "pure ascii",
			input: "func main() { fmt.Println(\"hello\") }",
			want:  "func main() { fmt.Println(\"hello\") }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeConfusables(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeConfusables(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeConfusables_PatternMatching(t *testing.T) {
	// Simulate what scopeanalysis does: check if normalized line matches a pattern
	patterns := []string{"os/exec", "subprocess", "eval(", "getattr("}

	attacks := []struct {
		name    string
		line    string
		pattern string
	}{
		{"cyrillic e in os/exec", "import \"os/\u0435xec\"", "os/exec"},
		{"cyrillic in subprocess", "import sub\u0440ro\u0441ess", "subprocess"},
		{"cyrillic in eval", "\u0435v\u0430l(code)", "eval("},
		{"cyrillic in getattr", "g\u0435tattr(obj, name)", "getattr("},
	}

	for _, att := range attacks {
		t.Run(att.name, func(t *testing.T) {
			normalized := NormalizeConfusables(att.line)
			found := false
			for _, p := range patterns {
				if contains(normalized, p) {
					if p == att.pattern {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("normalized %q did not match pattern %q (normalized: %q)", att.line, att.pattern, normalized)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
