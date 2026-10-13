// Command httpc is a minimal, method-complete HTTP client for use INSIDE the Brig,
// where busybox wget cannot send DELETE/PUT/PATCH. It is the agent's HTTP primitive for
// REST-API targets (login, bearer auth, and the full set of methods a real API attack
// needs). It prints a status line then the body, so a caller can read both. It makes no
// outbound connection of its own beyond the URL it is told to fetch; in the egress-denied
// Brig that is the guest loopback only.
//
// Usage: httpc -X METHOD [-H 'Key: Value' ...] [-d BODY] URL
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type headers []string

func (h *headers) String() string { return strings.Join(*h, ",") }
func (h *headers) Set(v string) error {
	*h = append(*h, v)
	return nil
}

func main() {
	var (
		method = "GET"
		body   string
		hs     headers
		url    string
	)
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-X":
			i++
			if i < len(args) {
				method = strings.ToUpper(args[i])
			}
		case "-H":
			i++
			if i < len(args) {
				_ = hs.Set(args[i])
			}
		case "-d":
			i++
			if i < len(args) {
				body = args[i]
			}
		default:
			url = args[i]
		}
	}
	if url == "" {
		fmt.Fprintln(os.Stderr, "usage: httpc -X METHOD [-H 'k: v'] [-d BODY] URL")
		os.Exit(2)
	}
	if body != "" && method == "GET" {
		method = "POST"
	}

	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "httpc: "+err.Error())
		os.Exit(1)
	}
	for _, h := range hs {
		if k, v, ok := strings.Cut(h, ":"); ok {
			req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "httpc: "+err.Error())
		os.Exit(1)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	// Status line first (so a caller can read the code), then the body.
	fmt.Printf("HTTP/1.1 %s\n%s", resp.Status, string(b))
}
