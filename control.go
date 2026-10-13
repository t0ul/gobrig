package gobrig

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Controller is the Brig behavior the control server needs, factored out so the HTTP
// routing and guards are testable without a live, codesigned VM.
type Controller interface {
	ResetNow() error // restore the golden snapshot for a pristine, already-booted guest
	Ready() bool     // the in-Brig daemon answers and no reset is mid-flight
	Busy() bool      // a reset or save is in progress
	Available() bool // snapshot/restore is usable for this configuration
}

// ControlHandler builds the loopback control routes: GET /healthz for readiness and POST
// /reset to restore the golden snapshot on demand. It is pure HTTP over a Controller, so it
// tests without a VM.
func ControlHandler(c Controller) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if c.Ready() {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "ok\n")
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, "not ready\n")
	})
	mux.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !c.Available() {
			w.WriteHeader(http.StatusConflict)
			io.WriteString(w, "snapshot unavailable for this configuration\n")
			return
		}
		if c.Busy() {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, "reset already in progress\n")
			return
		}
		if err := c.ResetNow(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "reset failed: %v\n", err)
			return
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "reset ok\n")
	})
	return mux
}

// ProbeReady dials the vsock bridge and sends an empty detonation; a well-formed HTTP 200
// means the in-Brig daemon accepts commands (an empty body runs nothing). It reuses the
// daemon's real protocol as a readiness check.
func ProbeReady(bridgeAddr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if httpPing(bridgeAddr) {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}

func httpPing(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	const body = "{}"
	req := fmt.Sprintf("POST / HTTP/1.1\r\nHost: brig\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
	if _, err := io.WriteString(conn, req); err != nil {
		return false
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return false
	}
	return containsStatus200(line)
}

func containsStatus200(statusLine string) bool {
	return len(statusLine) >= 12 && statusLine[9:12] == "200"
}

// SnapshotUsable reports whether the golden snapshot exists, is non-empty, and is newer
// than every asset the Brig boots from. A snapshot older than any asset is stale: restoring
// it would silently resurrect an old guest — the same footgun as booting unrebuilt guest
// binaries — so it is ignored and rebuilt.
func SnapshotUsable(path string, assets []string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() == 0 {
		return false
	}
	snap := fi.ModTime()
	for _, a := range assets {
		if newestMod(a).After(snap) {
			return false
		}
	}
	return true
}

func newestMod(path string) time.Time {
	var newest time.Time
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}
