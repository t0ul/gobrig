// Command brigconfig writes an OCI config.json into a bundle directory from ContainerSpec
// knob flags, so the escape benchmark can choose the misconfiguration (privileged, host
// network, writable host bind, ...) from the command line without a one-off program.
//
//	go run ./cmd/brigconfig -privileged -root -hostnet -hostpath / -args /vulnapp vm-assets/bundle
package main

import (
	"flag"
	"log"
	"strings"

	"github.com/t0ul/gobrig"
)

func main() {
	priv := flag.Bool("privileged", false, "all capabilities, no seccomp, no masked paths")
	root := flag.Bool("root", false, "run as uid/gid 0 inside the container")
	hostnet := flag.Bool("hostnet", false, "share the host network namespace (service reachable on guest loopback)")
	noseccomp := flag.Bool("noseccomp", false, "omit the seccomp filter even when not privileged")
	dsock := flag.Bool("dockersock", false, "bind /var/run/docker.sock into the container")
	hostpath := flag.String("hostpath", "", "bind this host path into the container at /host, writable")
	args := flag.String("args", "/bin/sh", "space-separated container argv")
	flag.Parse()
	if flag.NArg() < 1 {
		log.Fatal("usage: brigconfig [flags] <bundle-dir>")
	}
	spec := gobrig.ContainerSpec{
		Privileged: *priv, RunAsRoot: *root, HostNetwork: *hostnet, NoSeccomp: *noseccomp,
		MountDockerSock: *dsock, WritableHostPath: *hostpath, Args: strings.Fields(*args),
	}
	if err := gobrig.WriteConfig(flag.Arg(0), spec); err != nil {
		log.Fatal(err)
	}
}
