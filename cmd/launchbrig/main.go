//go:build darwin

// Command launchbrig boots the Brig: a Linux microVM (Apple Virtualization Framework) that
// serves as the egress-denied detonation chamber, and bridges a host loopback port to the
// guest's vsock so the host can reach the in-Brig detonationd with no IP route into the host.
//
// The Apple Virtualization Framework requires the running binary to be codesigned with the
// com.apple.security.virtualization entitlement, so `go run` fails and the owner must sign.
// Use scripts/brig-up.sh, which rebuilds the guest binary, codesigns, and boots.
//
// No network device is attached, on purpose: the Brig is egress-denied by construction. A
// detonation inside the guest has no interface, no route, no DNS — it cannot beacon or
// exfiltrate. vsock is the one and only channel in. (Unlike the chamber this was ported
// from, there is no host egress broker at all: the Brig has zero network.)
package main

import (
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Code-Hex/vz/v3"
	"github.com/t0ul/gobrig"
	"golang.org/x/term"
)

func main() {
	dir := flag.String("dir", "vm-assets", "Brig assets directory (kernel Image, initramfs.cpio.gz, alpine-root, sandbox_init.sh)")
	bridge := flag.String("bridge", "127.0.0.1:5050", "host address for the guest-vsock command bridge (NOT :5000 — macOS AirPlay owns it)")
	snapshot := flag.Bool("snapshot", true, "save a golden snapshot after bring-up and fast-boot by restoring it (macOS 14+)")
	resetEach := flag.Bool("reset-each", false, "restore the golden snapshot before each detonation for a pristine guest")
	control := flag.String("control", "127.0.0.1:5002", "loopback control endpoint for GET /healthz and POST /reset; empty to disable")
	flag.Parse()

	kernelPath := filepath.Join(*dir, "Image")
	initrdPath := filepath.Join(*dir, "initramfs.cpio.gz")

	bootLoader, err := vz.NewLinuxBootLoader(kernelPath, vz.WithCommandLine("console=hvc0"), vz.WithInitrd(initrdPath))
	if err != nil {
		log.Fatalf("launchbrig: bootloader: %v", err)
	}
	config, err := vz.NewVirtualMachineConfiguration(bootLoader, 4, 2*1024*1024*1024)
	if err != nil {
		log.Fatalf("launchbrig: vm config: %v", err)
	}

	vmReader, vmWriter, _ := os.Pipe()
	hostReader, hostWriter, _ := os.Pipe()
	serialAttachment, err := vz.NewFileHandleSerialPortAttachment(vmReader, hostWriter)
	if err != nil {
		log.Fatalf("launchbrig: serial attachment: %v", err)
	}
	consoleConfig, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialAttachment)
	if err != nil {
		log.Fatalf("launchbrig: console config: %v", err)
	}
	config.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{consoleConfig})

	sharedDir, err := vz.NewSharedDirectory(*dir, true)
	if err != nil {
		log.Fatalf("launchbrig: shared dir: %v", err)
	}
	dirShare, err := vz.NewSingleDirectoryShare(sharedDir)
	if err != nil {
		log.Fatalf("launchbrig: dir share: %v", err)
	}
	fsConfig, err := vz.NewVirtioFileSystemDeviceConfiguration("assets")
	if err != nil {
		log.Fatalf("launchbrig: fs config: %v", err)
	}
	fsConfig.SetDirectoryShare(dirShare)
	config.SetDirectorySharingDevicesVirtualMachineConfiguration([]vz.DirectorySharingDeviceConfiguration{fsConfig})

	// virtio-vsock: the ONLY channel into the Brig. No network device is attached.
	vsockConfig, err := vz.NewVirtioSocketDeviceConfiguration()
	if err != nil {
		log.Fatalf("launchbrig: vsock config: %v", err)
	}
	config.SetSocketDevicesVirtualMachineConfiguration([]vz.SocketDeviceConfiguration{vsockConfig})

	if valid, err := config.Validate(); !valid || err != nil {
		log.Fatalf("launchbrig: invalid vm config: %v", err)
	}

	snapshotPath := ""
	savable := false
	if *snapshot {
		snapshotPath = filepath.Join(*dir, "golden.vmstate")
		if ok, verr := config.ValidateSaveRestoreSupport(); ok && verr == nil {
			savable = true
		} else {
			log.Printf("launchbrig: snapshot/restore unsupported (%v); continuing without snapshots", verr)
			snapshotPath = ""
		}
	}

	vm, err := vz.NewVirtualMachine(config)
	if err != nil {
		log.Fatalf("launchbrig: instantiate: %v", err)
	}
	ch := &chamber{vm: vm, snapshot: snapshotPath, bridge: *bridge, savable: savable, reset: *resetEach}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Fatalf("launchbrig: raw terminal: %v", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	assets := []string{kernelPath, initrdPath, filepath.Join(*dir, "alpine-root"), filepath.Join(*dir, "sandbox_init.sh")}
	restored := false
	if savable && gobrig.SnapshotUsable(snapshotPath, assets) {
		os.Stdout.Write([]byte("\r\nRestoring the Brig from golden snapshot...\r\n"))
		if rerr := ch.restoreBoot(); rerr != nil {
			os.Stdout.Write([]byte("\rrestore failed (" + rerr.Error() + "); cold booting\r\n"))
		} else {
			restored = true
		}
	}

	if restored {
		go io.Copy(os.Stdout, hostReader)
		go io.Copy(vmWriter, os.Stdin)
	} else {
		os.Stdout.Write([]byte("\r\nBooting the Brig...\r\n"))
		if err := vm.Start(); err != nil {
			term.Restore(int(os.Stdin.Fd()), oldState)
			log.Fatalf("\r\nlaunchbrig: start: %v\r\n", err)
		}
		go driveConsole(hostReader, vmWriter)
	}

	go startVsockBridge(ch, *bridge)
	go ch.serveControl(*control)

	if !restored && savable {
		go func() {
			if !gobrig.ProbeReady(*bridge, 90*time.Second) {
				os.Stdout.Write([]byte("\rdaemon not ready in time; skipping golden snapshot\r\n"))
				return
			}
			if serr := ch.saveGolden(); serr != nil {
				os.Stdout.Write([]byte("\rgolden snapshot not saved (" + serr.Error() + "); continuing\r\n"))
				return
			}
			os.Stdout.Write([]byte("\rgolden snapshot saved: " + snapshotPath + "\r\n"))
		}()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	os.Stdout.Write([]byte("\r\nTerminating the Brig...\r\n"))
	_, _ = vm.RequestStop()
}

// driveConsole watches the guest console and scripts the login + bring-up.
func driveConsole(hostReader io.Reader, vmWriter io.Writer) {
	buf := make([]byte, 1024)
	var stream string
	state := 0
	for {
		n, err := hostReader.Read(buf)
		if n > 0 {
			os.Stdout.Write(buf[:n])
			stream += string(buf[:n])
			switch state {
			case 0:
				if strings.Contains(stream, "login:") {
					time.Sleep(100 * time.Millisecond)
					vmWriter.Write([]byte("root\n"))
					stream, state = "", 1
				}
			case 1:
				if strings.Contains(strings.ToLower(stream), "password:") {
					time.Sleep(100 * time.Millisecond)
					vmWriter.Write([]byte("passwd\n"))
					stream, state = "", 2
				}
			case 2:
				if strings.Contains(stream, "#") {
					time.Sleep(300 * time.Millisecond)
					vmWriter.Write([]byte(bringUpCmd))
					go io.Copy(vmWriter, os.Stdin)
					stream, state = "", 3
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// bringUpCmd mounts the shared assets, chroots into Alpine, and runs sandbox_init.sh with
// GOBRIG=1 set so in-guest code can detect the Brig (the Warden's marker). No networking is
// brought up: the Brig is egress-denied by construction.
const bringUpCmd = "mkdir -p /mnt/assets && mount -t virtiofs assets /mnt/assets && cp -a /mnt/assets/alpine-root /alpine && mkdir -p /alpine/mnt/assets && mount -o bind /mnt/assets /alpine/mnt/assets && mount -t proc none /alpine/proc && mount -t sysfs none /alpine/sys && mount -o bind /dev /alpine/dev && chroot /alpine /bin/sh -c 'GOBRIG=1 /bin/sh /mnt/assets/sandbox_init.sh'\n"

// startVsockBridge accepts TCP on host loopback and pipes each connection to the guest's
// vsock listener through the chamber (which applies -reset-each).
func startVsockBridge(ch *chamber, addr string) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("vsock bridge: listen %s: %v (is AirPlay on :5000? pass -bridge 127.0.0.1:5050)", addr, err)
		return
	}
	os.Stdout.Write([]byte("\r\nvsock bridge up: " + addr + " -> guest vsock:5000\r\n"))
	for {
		tcpConn, err := ln.Accept()
		if err != nil {
			continue
		}
		go ch.handleConn(tcpConn)
	}
}
