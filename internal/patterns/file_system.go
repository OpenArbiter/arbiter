package patterns

var FileSystemWrite = Category{
	Name:        "file_system_write",
	Description: "Can write to the file system",
	Patterns: []string{
		// Go
		"os.Create", "os.WriteFile", "os.Remove", "os.RemoveAll",
		"os.MkdirAll", "ioutil.WriteFile",
		// Python
		"open(", "shutil.rmtree",
		// JavaScript
		"fs.writeFile", "fs.unlink",
		// Java
		"FileOutputStream",
		// C#
		"File.WriteAll", "File.Create",
		// PHP
		"file_put_contents(", "fwrite(", "chmod(",
		// C/C++
		"fopen(", "fprintf(",
		// Rust/Kotlin
		".writeBytes(", ".createFile(",
		// Swift
		"FileManager.default",
		// Generic
		"setExecutable(",
	},
}
