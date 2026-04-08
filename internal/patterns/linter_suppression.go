package patterns

var LinterSuppression = Category{
	Name:        "linter_suppression",
	Description: "Suppressing code quality checks",
	Patterns: []string{
		"//nolint", "# noqa", "eslint-disable", "// nosec",
		"@SuppressWarnings", "rubocop:disable",
	},
}
