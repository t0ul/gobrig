package gobrig

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// DefaultMarker is the environment variable the Brig sets inside the guest so code can tell
// it is locked in the contained VM.
const DefaultMarker = "GOBRIG"

// Warden is the fail-closed containment gate — it guards the Brig. Any escape / RCE
// experiment MUST pass Require() before it runs: Require returns an error unless the
// process is locked in the Brig — a VM that carries the Brig marker AND has no external
// network interface (the Brig is built with no guest NIC; egress, if any, is brokered by
// the host). A normal host has a NIC, so Require refuses there, and offensive probes can
// never run outside the disposable, network-isolated VM. Both signals are required, so
// neither a stray env var on the host nor a NIC-less-but-unmarked environment is mistaken
// for the Brig.
//
// The probes are injectable so the Warden's logic is testable without a VM; the defaults
// read the real environment and interfaces.
type Warden struct {
	Marker string                   // env var that marks the Brig; "" uses DefaultMarker
	Getenv func(string) string      // "" uses os.Getenv
	NICs   func() ([]string, error) // external (non-loopback) interface names; nil uses the real list
}

func (w Warden) marker() string {
	if w.Marker != "" {
		return w.Marker
	}
	return DefaultMarker
}

func (w Warden) getenv(k string) string {
	if w.Getenv != nil {
		return w.Getenv(k)
	}
	return os.Getenv(k)
}

func (w Warden) nics() ([]string, error) {
	if w.NICs != nil {
		return w.NICs()
	}
	return externalNICs()
}

// InBrig reports whether the process is locked in the Brig, and a human reason when not.
func (w Warden) InBrig() (bool, string) {
	if w.getenv(w.marker()) == "" {
		return false, "Brig marker " + w.marker() + " is not set"
	}
	nics, err := w.nics()
	if err != nil {
		return false, "cannot read network interfaces: " + err.Error()
	}
	if len(nics) > 0 {
		return false, "external network interface(s) present (" + strings.Join(nics, ",") + "); the Brig has none"
	}
	return true, ""
}

// Require is the fail-closed guard: nil only inside the Brig, else an error saying why.
// Call it before anything offensive; on any failure, do not proceed.
func (w Warden) Require() error {
	ok, why := w.InBrig()
	if ok {
		return nil
	}
	return fmt.Errorf("gobrig: refusing to run outside the Brig: %s", why)
}

// externalNICs returns the names of up, non-loopback interfaces that carry a non-loopback
// address. The Brig guest has none; a host has at least one.
func externalNICs() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err == nil && !ip.IsLoopback() {
				out = append(out, ifc.Name)
				break
			}
		}
	}
	return out, nil
}
