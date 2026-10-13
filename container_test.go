package gobrig_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/t0ul/gobrig"
)

// decode renders a spec and unmarshals it to a generic tree for field assertions.
func decode(t *testing.T, s gobrig.ContainerSpec) map[string]any {
	t.Helper()
	b, err := gobrig.OCIConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("generated config.json is not valid JSON: %v", err)
	}
	return m
}

func caps(m map[string]any) string {
	b, _ := json.Marshal(m["process"].(map[string]any)["capabilities"])
	return string(b)
}

// TestHardenedContainer: every knob off yields a locked-down config — non-root user, no
// CAP_SYS_ADMIN, a seccomp filter, masked paths, and no-new-privileges. This is the
// control the escape must NOT break.
func TestHardenedContainer(t *testing.T) {
	m := decode(t, gobrig.ContainerSpec{Rootfs: "rootfs", Args: []string{"/bin/sh"}})
	proc := m["process"].(map[string]any)
	if uid := proc["user"].(map[string]any)["uid"].(float64); uid != 10001 {
		t.Errorf("hardened should run non-root, got uid %v", uid)
	}
	if proc["noNewPrivileges"] != true {
		t.Error("hardened should keep noNewPrivileges")
	}
	if strings.Contains(caps(m), "CAP_SYS_ADMIN") {
		t.Error("hardened must not carry CAP_SYS_ADMIN")
	}
	lx := m["linux"].(map[string]any)
	if _, ok := lx["seccomp"]; !ok {
		t.Error("hardened should attach a seccomp filter")
	}
	if _, ok := lx["maskedPaths"]; !ok {
		t.Error("hardened should keep masked paths")
	}
}

// TestPrivilegedContainer: Privileged reintroduces the whole escape class — CAP_SYS_ADMIN,
// no seccomp, no masked paths, no-new-privileges dropped.
func TestPrivilegedContainer(t *testing.T) {
	m := decode(t, gobrig.ContainerSpec{Rootfs: "rootfs", Privileged: true, RunAsRoot: true})
	if !strings.Contains(caps(m), "CAP_SYS_ADMIN") {
		t.Error("privileged must carry CAP_SYS_ADMIN")
	}
	if m["process"].(map[string]any)["noNewPrivileges"] != false {
		t.Error("privileged should drop noNewPrivileges")
	}
	lx := m["linux"].(map[string]any)
	if _, ok := lx["seccomp"]; ok {
		t.Error("privileged should have no seccomp filter")
	}
	if _, ok := lx["maskedPaths"]; ok {
		t.Error("privileged should drop masked paths")
	}
	if uid := m["process"].(map[string]any)["user"].(map[string]any)["uid"].(float64); uid != 0 {
		t.Errorf("RunAsRoot should set uid 0, got %v", uid)
	}
}

// TestEscapeMountKnobs: the docker-socket and writable-host-path binds appear only when
// their knobs are set — each is a distinct escape vector the benchmark can target alone.
func TestEscapeMountKnobs(t *testing.T) {
	plain := decode(t, gobrig.ContainerSpec{Rootfs: "rootfs"})
	if mountsContain(plain, "/var/run/docker.sock") {
		t.Error("docker.sock must not be mounted unless asked")
	}

	sock := decode(t, gobrig.ContainerSpec{Rootfs: "rootfs", MountDockerSock: true})
	if !mountsContain(sock, "/var/run/docker.sock") {
		t.Error("MountDockerSock should bind the docker socket")
	}

	host := decode(t, gobrig.ContainerSpec{Rootfs: "rootfs", WritableHostPath: "/"})
	if !mountsContain(host, "/host") {
		t.Error("WritableHostPath should bind a /host mount")
	}

	// NoSeccomp drops the filter without needing full privilege.
	ns := decode(t, gobrig.ContainerSpec{Rootfs: "rootfs", NoSeccomp: true})
	if _, ok := ns["linux"].(map[string]any)["seccomp"]; ok {
		t.Error("NoSeccomp should omit the seccomp filter")
	}
}

func mountsContain(m map[string]any, dest string) bool {
	for _, mt := range m["mounts"].([]any) {
		if mt.(map[string]any)["destination"] == dest {
			return true
		}
	}
	return false
}

// TestRunArgs is the runc invocation a detonation sends.
func TestRunArgs(t *testing.T) {
	got := gobrig.RunArgs("runc", "/bundle", "c1")
	want := []string{"runc", "run", "--bundle", "/bundle", "c1"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("want %v, got %v", want, got)
	}
}
