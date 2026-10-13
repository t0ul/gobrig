package gobrig_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/t0ul/gobrig"
)

// fakeController stands in for the live, codesigned VM so the control routing and guards
// test without one. It is a test double for the interface, not the system under test (the
// handler logic is).
type fakeController struct {
	ready, busy, available bool
	resetErr               error
	resetCalls             int
}

func (f *fakeController) Ready() bool     { return f.ready }
func (f *fakeController) Busy() bool      { return f.busy }
func (f *fakeController) Available() bool { return f.available }
func (f *fakeController) ResetNow() error { f.resetCalls++; return f.resetErr }

func do(h http.Handler, method, path string) int {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w.Code
}

func TestControlHandler(t *testing.T) {
	ready := gobrig.ControlHandler(&fakeController{ready: true, available: true})
	if got := do(ready, "GET", "/healthz"); got != 200 {
		t.Errorf("healthz ready: want 200, got %d", got)
	}
	notReady := gobrig.ControlHandler(&fakeController{ready: false})
	if got := do(notReady, "GET", "/healthz"); got != 503 {
		t.Errorf("healthz not-ready: want 503, got %d", got)
	}

	if got := do(ready, "GET", "/reset"); got != 405 {
		t.Errorf("reset GET: want 405, got %d", got)
	}
	unavail := gobrig.ControlHandler(&fakeController{available: false})
	if got := do(unavail, "POST", "/reset"); got != 409 {
		t.Errorf("reset unavailable: want 409, got %d", got)
	}
	busy := gobrig.ControlHandler(&fakeController{available: true, busy: true})
	if got := do(busy, "POST", "/reset"); got != 503 {
		t.Errorf("reset busy: want 503, got %d", got)
	}
	okCtl := &fakeController{available: true}
	if got := do(gobrig.ControlHandler(okCtl), "POST", "/reset"); got != 200 || okCtl.resetCalls != 1 {
		t.Errorf("reset ok: want 200 and 1 call, got %d / %d", got, okCtl.resetCalls)
	}
}

// TestSnapshotUsable uses a real temp tree: a snapshot newer than its assets is usable; an
// asset touched after the snapshot makes it stale; a missing or empty snapshot is unusable.
func TestSnapshotUsable(t *testing.T) {
	dir := t.TempDir()
	asset := filepath.Join(dir, "Image")
	snap := filepath.Join(dir, "golden.vmstate")
	if err := os.WriteFile(asset, []byte("kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(snap, []byte("state"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !gobrig.SnapshotUsable(snap, []string{asset}) {
		t.Error("snapshot newer than asset should be usable")
	}

	// Touch the asset after the snapshot: now stale.
	time.Sleep(10 * time.Millisecond)
	now := time.Now()
	if err := os.Chtimes(asset, now, now); err != nil {
		t.Fatal(err)
	}
	if gobrig.SnapshotUsable(snap, []string{asset}) {
		t.Error("snapshot older than an asset must be stale")
	}

	if gobrig.SnapshotUsable(filepath.Join(dir, "nope"), []string{asset}) {
		t.Error("missing snapshot must be unusable")
	}
}
