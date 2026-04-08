package patterns

var NetworkAccess = Category{
	Name:        "network_access",
	Description: "Can make network requests",
	Patterns: []string{
		// Go
		"net/http", "net.Dial", "http.Get", "http.Post", "http.NewRequest",
		// Go DNS
		"net.LookupHost", "net.LookupAddr", "net.LookupTXT",
		"net.LookupCNAME", "net.LookupMX",
		// Python
		"requests.get", "requests.post", "urllib",
		// Python DNS
		"socket.getaddrinfo", "dns.resolver", "dns.query",
		// JavaScript
		"fetch(", "axios", "XMLHttpRequest", "WebSocket",
		// JavaScript DNS
		"dns.lookup", "dns.resolve",
		// Java/Kotlin
		"HttpURLConnection", ".readText()",
		// Java DNS
		"InetAddress.getByName",
		// C#
		"HttpClient", "WebClient", "HttpWebRequest",
		// C# DNS
		"Dns.GetHostEntry", "Dns.GetHostAddresses",
		// PHP
		"curl_init(", "curl_exec(", "file_get_contents(\"http",
		// PHP DNS
		"gethostbyname(", "dns_get_record(",
		// Rust
		"TcpStream", "UdpSocket", "hyper::Client",
		// Swift
		"URL(string:", "URLSession",
		// C
		"socket(", "getaddrinfo(",
	},
}
