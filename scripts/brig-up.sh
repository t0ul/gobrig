#!/usr/bin/env bash
# brig-up.sh — build, codesign, and boot the Brig (the contained Apple vz microVM).
#
# Why a script you run: booting the VM needs the com.apple.security.virtualization
# entitlement, which means codesigning on your machine with your identity. Claude builds
# everything up to the boot; you run this to sign and launch. Nothing here reaches the
# network — the Brig is built with no guest NIC.
#
# Requires CH-2 (the vz boot port: cmd/launchbrig + the guest cmd/detonationd). Until those
# land this script detects their absence and exits cleanly, so it is safe to commit now.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENT="$ROOT/scripts/brig.entitlements.plist"
BRIDGE="${BRIDGE:-127.0.0.1:5050}"   # NOT :5000 — macOS AirPlay Receiver owns that port
IDENTITY="${CODESIGN_IDENTITY:--}"   # "-" = ad-hoc; set CODESIGN_IDENTITY to a real identity if needed
GUEST_ARCH="${GUEST_ARCH:-arm64}"

if [ ! -d "$ROOT/cmd/launchbrig" ]; then
  echo "brig-up: cmd/launchbrig not present yet (CH-2 not landed). Nothing to boot."
  echo "         This script is ready for when the vz boot port lands."
  exit 0
fi

echo "==> rebuilding guest binaries (stale baked binaries silently disable the hardened path)"
# The guest daemon runs INSIDE the Linux VM; always rebuild before boot.
GOOS=linux GOARCH="$GUEST_ARCH" CGO_ENABLED=0 \
  go build -o "$ROOT/vm-assets/detonationd" "$ROOT/cmd/detonationd"

echo "==> building the host launcher (cgo + Virtualization.framework)"
go build -o "$ROOT/bin/launchbrig" "$ROOT/cmd/launchbrig"

echo "==> codesigning with the virtualization entitlement ($IDENTITY)"
codesign --force --sign "$IDENTITY" --entitlements "$ENT" "$ROOT/bin/launchbrig"
echo "    signed: $(codesign -dv "$ROOT/bin/launchbrig" 2>&1 | head -1 || true)"

echo "==> booting the Brig on bridge $BRIDGE (egress-denied, ephemeral, no guest NIC)"
exec "$ROOT/bin/launchbrig" -bridge "$BRIDGE" "$@"
