package join

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-auth/pkg/auth"
	"github.com/plasmash/plasmactl-platform/pkg/provisioner"
	"github.com/plasmash/plasmactl-node/pkg/node"
	nodeprov "github.com/plasmash/plasmactl-node/pkg/provisioner"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// ovhServiceNameRE matches OVH dedicated server canonical names of the form
// nsNNNNNN.ip-A-B-C.net (the value returned by GET /dedicated/server and
// expected as path parameter on /dedicated/server/{serviceName}).
var ovhServiceNameRE = regexp.MustCompile(`^ns\d+\.ip-(\d{1,3}-){2}\d{1,3}\.net$`)

// JoinResult is the structured output for node:join.
type JoinResult struct {
	Hostname  string   `json:"hostname"`
	Platform  string   `json:"platform"`
	Pool      string   `json:"pool"`
	ServerID  string   `json:"server_id"`
	PublicIP  string   `json:"public_ip,omitempty"`
	PrivateIP string   `json:"private_ip,omitempty"`
	Zones     []string `json:"zones,omitempty"`
	File      string   `json:"file"`
	DryRun    bool     `json:"dry_run"`
}

// Join implements the node:join command. It adopts an existing provider
// resource by writing a stub node YAML with provider_metadata.server_id,
// then invoking the provisioner in ImportOnly mode so TF import blocks
// adopt the resource without creating anything new.
type Join struct {
	action.WithLogger
	action.WithTerm

	Keyring     keyring.Keyring
	Platform    string
	ServerID    string
	Pool        string
	Hostname    string
	DryRun                 bool
	AutoApprove            bool
	AllowDestructiveUpdate bool

	result *JoinResult
}

// Result returns the structured result for JSON output.
func (j *Join) Result() any {
	return j.result
}

// Execute runs the node:join action.
func (j *Join) Execute() error {
	platformDir := filepath.Join("platforms", j.Platform)
	platformFile := filepath.Join(platformDir, "platform.yaml")
	nodesDir := filepath.Join(platformDir, "nodes")

	if _, err := os.Stat(platformDir); os.IsNotExist(err) {
		return fmt.Errorf("platform %q not found", j.Platform)
	}

	platformData, err := os.ReadFile(platformFile)
	if err != nil {
		return fmt.Errorf("failed to read platform.yaml: %w", err)
	}

	var platform schema.Platform
	if err := yaml.Unmarshal(platformData, &platform); err != nil {
		return fmt.Errorf("failed to parse platform.yaml: %w", err)
	}

	if platform.Infrastructure.MetalProvider == "manual" {
		return fmt.Errorf("cannot join a node under manual provider - use node:add to register manual nodes")
	}

	pool, ok := platform.Pools[j.Pool]
	if !ok {
		return fmt.Errorf("pool %q not defined in platform.yaml (known pools: %s)", j.Pool, strings.Join(poolNames(platform.Pools), ", "))
	}

	switch platform.Infrastructure.MetalProvider {
	case "ovh":
		if !ovhServiceNameRE.MatchString(j.ServerID) {
			return fmt.Errorf("--server-id %q is not a valid OVH service name (expected ns<N>.ip-<a>-<b>-<c>.net, e.g. ns1234567.ip-192-0-2.net). Use the canonical name from GET /dedicated/server, not the numeric admin-panel ID", j.ServerID)
		}
	}

	if existing, file := findNodeByServerID(nodesDir, j.ServerID); existing != "" {
		return fmt.Errorf("server_id %q already joined as node %s (%s)", j.ServerID, existing, file)
	}

	if strings.TrimSpace(j.Hostname) == "" {
		return fmt.Errorf("--hostname is required (cluster-side hostname for the joined node)")
	}

	nodeFile := filepath.Join(nodesDir, j.ServerID+".yaml")
	if _, err := os.Stat(nodeFile); !os.IsNotExist(err) {
		return fmt.Errorf("node yaml for server-id %q already exists at %s", j.ServerID, nodeFile)
	}

	if err := os.MkdirAll(nodesDir, 0755); err != nil {
		return fmt.Errorf("failed to create nodes directory: %w", err)
	}

	// Stub node YAML — minimal data needed for the provisioner to discover
	// the pending adoption via scanExistingNodes.
	stub := &node.Node{
		Hostname: j.Hostname,
		Zones:    pool.Zones,
		Machine:  pool.Machine,
		ProviderMetadata: node.ProviderMetadata{
			ServerID: j.ServerID,
		},
	}
	stub.AddZoneLabels()

	stubData, err := yaml.Marshal(stub)
	if err != nil {
		return fmt.Errorf("failed to marshal stub node: %w", err)
	}
	if err := os.WriteFile(nodeFile, stubData, 0644); err != nil {
		return fmt.Errorf("failed to write stub node file: %w", err)
	}

	// Track whether we've successfully reached the final write. Until then,
	// the stub is transient — on TF failure or dry-run exit, remove it so a
	// retry isn't blocked by a leftover file.
	committed := false
	defer func() {
		if committed {
			return
		}
		if err := os.Remove(nodeFile); err != nil && !os.IsNotExist(err) {
			j.Log().Warn("failed to remove stub file on cleanup", "file", nodeFile, "error", err)
		}
	}()

	j.Term().Info().Printfln("Joining server %q into pool %q as %s", j.ServerID, j.Pool, j.Hostname)

	credsConfig, err := resolveProviderCreds(context.Background(), platform, platformDir, j.Keyring)
	if err != nil {
		return err
	}

	existingNodes := scanExistingForJoin(nodesDir, platform.Pools)

	config := provisioner.ProviderConfig{
		EnvName:           j.Platform,
		Pools:             poolsToSpecs(platform.Pools),
		Provider:          platform.Infrastructure.MetalProvider,
		APIToken:          credsConfig.APIToken,
		OVHClientID:       credsConfig.OVHClientID,
		OVHClientSecret:   credsConfig.OVHClientSecret,
		OVHEndpoint:       credsConfig.OVHEndpoint,
		ScalewayAccessKey: credsConfig.ScalewayAccessKey,
		ScalewaySecretKey: credsConfig.ScalewaySecretKey,
		Zone:              platform.Infrastructure.Zone,
		Region:            platform.Infrastructure.Region,
		ProjectID:         platform.Infrastructure.ProjectID,
		Image:             platform.Infrastructure.Image,
		SSHKeyID:          platform.Infrastructure.SSHKeyID,
		ExistingNodes:     existingNodes,
		ImportOnly:        true,
	}

	mgr, err := provisioner.NewManager(j.Platform, j.DryRun, j.AutoApprove)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}
	defer func() {
		if cerr := mgr.Close(); cerr != nil {
			j.Log().Warn("failed to clean up provisioner state", "error", cerr)
		}
	}()

	if err := mgr.CleanState(); err != nil {
		return fmt.Errorf("failed to clean provisioning state: %w", err)
	}

	if err := mgr.GenerateHCL(config); err != nil {
		return fmt.Errorf("failed to generate HCL: %w", err)
	}

	ctx := context.Background()

	j.Term().Info().Println("Initializing provisioner...")
	if err := mgr.Init(ctx); err != nil {
		return fmt.Errorf("provisioner init failed: %w", err)
	}

	j.Term().Info().Println("Planning adoption...")
	hasChanges, err := mgr.Plan(ctx)
	if err != nil {
		return fmt.Errorf("provisioner plan failed: %w", err)
	}

	// Best-effort plan diff print: degraded UX on failure (we still classify).
	if planText, terr := mgr.PlanText(ctx); terr == nil {
		j.Term().Info().Println(planText)
	} else {
		j.Log().Warn("could not render plan diff", "error", terr)
	}

	if !hasChanges {
		j.Term().Info().Println("Nothing to apply — state already aligned")
	}

	if j.DryRun {
		j.Term().Warning().Println("Dry run — skipping apply")
		j.result = &JoinResult{
			Hostname: j.Hostname,
			Platform: j.Platform,
			Pool:     j.Pool,
			ServerID: j.ServerID,
			Zones:    pool.Zones,
			File:     nodeFile,
			DryRun:   true,
		}
		return nil
	}

	// Classify the plan and route through the confirmation gate.
	summary, serr := mgr.PlanSummary(ctx)
	if serr != nil {
		return fmt.Errorf("could not classify plan changes: %w", serr)
	}
	proceed, cerr := classifyAndConfirm(j, summary, newStdinPrompter(os.Stdout))
	if cerr != nil {
		return cerr
	}
	if !proceed {
		j.Term().Info().Println("Aborted.")
		return nil
	}

	if err := mgr.ApplyPlan(ctx); err != nil {
		return fmt.Errorf("provisioner apply failed: %w", err)
	}

	servers, err := mgr.GetOutputs(ctx)
	if err != nil {
		return fmt.Errorf("failed to read provisioning outputs: %w", err)
	}

	var joined *provisioner.ServerOutput
	for idx, s := range servers {
		if s.ServerID == j.ServerID {
			joined = &servers[idx]
			break
		}
	}
	if joined == nil {
		return fmt.Errorf("TF outputs did not include server_id %q — adoption may have failed", j.ServerID)
	}

	// Enrich the node yaml with provider-fetched metadata that TF outputs
	// don't provide for adopted resources (PublicMAC, Disks) or that TF
	// leaves empty for imports (PrivateIP, PrivateMAC).
	nodeprovCfg := nodeprov.Config{
		Provider: platform.Infrastructure.MetalProvider,
		Keyring:  j.Keyring,
	}
	fetcher, err := nodeprov.NewMetadataFetcher(ctx, nodeprovCfg)
	if err != nil {
		return fmt.Errorf("init metadata fetcher: %w", err)
	}
	reconciler, err := nodeprov.NewVRackReconciler(ctx, nodeprovCfg)
	if err != nil {
		return fmt.Errorf("init vrack reconciler: %w", err)
	}

	// Provider has no enrichment impl (e.g. scaleway today): write the node
	// yaml from TF outputs only, matching pre-B4 behavior.
	var enriched *enrichOutput
	if fetcher != nil {
		out, err := enrichNodeFields(ctx, enrichInput{
			Provider:          platform.Infrastructure.MetalProvider,
			PlatformName:      platform.Name,
			ServerID:          j.ServerID,
			VLANID:            platform.Infrastructure.PrivateVLANID,
			ExistingPrivateIP: joined.PrivateIP,
			PrivateNetwork:    platform.Networking.PrivateNetwork,
			NodesDir:          nodesDir,
			Fetcher:           fetcher,
			Reconciler:        reconciler,
		})
		if err != nil {
			return fmt.Errorf("enrich node: %w", err)
		}
		enriched = out
	}

	// Pick fields from enriched if available, else fall back to TF outputs.
	privateIP := joined.PrivateIP
	privateMAC := joined.PrivateMAC
	publicMAC := ""
	var disks []string
	publicGateway := ""
	publicPrefix := 0
	if enriched != nil {
		privateIP = enriched.PrivateIP
		privateMAC = enriched.PrivateMAC
		publicMAC = enriched.PublicMAC
		disks = enriched.Disks
		publicGateway = enriched.PublicGateway
		publicPrefix = enriched.PublicPrefix
	}

	n := &node.Node{
		Hostname: j.Hostname, // operator-provided, authoritative
		Zones:    pool.Zones,
		Machine:  pool.Machine,
		Network: node.Network{
			PublicIP:      joined.PublicIP,
			FailoverIP:    joined.FailoverIP,
			PrivateIP:     privateIP,
			PublicMAC:     publicMAC,
			PrivateMAC:    privateMAC,
			PublicGateway: publicGateway,
			PublicPrefix:  publicPrefix,
		},
		Disks: disks,
		ProviderMetadata: node.ProviderMetadata{
			Provider: platform.Infrastructure.MetalProvider,
			ServerID: j.ServerID, // canonical, == filename
			Zone:     joined.Zone,
			Region:   joined.Region,
			Machine:  joined.Machine,
		},
	}
	n.AddZoneLabels()

	data, err := yaml.Marshal(n)
	if err != nil {
		return fmt.Errorf("failed to marshal joined node: %w", err)
	}
	if err := os.WriteFile(nodeFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write node file: %w", err)
	}
	committed = true

	j.Term().Success().Printfln("Joined %s (server_id=%s)", j.Hostname, j.ServerID)
	j.Term().Info().Printfln("  Public IP:  %s", joined.PublicIP)
	if privateIP != "" {
		j.Term().Info().Printfln("  Private IP: %s", privateIP)
	}
	j.Term().Info().Printfln("  Pool:       %s", j.Pool)
	j.Term().Info().Printfln("  File:       %s", nodeFile)

	j.result = &JoinResult{
		Hostname:  j.Hostname,
		Platform:  j.Platform,
		Pool:      j.Pool,
		ServerID:  j.ServerID,
		PublicIP:  joined.PublicIP,
		PrivateIP: privateIP,
		Zones:     pool.Zones,
		File:      nodeFile,
	}
	return nil
}

func (j *Join) resolveAPIToken(platform schema.Platform) (string, error) {
	apiToken := platform.Infrastructure.API.Token
	if apiToken == "" {
		return "", nil
	}
	if !strings.HasPrefix(apiToken, "{{ .keyring.") {
		return apiToken, nil
	}
	if j.Keyring == nil {
		return "", fmt.Errorf("API token is a keyring reference but keyring is not available")
	}
	provider := platform.Infrastructure.MetalProvider
	tok, err := j.Keyring.GetForURL(provider)
	if err != nil {
		return "", fmt.Errorf("failed to get API token from keyring for %s: %w", provider, err)
	}
	return tok.Password, nil
}

// resolveProviderCreds dispatches by platform.yaml schema shape:
//
//	new 2-field shape (api.client_id or api.access_key set) → auth.Resolve
//	legacy single-token shape (api.token set)               → existing path
//
// Returns a partial ProviderConfig with only credential fields populated;
// caller fills in EnvName/Pools/Zone/Region/Image/SSHKeyID/etc. from
// platform config.
func resolveProviderCreds(ctx context.Context, platform schema.Platform, platformDir string, k keyring.Keyring) (provisioner.ProviderConfig, error) {
	api := platform.Infrastructure.API
	switch {
	case api.ClientID != "" || api.AccessKey != "":
		creds, err := auth.Resolve(ctx, k, platformDir, platform.Infrastructure.MetalProvider)
		if err != nil {
			return provisioner.ProviderConfig{}, err
		}
		return provisioner.ProviderConfig{
			OVHClientID:       creds.OVHClientID,
			OVHClientSecret:   creds.OVHClientSecret,
			OVHEndpoint:       creds.OVHEndpoint,
			ScalewayAccessKey: creds.ScalewayAccessKey,
			ScalewaySecretKey: creds.ScalewaySecretKey,
		}, nil
	case api.Token != "":
		// Legacy: wrap the pre-existing resolver as a method invocation.
		tmpJ := &Join{Keyring: k}
		tok, err := tmpJ.resolveAPIToken(platform)
		if err != nil {
			return provisioner.ProviderConfig{}, err
		}
		return provisioner.ProviderConfig{APIToken: tok}, nil
	default:
		return provisioner.ProviderConfig{}, fmt.Errorf("no credentials configured for platform %q (set api.client_id/client_secret for ovh, api.access_key/secret_key for scaleway, or api.token for legacy providers)", platform.Name)
	}
}

func poolsToSpecs(pools map[string]schema.Pool) []provisioner.PoolSpec {
	var specs []provisioner.PoolSpec
	for name, p := range pools {
		specs = append(specs, provisioner.PoolSpec{
			Name:    name,
			Zones:   p.Zones,
			Machine: p.Machine,
			Count:   p.Count,
		})
	}
	return specs
}

// scanExistingForJoin walks nodesDir and builds ExistingNode entries for every
// node YAML with provider_metadata.server_id set. Needed for the provisioner
// to constrain pool counts in ImportOnly mode.
//
// Pool membership is derived from the node's `zones` list (set-equality with a
// pool's declared zones), not from the hostname. The hostname is operator-
// authoritative (per the canonical-server-id naming work) and no longer
// encodes pool/index. Within each pool, ExistingNodes are sorted by ImportID
// and assigned Index = 0..N-1 — Index ties an entry to a template-generated
// resource <env>_<pool>_<idx>; the value just needs to be unique within pool.
func scanExistingForJoin(nodesDir string, pools map[string]schema.Pool) []provisioner.ExistingNode {
	entries, err := os.ReadDir(nodesDir)
	if err != nil {
		return nil
	}

	byPool := make(map[string][]provisioner.ExistingNode)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") || entry.Name() == ".gitkeep" {
			continue
		}
		n, err := node.Load(filepath.Join(nodesDir, entry.Name()))
		if err != nil || n.ProviderMetadata.ServerID == "" {
			continue
		}
		poolName := poolForNodeZones(n.Zones, pools)
		if poolName == "" {
			continue
		}
		byPool[poolName] = append(byPool[poolName], provisioner.ExistingNode{
			Pool:     poolName,
			ImportID: n.ProviderMetadata.ServerID,
		})
	}

	var out []provisioner.ExistingNode
	for _, nodes := range byPool {
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].ImportID < nodes[j].ImportID })
		for i := range nodes {
			nodes[i].Index = i
		}
		out = append(out, nodes...)
	}
	return out
}

// poolForNodeZones returns the name of the pool whose declared Zones exactly
// match the node's Zones (set equality, order-insensitive). Returns "" if no
// pool matches or multiple pools match (ambiguous configuration — a node
// should belong to exactly one pool).
func poolForNodeZones(nodeZones []string, pools map[string]schema.Pool) string {
	nodeSet := make(map[string]bool, len(nodeZones))
	for _, z := range nodeZones {
		nodeSet[z] = true
	}
	var match string
	for name, pool := range pools {
		if len(pool.Zones) != len(nodeSet) {
			continue
		}
		ok := true
		for _, z := range pool.Zones {
			if !nodeSet[z] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if match != "" {
			return "" // ambiguous: more than one pool has these exact zones
		}
		match = name
	}
	return match
}

func findNodeByServerID(nodesDir, serverID string) (string, string) {
	entries, err := os.ReadDir(nodesDir)
	if err != nil {
		return "", ""
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(nodesDir, entry.Name())
		n, err := node.Load(path)
		if err != nil {
			continue
		}
		if n.ProviderMetadata.ServerID == serverID {
			return n.Hostname, path
		}
	}
	return "", ""
}

func poolNames(pools map[string]schema.Pool) []string {
	names := make([]string, 0, len(pools))
	for n := range pools {
		names = append(names, n)
	}
	return names
}
