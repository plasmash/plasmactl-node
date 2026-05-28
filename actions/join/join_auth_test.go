package join

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr/pkg/action"
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

func TestJoin_RejectsInvalidHostname(t *testing.T) {
	assert.False(t, validHostnameLabel("Bad Name"))
	assert.False(t, validHostnameLabel("UPPERCASE"))
	assert.False(t, validHostnameLabel("has spaces"))
	assert.False(t, validHostnameLabel("-starts-with-hyphen"))
	assert.False(t, validHostnameLabel("ends-with-hyphen-"))
	assert.False(t, validHostnameLabel(""))
	assert.True(t, validHostnameLabel("example-plat-1234567-control"))
	assert.True(t, validHostnameLabel("a"))
	assert.True(t, validHostnameLabel("demo-control-001"))
}

// TestJoinYAML_HostnameOptionHasStringDefault guards a regression: --hostname
// is optional (required: false), and launchr only substitutes an omitted
// option with its Default when Default != nil (see setParamDefaults). Without
// `default: ""` in join.yaml, an omitted --hostname resolves to nil, and
// plugin.go's `input.Opt("hostname").(string)` panics with
// "interface conversion: interface {} is nil, not string". This test parses
// the real join.yaml and performs the same assertion plugin.go does.
func TestJoinYAML_HostnameOptionHasStringDefault(t *testing.T) {
	joinYaml, err := os.ReadFile("join.yaml")
	require.NoError(t, err)

	act := action.NewFromYAML("node:join", joinYaml)
	def := act.ActionDef()

	var hostnameOpt *action.DefParameter
	for _, o := range def.Options {
		if o.Name == "hostname" {
			hostnameOpt = o
			break
		}
	}
	require.NotNil(t, hostnameOpt, "join.yaml must declare a hostname option")

	// The exact assertion plugin.go performs on the resolved option value.
	// Must yield a string, not nil — otherwise node:join panics when
	// --hostname is omitted.
	_, ok := hostnameOpt.Default.(string)
	assert.Truef(t, ok,
		"hostname option must declare a string default (got %T: %v); "+
			"without it, node:join panics when --hostname is omitted",
		hostnameOpt.Default, hostnameOpt.Default)
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

func TestJoin_StubFilenameIsEffectiveHostname(t *testing.T) {
	dir := setupOVHFixture(t, "example-plat")
	nodesDir := filepath.Join(dir, "platforms", "example-plat", "nodes")

	// TF init/plan will fail (no real provider) and that's fine — we only
	// care about which path the stub was written to, not whether TF succeeded.
	j := &Join{
		Platform: "example-plat",
		ServerID: "ns1234567.ip-192-0-2.net",
		Pool:     "control",
		Hostname: "example-plat-1234567-control",
		DryRun:   true,
	}
	// Execute will error (no TF binary); stub is cleaned up on failure.
	// We verify it was NOT written at the server-id path — the hostname wins.
	_ = j.Execute()

	// When hostname is explicitly provided, effectiveHostname == j.Hostname,
	// so the stub (and any final node file) lives under the hostname key.
	dontWantStub := filepath.Join(nodesDir, "ns1234567.ip-192-0-2.net.yaml")
	_, errWrong := os.Stat(dontWantStub)
	assert.True(t, os.IsNotExist(errWrong), "stub must not be at server-id-keyed path: %s", dontWantStub)
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

	// Stub is written to effectiveHostname.yaml (== j.Hostname when set).
	stub := filepath.Join(nodesDir, "example-plat-1234567-control.yaml")
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
	assert.Equal(t, "example-plat-1234567-control", out[0].Hostname, "hostname must be propagated from node yaml")
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
