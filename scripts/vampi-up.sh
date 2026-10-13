#!/bin/bash
# vampi-up.sh — bring the real OWASP VAmPI API up INSIDE a running Brig, egress-denied,
# as a diversified discovery target (BOLA/IDOR, broken JWT, mass-assignment, excess data).
#
# How it works, and why:
#   - The Brig shares vm-assets to the guest over virtiofs, READ-ONLY to the guest. So we
#     export VAmPI's container rootfs on the host into vm-assets/vampi-root (one time),
#     then in the guest mount an OVERLAY (lower = the clean read-only virtiofs rootfs,
#     upper = a tmpfs) so VAmPI can write its SQLite DB without copying 160 MB (a plain
#     copy over virtiofs is slow, and busybox tar corrupts a file in transit — overlay
#     avoids both). VAmPI then runs via chroot with the image's own env (LANG=C.UTF-8),
#     binding the guest loopback on :5000, reachable through the vsock bridge like vulnapp.
#
# Prereqs: a booted Brig (scripts/brig-up.sh), Docker on the host, and the bridge at
# 127.0.0.1:5050. Run from the gobrig repo root:  bash scripts/vampi-up.sh
set -eu
BRIDGE="${BRIDGE:-127.0.0.1:5050}"
ASSETS="${ASSETS:-vm-assets}"
IMAGE="${IMAGE:-erev0s/vampi:latest}"

det() { # run argv in the Brig over the bridge; prints the combined output
  local argv="$1"
  curl -s -m 180 -X POST "http://$BRIDGE/" -H 'Content-Type: application/json' \
    -d "{\"argv\":$argv,\"trace_id\":\"vampi-up\"}" \
    | python3 -c 'import sys,json;print(json.load(sys.stdin).get("output",""))'
}

if [ ! -d "$ASSETS/vampi-root" ]; then
  echo "[host] pulling + exporting $IMAGE rootfs -> $ASSETS/vampi-root"
  docker pull --platform linux/arm64 "$IMAGE"
  cid=$(docker create --platform linux/arm64 "$IMAGE")
  mkdir -p "$ASSETS/vampi-root"
  docker export "$cid" | tar -C "$ASSETS/vampi-root" -xf -
  docker rm "$cid" >/dev/null
fi
echo "[host] vampi-root ready ($(du -sh "$ASSETS/vampi-root" | awk '{print $1}'))"

echo "[brig] overlay-mounting + launching VAmPI on :5000"
det '["sh","-c","umount /merged/proc /merged/dev /merged/sys 2>/dev/null; umount /merged 2>/dev/null; umount /ovl 2>/dev/null; rm -rf /ovl /merged; mkdir -p /ovl /merged && mount -t tmpfs -o size=256m tmpfs /ovl && mkdir -p /ovl/up /ovl/work && mount -t overlay overlay -o lowerdir=/mnt/assets/vampi-root,upperdir=/ovl/up,workdir=/ovl/work /merged && mkdir -p /merged/proc /merged/dev /merged/sys && mount -t proc none /merged/proc; mount -o bind /dev /merged/dev; mount -t sysfs none /merged/sys; chroot /merged /bin/sh -c \"cd /vampi && PATH=/usr/local/bin:/usr/bin:/bin LANG=C.UTF-8 /usr/local/bin/python app.py >/vampi/boot.log 2>&1 &\"; sleep 8; echo launched"]'

echo "[brig] populating the DB + verifying"
det '["sh","-c","wget -qO- http://127.0.0.1:5000/createdb 2>&1 | head -c 120; echo; wget -qO- http://127.0.0.1:5000/users/v1 2>&1 | head -c 120"]'
echo
echo "VAmPI is up in the Brig at http://127.0.0.1:5000 (reach it via the bridge like vulnapp)."
