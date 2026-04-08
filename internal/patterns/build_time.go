package patterns

var BuildTimeExecution = Category{
	Name:        "build_time_execution",
	Description: "Executes commands at build time",
	Patterns: []string{
		// Go
		"//go:generate", "go:generate",
		// Git hooks
		"pre-commit", "post-commit", "pre-push",
		// Make
		"Makefile:", "$(shell",
		// Package manager hooks
		"postinstall", "preinstall", "postbuild",
		// Python build hooks
		"cmdclass", "entry_points", "console_scripts",
		"build-backend", "data_files",
		// Shell attack patterns
		"/dev/tcp", "netcat",
		"bash -i", "bash -c",
		"curl ", "wget ",
		"rm -rf /",
	},
}
