package stormbreak

import (
	"errors"
	"fmt"
	"testing"
)

func TestPermanent(t *testing.T) {
	original := errors.New("invalid credentials")
	err := Permanent(original)
	if !IsPermanent(fmt.Errorf("outer: %w", err)) {
		t.Fatal("wrapped permanent error not detected")
	}
	if !errors.Is(err, original) {
		t.Fatal("permanent error does not unwrap")
	}
	if Permanent(nil) != nil {
		t.Fatal("Permanent(nil) must be nil")
	}
}

func TestClassifierHelpers(t *testing.T) {
	err := errors.New("failure")
	if NeverRetry(err) || !AlwaysRetry(err) || AlwaysRetry(nil) {
		t.Fatal("classifier helper result mismatch")
	}
}
