// Package provisioner defines provider-agnostic interfaces used by node:join
// to enrich node yaml with provider-fetched metadata (MACs, disks) and to
// reconcile platform-private network membership (e.g. OVH vRack).
//
// Provider-specific implementations live in subpackages under this one
// (currently pkg/provisioner/ovh). Scaleway impls are deliberately absent
// — node:join is OVH-only today.
package provisioner

import (
	"context"
	"fmt"
	"net/http"

	"github.com/launchrctl/keyring"
	"github.com/plasmash/plasmactl-auth/pkg/auth"
	"github.com/plasmash/plasmactl-node/pkg/provisioner/ovh"
)

// NodeMetadata is what a fetcher returns for a single server.
//
// PublicGateway / PublicPrefix encode the provider's public-IP routing
// model: gateway IP + host-side address prefix length. For OVH dedicated
// the gateway is in the 100.64.0.0/10 CGN range and the prefix is /32
// (point-to-point routing). For other providers, fetchers populate the
// values matching their conventions. When both are empty/zero, the
// downstream inventory plugin falls back to its legacy Scaleway-pattern
// derivation (gateway = public_ip[:3]+".1", prefix = 24).
type NodeMetadata struct {
	PublicMAC     string
	PrivateMAC    string
	Disks         []DiskSpec
	PublicGateway string
	PublicPrefix  int
	// FailoverIP is the first non-primary IPv4 routed to the server (OVH
	// Additional IP / "failover" product). Empty when no extra IP is
	// routed. Provider-specific: OVH populates from /dedicated/server/
	// {name}/ips; other providers may surface a Scaleway-style TF output
	// or leave empty. Downstream, the inventory plugin derives
	// failover_network = "{ip}/32" and the os_flatcar Flatcar template
	// attaches the /32 to the public NIC + sets PreferredSource on the
	// default route.
	FailoverIP string
}

// DiskSpec describes one physical disk on the server. Used to synthesize
// kernel-conventional /dev paths (e.g. /dev/nvme0n1) downstream.
type DiskSpec struct {
	Type       string // "NVMe" | "SATA" | "SSD" | "HDD" | ...
	CapacityGB int
}

// NodeMetadataFetcher returns server-level metadata that plasmactl-node
// writes into the v2 node yaml. Implementations are provider-specific.
type NodeMetadataFetcher interface {
	Fetch(ctx context.Context, serverID string) (*NodeMetadata, error)
}

// VRackReconciler ensures a server's private interface is in the platform's
// vRack on the configured VLAN. Implementations should be idempotent —
// no-op when state already matches.
type VRackReconciler interface {
	Reconcile(ctx context.Context, platformName string, vlanID int, serverID, privateMAC string) error
}

// Config carries everything the factories need to wire up the right impl.
type Config struct {
	Provider string
	Keyring  keyring.Keyring
	Region   string // OVH account_region (used as base-URL hint when set)
}

// NewMetadataFetcher returns a provider-specific MetadataFetcher, or
// (nil, nil) when the provider has no impl yet. Callers MUST treat nil as
// "no enrichment available — skip" rather than as an error.
func NewMetadataFetcher(ctx context.Context, cfg Config) (NodeMetadataFetcher, error) {
	switch cfg.Provider {
	case "ovh":
		return newOVHMetadataFetcher(ctx, cfg)
	default:
		return nil, nil
	}
}

// NewVRackReconciler returns a provider-specific VRackReconciler, or
// (nil, nil) for providers that don't manage a vRack-like fabric. Callers
// MUST treat nil as "no reconciliation needed for this provider".
func NewVRackReconciler(ctx context.Context, cfg Config) (VRackReconciler, error) {
	switch cfg.Provider {
	case "ovh":
		return newOVHVRackReconciler(ctx, cfg)
	default:
		return nil, nil
	}
}

// SynthesizeDiskPaths returns kernel-conventional /dev paths for the given
// disk type and count. Currently delegates to the OVH impl since the naming
// convention (NVMe → /dev/nvmeXn1, others → /dev/sdX) is universal across
// providers we support. If a provider needs different paths in the future,
// this should become a method on NodeMetadataFetcher.
func SynthesizeDiskPaths(diskType string, count int) []string {
	return ovh.SynthesizeDiskPaths(diskType, count)
}

// providerHTTPClient mints an OAuth2-Bearer http.Client via plasmactl-auth.
// Used by both OVH metadata + vrack impls; centralised so tests can override
// the underlying transport via plasmactl-auth's own test hooks.
//
// Returns the client, the resolved Credentials (so callers can pull
// region-specific base URLs), and any error.
func providerHTTPClient(ctx context.Context, cfg Config) (*http.Client, auth.Credentials, error) {
	creds, err := auth.Resolve(ctx, cfg.Keyring, "", cfg.Provider)
	if err != nil {
		return nil, auth.Credentials{}, fmt.Errorf("provisioner: resolve %s credentials: %w", cfg.Provider, err)
	}
	provider, ok := auth.Get(cfg.Provider)
	if !ok {
		return nil, auth.Credentials{}, fmt.Errorf("provisioner: unknown provider %q", cfg.Provider)
	}
	c, err := provider.HTTPClient(ctx, creds)
	if err != nil {
		return nil, auth.Credentials{}, fmt.Errorf("provisioner: build %s http client: %w", cfg.Provider, err)
	}
	return c, creds, nil
}
