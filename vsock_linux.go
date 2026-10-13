//go:build linux

package gobrig

import (
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// DefaultVsockPort is the guest vsock port the in-Brig daemon listens on. It is the one and
// only channel into the Brig: the guest has no NIC, so nothing reaches the daemon except the
// host bridging to this vsock port.
const DefaultVsockPort = 5000

// Listen opens an AF_VSOCK stream listener on port, accepting from any CID. Go's net package
// has no vsock support, so the listener is built on the raw syscalls in golang.org/x/sys/unix.
// Serve the returned listener with http.Serve(ln, (&Daemon{}).Handler()).
func Listen(port uint32) (net.Listener, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("gobrig: vsock socket: %w", err)
	}
	if err := unix.Bind(fd, &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: port}); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("gobrig: vsock bind: %w", err)
	}
	if err := unix.Listen(fd, 8); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("gobrig: vsock listen: %w", err)
	}
	return &vsockListener{fd: fd, port: port}, nil
}

type vsockAddr struct {
	cid  uint32
	port uint32
}

func (vsockAddr) Network() string  { return "vsock" }
func (a vsockAddr) String() string { return fmt.Sprintf("vsock:%d.%d", a.cid, a.port) }

type vsockListener struct {
	fd   int
	port uint32
}

func (l *vsockListener) Accept() (net.Conn, error) {
	nfd, _, err := unix.Accept(l.fd)
	if err != nil {
		return nil, err
	}
	return &vsockConn{fd: nfd}, nil
}

func (l *vsockListener) Close() error   { return unix.Close(l.fd) }
func (l *vsockListener) Addr() net.Addr { return vsockAddr{cid: unix.VMADDR_CID_ANY, port: l.port} }

// vsockConn adapts a raw vsock fd to net.Conn. http.Serve sets deadlines; vsock fds here
// treat them as no-ops, which is acceptable for the short-lived detonation exchanges.
type vsockConn struct{ fd int }

func (c *vsockConn) Read(p []byte) (int, error) {
	n, err := unix.Read(c.fd, p)
	if n == 0 && err == nil {
		return 0, io.EOF
	}
	return n, err
}

func (c *vsockConn) Write(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		n, err := unix.Write(c.fd, p[total:])
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
		total += n
	}
	return total, nil
}

func (c *vsockConn) Close() error                     { return unix.Close(c.fd) }
func (c *vsockConn) LocalAddr() net.Addr              { return vsockAddr{} }
func (c *vsockConn) RemoteAddr() net.Addr             { return vsockAddr{} }
func (c *vsockConn) SetDeadline(time.Time) error      { return nil }
func (c *vsockConn) SetReadDeadline(time.Time) error  { return nil }
func (c *vsockConn) SetWriteDeadline(time.Time) error { return nil }
