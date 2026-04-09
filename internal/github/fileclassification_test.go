package github

import "testing"

func TestClassifyFile(t *testing.T) {
	tests := []struct {
		path string
		want FileClass
	}{
		// Source files
		{"main.go", FileClassSource},
		{"internal/engine/engine.go", FileClassSource},
		{"src/app.py", FileClassSource},

		// Test files
		{"main_test.go", FileClassTest},
		{"tests/test_app.py", FileClassTest},
		{"src/__tests__/app.test.js", FileClassTest},
		{"spec/models/user_spec.rb", FileClassTest},

		// Build files
		{"setup.py", FileClassBuild},
		{"setup.cfg", FileClassBuild},
		{"conftest.py", FileClassBuild},
		{"tests/conftest.py", FileClassBuild},
		{"Makefile", FileClassBuild},
		{"noxfile.py", FileClassBuild},
		{"tox.ini", FileClassBuild},
		{"CMakeLists.txt", FileClassBuild},
		{"foo.pth", FileClassBuild},
		{".pre-commit-config.yaml", FileClassBuild},

		// CI files
		{".github/workflows/ci.yml", FileClassCI},
		{".gitlab-ci.yml", FileClassCI},
		{".circleci/config.yml", FileClassCI},

		// Dependency files
		{"requirements.txt", FileClassDependency},
		{"go.mod", FileClassDependency},
		{"package.json", FileClassDependency},
		{"Cargo.toml", FileClassDependency},
		{"Gemfile", FileClassDependency},

		// Vendored code
		{"vendor/github.com/foo/bar/bar.go", FileClassVendored},
		{"third_party/lib/init.py", FileClassVendored},

		// Infrastructure
		{"Dockerfile", FileClassInfra},
		{"deploy/main.tf", FileClassInfra},
		{"k8s/deployment.yml", FileClassInfra},

		// Docs
		{"README.md", FileClassDocs},
		{"docs/architecture.md", FileClassDocs},

		// Config (existing isConfigFile checks infra-style configs)
		{".arbiter.yml", FileClassConfig},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := ClassifyFile(tt.path)
			if got != tt.want {
				t.Errorf("ClassifyFile(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestSeverityMultiplier(t *testing.T) {
	tests := []struct {
		class FileClass
		want  float64
	}{
		{FileClassSource, 1.0},
		{FileClassTest, 1.5},
		{FileClassBuild, 2.0},
		{FileClassCI, 2.0},
		{FileClassVendored, 2.0},
		{FileClassInfra, 1.5},
		{FileClassDocs, 1.0},
		{FileClassDependency, 1.0},
	}

	for _, tt := range tests {
		t.Run(string(tt.class), func(t *testing.T) {
			got := SeverityMultiplier(tt.class)
			if got != tt.want {
				t.Errorf("SeverityMultiplier(%q) = %v, want %v", tt.class, got, tt.want)
			}
		})
	}
}

func TestClassifyFiles(t *testing.T) {
	files := []PRFileInfo{
		{Filename: "main.go"},
		{Filename: "setup.py"},
		{Filename: "vendor/lib/foo.go"},
	}
	result := ClassifyFiles(files)
	if result["main.go"] != FileClassSource {
		t.Errorf("main.go = %q, want source", result["main.go"])
	}
	if result["setup.py"] != FileClassBuild {
		t.Errorf("setup.py = %q, want build", result["setup.py"])
	}
	if result["vendor/lib/foo.go"] != FileClassVendored {
		t.Errorf("vendor/lib/foo.go = %q, want vendored", result["vendor/lib/foo.go"])
	}
}
