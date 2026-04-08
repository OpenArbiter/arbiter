package patterns

var NetworkAccess = Category{
	Name:        "network_access",
	Description: "Can make network requests",
	Patterns: []string{
		// Go
		"net/http", "net.Dial", "http.Get", "http.Post", "http.NewRequest",
		// Python
		"requests.get", "requests.post", "urllib",
		// JavaScript
		"fetch(", "axios", "XMLHttpRequest", "WebSocket",
		// Java/Kotlin
		"HttpURLConnection", ".readText()",
		// C#
		"HttpClient", "WebClient", "HttpWebRequest",
		// PHP
		"curl_init(", "curl_exec(", "file_get_contents(\"http",
		// Rust
		"TcpStream", "UdpSocket", "hyper::Client",
		// Swift
		"URL(string:", "URLSession",
		// C
		"socket(",
	},
}
