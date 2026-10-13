package gobrig_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t0ul/gobrig"
)

// TestWriteConfig writes a privileged container's config.json into a real bundle dir and
// checks it is valid, roots at the bundle-relative rootfs, and carries the escape class.
func TestWriteConfig(t *testing.T) {
	dir := t.TempDir()
	if err := gobrig.WriteConfig(dir, gobrig.ContainerSpec{Privileged: true, RunAsRoot: true}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("config.json invalid: %v", err)
	}
	if m["root"].(map[string]any)["path"] != "rootfs" {
		t.Errorf("root path should be the bundle-relative rootfs, got %v", m["root"])
	}
	if !strings.Contains(string(b), "CAP_SYS_ADMIN") {
		t.Error("privileged config should carry CAP_SYS_ADMIN")
	}
}

// TestCopyThenRun checks the two detonations: copy the read-only bundle into a writable
// work dir, then runc run it there.
func TestCopyThenRun(t *testing.T) {
	ds := gobrig.CopyThenRun("/mnt/assets/runc", "/mnt/assets/bundle", "/tmp", "c1")
	if len(ds) != 2 {
		t.Fatalf("want 2 detonations, got %d", len(ds))
	}
	if got := strings.Join(ds[0].Argv, " "); got != "cp -a /mnt/assets/bundle /tmp/c1" {
		t.Errorf("copy step wrong: %q", got)
	}
	if got := strings.Join(ds[1].Argv, " "); got != "/mnt/assets/runc run --bundle /tmp/c1 c1" {
		t.Errorf("run step wrong: %q", got)
	}
}
