package gobrig_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t0ul/gobrig"
)

// TestDaemonRunArgvNoShell runs a real command with no shell and checks the output comes
// back, and that a shell-only construct is taken literally (not interpreted) — the no-shell
// property.
func TestDaemonRunArgvNoShell(t *testing.T) {
	d := &gobrig.Daemon{}
	if out := d.RunArgv(context.Background(), []string{"echo", "brig-ok"}); !strings.Contains(out, "brig-ok") {
		t.Errorf("expected echo output, got %q", out)
	}
	// With no shell, "echo $HOME" prints the literal, never an expansion.
	out := d.RunArgv(context.Background(), []string{"echo", "$HOME"})
	if !strings.Contains(out, "$HOME") {
		t.Errorf("no-shell exec should print the literal $HOME, got %q", out)
	}
	if out := d.RunArgv(context.Background(), nil); !strings.Contains(out, "no argv") {
		t.Errorf("empty argv should be reported, got %q", out)
	}
}

// TestDaemonClientRoundTrip wires the real Client to the real Daemon over an httptest
// server (no mock): a detonation runs and its output and trace id come back intact.
func TestDaemonClientRoundTrip(t *testing.T) {
	d := &gobrig.Daemon{}
	ts := httptest.NewServer(d.Handler())
	defer ts.Close()

	c := gobrig.Client{Bridge: strings.TrimPrefix(ts.URL, "http://")}
	res, err := c.Detonate(context.Background(), gobrig.Detonation{Argv: []string{"echo", "loop-closed"}, TraceID: "tz"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "loop-closed") || res.TraceID != "tz" {
		t.Errorf("client<->daemon round trip wrong: %+v", res)
	}
}
