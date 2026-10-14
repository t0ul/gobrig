package main

import "testing"

func TestIsLoopback(t *testing.T) {
	for _, h := range []string{"127.0.0.1", "::1", "localhost", "127.5.5.5"} {
		if !isLoopback(h) {
			t.Errorf("isLoopback(%q) = false, want true", h)
		}
	}
	for _, h := range []string{"0.0.0.0", "10.0.0.1", "93.184.216.34", "example.com", ""} {
		if isLoopback(h) {
			t.Errorf("isLoopback(%q) = true, want false (containment must refuse this)", h)
		}
	}
}
