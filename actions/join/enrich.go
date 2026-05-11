// enrich.go — node:join enrichment phase.
//
// After the TF apply step, this enriches the node yaml fields that TF
// outputs do not provide for adopted OVH dedicated servers (specifically:
// private_ip, public_mac, private_mac, disks). Also reconciles the
// server's vRack membership when the provider supports it.
//
// Provider abstraction lives in pkg/provisioner; this file just wires the
// pieces together with IPAllocator (existing).

package join

import (
	"context"
	"fmt"

	"github.com/plasmash/plasmactl-node/internal/allocator"
	provisioner "github.com/plasmash/plasmactl-node/pkg/provisioner"
)

type enrichInput struct {
	Provider          string                          // platform.Infrastructure.MetalProvider
	PlatformName      string                          // platform.Name
	ServerID          string                          // canonical server ID
	VLANID            int                             // platform.Infrastructure.PrivateVLANID
	ExistingPrivateIP string                          // joined.PrivateIP from TF (often empty for adopted OVH)
	PrivateNetwork    string                          // platform.Networking.PrivateNetwork (CIDR)
	NodesDir          string                          // platforms/<name>/nodes
	Fetcher           provisioner.NodeMetadataFetcher // nil ⇒ caller skipped or no impl
	Reconciler        provisioner.VRackReconciler     // nil ⇒ provider has no vrack-like fabric
}

type enrichOutput struct {
	PublicMAC  string
	PrivateMAC string
	PrivateIP  string
	// Disks are /dev/... paths synthesized from DiskSpec.Type via
	// provisioner.SynthesizeDiskPaths. node.Node.Disks is []string, so we
	// flatten to paths here rather than carrying the structured form.
	Disks []string
	// PublicGateway / PublicPrefix come from the provider's network spec
	// (e.g. OVH /specifications/network routing.ipv4.gateway). Empty/zero
	// means the inventory plugin will fall back to derivation.
	PublicGateway string
	PublicPrefix  int
}

// enrichNodeFields runs the post-TF-apply enrichment. Returns populated
// fields ready to write into node.Node + Network. Errors if Fetcher is nil
// (provider has no impl yet — caller must decide whether to skip the join
// or surface the error).
func enrichNodeFields(ctx context.Context, in enrichInput) (*enrichOutput, error) {
	if in.Fetcher == nil {
		return nil, fmt.Errorf("enrichNodeFields: nil Fetcher (provider %q has no impl)", in.Provider)
	}

	meta, err := in.Fetcher.Fetch(ctx, in.ServerID)
	if err != nil {
		return nil, fmt.Errorf("metadata fetch: %w", err)
	}

	if in.Reconciler != nil {
		if meta.PrivateMAC == "" {
			return nil, fmt.Errorf("metadata fetcher returned empty PrivateMAC; cannot reconcile vRack")
		}
		if err := in.Reconciler.Reconcile(ctx, in.PlatformName, in.VLANID, in.ServerID, meta.PrivateMAC); err != nil {
			return nil, fmt.Errorf("vrack reconcile: %w", err)
		}
	}

	// IP allocation: only allocate if TF didn't provide one (always true for
	// adopted OVH servers — TF leaves PrivateIP empty for imported
	// ovh_dedicated_server resources because order-block fields aren't
	// repopulated by Read).
	privateIP := in.ExistingPrivateIP
	if privateIP == "" {
		ipAlloc, err := allocator.NewIPAllocator(in.PrivateNetwork, in.NodesDir)
		if err != nil {
			return nil, fmt.Errorf("ip allocator: %w", err)
		}
		ip, err := ipAlloc.Allocate()
		if err != nil {
			return nil, fmt.Errorf("ip alloc: %w", err)
		}
		privateIP = ip
	}

	// Disk path synthesis: NVMe vs SATA decision is based on the first disk
	// (mirrors legacy platform_nodes.py disk-path-resolution behavior).
	var disks []string
	if len(meta.Disks) > 0 {
		disks = provisioner.SynthesizeDiskPaths(meta.Disks[0].Type, len(meta.Disks))
	}

	return &enrichOutput{
		PublicMAC:     meta.PublicMAC,
		PrivateMAC:    meta.PrivateMAC,
		PrivateIP:     privateIP,
		Disks:         disks,
		PublicGateway: meta.PublicGateway,
		PublicPrefix:  meta.PublicPrefix,
	}, nil
}
