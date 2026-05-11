package provisioner

import (
	"context"
	"fmt"

	"github.com/plasmash/plasmactl-node/pkg/provisioner/ovh"
)

// newOVHVRackReconciler mints an OAuth2-Bearer-backed OVH client via
// plasmactl-auth and returns an adapter wrapping ovh.VRackReconciler.
func newOVHVRackReconciler(ctx context.Context, cfg Config) (VRackReconciler, error) {
	c, creds, err := providerHTTPClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	base := ovhAPIBase(creds.OVHAccountRegion)
	if base == "" {
		return nil, fmt.Errorf("provisioner/ovh: unrecognised region %q (expected eu|ca|us)", creds.OVHAccountRegion)
	}
	return &ovhVRackAdapter{
		r: &ovh.VRackReconciler{
			Client: &ovh.Client{HTTP: c, BaseURL: base},
		},
	}, nil
}

type ovhVRackAdapter struct{ r *ovh.VRackReconciler }

func (a *ovhVRackAdapter) Reconcile(ctx context.Context, platformName string, vlanID int, serverID, privateMAC string) error {
	return a.r.Reconcile(ctx, platformName, vlanID, serverID, privateMAC)
}
