package patterns

// SecurityWeakening detects changes that deliberately disable or weaken
// security controls. These aren't bugs — they're intentional downgrades
// of the security posture, which is squarely in Arbiter's scope.
var SecurityWeakening = Category{
	Name:        "security_weakening",
	Description: "Disables or weakens security controls",
	Patterns: []string{
		// Python SSL verification bypass
		"ssl._create_unverified_context",
		"ssl._create_default_https_context",
		"verify=False",
		// Environment variables that disable TLS/SSL verification
		"NODE_TLS_REJECT_UNAUTHORIZED",
		"GIT_SSL_NO_VERIFY",
		"PYTHONHTTPSVERIFY",
		"REQUESTS_CA_BUNDLE",
		"CURL_CA_BUNDLE",
		"SSL_CERT_FILE",
		"SSL_CERT_DIR",
		// Go TLS skip
		"InsecureSkipVerify",
		// Java/Kotlin trust-all
		"TrustAllCerts", "X509TrustManager",
		// Generic
		"--no-check-certificate",
		"--insecure",
		"-k ",
	},
}
