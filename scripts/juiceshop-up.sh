#!/bin/bash
# juiceshop-up.sh — bring OWASP Juice Shop up INSIDE a running Brig, egress-denied, as a
# breadth benchmark target with a REAL ground-truth ruler: its /api/Challenges endpoint
# exposes a server-side `solved` boolean per challenge, so a capture needs no planted
# canary — solving a challenge is oracle-authoritative by the app's own scoreboard.
#
# Juice Shop is a DISTROLESS image (no /bin/sh), so we cannot chroot+sh like VAmPI, and
# the guest's busybox is musl-dynamic and will not run in the glibc-ish rootfs. Instead we
# overlay-mount the rootfs (read-only virtiofs lower + tmpfs upper for its SQLite DB/logs)
# and launch via node itself: `node -e` spawns build/app.js DETACHED (new session), so it
# survives the detonation with the right cwd and no shell. It binds the guest loopback
# :3000, reached through the vsock bridge like the other targets.
#
# Prereqs: a booted Brig (scripts/brig-up.sh), Docker on the host, the bridge at
# 127.0.0.1:5050. Run from the gobrig repo root: bash scripts/juiceshop-up.sh
set -eu
BRIDGE="${BRIDGE:-127.0.0.1:5050}"
ASSETS="${ASSETS:-vm-assets}"
IMAGE="${IMAGE:-bkimminich/juice-shop:latest}"

det() { # run argv in the Brig; argv is a JSON array passed as $1
  curl -s -m 90 -X POST "http://$BRIDGE/" -H 'Content-Type: application/json' \
    -d "{\"argv\":$1,\"trace_id\":\"js-up\"}" \
    | python3 -c 'import sys,json;print(json.load(sys.stdin).get("output",""))'
}

if [ ! -d "$ASSETS/juiceshop-root" ]; then
  echo "[host] pulling + exporting $IMAGE rootfs -> $ASSETS/juiceshop-root"
  docker pull --platform linux/arm64 "$IMAGE"
  cid=$(docker create --platform linux/arm64 "$IMAGE")
  mkdir -p "$ASSETS/juiceshop-root"
  docker export "$cid" | tar -C "$ASSETS/juiceshop-root" -xf -
  docker rm "$cid" >/dev/null
fi
echo "[host] juiceshop-root ready ($(du -sh "$ASSETS/juiceshop-root" | awk '{print $1}'))"

echo "[brig] overlay-mount + launch Juice Shop detached on :3000"
det '["sh","-c","umount /js/proc /js/dev /js/sys 2>/dev/null; umount /js 2>/dev/null; umount /jsovl 2>/dev/null; rm -rf /jsovl /js; mkdir -p /jsovl /js && mount -t tmpfs -o size=512m tmpfs /jsovl && mkdir -p /jsovl/up /jsovl/work && mount -t overlay overlay -o lowerdir=/mnt/assets/juiceshop-root,upperdir=/jsovl/up,workdir=/jsovl/work /js && mkdir -p /js/proc /js/dev /js/sys /js/tmp && mount -t proc none /js/proc; mount -o bind /dev /js/dev; mount -t sysfs none /js/sys; echo ready"]'
det '["chroot","/js","/nodejs/bin/node","-e","const cp=require('child_process'),fs=require('fs');const o=fs.openSync('/tmp/js.log','a');cp.spawn('/nodejs/bin/node',['build/app.js'],{cwd:'/juice-shop',detached:true,stdio:['ignore',o,o]}).unref();console.log('spawned');"]'

echo "[brig] waiting for Juice Shop to come up (~30s)"
for i in $(seq 1 15); do
  v=$(det '["sh","-c","wget -qO- http://127.0.0.1:3000/rest/admin/application-version 2>/dev/null"]')
  case "$v" in *version*) echo "Juice Shop UP: $v"; break;; esac
  sleep 4
done
echo "Oracle: GET http://127.0.0.1:3000/api/Challenges (each has a server-side solved flag)."
