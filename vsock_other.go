//go:build !linux

package gobrig

import (
	"errors"
	"net"
)

// DefaultVsockPort mirrors the linux constant so callers compile on any platform.
const DefaultVsockPort = 5000

// Listen is unavailable off Linux: vsock is the guest-side channel, and the guest is Linux.
// On the host, serve the Daemon over TCP (DAEMON_TCP) instead.
func Listen(port uint32) (net.Listener, error) {
	return nil, errors.New("gobrig: vsock Listen is only available on linux (the guest); use TCP on the host")
}
