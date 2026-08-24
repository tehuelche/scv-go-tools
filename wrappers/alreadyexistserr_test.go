package wrappers_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/tehuelche/scv-go-tools/v3/wrappers"
)

func TestNewAlreadyExistsErr_NilStaysNil(t *testing.T) {
	if err := wrappers.NewAlreadyExistsErr(nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestNewAlreadyExistsErr_KeepsMessage(t *testing.T) {
	err := wrappers.NewAlreadyExistsErr(errors.New("duplicate key on trips_pkey"))

	if err.Error() != "duplicate key on trips_pkey" {
		t.Fatalf("message lost: %q", err.Error())
	}
}

// The whole point of the type: a caller asks what kind of failure this is
// without having to recognise wording that only one driver produces.
func TestNewAlreadyExistsErr_IsAlreadyExists(t *testing.T) {
	err := wrappers.NewAlreadyExistsErr(errors.New("E11000 duplicate key error"))

	if !errors.Is(err, wrappers.AlreadyExistsErr) {
		t.Fatal("expected errors.Is to recognise it")
	}
	if errors.Is(err, wrappers.NonExistentErr) {
		t.Fatal("it must not pass for a missing resource")
	}
}

// Callers add their own context on the way up; the answer has to survive it.
func TestNewAlreadyExistsErr_SurvivesWrapping(t *testing.T) {
	err := fmt.Errorf("migrating trip: %w", wrappers.NewAlreadyExistsErr(errors.New("boom")))

	if !errors.Is(err, wrappers.AlreadyExistsErr) {
		t.Fatal("expected errors.Is to see through the wrap")
	}
}
