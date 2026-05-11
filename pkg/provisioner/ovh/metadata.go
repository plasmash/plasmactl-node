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
	gw, err := f.fetchPublicGateway(ctx, serverID)
	if err != nil {
		return nil, err
	}
	out := &NodeMetadata{
		Disks:         disks,
		PublicGateway: gw,
		PublicPrefix:  32, // OVH dedicated routes individual /32s on host regardless of routing.ipv4.network
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

// fetchPublicGateway returns the IPv4 gateway from
// /dedicated/server/{name}/specifications/network. OVH's response shape
// is {routing: {ipv4: {network, ip, gateway}, ipv6: {...}}, ...}. We
// only need the ipv4 gateway today; the rest (ipv6, OLA, vrack) is left
// for a future caller. Errors if the response lacks a non-empty
// routing.ipv4.gateway — without it there's no way to write a working
// Flatcar default route.
func (f *MetadataFetcher) fetchPublicGateway(ctx context.Context, serverID string) (string, error) {
	var net struct {
		Routing struct {
			IPv4 struct {
				Gateway string `json:"gateway"`
			} `json:"ipv4"`
		} `json:"routing"`
	}
	if err := f.Client.GetJSON(ctx, "/dedicated/server/"+serverID+"/specifications/network", &net); err != nil {
		return "", fmt.Errorf("network spec: %w", err)
	}
	if net.Routing.IPv4.Gateway == "" {
		return "", fmt.Errorf("ovh: %s /specifications/network returned empty routing.ipv4.gateway", serverID)
	}
	return net.Routing.IPv4.Gateway, nil
}
