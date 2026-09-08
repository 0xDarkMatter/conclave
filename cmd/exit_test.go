package cmd

import (
	"errors"
	"fmt"
	"testing"
)

// TestExitCodeSurvivesWrapping is what `make check` and CI depend on: they
// branch on the exit code to tell real model drift from an unreachable
// catalog. If the code is lost when an error is wrapped, drift silently
// becomes a skip and a stale default ships.
func TestExitCodeSurvivesWrapping(t *testing.T) {
	base := withExitCode(ExitDrift, errors.New("2 model ids are missing"))
	wrapped := fmt.Errorf("models --check: %w", base)

	var coded exitCoder
	if !errors.As(wrapped, &coded) {
		t.Fatal("exit code lost through a wrap")
	}
	if coded.ExitCode() != ExitDrift {
		t.Fatalf("ExitCode = %d, want %d", coded.ExitCode(), ExitDrift)
	}
	if got := wrapped.Error(); got != "models --check: 2 model ids are missing" {
		t.Fatalf("message changed: %q", got)
	}
}

// TestWithExitCodeKeepsNilNil lets callers wrap a result directly, so a
// success is not turned into a failure carrying an exit code.
func TestWithExitCodeKeepsNilNil(t *testing.T) {
	if err := withExitCode(ExitDrift, nil); err != nil {
		t.Fatalf("withExitCode(nil) = %v, want nil", err)
	}
}

// TestDriftAndUnavailableAreDistinct: the whole point is that a build can fail
// on one and skip the other.
func TestDriftAndUnavailableAreDistinct(t *testing.T) {
	if ExitDrift == ExitCatalogUnavailable {
		t.Fatal("drift and catalog-unavailable share an exit code")
	}
	for _, code := range []int{ExitDrift, ExitCatalogUnavailable} {
		if code == 0 || code == 1 {
			t.Errorf("exit code %d collides with success or the generic failure", code)
		}
	}
}
