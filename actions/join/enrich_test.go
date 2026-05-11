package join

import (
	"context"
	"testing"

	provisioner "github.com/plasmash/plasmactl-node/pkg/provisioner"
)

// fakeFetcher returns canned metadata.
type fakeFetcher struct {
	publicMAC, privateMAC string
	disks                 []provisioner.DiskSpec
}

func (f *fakeFetcher) Fetch(ctx context.Context, serverID string) (*provisioner.NodeMetadata, error) {
	return &provisioner.NodeMetadata{
		PublicMAC:  f.publicMAC,
		PrivateMAC: f.privateMAC,
		Disks:      f.disks,
	}, nil
}

// fakeReconciler records calls; never errors.
type fakeReconciler struct {
	calls []reconcileCall
}
type reconcileCall struct {
	platform, serverID, privateMAC string
	vlanID                         int
}

func (r *fakeReconciler) Reconcile(ctx context.Context, platformName string, vlanID int, serverID, privateMAC string) error {
	r.calls = append(r.calls, reconcileCall{platformName, serverID, privateMAC, vlanID})
	return nil
}

func TestEnrichNodeFields_PopulatesAllNewFields(t *testing.T) {
	fetcher := &fakeFetcher{
		publicMAC:  "AA:BB:CC:00:00:01",
		privateMAC: "11:22:33:00:00:01",
		disks: []provisioner.DiskSpec{
			{Type: "NVMe", CapacityGB: 512},
			{Type: "NVMe", CapacityGB: 512},
		},
	}
	reconciler := &fakeReconciler{}

	got, err := enrichNodeFields(context.Background(), enrichInput{
		Provider:          "ovh",
		PlatformName:      "example-plat",
		ServerID:          "ns1.example.net",
		VLANID:            7,
		ExistingPrivateIP: "", // simulates "TF didn't return private_ip; allocate"
		PrivateNetwork:    "192.168.0.0/24",
		NodesDir:          t.TempDir(),
		Fetcher:           fetcher,
		Reconciler:        reconciler,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.PublicMAC != "AA:BB:CC:00:00:01" {
		t.Errorf("PublicMAC = %q", got.PublicMAC)
	}
	if got.PrivateMAC != "11:22:33:00:00:01" {
		t.Errorf("PrivateMAC = %q", got.PrivateMAC)
	}
	if got.PrivateIP == "" {
		t.Error("PrivateIP must be allocated when missing")
	}
	if len(got.Disks) != 2 || got.Disks[0] != "/dev/nvme0n1" {
		t.Errorf("Disks = %v, want [/dev/nvme0n1 /dev/nvme1n1]", got.Disks)
	}
	if len(reconciler.calls) != 1 {
		t.Errorf("reconciler.calls = %v, want one call", reconciler.calls)
	}
	if reconciler.calls[0].vlanID != 7 || reconciler.calls[0].privateMAC != "11:22:33:00:00:01" {
		t.Errorf("reconciler call = %+v", reconciler.calls[0])
	}
}

func TestEnrichNodeFields_KeepsExistingPrivateIP(t *testing.T) {
	fetcher := &fakeFetcher{
		publicMAC:  "AA:BB:CC:00:00:01",
		privateMAC: "11:22:33:00:00:01",
		disks:      []provisioner.DiskSpec{{Type: "NVMe", CapacityGB: 512}},
	}
	got, err := enrichNodeFields(context.Background(), enrichInput{
		Provider:          "ovh",
		PlatformName:      "example-plat",
		ServerID:          "ns1.example.net",
		VLANID:            7,
		ExistingPrivateIP: "192.168.0.42",
		PrivateNetwork:    "192.168.0.0/24",
		NodesDir:          t.TempDir(),
		Fetcher:           fetcher,
		Reconciler:        &fakeReconciler{},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.PrivateIP != "192.168.0.42" {
		t.Errorf("PrivateIP overwritten: got %q", got.PrivateIP)
	}
}

func TestEnrichNodeFields_NoFetcher_ReturnsError(t *testing.T) {
	_, err := enrichNodeFields(context.Background(), enrichInput{
		Provider:       "ovh",
		PlatformName:   "example-plat",
		ServerID:       "ns1.example.net",
		VLANID:         7,
		PrivateNetwork: "192.168.0.0/24",
		NodesDir:       t.TempDir(),
		Fetcher:        nil, // simulates non-OVH provider with no impl
		Reconciler:     nil,
	})
	if err == nil {
		t.Fatal("expected error when Fetcher is nil")
	}
}

func TestEnrichNodeFields_NilReconciler_FetcherPresent_CompletesSuccessfully(t *testing.T) {
	// Non-vrack providers (e.g., scaleway) will have nil Reconciler.
	// enrichNodeFields should still complete without calling Reconcile.
	fetcher := &fakeFetcher{
		publicMAC:  "AA:BB:CC:00:00:01",
		privateMAC: "11:22:33:00:00:01",
		disks:      []provisioner.DiskSpec{{Type: "NVMe", CapacityGB: 512}},
	}
	_, err := enrichNodeFields(context.Background(), enrichInput{
		Provider:          "scaleway",
		PlatformName:      "test",
		ServerID:          "s1",
		VLANID:            7,
		ExistingPrivateIP: "192.168.0.5",
		PrivateNetwork:    "192.168.0.0/24",
		NodesDir:          t.TempDir(),
		Fetcher:           fetcher,
		Reconciler:        nil,
	})
	if err != nil {
		t.Fatalf("expected no error with nil Reconciler, got: %v", err)
	}
}
