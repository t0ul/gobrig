package gobrig_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/t0ul/gobrig"
)

// TestClientDetonate drives the real HTTP transport against a server that mimics the Brig
// daemon (echoes the argv and trace id), so the client's request/response handling is
// exercised end to end with no mock.
func TestClientDetonate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in gobrig.Detonation
		_ = json.NewDecoder(r.Body).Decode(&in)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gobrig.Result{Output: "ran: " + strings.Join(in.Argv, " "), TraceID: in.TraceID})
	}))
	defer ts.Close()

	c := gobrig.Client{Bridge: strings.TrimPrefix(ts.URL, "http://")}
	res, err := c.Detonate(context.Background(), gobrig.Detonation{Argv: []string{"id"}, TraceID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "ran: id" || res.TraceID != "t1" {
		t.Errorf("unexpected result: %+v", res)
	}
}
