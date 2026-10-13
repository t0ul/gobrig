package gobrig_test

import (
	"testing"

	"github.com/t0ul/gobrig"
)

func TestPolicyCheck(t *testing.T) {
	p := gobrig.Policy{}
	if err := p.Check([]string{"id"}); err != nil {
		t.Errorf("allowlisted argv[0] should pass: %v", err)
	}
	if err := p.Check([]string{"curl", "http://x"}); err == nil {
		t.Error("non-allowlisted argv[0] must be refused")
	}
	if err := p.Check(nil); err == nil {
		t.Error("empty argv must be refused")
	}
	// allowlisted argv[0] but a forbidden signature elsewhere must still trip.
	if err := p.Check([]string{"echo", "rm -rf /"}); err == nil {
		t.Error("a forbidden signature must trip the secondary tripwire")
	}
}
