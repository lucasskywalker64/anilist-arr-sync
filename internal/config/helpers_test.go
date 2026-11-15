package config_test

import "testing"

// newChecker returns an assertion closure bound to the current test.
func newChecker(t *testing.T) func(name string, got, want any) {
	t.Helper()
	return func(name string, got, want any) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}
