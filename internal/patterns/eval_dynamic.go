package patterns

var EvalDynamic = Category{
	Name:        "eval_dynamic",
	Description: "Dynamic code execution",
	Patterns: []string{
		// Generic
		"eval(", "exec(", "Function(",
		// Go
		"reflect.Value", "unsafe.Pointer", "//go:linkname",
		"plugin.Open", "\"plugin\"",
		// Python — indirect execution primitives
		"getattr(", "setattr(", "globals()", "__builtins__",
		"__import__(", "importlib", "compile(",
		"types.FunctionType", "ctypes.",
		// Rust
		"unsafe {", "unsafe fn",
		// PHP
		"unserialize(", "call_user_func",
		// Java/Kotlin
		"Class.forName(", "ScriptEngineManager",
		// C#
		"Assembly.Load", "Activator.CreateInstance", "BinaryFormatter",
		// Java
		"ObjectInputStream",
		// C/C++
		"dlopen(", "dlsym(",
		// Swift
		"NSClassFromString(",
	},
}
