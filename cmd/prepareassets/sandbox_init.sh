#!/bin/sh
# In-Brig init, run inside the Alpine chroot by launchbrig. The Brig is egress-denied:
# launchbrig attaches no network device, so there is no interface to bring up, no DNS,
# no NTP. detonationd is a static Go binary on the shared /mnt/assets mount, reached only
# over vsock:5000. GOBRIG=1 marks the Brig so in-guest code can detect it (the Warden).
# The daemon runs as the non-root user 'sandbox', so every command it detonates inherits
# a non-root, no-network context; each detonation also runs in a fresh temp dir.
echo "=============================================="
echo "  BRIG UP (egress-denied, no network device)"
echo "=============================================="

if [ ! -f /mnt/assets/detonationd ]; then
    echo "  WARNING: /mnt/assets/detonationd not found."
    echo "  Build it first:  go run ./cmd/prepareassets"
    echo ""
    echo "Interactive shell ready."
    exec /bin/sh
fi

# Assets live on a read-only virtiofs mount; the exec bit carries over from the host
# build. Best-effort, never fatal.
chmod +x /mnt/assets/detonationd /mnt/assets/runc 2>/dev/null || true

# The daemon runs as ROOT inside the Brig. The Brig is the containment boundary (a
# disposable, egress-denied VM with no NIC), so root-in-the-guest is required and safe: a
# benchmark must run — and then escape — misconfigured containers, which needs root to
# create namespaces, mount, and hold caps. The VM is the wall, not the guest user.
echo "Starting detonationd as root (vsock:5000, GOBRIG set)..."
GOBRIG=1 /mnt/assets/detonationd > /tmp/detonation.log 2>&1 &
echo "  detonationd started (PID $!) — tail /tmp/detonation.log to watch"

echo ""
echo "Interactive shell ready."
exec /bin/sh
