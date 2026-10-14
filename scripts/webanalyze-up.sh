#!/bin/bash
# webanalyze-up.sh — provision the webanalyze technology fingerprinter into the Brig's
# shared assets, the same way dalfox is shipped in: a static arm64 binary plus its offline
# signature database, dropped into vm-assets (virtiofs-mounted read-only at /mnt/assets in
# the guest). The recon stage's `fingerprint` agent tool then runs it in-Brig.
#
# Why these choices:
#   - webanalyze (github.com/rverton/webanalyze) is a pure-Go Wappalyzer port: no CGO, so it
#     cross-compiles to a static linux/arm64 ELF that runs in the guest with nothing else.
#   - It is pinned to v0.4.6; the module's go.sum hash is the integrity anchor. We build it
#     in a throwaway module so gobrig's own go.mod stays clean (nothing here imports it).
#   - The signature DB (technologies.json, ~2 MB) ships VENDORED alongside the module, so we
#     copy that exact file. The tool is run with `-apps <that file>` and never `-update`, so
#     it makes no outbound request for signatures — which matters because the Brig is egress
#     denied (a stray fetch would just DNS-fail, but we avoid it by design).
#   - It is a GENERAL off-the-shelf tool: it carries no endpoint and no payload for the
#     target. The agent aims it at surfaces it discovers; the fingerprint drives which vuln
#     class the planner tests where (JS-framework HTML surface -> XSS; JSON API -> authz).
#
# Run from the gobrig repo root:  bash scripts/webanalyze-up.sh
# The Brig need not be running for this step (it only writes host-side assets); boot it
# with scripts/brig-up.sh afterwards so the guest sees /mnt/assets/webanalyze.
set -eu

ASSETS="${ASSETS:-vm-assets}"
WA_VERSION="${WA_VERSION:-v0.4.6}"
WA_PKG="github.com/rverton/webanalyze"

mkdir -p "$ASSETS"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "[host] resolving $WA_PKG@$WA_VERSION in a throwaway module (keeps gobrig go.mod clean)"
(
  cd "$tmp"
  cat > go.mod <<EOF
module webanalyze-build
go 1.27
EOF
  GOFLAGS=-mod=mod go get "$WA_PKG@$WA_VERSION"

  echo "[host] cross-building static linux/arm64 webanalyze -> $ASSETS/webanalyze"
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOFLAGS=-mod=mod \
    go build -ldflags='-s -w' -o "$OLDPWD/$ASSETS/webanalyze" "$WA_PKG/cmd/webanalyze"

  echo "[host] copying vendored signature DB -> $ASSETS/technologies.json"
  moddir="$(go list -m -f '{{.Dir}}' "$WA_PKG")"
  cp "$moddir/technologies.json" "$OLDPWD/$ASSETS/technologies.json"
  chmod u+w "$OLDPWD/$ASSETS/technologies.json"
)

echo
echo "[host] provisioned (sha256):"
shasum -a 256 "$ASSETS/webanalyze" "$ASSETS/technologies.json"
echo
file "$ASSETS/webanalyze" 2>/dev/null || true
echo "[host] done. In the Brig these are at /mnt/assets/webanalyze and /mnt/assets/technologies.json"
