package gobrig

import "encoding/json"

// ContainerSpec describes a container to run inside the Brig, with the misconfiguration
// knobs that open (or close) the container-escape classes GORED flags: running as root, a
// privileged container (all capabilities, no seccomp, no masked/readonly paths), a mounted
// docker socket, or a writable host-path bind. A hardened spec (every knob off) produces a
// locked-down config; flipping one knob reintroduces exactly one escape class, so a
// benchmark can target them one at a time and a hardened control is the same config with
// the knob off.
type ContainerSpec struct {
	Rootfs           string   // path to the container root filesystem (an OCI bundle's rootfs)
	Args             []string // process argv inside the container; "" uses ["/bin/sh"]
	Env              []string
	Cwd              string // "" uses "/"
	RunAsRoot        bool   // uid/gid 0 inside the container (the common default misconfig)
	Privileged       bool   // all capabilities, no seccomp, no masked/readonly paths
	NoSeccomp        bool   // omit the seccomp filter even when not privileged
	MountDockerSock  bool   // bind /var/run/docker.sock into the container (classic escape)
	WritableHostPath string // bind this host path into the container, writable ("" = none)
	HostNetwork      bool   // share the host's network namespace (no netns) — a misconfig, and
	// what lets a service in the container be reached on the host's loopback
}

// Minimal OCI runtime-spec subset (what runc needs to start a container).
type ociSpec struct {
	OCIVersion string     `json:"ociVersion"`
	Process    ociProcess `json:"process"`
	Root       ociRoot    `json:"root"`
	Hostname   string     `json:"hostname"`
	Mounts     []ociMount `json:"mounts"`
	Linux      ociLinux   `json:"linux"`
}

type ociProcess struct {
	Terminal        bool     `json:"terminal"`
	User            ociUser  `json:"user"`
	Args            []string `json:"args"`
	Env             []string `json:"env"`
	Cwd             string   `json:"cwd"`
	Capabilities    ociCaps  `json:"capabilities"`
	NoNewPrivileges bool     `json:"noNewPrivileges"`
}

type ociUser struct {
	UID int `json:"uid"`
	GID int `json:"gid"`
}

type ociCaps struct {
	Bounding  []string `json:"bounding"`
	Effective []string `json:"effective"`
	Permitted []string `json:"permitted"`
}

type ociRoot struct {
	Path     string `json:"path"`
	Readonly bool   `json:"readonly"`
}

type ociMount struct {
	Destination string   `json:"destination"`
	Type        string   `json:"type,omitempty"`
	Source      string   `json:"source,omitempty"`
	Options     []string `json:"options,omitempty"`
}

type ociNamespace struct {
	Type string `json:"type"`
}

type ociSeccomp struct {
	DefaultAction string   `json:"defaultAction"`
	Architectures []string `json:"architectures,omitempty"`
}

type ociLinux struct {
	Namespaces    []ociNamespace `json:"namespaces"`
	MaskedPaths   []string       `json:"maskedPaths,omitempty"`
	ReadonlyPaths []string       `json:"readonlyPaths,omitempty"`
	Seccomp       *ociSeccomp    `json:"seccomp,omitempty"`
}

// baseCaps is a conservative non-privileged capability set; allCaps is the full set a
// privileged container carries (CAP_SYS_ADMIN among them is the escape-enabling one).
var baseCaps = []string{"CAP_CHOWN", "CAP_DAC_OVERRIDE", "CAP_FSETID", "CAP_FOWNER", "CAP_SETGID", "CAP_SETUID", "CAP_NET_BIND_SERVICE"}

var allCaps = []string{
	"CAP_CHOWN", "CAP_DAC_OVERRIDE", "CAP_DAC_READ_SEARCH", "CAP_FOWNER", "CAP_FSETID",
	"CAP_KILL", "CAP_SETGID", "CAP_SETUID", "CAP_SETPCAP", "CAP_LINUX_IMMUTABLE",
	"CAP_NET_BIND_SERVICE", "CAP_NET_BROADCAST", "CAP_NET_ADMIN", "CAP_NET_RAW",
	"CAP_IPC_LOCK", "CAP_IPC_OWNER", "CAP_SYS_MODULE", "CAP_SYS_RAWIO", "CAP_SYS_CHROOT",
	"CAP_SYS_PTRACE", "CAP_SYS_PACCT", "CAP_SYS_ADMIN", "CAP_SYS_BOOT", "CAP_SYS_NICE",
	"CAP_SYS_RESOURCE", "CAP_SYS_TIME", "CAP_SYS_TTY_CONFIG", "CAP_MKNOD", "CAP_LEASE",
	"CAP_AUDIT_WRITE", "CAP_AUDIT_CONTROL", "CAP_SETFCAP", "CAP_MAC_OVERRIDE", "CAP_MAC_ADMIN",
	"CAP_SYSLOG", "CAP_WAKE_ALARM", "CAP_BLOCK_SUSPEND", "CAP_AUDIT_READ", "CAP_PERFMON", "CAP_BPF",
}

var defaultMaskedPaths = []string{"/proc/kcore", "/proc/keys", "/proc/latency_stats", "/proc/timer_list", "/proc/sched_debug", "/sys/firmware"}
var defaultReadonlyPaths = []string{"/proc/asound", "/proc/bus", "/proc/fs", "/proc/irq", "/proc/sys", "/proc/sysrq-trigger"}

// OCIConfig renders the runc-compatible config.json for the spec. The knobs map to real
// runtime-spec fields: Privileged gives allCaps, drops seccomp and the masked/readonly path
// protections; RunAsRoot sets uid/gid 0; MountDockerSock and WritableHostPath add bind
// mounts. A hardened spec keeps noNewPrivileges, a non-root user, a seccomp filter, and the
// masked/readonly paths.
func OCIConfig(s ContainerSpec) ([]byte, error) {
	args := s.Args
	if len(args) == 0 {
		args = []string{"/bin/sh"}
	}
	cwd := s.Cwd
	if cwd == "" {
		cwd = "/"
	}
	uid, gid := 10001, 10001
	if s.RunAsRoot {
		uid, gid = 0, 0
	}
	caps := baseCaps
	if s.Privileged {
		caps = allCaps
	}

	mounts := []ociMount{
		{Destination: "/proc", Type: "proc", Source: "proc"},
		{Destination: "/dev", Type: "tmpfs", Source: "tmpfs", Options: []string{"nosuid", "strictatime", "mode=755", "size=65536k"}},
		{Destination: "/sys", Type: "sysfs", Source: "sysfs", Options: []string{"nosuid", "noexec", "nodev", "ro"}},
		{Destination: "/dev/shm", Type: "tmpfs", Source: "shm", Options: []string{"nosuid", "noexec", "nodev", "mode=1777", "size=65536k"}},
	}
	if s.MountDockerSock {
		mounts = append(mounts, ociMount{Destination: "/var/run/docker.sock", Type: "bind", Source: "/var/run/docker.sock", Options: []string{"rbind", "rw"}})
	}
	if s.WritableHostPath != "" {
		mounts = append(mounts, ociMount{Destination: "/host", Type: "bind", Source: s.WritableHostPath, Options: []string{"rbind", "rw"}})
	}

	// No cgroup namespace: the Brig's kernel runtime does not need it for the escape
	// classes. HostNetwork drops the network namespace too, so a service in the container
	// binds the host's loopback and is reachable from the guest — itself a misconfig.
	ns := []ociNamespace{{Type: "pid"}, {Type: "ipc"}, {Type: "uts"}, {Type: "mount"}}
	if !s.HostNetwork {
		ns = append(ns, ociNamespace{Type: "network"})
	}
	lx := ociLinux{Namespaces: ns}
	if s.Privileged {
		// A privileged container drops the path protections and the seccomp filter.
		lx.Seccomp = nil
	} else {
		lx.MaskedPaths = defaultMaskedPaths
		lx.ReadonlyPaths = defaultReadonlyPaths
		if !s.NoSeccomp {
			lx.Seccomp = &ociSeccomp{DefaultAction: "SCMP_ACT_ALLOW", Architectures: []string{"SCMP_ARCH_AARCH64"}}
		}
	}

	spec := ociSpec{
		OCIVersion: "1.0.2",
		Process: ociProcess{
			Terminal: false,
			User:     ociUser{UID: uid, GID: gid},
			Args:     args,
			Env:      append([]string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}, s.Env...),
			Cwd:      cwd,
			// A privileged or host-mounting container drops no-new-privileges, the way a
			// real misconfigured deployment does; a hardened one keeps it.
			NoNewPrivileges: !s.Privileged,
			Capabilities:    ociCaps{Bounding: caps, Effective: caps, Permitted: caps},
		},
		Root:     ociRoot{Path: s.Rootfs, Readonly: false},
		Hostname: "brig",
		Mounts:   mounts,
		Linux:    lx,
	}
	return json.MarshalIndent(spec, "", "  ")
}

// RunArgs is the argv that starts a prepared OCI bundle (a dir holding config.json + its
// rootfs) with runc, sent as a Detonation into the Brig.
func RunArgs(runc, bundleDir, id string) []string {
	return []string{runc, "run", "--bundle", bundleDir, id}
}
