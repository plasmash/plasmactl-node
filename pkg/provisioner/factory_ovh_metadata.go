package provisioner

import (
	"context"
	"fmt"

	"github.com/plasmash/plasmactl-node/pkg/provisioner/ovh"
)

// newOVHMetadataFetcher mints an OAuth2-Bearer-backed OVH client via
// plasmactl-auth and returns an adapter that converts ovh.NodeMetadata to
// the provisioner-package's canonical NodeMetadata type.
func newOVHMetadataFetcher(ctx context.Context, cfg Config) (NodeMetadataFetcher, error) {
	c, creds, err := providerHTTPClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	base := ovhAPIBase(creds.OVHAccountRegion)
	if base == "" {
		return nil, fmt.Errorf("provisioner/ovh: unrecognised region %q (expected eu|ca|us)", creds.OVHAccountRegion)
	}
	return &ovhMetadataFetcherAdapter{
		f: &ovh.MetadataFetcher{
			Client: &ovh.Client{HTTP: c, BaseURL: base},
		},
	}, nil
}

// ovhMetadataFetcherAdapter converts ovh.NodeMetadata → provisioner.NodeMetadata.
type ovhMetadataFetcherAdapter struct{ f *ovh.MetadataFetcher }

func (a *ovhMetadataFetcherAdapter) Fetch(ctx context.Context, serverID string) (*NodeMetadata, error) {
	m, err := a.f.Fetch(ctx, serverID)
	if err != nil {
		return nil, err
	}
	disks := make([]DiskSpec, len(m.Disks))
	for i, d := range m.Disks {
		disks[i] = DiskSpec{Type: d.Type, CapacityGB: d.CapacityGB}
	}
	return &NodeMetadata{
		PublicMAC:     m.PublicMAC,
		PrivateMAC:    m.PrivateMAC,
		Disks:         disks,
		PublicGateway: m.PublicGateway,
		PublicPrefix:  m.PublicPrefix,
		FailoverIP:    m.FailoverIP,
	}, nil
}

// ovhAPIBase maps an OVH account region to its API base URL.
// Mirrors plasmactl-auth's internal ovhAPIBase so we don't need to export it.
func ovhAPIBase(region string) string {
	switch region {
	case "eu":
		return "https://eu.api.ovh.com/1.0"
	case "ca":
		return "https://ca.api.ovh.com/1.0"
	case "us":
		return "https://us.api.ovhcloud.com/1.0"
	}
	return ""
}
