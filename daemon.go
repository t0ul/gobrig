package gobrig

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Daemon is the in-Brig executor — the server side of the Client transport. It runs a
// Detonation's argv with NO shell (exec argv[0] argv[1:]) and returns the combined output.
// It applies no Policy itself: the host side gates argv before sending, and the Brig (an
// egress-denied, ephemeral VM) is already the containment boundary. Inside the real Brig it
// serves over vsock (the guest has no NIC); over TCP for host-side development and tests.
type Daemon struct {
	Timeout time.Duration              // per-detonation wall; 0 uses 60s
	Log     func(traceID, argv string) // optional structured log hook; output is never logged
}

// RunArgv executes argv[0] with argv[1:], no shell, and returns combined stdout+stderr.
// Running with no shell is what makes the detonation immune to argument injection.
func (d *Daemon) RunArgv(ctx context.Context, argv []string) string {
	if len(argv) == 0 {
		return "[gobrig] no argv"
	}
	to := d.Timeout
	if to == 0 {
		to = 60 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	out, err := cmd.CombinedOutput()
	s := string(out)
	if err != nil {
		s += "\n[exit] " + err.Error()
	}
	return s
}

// Handler serves detonations over HTTP (POST a JSON Detonation, get a JSON Result) — the
// wire the Client speaks, identical over TCP or vsock.
func (d *Daemon) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var det Detonation
		if err := json.NewDecoder(r.Body).Decode(&det); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if d.Log != nil {
			d.Log(det.TraceID, strings.Join(det.Argv, " "))
		}
		out := d.RunArgv(r.Context(), det.Argv)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Result{Output: out, TraceID: det.TraceID})
	})
}
