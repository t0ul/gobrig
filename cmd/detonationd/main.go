// Command detonationd is the in-Brig exec daemon: it serves gobrig.Daemon so the host-side
// Client can run detonations inside the contained VM. CH-2a serves over TCP (host-side dev
// and tests, and inside the VM when the launcher forwards a TCP bridge). CH-2b adds the
// vsock listener for the fully NIC-less guest, alongside the vz boot.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/t0ul/gobrig"
)

func main() {
	addr := os.Getenv("BRIG_DAEMON_ADDR")
	if addr == "" {
		addr = "127.0.0.1:5000"
	}
	d := &gobrig.Daemon{
		Log: func(traceID, argv string) {
			b, _ := json.Marshal(map[string]string{"trace_id": traceID, "span": "brig.exec", "argv": argv})
			fmt.Println(string(b))
		},
	}
	log.Printf("gobrig detonationd listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, d.Handler()))
}
