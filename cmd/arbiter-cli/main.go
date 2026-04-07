package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func main() {
	baseURL := os.Getenv("ARBITER_URL")
	if baseURL == "" {
		baseURL = "https://api.openarbiter.com"
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "health":
		get(baseURL + "/health")
	case "stats":
		get(baseURL + "/stats")
	case "proposals":
		if len(os.Args) < 3 {
			fmt.Println("Usage: arbiter-cli proposals <tenant_id>")
			fmt.Println("Example: arbiter-cli proposals github:121739272")
			os.Exit(1)
		}
		get(baseURL + "/api/proposals?tenant_id=" + os.Args[2])
	case "decision":
		if len(os.Args) < 3 {
			fmt.Println("Usage: arbiter-cli decision <proposal_id>")
			os.Exit(1)
		}
		token := requireEnv("GITHUB_TOKEN")
		getAuth(baseURL+"/api/decision?proposal_id="+os.Args[2], token)
	case "challenge":
		if len(os.Args) < 5 {
			fmt.Println("Usage: arbiter-cli challenge <proposal_id> <severity> <summary>")
			fmt.Println("Example: arbiter-cli challenge gh:org/repo:pr:1:sha:abc high 'Missing error handling'")
			os.Exit(1)
		}
		token := requireEnv("GITHUB_TOKEN")
		body := fmt.Sprintf(`{"proposal_id":"%s","challenge_type":"hidden_behavior_change","target":"PR","severity":"%s","summary":"%s"}`,
			os.Args[2], os.Args[3], strings.Join(os.Args[4:], " "))
		postAuth(baseURL+"/api/challenge", body, token)
	case "resolve":
		if len(os.Args) < 3 {
			fmt.Println("Usage: arbiter-cli resolve <challenge_id> [note]")
			os.Exit(1)
		}
		token := requireEnv("GITHUB_TOKEN")
		note := "Resolved via CLI"
		if len(os.Args) > 3 {
			note = strings.Join(os.Args[3:], " ")
		}
		body := fmt.Sprintf(`{"challenge_id":"%s","note":"%s","action":"resolve"}`, os.Args[2], note)
		postAuth(baseURL+"/api/challenge/resolve", body, token)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Arbiter CLI")
	fmt.Println()
	fmt.Println("Usage: arbiter-cli <command> [args]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  health                          Check service health")
	fmt.Println("  stats                           Show operational stats")
	fmt.Println("  proposals <tenant_id>           List open proposals")
	fmt.Println("  challenge <proposal> <sev> <msg> Create a challenge")
	fmt.Println("  resolve <challenge_id> [note]   Resolve a challenge")
	fmt.Println()
	fmt.Println("Environment:")
	fmt.Println("  ARBITER_URL    Base URL (default: https://api.openarbiter.com)")
	fmt.Println("  GITHUB_TOKEN   Required for write operations")
}

func get(url string) {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	prettyPrint(resp)
}

func getAuth(url, token string) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	prettyPrint(resp)
}

func postAuth(url, body, token string) {
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	prettyPrint(resp)
}

func prettyPrint(resp *http.Response) {
	data, _ := io.ReadAll(resp.Body)
	var parsed any
	if err := json.Unmarshal(data, &parsed); err == nil {
		pretty, _ := json.MarshalIndent(parsed, "", "  ")
		fmt.Println(string(pretty))
	} else {
		fmt.Println(string(data))
	}
	if resp.StatusCode >= 400 {
		os.Exit(1)
	}
}

func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		fmt.Fprintf(os.Stderr, "%s is required\n", key)
		os.Exit(1)
	}
	return val
}
