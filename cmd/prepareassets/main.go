// Command prepareassets provisions the Brig's VM assets into -dir (default vm-assets):
// the PUI PUI Linux kernel, the Alpine mini root filesystem, the cross-compiled guest
// detonationd, and sandbox_init.sh. Run it on the host (it downloads the kernel and
// rootfs, so it needs network at provisioning time — the RUNTIME chamber is still
// egress-denied). Then codesign and boot with scripts/brig-up.sh.
//
// Decoupled from emaildrop: no models, no egress tools (the Brig has zero network), no
// emaildrop imports — the download/extract helpers are inlined here.
package main

import (
	"archive/tar"
	"compress/gzip"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// The Brig's kernel is the Kata Containers arm64 guest kernel — the same one Apple's
	// own containerization stack boots under Virtualization.framework. It has VIRTIO
	// compiled in (not modules) plus cgroups and the namespaces runc needs, so containers
	// run inside the Brig. (PUI PUI, used before, is a minimal exec kernel with no cgroups
	// and no namespaces — it cannot host containers.)
	kataURL = "https://github.com/kata-containers/kata-containers/releases/download/3.17.0/kata-static-3.17.0-arm64.tar.xz"
	// PUI PUI still supplies the minimal busybox initramfs (pure userland) that drives our
	// virtiofs + chroot bring-up; only its kernel is replaced by the Kata one above.
	initramfsSrcURL = "https://github.com/Code-Hex/puipui-linux/releases/download/v1.0.3/puipui_linux_v1.0.3_aarch64.tar.gz"
	rootfsURL       = "https://dl-cdn.alpinelinux.org/alpine/v3.20/releases/aarch64/alpine-minirootfs-3.20.3-aarch64.tar.gz"
	// Static OCI runtime that runs containers inside the Brig (no daemon, no network).
	runcURL = "https://github.com/opencontainers/runc/releases/download/v1.2.4/runc.arm64"
)

//go:embed sandbox_init.sh
var sandboxInit []byte

func main() {
	dir := flag.String("dir", "vm-assets", "Brig assets directory to provision")
	flag.Parse()
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		log.Fatal(err)
	}
	for _, step := range []struct {
		name string
		fn   func(string) error
	}{
		{"kernel", prepareKernel},
		{"initramfs", prepareInitramfs},
		{"rootfs", prepareRootFS},
		{"runc", prepareRunc},
		{"container bundle", prepareBundle},
		{"guest binaries", buildGuestBinaries},
		{"sandbox_init.sh", installInit},
	} {
		if err := step.fn(*dir); err != nil {
			log.Fatalf("prepareassets: %s: %v", step.name, err)
		}
	}
	fmt.Printf("prepareassets: done. Boot with: scripts/brig-up.sh (needs codesign)\n")
}

func prepareKernel(dir string) error {
	img := filepath.Join(dir, "Image")
	if _, err := os.Stat(img); err == nil {
		fmt.Println("kernel: already present")
		return nil
	}
	fmt.Println("kernel: downloading Kata container kernel (arm64)...")
	tarPath := filepath.Join(dir, "kata.tar.xz")
	if err := downloadFile(kataURL, tarPath); err != nil {
		return err
	}
	defer os.Remove(tarPath)
	ex := filepath.Join(dir, "kata-extract")
	if err := os.MkdirAll(ex, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(ex)
	// System tar handles the .xz; the kernel lands at opt/kata/share/kata-containers/.
	if out, err := exec.Command("tar", "-xf", tarPath, "-C", ex).CombinedOutput(); err != nil {
		return fmt.Errorf("untar kata: %v: %s", err, out)
	}
	src := filepath.Join(ex, "opt", "kata", "share", "kata-containers", "vmlinux.container")
	if _, err := os.Stat(src); err != nil {
		if found := findFile(ex, "vmlinux.container"); found != "" {
			src = found
		} else {
			return fmt.Errorf("vmlinux.container not found in the kata tarball")
		}
	}
	// cp -L to dereference the versioned symlink kata ships.
	if out, err := exec.Command("cp", "-L", src, img).CombinedOutput(); err != nil {
		return fmt.Errorf("cp kernel: %v: %s", err, out)
	}
	return nil
}

// prepareInitramfs fetches the minimal busybox initramfs (from PUI PUI) that drives the
// bring-up. Only the initrd is kept; its kernel is discarded in favour of the Kata kernel.
func prepareInitramfs(dir string) error {
	initrd := filepath.Join(dir, "initramfs.cpio.gz")
	if _, err := os.Stat(initrd); err == nil {
		fmt.Println("initramfs: already present")
		return nil
	}
	fmt.Println("initramfs: downloading minimal busybox initramfs...")
	tarPath := filepath.Join(dir, "puipui.tar.gz")
	if err := downloadFile(initramfsSrcURL, tarPath); err != nil {
		return err
	}
	defer os.Remove(tarPath)
	if err := extractTarGz(tarPath, dir); err != nil {
		return err
	}
	os.Remove(filepath.Join(dir, "Image.gz")) // discard PUI PUI's kernel; we use the Kata one
	if _, err := os.Stat(initrd); err != nil {
		return fmt.Errorf("initramfs.cpio.gz not found in the initramfs source tarball")
	}
	return nil
}

// findFile returns the first path at or under root whose base name matches name, or "".
func findFile(root, name string) string {
	var hit string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == name && hit == "" {
			hit = p
		}
		return nil
	})
	return hit
}

func prepareRootFS(dir string) error {
	alpineDir := filepath.Join(dir, "alpine-root")
	if _, err := os.Stat(alpineDir); err == nil {
		fmt.Println("rootfs: already present")
		return nil
	}
	fmt.Println("rootfs: downloading Alpine mini root filesystem...")
	if err := os.MkdirAll(alpineDir, 0o755); err != nil {
		return err
	}
	tarPath := filepath.Join(dir, "alpine-rootfs.tar.gz")
	if err := downloadFile(rootfsURL, tarPath); err != nil {
		return err
	}
	defer os.Remove(tarPath)
	// Alpine's rootfs tar carries device nodes and ownership the Go tar reader cannot
	// recreate without root, so shell out to the system tar here.
	out, err := exec.Command("tar", "-xzf", tarPath, "-C", alpineDir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar: %v: %s", err, out)
	}
	return nil
}

// buildGuestBinaries cross-compiles the in-guest detonationd into the shared assets dir.
// The guest is Alpine on Apple Silicon (linux/arm64); detonationd is pure Go (CGO off),
// so it is a static binary with no libc dependency. Always rebuild: a stale baked binary
// silently serves an old daemon.
func buildGuestBinaries(dir string) error {
	dest := filepath.Join(dir, "detonationd")
	fmt.Println("guest: cross-compiling detonationd (linux/arm64, static)...")
	cmd := exec.Command("go", "build", "-o", dest, "./cmd/detonationd")
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %v: %s", err, out)
	}
	return nil
}

func installInit(dir string) error {
	return os.WriteFile(filepath.Join(dir, "sandbox_init.sh"), sandboxInit, 0o755)
}

// prepareRunc downloads the static OCI runtime that runs containers inside the Brig. It is
// a single static arm64 binary (no daemon), served into the guest over the shared mount.
func prepareRunc(dir string) error {
	dest := filepath.Join(dir, "runc")
	if _, err := os.Stat(dest); err == nil {
		fmt.Println("runc: already present")
		return nil
	}
	fmt.Println("runc: downloading static OCI runtime...")
	if err := downloadFile(runcURL, dest); err != nil {
		return err
	}
	return os.Chmod(dest, 0o755)
}

// prepareBundle lays down the container's base OCI bundle rootfs by copying the Alpine
// rootfs. config.json is NOT baked: the host writes it per experiment with gobrig.WriteConfig
// (so the misconfig knobs are chosen at run time). The guest runc run reads this bundle.
func prepareBundle(dir string) error {
	bundleRoot := filepath.Join(dir, "bundle", "rootfs")
	if _, err := os.Stat(bundleRoot); err == nil {
		fmt.Println("bundle: already present")
		return nil
	}
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		return err
	}
	fmt.Println("bundle: copying Alpine rootfs into the container bundle...")
	// System cp -a preserves perms and device nodes the Go copy cannot recreate unprivileged.
	src := filepath.Join(dir, "alpine-root") + string(os.PathSeparator) + "."
	out, err := exec.Command("cp", "-a", src, bundleRoot).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cp rootfs: %v: %s", err, out)
	}
	return nil
}

// --- inlined download / extract helpers (no verification; VM provisioning assets) ---

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %s", url, resp.Status)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

// extractTarGz extracts a .tar.gz into destDir, rejecting entries whose path escapes it
// (tar-slip defense).
func extractTarGz(srcPath, destDir string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	cleanDest := filepath.Clean(destDir) + string(os.PathSeparator)
	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, header.Name)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDest) &&
			filepath.Clean(target) != filepath.Clean(destDir) {
			return fmt.Errorf("tar entry escapes destination: %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
}

func gunzipFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	gzr, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer gzr.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, gzr)
	return err
}
