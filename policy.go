package gobrig

import (
	"fmt"
	"strings"
)

// DefaultAllowed is the Brig's own-exec allowlist: only these argv[0] programs may be run
// through the Brig's detonation tool (for container setup and the oracle check). An
// allowlist is not bypassable the way a blocklist is. This governs the Brig's DIRECT exec
// surface only — an escape experiment's exploit executes inside the target container
// (reached over HTTP), not through this gate.
var DefaultAllowed = []string{
	"uname", "ls", "cat", "head", "tail", "echo", "pwd", "id",
	"env", "date", "whoami", "wc", "grep", "sort", "true",
}

// ForbiddenSignatures is the secondary tripwire scanned across the whole argv.
var ForbiddenSignatures = []string{"rm -rf", "nc -e", "mkfifo", "> /dev/tcp"}

// Policy gates a detonation's argv: argv[0] must be allowlisted (primary), and no forbidden
// signature may appear anywhere in the argv (secondary).
type Policy struct {
	Allowed []string // "" uses DefaultAllowed
	Block   []string // nil uses ForbiddenSignatures
}

func (p Policy) allowed() []string {
	if len(p.Allowed) > 0 {
		return p.Allowed
	}
	return DefaultAllowed
}

func (p Policy) block() []string {
	if p.Block != nil {
		return p.Block
	}
	return ForbiddenSignatures
}

// Check returns nil if argv is permitted, else an error naming the reason.
func (p Policy) Check(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("gobrig: empty argv")
	}
	ok := false
	for _, a := range p.allowed() {
		if argv[0] == a {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("gobrig: argv[0] %q not in the allowlist", argv[0])
	}
	joined := strings.Join(argv, " ")
	for _, sig := range p.block() {
		if strings.Contains(joined, sig) {
			return fmt.Errorf("gobrig: argv contains a forbidden signature %q", sig)
		}
	}
	return nil
}
