package gobrig

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// HTTPGet fetches url from INSIDE the Brig and returns the response body. The Brig has no
// NIC, so a container's hostNetwork service is reachable only from the guest; this runs
// `wget -qO- <url>` as a detonation (no shell, url passed as a single argv), which is the
// agent's one HTTP primitive over the bridge.
func HTTPGet(ctx context.Context, c Client, url string) (string, error) {
	r, err := c.Detonate(ctx, Detonation{Argv: []string{"wget", "-qO-", url}, TraceID: "httpget"})
	if err != nil {
		return "", err
	}
	return r.Output, nil
}

// nonce returns a short random marker used to confirm code execution (a value the target
// could only emit by running the injected command).
func nonce() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "n" + hex.EncodeToString(b)
}
