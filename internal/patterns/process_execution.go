package patterns

var ProcessExecution = Category{
	Name:        "process_execution",
	Description: "Can execute system commands",
	Patterns: []string{
		// Go
		"os/exec", "exec.Command", "exec.Run",
		"syscall.Exec", "syscall.ForkExec", "\"syscall\"",
		// Python
		"subprocess", "os.system(", "popen(",
		// JavaScript/Node
		"child_process",
		// Java/Kotlin
		"Runtime.exec", "ProcessBuilder",
		// C/C++
		"system(", "popen(", "execvp(", "execve(",
		// C#
		"Process.Start", "ProcessStartInfo",
		// PHP
		"shell_exec(", "passthru(", "proc_open(",
		// Rust
		"std::process::Command", "Command::new(",
		// Swift
		"Process()", "process.run(", "NSTask",
		// Generic
		"| sh", "| bash",
	},
}
