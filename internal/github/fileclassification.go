package github

import "strings"

// FileClass represents a file's execution context, which determines
// what scrutiny level and pattern sets apply.
type FileClass string

const (
	FileClassSource     FileClass = "source"
	FileClassTest       FileClass = "test"
	FileClassBuild      FileClass = "build"
	FileClassCI         FileClass = "ci"
	FileClassDependency FileClass = "dependency"
	FileClassVendored   FileClass = "vendored"
	FileClassInfra      FileClass = "infrastructure"
	FileClassDocs       FileClass = "docs"
	FileClassConfig     FileClass = "config"
)

// ClassifyFile determines the execution context of a file based on its path.
func ClassifyFile(path string) FileClass {
	lower := strings.ToLower(path)
	base := lower
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		base = lower[idx+1:]
	}

	// Order matters — more specific classifications first

	// Vendored code
	if isVendoredFile(lower) {
		return FileClassVendored
	}

	// CI config
	if isCIFile(lower) {
		return FileClassCI
	}

	// Build files — includes Python build hooks that execute at install/test time
	if isBuildSystemFile(base, lower) {
		return FileClassBuild
	}

	// Dependency files
	if isDepsFile(lower) {
		return FileClassDependency
	}

	// Test files
	if isTestFile(lower) {
		return FileClassTest
	}

	// Infrastructure
	if isInfraFile(lower) {
		return FileClassInfra
	}

	// Documentation
	if isDocsFile(lower) {
		return FileClassDocs
	}

	// Config
	if isConfigFile(lower) {
		return FileClassConfig
	}

	return FileClassSource
}

// isBuildSystemFile returns true for files that execute code during build/install/test
// collection — beyond CI configs.
func isBuildSystemFile(base, lower string) bool {
	// Python build files that execute at install or collection time
	pythonBuild := []string{
		"setup.py", "setup.cfg", "conftest.py",
		"noxfile.py", "tox.ini", "fabfile.py",
	}
	for _, f := range pythonBuild {
		if base == f {
			return true
		}
	}
	// .pth files execute at interpreter startup
	if strings.HasSuffix(base, ".pth") {
		return true
	}

	// Other build files
	buildFiles := []string{
		"makefile", "gnumakefile", "rakefile",
		"justfile", "earthfile", "tiltfile",
		"sconscript", "sconstruct",
		"cmakelists.txt", "meson.build",
	}
	for _, f := range buildFiles {
		if base == f {
			return true
		}
	}

	// Bazel
	if base == "build" || base == "build.bazel" || base == "workspace" || base == "workspace.bazel" {
		return true
	}

	return false
}

// isVendoredFile returns true for files in vendored/third-party directories.
func isVendoredFile(lower string) bool {
	vendorPrefixes := []string{
		"vendor/", "third_party/", "extern/", "deps/", "lib/vendor/",
	}
	for _, prefix := range vendorPrefixes {
		if strings.HasPrefix(lower, prefix) || strings.Contains(lower, "/"+prefix) {
			return true
		}
	}
	return strings.Contains(lower, "node_modules/")
}

// isInfraFile returns true for infrastructure-as-code files.
func isInfraFile(lower string) bool {
	if strings.HasSuffix(lower, ".tf") || strings.HasSuffix(lower, ".tfvars") {
		return true
	}
	base := lower
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		base = lower[idx+1:]
	}
	infraFiles := []string{"dockerfile", "docker-compose.yml", "docker-compose.yaml",
		"kustomization.yml", "kustomization.yaml"}
	for _, f := range infraFiles {
		if base == f {
			return true
		}
	}
	return containsAny(lower, "helm/", "k8s/", "kubernetes/", "terraform/")
}

// isDocsFile returns true for documentation files.
func isDocsFile(lower string) bool {
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".rst") ||
		strings.HasSuffix(lower, ".adoc") ||
		strings.HasPrefix(lower, "docs/") || strings.Contains(lower, "/docs/")
}

// SeverityMultiplier returns the severity escalation factor for a file class.
// Signals found in higher-scrutiny file classes are treated as more severe.
func SeverityMultiplier(class FileClass) float64 {
	switch class {
	case FileClassBuild, FileClassCI, FileClassVendored:
		return 2.0
	case FileClassTest, FileClassInfra:
		return 1.5
	default:
		return 1.0
	}
}

// ClassifyFiles returns a map of filename → FileClass for all files.
func ClassifyFiles(files []PRFileInfo) map[string]FileClass {
	result := make(map[string]FileClass, len(files))
	for i := range files {
		result[files[i].Filename] = ClassifyFile(files[i].Filename)
	}
	return result
}
