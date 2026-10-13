//go:build darwin

package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Code-Hex/vz/v3"
	"github.com/t0ul/gobrig"
)

const vsockPort = 5000 // guest vsock port where detonationd listens

var errNoSnapshot = errors.New("golden snapshot unavailable")

// chamber wraps the Brig VM with Apple Virtualization save/restore so the guest can be
// reset to a pristine, already-booted state. It implements gobrig.Controller. reset and
// the /reset control are mutually exclusive with in-flight bridge connections via gate: a
// detonation holds it shared (RLock); a reset holds it exclusive (Lock).
type chamber struct {
	vm       *vz.VirtualMachine
	snapshot string
	bridge   string
	savable  bool
	reset    bool

	gate      sync.RWMutex
	dirty     atomic.Bool
	resetting atomic.Bool
}

// Available/Busy/Ready implement gobrig.Controller.
func (c *chamber) Available() bool { return c.savable && c.snapshot != "" }
func (c *chamber) Busy() bool      { return c.resetting.Load() }

func (c *chamber) Ready() bool {
	if c.resetting.Load() {
		return false
	}
	return gobrig.ProbeReady(c.bridge, 1500*time.Millisecond)
}

func (c *chamber) saveGolden() error {
	if !c.Available() {
		return errNoSnapshot
	}
	c.gate.Lock()
	defer c.gate.Unlock()
	c.resetting.Store(true)
	defer c.resetting.Store(false)

	if err := c.vm.Pause(); err != nil {
		return fmt.Errorf("pause: %w", err)
	}
	if err := waitState(c.vm, vz.VirtualMachineStatePaused, 15*time.Second); err != nil {
		return err
	}
	tmp := c.snapshot + ".tmp"
	saveErr := c.vm.SaveMachineStateToPath(tmp)
	if err := c.vm.Resume(); err != nil {
		return fmt.Errorf("resume after save: %w", err)
	}
	_ = waitState(c.vm, vz.VirtualMachineStateRunning, 15*time.Second)
	if saveErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("save: %w", saveErr)
	}
	return os.Rename(tmp, c.snapshot)
}

func (c *chamber) restoreGolden() error {
	if !c.Available() {
		return errNoSnapshot
	}
	if err := c.vm.Stop(); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	if err := waitState(c.vm, vz.VirtualMachineStateStopped, 15*time.Second); err != nil {
		return err
	}
	if err := c.vm.RestoreMachineStateFromURL(c.snapshot); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	if err := waitState(c.vm, vz.VirtualMachineStatePaused, 15*time.Second); err != nil {
		return err
	}
	if err := c.vm.Resume(); err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	if err := waitState(c.vm, vz.VirtualMachineStateRunning, 15*time.Second); err != nil {
		return err
	}
	if !gobrig.ProbeReady(c.bridge, 15*time.Second) {
		return errors.New("daemon not ready after restore")
	}
	return nil
}

// ResetNow restores the golden snapshot unconditionally (the /reset control).
func (c *chamber) ResetNow() error {
	if !c.Available() {
		return errNoSnapshot
	}
	c.gate.Lock()
	defer c.gate.Unlock()
	c.resetting.Store(true)
	defer c.resetting.Store(false)
	if err := c.restoreGolden(); err != nil {
		return err
	}
	c.dirty.Store(false)
	return nil
}

func (c *chamber) resetIfDirty() {
	if !c.reset || !c.Available() || !c.dirty.Load() {
		return
	}
	c.gate.Lock()
	defer c.gate.Unlock()
	if !c.dirty.Load() {
		return
	}
	c.resetting.Store(true)
	defer c.resetting.Store(false)
	if err := c.restoreGolden(); err != nil {
		os.Stdout.Write([]byte("\rreset-each: restore failed: " + err.Error() + "\r\n"))
		return
	}
	c.dirty.Store(false)
}

// restoreBoot restores the golden into a freshly instantiated VM and resumes it.
func (c *chamber) restoreBoot() error {
	if err := c.vm.RestoreMachineStateFromURL(c.snapshot); err != nil {
		return err
	}
	if err := waitState(c.vm, vz.VirtualMachineStatePaused, 15*time.Second); err != nil {
		return err
	}
	return c.vm.Resume()
}

// handleConn bridges one host TCP connection to the guest's detonation daemon over vsock.
func (c *chamber) handleConn(tcpConn net.Conn) {
	defer tcpConn.Close()
	c.resetIfDirty()

	c.gate.RLock()
	defer c.gate.RUnlock()

	devices := c.vm.SocketDevices()
	if len(devices) == 0 {
		return
	}
	vsockConn, err := devices[0].Connect(vsockPort)
	if err != nil {
		return
	}
	defer vsockConn.Close()
	c.dirty.Store(true)

	done := make(chan struct{}, 2)
	go func() { io.Copy(vsockConn, tcpConn); done <- struct{}{} }()
	go func() { io.Copy(tcpConn, vsockConn); done <- struct{}{} }()
	<-done
}

// serveControl runs the loopback control server (GET /healthz, POST /reset) using the
// shared, VM-free ControlHandler over this chamber.
func (c *chamber) serveControl(addr string) {
	if addr == "" {
		return
	}
	srv := &http.Server{Addr: addr, Handler: gobrig.ControlHandler(c)}
	os.Stdout.Write([]byte("\r\ncontrol up: " + addr + " (GET /healthz, POST /reset)\r\n"))
	if err := srv.ListenAndServe(); err != nil {
		os.Stdout.Write([]byte("\rcontrol server: " + err.Error() + "\r\n"))
	}
}

// waitState blocks until the VM reaches target, enters an error state, or times out.
func waitState(vm *vz.VirtualMachine, target vz.VirtualMachineState, timeout time.Duration) error {
	if vm.State() == target {
		return nil
	}
	notify := vm.StateChangedNotify()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case s := <-notify:
			if s == target {
				return nil
			}
			if s == vz.VirtualMachineStateError {
				return errors.New("vm entered error state")
			}
		case <-timer.C:
			return fmt.Errorf("timeout waiting for vm state %v (now %v)", target, vm.State())
		}
	}
}
