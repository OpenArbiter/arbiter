package patterns

// VulnPatterns detects common vulnerability patterns (OWASP Top 10 style).
// These are code constructs that are almost always insecure.
var VulnPatterns = Category{
	Name:        "vulnerability_pattern",
	Description: "Common security vulnerability pattern",
	Patterns: []string{
		// SQL injection — string formatting in queries
		"cursor.execute(f\"", "cursor.execute(f'",
		"cursor.execute(\"%s\"", "cursor.execute('%s'",
		".execute(\"SELECT", ".execute(\"INSERT", ".execute(\"UPDATE", ".execute(\"DELETE",
		".format(sql", "% sql", "+ sql",
		"raw(\"SELECT", "raw(\"INSERT", "raw(\"UPDATE", "raw(\"DELETE",
		// Unsafe temp files
		"open(\"/tmp/", "open('/tmp/",
		"os.path.join(\"/tmp\"", "os.path.join('/tmp'",
		// SSRF (user-controlled URLs)
		"request.args.get", "request.form.get",
	},
}
