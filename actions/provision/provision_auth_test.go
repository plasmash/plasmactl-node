package provision

import (
	"context"
	"testing"

	"github.com/launchrctl/keyring"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/plasmash/plasmactl-auth/pkg/auth"
)

func newTestKeyring(t *testing.T) keyring.Keyring {
	t.Helper()
	return keyring.NewService(keyring.NewFileStore(nil), nil)
}

func TestResolveProvisionCreds_NewShape_OVH(t *testing.T) {
	k := newTestKeyring(t)
	for _, kv := range []keyring.KeyValueItem{
		{Key: "ovh_client_id", Value: "CID"},
		{Key: "ovh_client_secret", Value: "CSEC"},
		{Key: "ovh_account_region", Value: "eu"},
	} {
		require.NoError(t, k.AddItem(kv))
	}
	platform := schema.Platform{
		Name: "p",
		Infrastructure: schema.Infrastructure{
			MetalProvider: "ovh",
			API: schema.APIConfig{
				ClientID:     "{{ .keyring.ovh_client_id }}",
				ClientSecret: "{{ .keyring.ovh_client_secret }}",
			},
		},
	}
	pc, err := resolveProvisionCreds(context.Background(), platform, "/tmp", k)
	require.NoError(t, err)
	assert.Equal(t, "CID", pc.OVHClientID)
	assert.Equal(t, "ovh-eu", pc.OVHEndpoint)
	assert.Empty(t, pc.APIToken)
}

func TestResolveProvisionCreds_LegacyShape_OVH(t *testing.T) {
	k := newTestKeyring(t)
	// CredentialsItem.isEmpty rejects empty Username — use a placeholder.
	require.NoError(t, k.AddItem(keyring.CredentialsItem{
		URL: "ovh", Username: "token", Password: "T",
	}))
	platform := schema.Platform{
		Name: "p",
		Infrastructure: schema.Infrastructure{
			MetalProvider: "ovh",
			API:           schema.APIConfig{Token: "{{ .keyring.ovh_api_token }}"},
		},
	}
	pc, err := resolveProvisionCreds(context.Background(), platform, "/tmp", k)
	require.NoError(t, err)
	assert.Equal(t, "T", pc.APIToken)
}

func TestResolveProvisionCreds_NoCreds(t *testing.T) {
	k := newTestKeyring(t)
	platform := schema.Platform{
		Name: "p",
		Infrastructure: schema.Infrastructure{MetalProvider: "ovh"},
	}
	_, err := resolveProvisionCreds(context.Background(), platform, "/tmp", k)
	require.Error(t, err)
}

func TestResolveDNSCreds_NewShape_OVH(t *testing.T) {
	k := newTestKeyring(t)
	for _, kv := range []keyring.KeyValueItem{
		{Key: "ovh_client_id", Value: "CID"},
		{Key: "ovh_client_secret", Value: "CSEC"},
		{Key: "ovh_account_region", Value: "eu"},
	} {
		require.NoError(t, k.AddItem(kv))
	}
	platform := schema.Platform{
		Infrastructure: schema.Infrastructure{MetalProvider: "ovh"},
		DNS: schema.DNSConfig{
			Provider: "ovh",
			API: schema.APIConfig{
				ClientID:     "{{ .keyring.ovh_client_id }}",
				ClientSecret: "{{ .keyring.ovh_client_secret }}",
			},
		},
	}
	c, err := resolveDNSCredsV2(context.Background(), platform, "/tmp", k)
	require.NoError(t, err)
	assert.Equal(t, "CID", c.OVHClientID)
	assert.Equal(t, "ovh-eu", c.OVHEndpoint)
	assert.Empty(t, c.Token)
}

func TestResolveDNSCreds_LegacyShape_Cloudflare(t *testing.T) {
	k := newTestKeyring(t)
	require.NoError(t, k.AddItem(keyring.CredentialsItem{
		URL: "cloudflare", Username: "user", Password: "TOKEN",
	}))
	platform := schema.Platform{
		Infrastructure: schema.Infrastructure{MetalProvider: "cloudflare"},
		DNS: schema.DNSConfig{
			Provider: "cloudflare",
			API:      schema.APIConfig{Token: "{{ .keyring.cloudflare_api_token }}"},
		},
	}
	c, err := resolveDNSCredsV2(context.Background(), platform, "/tmp", k)
	require.NoError(t, err)
	assert.Equal(t, "TOKEN", c.Token)
	assert.Equal(t, "user", c.Username)
	assert.Empty(t, c.OVHClientID)
}
