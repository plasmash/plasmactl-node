package ovh

import (
	"context"
	"fmt"
	"strings"
)

// DiskSpec mirrors provisioner.DiskSpec. Defined separately to keep this
// package self-contained; the factory adapter in
// pkg/provisioner/factory_ovh_metadata.go converts between them.
type DiskSpec struct {
	Type       string
	CapacityGB int
}

// NodeMetadata mirrors provisioner.NodeMetadata.
//
// PublicGateway / PublicPrefix capture OVH's point-to-point public-IP
// routing model: the gateway is in the 100.64.0.0/10 CGN range (NOT in
// the same /24 as the host's public IP), and the host's address is
// routed as /32 regardless of what /specifications/network reports as
// `routing.ipv4.network`. Without these, the inventory plugin's
// Scaleway-pattern derivation (gateway = public_ip[:3] + ".1", mask /24)
// produces a non-functional Flatcar network on OVH dedicated.
type NodeMetadata struct {
	PublicMAC     string
	PrivateMAC    string
	Disks         []DiskSpec
	PublicGateway string
	PublicPrefix  int
	// FailoverIP is a non-primary IPv4 routed to the server (OVH Additional
	// IP, typed `failover` server-side). Empty when no extra IP is routed.
	// First non-primary v4 wins; IPv6 entries are ignored. Operator-driven
	// product: ordered via OVH Manager and routed to a specific server at
	// order time; discoverable post-provision via /dedicated/server/{name}/ips.
	FailoverIP string
}

// MetadataFetcher implements provisioner.NodeMetadataFetcher for OVH.
type MetadataFetcher struct {
	Client *Client
}

// Fetch queries four OVH endpoints to assemble a NodeMetadata for the given
// dedicated server canonical name (e.g. "ns1234567.ip-192-0-2.net"):
//
//	GET /dedicated/server/{name}/networkInterfaceController          → []macAddress
//	GET /dedicated/server/{name}/networkInterfaceController/{mac}    → {mac, linkType, ...}
//	GET /dedicated/server/{name}/specifications/hardware             → {diskGroups: [...]}
//	GET /dedicated/server/{name}/specifications/network              → {routing: {ipv4: {gateway, ...}, ...}}
//
// linkType matching (per OVH NetworkInterfaceControllerLinkTypeEnum):
//   - public-side:  "public" or "public_lag"
//   - private-side: "private" or "private_lag"
//
// Errors if no public-side or no private-side MAC is found, or if the
// network spec lacks a usable ipv4 gateway.
func (f *MetadataFetcher) Fetch(ctx context.Context, serverID string) (*NodeMetadata, error) {
	macs, err := f.fetchMACs(ctx, serverID)
	if err != nil {
		return nil, err
	}
	disks, err := f.fetchDisks(ctx, serverID)
	if err != nil {
		return nil, err
	}
	gw, primary, err := f.fetchPublicRouting(ctx, serverID)
	if err != nil {
		return nil, err
	}
	failover, err := f.fetchFailoverIP(ctx, serverID, primary)
	if err != nil {
		return nil, err
	}
	out := &NodeMetadata{
		Disks:         disks,
		PublicGateway: gw,
		PublicPrefix:  32, // OVH dedicated routes individual /32s on host regardless of routing.ipv4.network
		FailoverIP:    failover,
	}
	for _, m := range macs {
		switch {
		case m.linkType == "public" || m.linkType == "public_lag":
			if out.PublicMAC == "" {
				out.PublicMAC = m.mac
			}
		case m.linkType == "private" || m.linkType == "private_lag":
			if out.PrivateMAC == "" {
				out.PrivateMAC = m.mac
			}
		}
	}
	if out.PublicMAC == "" {
		return nil, fmt.Errorf("ovh: no public-linkType interface found for %s", serverID)
	}
	if out.PrivateMAC == "" {
		return nil, fmt.Errorf("ovh: no private-linkType interface found for %s (vRack-eligible interface required)", serverID)
	}
	return out, nil
}

type macInfo struct{ mac, linkType string }

func (f *MetadataFetcher) fetchMACs(ctx context.Context, serverID string) ([]macInfo, error) {
	var macs []string
	if err := f.Client.GetJSON(ctx, "/dedicated/server/"+serverID+"/networkInterfaceController", &macs); err != nil {
		return nil, fmt.Errorf("list MACs: %w", err)
	}
	out := make([]macInfo, 0, len(macs))
	for _, m := range macs {
		var info struct {
			Mac      string `json:"mac"`
			LinkType string `json:"linkType"`
		}
		if err := f.Client.GetJSON(ctx, "/dedicated/server/"+serverID+"/networkInterfaceController/"+m, &info); err != nil {
			return nil, fmt.Errorf("inspect MAC %s: %w", m, err)
		}
		// Use the detail endpoint's normalized MAC (canonical formatting) over
		// the list-endpoint string used to build the URL.
		out = append(out, macInfo{mac: info.Mac, linkType: info.LinkType})
	}
	return out, nil
}

func (f *MetadataFetcher) fetchDisks(ctx context.Context, serverID string) ([]DiskSpec, error) {
	var hw struct {
		DiskGroups []struct {
			DiskType      string `json:"diskType"`
			NumberOfDisks int    `json:"numberOfDisks"`
			DiskSize      struct {
				Value int    `json:"value"`
				Unit  string `json:"unit"`
			} `json:"diskSize"`
		} `json:"diskGroups"`
	}
	if err := f.Client.GetJSON(ctx, "/dedicated/server/"+serverID+"/specifications/hardware", &hw); err != nil {
		return nil, fmt.Errorf("hardware spec: %w", err)
	}
	var out []DiskSpec
	for _, g := range hw.DiskGroups {
		gb := g.DiskSize.Value
		switch strings.ToUpper(g.DiskSize.Unit) {
		case "TB":
			gb *= 1024
		case "MB":
			gb /= 1024
		// GB is the base unit; leave as-is
		}
		for i := 0; i < g.NumberOfDisks; i++ {
			out = append(out, DiskSpec{Type: g.DiskType, CapacityGB: gb})
		}
	}
	return out, nil
}

// fetchPublicRouting returns the IPv4 gateway and the primary IPv4 from
// /dedicated/server/{name}/specifications/network. Response shape:
// {routing: {ipv4: {network, ip, gateway}, ipv6: {...}}, ...}.
//
// Gateway is required (empty → error). Primary ip may be empty on legacy
// responses; callers tolerating its absence should treat that as "cannot
// detect failover IPs" rather than failing the whole fetch.
//
// The primary IPv4 (`routing.ipv4.ip`) is the bare host address — matches
// the bare-string entry that /dedicated/server/{name}/ips returns once
// stripped of its /32 suffix. That equivalence is what makes failover
// discovery work without a second `/dedicated/server/{name}` summary call.
func (f *MetadataFetcher) fetchPublicRouting(ctx context.Context, serverID string) (gateway, primary string, err error) {
	var spec struct {
		Routing struct {
			IPv4 struct {
				Gateway string `json:"gateway"`
				IP      string `json:"ip"`
			} `json:"ipv4"`
		} `json:"routing"`
	}
	if err := f.Client.GetJSON(ctx, "/dedicated/server/"+serverID+"/specifications/network", &spec); err != nil {
		return "", "", fmt.Errorf("network spec: %w", err)
	}
	if spec.Routing.IPv4.Gateway == "" {
		return "", "", fmt.Errorf("ovh: %s /specifications/network returned empty routing.ipv4.gateway", serverID)
	}
	return spec.Routing.IPv4.Gateway, spec.Routing.IPv4.IP, nil
}

// fetchFailoverIP queries /dedicated/server/{name}/ips and returns the
// first IPv4 entry that isn't the primary, stripped of its /32 suffix.
// Returns "" with no error when no extra v4 is routed (normal state for
// non-ingress nodes).
//
// Verified response shape: flat JSON array of CIDR strings — primary
// IPv4 /32, optional additional IPv4 /32 entries, optional IPv6 /56
// prefix, e.g.
//
//	["192.0.2.14/32","203.0.113.7/32","2001:db8:0:100::/56"]
//
// IPv6 prefixes are ignored — failover IP / Additional IP product is
// IPv4-only on OVH dedicated, and inbound v6 ingress isn't in scope.
// We don't probe /ip/service/ip-{addr} to confirm type=="failover"; the
// listing is authoritative for routing, and the heuristic "any non-primary
// v4 routed to this server" is more robust to OVH's evolving taxonomy
// (Additional IPs default to type=failover post-2024 but the API enum
// still permits other types). If we later need to discriminate between
// failover (movable) and dedicated (sticky) additional IPs, that GET goes
// here.
func (f *MetadataFetcher) fetchFailoverIP(ctx context.Context, serverID, primary string) (string, error) {
	var ips []string
	if err := f.Client.GetJSON(ctx, "/dedicated/server/"+serverID+"/ips", &ips); err != nil {
		return "", fmt.Errorf("list ips: %w", err)
	}
	for _, raw := range ips {
		if strings.Contains(raw, ":") {
			continue // IPv6 prefix
		}
		addr := strings.TrimSuffix(raw, "/32")
		if addr == primary {
			continue
		}
		return addr, nil
	}
	return "", nil
}
