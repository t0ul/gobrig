package gobrig

import (
	"os"
	"path/filepath"
)

// WriteConfig renders the OCI config.json for spec into bundleDir, which must also hold a
// rootfs/ directory. The container root is the bundle-relative "rootfs". The host writes
// this before a run (the assets mount is read-only to the guest, writable from the host),
// so the misconfig knobs are chosen per experiment without rebuilding anything in the guest.
func WriteConfig(bundleDir string, spec ContainerSpec) error {
	spec.Rootfs = "rootfs"
	b, err := OCIConfig(spec)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bundleDir, "config.json"), b, 0o644)
}

// CopyThenRun returns the ordered detonations that run a container from a read-only bundle
// on the shared assets mount. The bundle is first copied into a writable work dir in the
// guest (the assets virtiofs is read-only, while runc needs to write container state and a
// writable rootfs), then started with runc. id names both the work copy and the container.
func CopyThenRun(runc, roBundle, workDir, id string) []Detonation {
	dst := filepath.Join(workDir, id)
	return []Detonation{
		{Argv: []string{"cp", "-a", roBundle, dst}, TraceID: id + "-cp"},
		{Argv: RunArgs(runc, dst, id), TraceID: id + "-run"},
	}
}
