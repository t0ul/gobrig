package gobrig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Detonation is one argv to run inside the Brig with NO shell (exec argv[0] argv[1:]).
// There is deliberately no shell-string form: a no-shell array is not vulnerable to the
// argument injection the Brig is built to resist.
type Detonation struct {
	Argv    []string `json:"argv"`
	TraceID string   `json:"trace_id"`
}

// Result is the Brig's reply: the combined output and the echoed trace id.
type Result struct {
	Output  string `json:"output"`
	TraceID string `json:"trace_id"`
}

// Client talks to a running Brig's control bridge over loopback HTTP: it POSTs a Detonation
// and reads the Result (the daemon answers HTTP/1.1 on the bridge regardless of path). It
// runs host-side — driving container setup and the oracle check — and is plain net/http
// with no cgo, so it builds and tests anywhere.
type Client struct {
	Bridge  string       // host bridge address, e.g. 127.0.0.1:5050
	HTTP    *http.Client // nil uses a 30s client
	MaxResp int64        // max bridge response bytes; <=0 uses defaultMaxResp
}

// defaultMaxResp bounds the bridge response read. It must comfortably exceed a real web
// asset a tool might fetch through the Brig (e.g. a single-page app's main.js, which runs
// well over 1 MiB), or the Result JSON is truncated and fails to decode. 32 MiB still bounds
// memory against a runaway command's output.
const defaultMaxResp = 32 << 20

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Detonate runs argv in the Brig and returns its output. It is the raw transport and does
// NOT apply Policy; a caller handling untrusted argv should Policy.Check first.
func (c Client) Detonate(ctx context.Context, d Detonation) (Result, error) {
	body, _ := json.Marshal(d)
	url := "http://" + c.Bridge + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	max := c.MaxResp
	if max <= 0 {
		max = defaultMaxResp
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, max))
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("gobrig: bridge %s: HTTP %d", c.Bridge, resp.StatusCode)
	}
	var out Result
	if err := json.Unmarshal(data, &out); err != nil {
		return Result{}, fmt.Errorf("gobrig: decode: %w", err)
	}
	return out, nil
}
