package patterns

var EnvironmentAccess = Category{
	Name:        "environment_access",
	Description: "Reads environment variables (potential secret access)",
	Patterns: []string{
		"os.Getenv", "os.Environ", "process.env",
		"os.environ", "getenv(",
	},
}
