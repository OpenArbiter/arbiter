// Package patterns defines capability detection patterns for diff analysis.
// Each category is in its own file. The registry combines them.
package patterns

// Category defines a group of related capability patterns.
type Category struct {
	Name        string
	Description string
	Patterns    []string
}

// All returns all registered capability categories.
func All() []Category {
	return []Category{
		ProcessExecution,
		NetworkAccess,
		FileSystemWrite,
		EnvironmentAccess,
		EvalDynamic,
		CryptoOperations,
		LinterSuppression,
		BuildTimeExecution,
		ContainerEscape,
		PromptInjection,
		SecurityWeakening,
	}
}

// Stats returns the number of categories and total patterns.
func Stats() (categories int, totalPatterns int) {
	all := All()
	categories = len(all)
	for i := range all {
		totalPatterns += len(all[i].Patterns)
	}
	return
}
