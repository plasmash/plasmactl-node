package provisioner

import (
	"context"
	"fmt"

	"github.com/plasmash/plasmactl-node/pkg/provisioner/ovh"
)

func newOVHDisplayNameFetcher(ctx context.Context, cfg Config) (DisplayNameFetcher, error) {
	c, creds, err := providerHTTPClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	base := ovhAPIBase(creds.OVHAccountRegion)
	if base == "" {
		return nil, fmt.Errorf("provisioner/ovh: unrecognised region %q (expected eu|ca|us)", creds.OVHAccountRegion)
	}
	return &ovh.DisplayNameFetcher{
		Client: &ovh.Client{HTTP: c, BaseURL: base},
	}, nil
}
