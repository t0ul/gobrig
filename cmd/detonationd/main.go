// Command detonationd is the in-Brig exec daemon: it serves gobrig.Daemon so the host-side
// Client can run detonations inside the contained VM. Inside the Brig it listens on vsock
// (the guest has no NIC); set DAEMON_TCP=host:port to serve over TCP for host-side dev and
// tests. Either way it serves the same HTTP wire the Client speaks.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/t0ul/gobrig"
)

func main() {
	d := &gobrig.Daemon{
		Log: func(traceID, argv string) {
			b, _ := json.Marshal(map[string]string{"trace_id": traceID, "span": "brig.exec", "argv": argv})
			fmt.Println(string(b))
		},
	}
	ln, where, err := listen()
	if err != nil {
		log.Fatalf("detonationd: %v", err)
	}
	log.Printf("gobrig detonationd listening on %s", where)
	log.Fatal(http.Serve(ln, d.Handler()))
}

func listen() (net.Listener, string, error) {
	if addr := os.Getenv("DAEMON_TCP"); addr != "" {
		ln, err := net.Listen("tcp", addr)
		return ln, "tcp " + addr, err
	}
	ln, err := gobrig.Listen(gobrig.DefaultVsockPort)
	return ln, fmt.Sprintf("vsock port %d", gobrig.DefaultVsockPort), err
}
