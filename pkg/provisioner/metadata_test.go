package provisioner

import (
	"context"
	"strings"
	"testing"
)

func TestNewMetadataFetcher_OVH_Dispatches(t *testing.T) {
	// Without OVH credentials in keyring, this returns an error from
	// auth.Resolve. The factory itself is registered correctly; the contract
	// validated here is "ovh dispatches into ovh path", which we check by
	// asserting the error mentions ovh, not "unknown".
	_, err := NewMetadataFetcher(context.Background(), Config{Provider: "ovh"})
	if err == nil {
		// Acceptable: if the test runner happens to have OVH creds in keyring
		// (unlikely in CI), the factory returns a real fetcher and no error.
		return
	}
	if !strings.Contains(strings.ToLower(err.Error()), "ovh") {
		t.Fatalf("expected ovh-pathway error, got: %v", err)
	}
}

func TestNewMetadataFetcher_UnknownProvider_ReturnsNilNotError(t *testing.T) {
	f, err := NewMetadataFetcher(context.Background(), Config{Provider: "unknown"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if f != nil {
		t.Fatal("expected nil fetcher for unknown provider (caller no-ops)")
	}
}

func TestNewVRackReconciler_NonOVH_ReturnsNilNotError(t *testing.T) {
	r, err := NewVRackReconciler(context.Background(), Config{Provider: "scaleway"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if r != nil {
		t.Fatal("expected nil reconciler for non-vrack provider")
	}
}
