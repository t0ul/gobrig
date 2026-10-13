#!/usr/bin/env bash
# brig-up.sh — provision, codesign, and boot the Brig (the contained Apple vz microVM).
#
# Why a script you run: booting the VM needs the com.apple.security.virtualization
# entitlement, which means codesigning on your machine with your identity. Claude builds
# everything up to the boot; you run this to provision, sign, and launch.
#
# prepareassets downloads the kernel + Alpine rootfs on first run (network at PROVISION
# time only — the running chamber is still egress-denied) and cross-builds detonationd.
# The guest comes up with no network device; vsock is the only channel in.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENT="$ROOT/scripts/brig.entitlements.plist"
BRIDGE="${BRIDGE:-127.0.0.1:5050}"  # NOT :5000 — macOS AirPlay Receiver owns that port
IDENTITY="${CODESIGN_IDENTITY:--}"  # "-" = ad-hoc; set CODESIGN_IDENTITY for a real identity
cd "$ROOT"

echo "==> provisioning Brig assets (first run downloads kernel + rootfs; always rebuilds detonationd)"
go run ./cmd/prepareassets -dir "$ROOT/vm-assets"
if [ ! -f "$ROOT/vm-assets/Image" ]; then
  echo "brig-up: kernel not provisioned (see prepareassets output above)."
  exit 1
fi

echo "==> building the host launcher (cgo + Virtualization.framework)"
go build -o "$ROOT/bin/launchbrig" ./cmd/launchbrig

echo "==> codesigning with the virtualization entitlement ($IDENTITY)"
codesign --force --sign "$IDENTITY" --entitlements "$ENT" "$ROOT/bin/launchbrig"
echo "    signed: $(codesign -dv "$ROOT/bin/launchbrig" 2>&1 | head -1 || true)"

echo "==> booting the Brig on bridge $BRIDGE (egress-denied, ephemeral, no guest NIC)"
exec "$ROOT/bin/launchbrig" -bridge "$BRIDGE" -dir "$ROOT/vm-assets" "$@"
