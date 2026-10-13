// Package gobrig is an app-agnostic containment chamber for offensive experiments — the
// Brig. A brig is both a pirate ship and the cell aboard one, which is the point: GORED
// detonates autonomous RCE and container-escape benchmarks inside the Brig, an egress-
// denied, ephemeral Apple `vz` microVM, so a successful exploit reaches only a disposable,
// network-isolated VM and never the real host. It is ported and generalized from
// emaildrop's detonation chamber, decoupled from it (no emaildrop import).
//
// This file set is the host-side, pure-Go core — the fail-closed Warden (nothing offensive
// runs unless it is locked in the Brig), the exec Policy, the Detonation/Result types, and
// the bridge Client — all of which build and test on any platform. The VM boot itself
// (Apple Virtualization, cgo, macOS, a codesigned virtualization entitlement) is a separate
// build-tagged increment; booting the Brig needs codesigning (see scripts/brig-up.sh) and
// is not exercised by `go test`.
//
// Containment boundary: the Brig isolates container -> VM-guest-host. It is NOT a defense
// against, and GORED never attempts, hypervisor escape (VM -> real host); the hull is the
// trusted wall.
package gobrig
