package gobrig_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/t0ul/gobrig"
)

// fakeVulnApp simulates a command-injection target: GET /exec?cmd=X runs X in a shell, so
// an "echo <nonce>" payload comes back echoed. Any other endpoint/param is inert. It is the
// target shape, not a mock of the hunter (the hunter logic is what is under test).
func fakeVulnApp(_ context.Context, rawurl string) (string, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return "", err
	}
	if u.Path != "/exec" {
		return "not found", nil
	}
	cmd := u.Query().Get("cmd")
	if strings.HasPrefix(cmd, "echo ") {
		return strings.TrimPrefix(cmd, "echo ") + "\n", nil // the shell would echo the argument
	}
	return "", nil
}

// TestDiscoverRCE: the hunter finds the injectable endpoint/param without being told which,
// and confirms by the echoed nonce.
func TestDiscoverRCE(t *testing.T) {
	f, ok := gobrig.DiscoverRCE(context.Background(), fakeVulnApp, "http://127.0.0.1:8088", nil, nil)
	if !ok || !f.Confirmed {
		t.Fatalf("expected a confirmed RCE, got %+v", f)
	}
	if f.Endpoint != "/exec" || f.Param != "cmd" {
		t.Errorf("want /exec?cmd=, got %s?%s=", f.Endpoint, f.Param)
	}
	if f.CWE != "CWE-78" {
		t.Errorf("want CWE-78, got %q", f.CWE)
	}
	// The exploit builder targets the discovered point.
	if got := f.Exploit("http://127.0.0.1:8088", "cat /flag"); got != "http://127.0.0.1:8088/exec?cmd=cat+%2Fflag" {
		t.Errorf("exploit url wrong: %s", got)
	}
}

// TestDiscoverRCENone: a target with no injectable param yields no finding.
func TestDiscoverRCENone(t *testing.T) {
	safe := func(_ context.Context, _ string) (string, error) { return "static page", nil }
	if _, ok := gobrig.DiscoverRCE(context.Background(), safe, "http://x", nil, nil); ok {
		t.Error("a non-vulnerable target must not be flagged")
	}
}
