// Command c2listener is a LOOPBACK-ONLY command-and-control listener for the Brig's
// contained reverse-shell benchmark (MITRE ATT&CK TA0011, Command and Control). It is
// benign instrumentation, not an exploit: it accepts one reverse-shell connection on the
// guest loopback, drives a single command over the channel to prove the foothold is
// interactive (by default it reads a planted in-container canary), captures whatever comes
// back, writes it to a file, and exits.
//
// Containment is built in: it refuses to bind anything but a loopback address, so a foothold
// established against it can never reach off the box. The Brig itself has no NIC, so this is
// belt-and-suspenders — but it makes the intent explicit in code: the C2 channel is loopback
// only, and an external beacon is impossible by construction.
//
// It carries no knowledge of the canary's value: it drives a command and records the bytes
// the channel returns. Whether those bytes contain the expected canary is decided by the
// caller's oracle on the real captured output, not here.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

const doneMarker = "__C2_DONE__"

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func main() {
	addr := flag.String("addr", "127.0.0.1:4555", "loopback listen address (host:port); a non-loopback host is refused")
	cmd := flag.String("cmd", "id; cat /flag-in-container", "command driven over the channel to prove the interactive foothold")
	outPath := flag.String("out", "/tmp/c2.out", "file to write the captured channel output to")
	wait := flag.Duration("wait", 10*time.Second, "how long to wait for the reverse shell to connect and answer")
	flag.Parse()

	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "c2listener: bad -addr:", err)
		os.Exit(2)
	}
	if !isLoopback(host) {
		// Containment, by construction: never listen on a routable address.
		fmt.Fprintf(os.Stderr, "c2listener: refusing to bind non-loopback host %q (containment)\n", host)
		os.Exit(2)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "c2listener: listen:", err)
		os.Exit(1)
	}
	defer ln.Close()
	fmt.Printf("c2listener: waiting for a reverse shell on %s (loopback only)\n", *addr)

	deadline := time.Now().Add(*wait)
	if l, ok := ln.(*net.TCPListener); ok {
		_ = l.SetDeadline(deadline)
	}
	conn, err := ln.Accept()
	if err != nil {
		fmt.Fprintln(os.Stderr, "c2listener: no reverse shell connected:", err)
		_ = os.WriteFile(*outPath, []byte(""), 0o644)
		os.Exit(1)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	// Drive the command over the channel; the trailing echo lets us know when to stop reading.
	if _, err := fmt.Fprintf(conn, "%s; echo %s\n", *cmd, doneMarker); err != nil {
		fmt.Fprintln(os.Stderr, "c2listener: write:", err)
	}

	var sb strings.Builder
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, doneMarker) {
			break
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	captured := sb.String()
	_ = os.WriteFile(*outPath, []byte(captured), 0o644)
	// Print the capture with a marker so a caller reading stdout (or the file) can find it.
	fmt.Printf("C2-CHANNEL-CAPTURE<<\n%s>>C2-CHANNEL-CAPTURE\n", captured)
}
