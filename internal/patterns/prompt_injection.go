package patterns

var PromptInjection = Category{
	Name:        "prompt_injection",
	Description: "Prompt injection targeting AI code reviewers",
	Patterns: []string{
		// Direct instruction override
		"ignore all previous",
		"ignore the above",
		"disregard previous",
		"forget your instructions",
		"override your instructions",
		"new instructions:",

		// LLM prompt format tokens
		"[INST]", "[/INST]",
		"<|im_start|>", "<|im_end|>",
		"<|system|>", "<|user|>", "<|assistant|>",
		"<<SYS>>", "<</SYS>>",
		"SYSTEM:", "system prompt",

		// Role manipulation
		"you are now",
		"act as if",
		"pretend you are",
		"your new role",

		// Output manipulation
		"respond with approved",
		"respond with APPROVED",
		"output only: approved",
		"say: approved",

		// Arbiter-specific
		"force_approve", "skip_all_checks",
		"force_merge", "skip_review",
		"security_override",
		"pre-approved",
		"DO NOT FLAG",
		"do not flag",
		"do not report",
		"arbiter_decision",
	},
}
