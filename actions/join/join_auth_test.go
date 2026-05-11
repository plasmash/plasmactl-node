package join

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/launchrctl/keyring"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Side-effect: register OVH and Scaleway providers in the auth registry.
	_ "github.com/plasmash/plasmactl-auth/pkg/auth"
)

func newTestKeyring(t *testing.T) keyring.Keyring {
	t.Helper()
	return keyring.NewService(keyring.NewFileStore(nil), nil)
}

// setupOVHFixture creates a minimal platforms/<name>/ tree with platform.yaml
// for an OVH-provider single-control-pool fixture, then chdirs into the temp
// dir (restoring the previous cwd via t.Cleanup). Returns the temp dir.
func setupOVHFixture(t *testing.T, platformName string) string {
	t.Helper()
	dir := t.TempDir()
	platforms := filepath.Join(dir, "platforms", platformName)
	require.NoError(t, os.MkdirAll(filepath.Join(platforms, "nodes"), 0755))
	pyaml := []byte(`name: ` + platformName + `
infrastructure:
  metal_provider: ovh
  api:
    client_id: cid
    client_secret: csec
pools:
  control:
    zones: [platform.foundation.cluster.control]
    machine: advance-1
    count: 1
`)
	require.NoError(t, os.WriteFile(filepath.Join(platforms, "platform.yaml"), pyaml, 0644))
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	require.NoError(t, os.Chdir(dir))
	return dir
}

func TestResolveProviderCreds_NewShape_OVH(t *testing.T) {
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
	pc, err := resolveProviderCreds(context.Background(), platform, "/tmp", k)
	require.NoError(t, err)
	assert.Equal(t, "CID", pc.OVHClientID)
	assert.Equal(t, "CSEC", pc.OVHClientSecret)
	assert.Equal(t, "ovh-eu", pc.OVHEndpoint)
	assert.Empty(t, pc.APIToken)
}

func TestResolveProviderCreds_NewShape_Scaleway(t *testing.T) {
	k := newTestKeyring(t)
	for _, kv := range []keyring.KeyValueItem{
		{Key: "scaleway_access_key", Value: "AK"},
		{Key: "scaleway_secret_key", Value: "SK"},
	} {
		require.NoError(t, k.AddItem(kv))
	}
	platform := schema.Platform{
		Name: "p",
		Infrastructure: schema.Infrastructure{
			MetalProvider: "scaleway",
			API: schema.APIConfig{
				AccessKey: "{{ .keyring.scaleway_access_key }}",
				SecretKey: "{{ .keyring.scaleway_secret_key }}",
			},
		},
	}
	pc, err := resolveProviderCreds(context.Background(), platform, "/tmp", k)
	require.NoError(t, err)
	assert.Equal(t, "AK", pc.ScalewayAccessKey)
	assert.Equal(t, "SK", pc.ScalewaySecretKey)
	assert.Empty(t, pc.APIToken)
}

func TestResolveProviderCreds_LegacyShape_OVH(t *testing.T) {
	k := newTestKeyring(t)
	require.NoError(t, k.AddItem(keyring.CredentialsItem{
		URL: "ovh", Username: "token", Password: "LEGACY-TOKEN",
	}))
	platform := schema.Platform{
		Name: "p",
		Infrastructure: schema.Infrastructure{
			MetalProvider: "ovh",
			API:           schema.APIConfig{Token: "{{ .keyring.ovh_api_token }}"},
		},
	}
	pc, err := resolveProviderCreds(context.Background(), platform, "/tmp", k)
	require.NoError(t, err)
	assert.Equal(t, "LEGACY-TOKEN", pc.APIToken)
	assert.Empty(t, pc.OVHClientID)
}

func TestResolveProviderCreds_NoCredsConfigured(t *testing.T) {
	k := newTestKeyring(t)
	platform := schema.Platform{
		Name: "p",
		Infrastructure: schema.Infrastructure{
			MetalProvider: "ovh",
			API:           schema.APIConfig{},
		},
	}
	_, err := resolveProviderCreds(context.Background(), platform, "/tmp", k)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no credentials")
}

func TestJoin_RejectsMissingHostname(t *testing.T) {
	setupOVHFixture(t, "example-plat")

	j := &Join{
		Platform: "example-plat",
		ServerID: "ns1234567.ip-192-0-2.net",
		Pool:     "control",
		Hostname: "", // missing
	}
	err := j.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostname")
}

func TestJoin_RejectsMalformedServerID_OVH(t *testing.T) {
	setupOVHFixture(t, "example-plat")

	j := &Join{
		Platform: "example-plat",
		ServerID: "499820", // numeric admin ID, NOT canonical service name
		Pool:     "control",
		Hostname: "example-plat-1234567-control",
	}
	err := j.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server-id")
	assert.Contains(t, err.Error(), "ns")
}

func TestJoin_StubFilenameIsServerID(t *testing.T) {
	dir := setupOVHFixture(t, "example-plat")
	nodesDir := filepath.Join(dir, "platforms", "example-plat", "nodes")

	// TF init/plan will fail (no real provider) and that's fine — we check
	// the stub path before that point.
	j := &Join{
		Platform: "example-plat",
		ServerID: "ns1234567.ip-192-0-2.net",
		Pool:     "control",
		Hostname: "example-plat-1234567-control",
		DryRun:   true,
	}
	_ = j.Execute()

	// Definitively NOT at the hostname-keyed path.
	dontWantStub := filepath.Join(nodesDir, "example-plat-1234567-control.yaml")
	_, errWrong := os.Stat(dontWantStub)
	assert.True(t, os.IsNotExist(errWrong), "stub must not be at hostname-keyed path: %s", dontWantStub)
}

func TestJoin_StubRemovedOnTFFailure(t *testing.T) {
	dir := setupOVHFixture(t, "example-plat")
	nodesDir := filepath.Join(dir, "platforms", "example-plat", "nodes")

	j := &Join{
		Platform: "example-plat",
		ServerID: "ns1234567.ip-192-0-2.net",
		Pool:     "control",
		Hostname: "example-plat-1234567-control",
		DryRun:   true,
	}
	// TF init/plan will fail (no real provider, no tofu binary in test env).
	// We don't care about the exit error; we care that the stub is gone.
	err := j.Execute()
	assert.Error(t, err, "expected Execute to fail when TF cannot run in test env")

	stub := filepath.Join(nodesDir, "ns1234567.ip-192-0-2.net.yaml")
	_, err = os.Stat(stub)
	assert.True(t, os.IsNotExist(err), "stub must be cleaned up after TF failure, but found at %s", stub)
}

func TestOVHServiceNameRegex(t *testing.T) {
	// Valid format passes the regex (further validation by TF still applies).
	assert.True(t, ovhServiceNameRE.MatchString("ns1234567.ip-192-0-2.net"))
	assert.True(t, ovhServiceNameRE.MatchString("ns123.ip-1-2-3.net"))
	assert.False(t, ovhServiceNameRE.MatchString("499820"))
	assert.False(t, ovhServiceNameRE.MatchString("ns1234567.ip-192-0-2"))    // missing .net
	assert.False(t, ovhServiceNameRE.MatchString("foo.ip-192-0-2-214.net"))  // wrong prefix
}

func TestScanExistingForJoin_MatchesByZones_NotHostname(t *testing.T) {
	dir := t.TempDir()
	nodesDir := filepath.Join(dir, "nodes")
	require.NoError(t, os.MkdirAll(nodesDir, 0755))

	// Stub yaml with a hostname that does NOT follow the legacy
	// <env>-<pool>-<NNN> shape — operator-provided per the canonical-server-id
	// naming work. The pool must be derived from the zones list, not the
	// hostname.
	stubYAML := []byte(`hostname: example-plat-1234567-control
zones:
  - platform
  - platform.foundation.cluster.control
machine: advance-1
provider_metadata:
  server_id: ns1234567.ip-192-0-2.net
`)
	require.NoError(t, os.WriteFile(filepath.Join(nodesDir, "ns1234567.ip-192-0-2.net.yaml"), stubYAML, 0644))

	pools := map[string]schema.Pool{
		"control": {
			Zones:   []string{"platform", "platform.foundation.cluster.control"},
			Machine: "advance-1",
			Count:   3,
		},
	}

	out := scanExistingForJoin(nodesDir, pools)
	require.Len(t, out, 1, "stub yaml with canonical-server-id naming should be matched")
	assert.Equal(t, "control", out[0].Pool)
	assert.Equal(t, "ns1234567.ip-192-0-2.net", out[0].ImportID)
	assert.Equal(t, 0, out[0].Index, "single node in pool gets Index=0")
}

func TestScanExistingForJoin_AssignsIndexByServerIDOrder(t *testing.T) {
	dir := t.TempDir()
	nodesDir := filepath.Join(dir, "nodes")
	require.NoError(t, os.MkdirAll(nodesDir, 0755))

	// Three stubs in the same pool with deliberately out-of-sort filenames
	// so the test catches accidental reliance on directory-walk order.
	for _, sid := range []string{"ns1234569.ip-198-51-100.net", "ns1234567.ip-192-0-2.net", "ns1234568.ip-198-51-100.net"} {
		stubYAML := []byte(`hostname: example-plat-` + sid[2:9] + `-control
zones:
  - platform
  - platform.foundation.cluster.control
machine: advance-1
provider_metadata:
  server_id: ` + sid + `
`)
		require.NoError(t, os.WriteFile(filepath.Join(nodesDir, sid+".yaml"), stubYAML, 0644))
	}

	pools := map[string]schema.Pool{
		"control": {
			Zones:   []string{"platform", "platform.foundation.cluster.control"},
			Machine: "advance-1",
			Count:   3,
		},
	}

	out := scanExistingForJoin(nodesDir, pools)
	require.Len(t, out, 3)

	// Indices must be 0, 1, 2 with no gaps and no duplicates, regardless of
	// which Index any specific server got assigned. Sort by Index and confirm.
	indices := []int{out[0].Index, out[1].Index, out[2].Index}
	got := map[int]bool{}
	for _, i := range indices {
		got[i] = true
	}
	assert.Equal(t, map[int]bool{0: true, 1: true, 2: true}, got, "indices must be 0..N-1, unique within pool")
}
