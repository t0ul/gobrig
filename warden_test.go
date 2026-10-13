package gobrig_test

import (
	"strings"
	"testing"

	"github.com/t0ul/gobrig"
)

// TestWardenRealHostRefuses exercises the REAL interface detector: on a machine with a
// network interface (the lab Mac), the Warden refuses even with the marker set — the
// fail-closed invariant that keeps offensive probes off the host.
func TestWardenRealHostRefuses(t *testing.T) {
	w := gobrig.Warden{Getenv: func(string) string { return "1" }} // marker set, REAL NICs
	ok, why := w.InBrig()
	if ok {
		t.Skip("no external NIC on this host; real-host refusal not exercised here")
	}
	if !strings.Contains(why, "network interface") {
		t.Errorf("expected a NIC reason, got %q", why)
	}
	if w.Require() == nil {
		t.Error("Require must fail-closed on a host with a NIC")
	}
}

// TestWardenInBrig simulates the Brig's conditions (marker set AND no external NIC) by
// injecting those environment inputs; the Warden must allow.
func TestWardenInBrig(t *testing.T) {
	w := gobrig.Warden{
		Getenv: func(k string) string {
			if k == gobrig.DefaultMarker {
				return "1"
			}
			return ""
		},
		NICs: func() ([]string, error) { return nil, nil },
	}
	if ok, why := w.InBrig(); !ok {
		t.Fatalf("Warden should allow inside the Brig, refused: %s", why)
	}
	if err := w.Require(); err != nil {
		t.Fatalf("Require should pass inside the Brig: %v", err)
	}
}

// TestWardenNeedsBothSignals: a marker without a clean NIC state, or a clean NIC state
// without the marker, must both refuse. Neither signal alone is the Brig.
func TestWardenNeedsBothSignals(t *testing.T) {
	noMarker := gobrig.Warden{
		Getenv: func(string) string { return "" },
		NICs:   func() ([]string, error) { return nil, nil },
	}
	if ok, _ := noMarker.InBrig(); ok {
		t.Error("must refuse when the marker is absent")
	}
	markedButNIC := gobrig.Warden{
		Getenv: func(string) string { return "1" },
		NICs:   func() ([]string, error) { return []string{"en0"}, nil },
	}
	if ok, _ := markedButNIC.InBrig(); ok {
		t.Error("must refuse when an external NIC is present")
	}
}
