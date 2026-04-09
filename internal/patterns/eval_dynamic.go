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
		// Insecure deserialization (arbitrary code execution via data)
		"pickle.load", "pickle.loads", "shelve.open",
		"yaml.load(", "yaml.UnsafeLoader", "yaml.FullLoader",
		"marshal.load", "marshal.loads",
		"jsonpickle.decode",
		// XML external entity injection
		"xml.etree.ElementTree.parse", "xml.sax.make_parser",
		"lxml.etree.parse", "XMLReader(",
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
