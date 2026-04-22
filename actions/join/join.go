package join

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-platform/pkg/provisioner"
	"github.com/plasmash/plasmactl-node/pkg/node"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

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
	DryRun      bool
	AutoApprove bool

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

	if existing, file := findNodeByServerID(nodesDir, j.ServerID); existing != "" {
		return fmt.Errorf("server_id %q already joined as node %s (%s)", j.ServerID, existing, file)
	}

	if j.Hostname == "" {
		nextIdx := nextPoolIndex(nodesDir, j.Platform, j.Pool)
		j.Hostname = fmt.Sprintf("%s-%s-%03d", j.Platform, j.Pool, nextIdx+1)
	}

	nodeFile := filepath.Join(nodesDir, j.Hostname+".yaml")
	if _, err := os.Stat(nodeFile); !os.IsNotExist(err) {
		return fmt.Errorf("node %q already exists at %s", j.Hostname, nodeFile)
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

	j.Term().Info().Printfln("Joining server %q into pool %q as %s", j.ServerID, j.Pool, j.Hostname)

	apiToken, err := j.resolveAPIToken(platform)
	if err != nil {
		return err
	}

	existingNodes := scanExistingForJoin(nodesDir, platform.Pools)

	config := provisioner.ProviderConfig{
		EnvName:       j.Platform,
		Pools:         poolsToSpecs(platform.Pools),
		Provider:      platform.Infrastructure.MetalProvider,
		APIToken:      apiToken,
		Zone:          platform.Infrastructure.Zone,
		Region:        platform.Infrastructure.Region,
		ProjectID:     platform.Infrastructure.ProjectID,
		Image:         platform.Infrastructure.Image,
		SSHKeyID:      platform.Infrastructure.SSHKeyID,
		ExistingNodes: existingNodes,
		ImportOnly:    true,
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

	if !j.AutoApprove {
		j.Term().Warning().Printfln("This will adopt server %q into Terraform state for platform %q.", j.ServerID, j.Platform)
		j.Term().Warning().Print("Apply? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			j.Term().Info().Println("Aborted.")
			return nil
		}
	}

	if err := mgr.Apply(ctx); err != nil {
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

	n := &node.Node{
		Hostname: joined.Hostname,
		Zones:    pool.Zones,
		Machine:  joined.Machine,
		Network: node.Network{
			PublicIP:   joined.PublicIP,
			FailoverIP: joined.FailoverIP,
			PrivateIP:  joined.PrivateIP,
			PrivateMAC: joined.PrivateMAC,
		},
		ProviderMetadata: node.ProviderMetadata{
			ServerID: joined.ServerID,
			Zone:     joined.Zone,
			Region:   joined.Region,
			Machine:  joined.Machine,
		},
	}
	n.AddZoneLabels()

	finalFile := filepath.Join(nodesDir, joined.Hostname+".yaml")
	data, err := yaml.Marshal(n)
	if err != nil {
		return fmt.Errorf("failed to marshal joined node: %w", err)
	}
	if err := os.WriteFile(finalFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write node file: %w", err)
	}

	// TF's canonical hostname may differ from the stub name — clean up the stub if so.
	if finalFile != nodeFile {
		if err := os.Remove(nodeFile); err != nil && !os.IsNotExist(err) {
			j.Log().Warn("failed to remove stub file", "file", nodeFile, "error", err)
		}
	}

	j.Term().Success().Printfln("Joined %s (server_id=%s)", joined.Hostname, joined.ServerID)
	j.Term().Info().Printfln("  Public IP:  %s", joined.PublicIP)
	if joined.PrivateIP != "" {
		j.Term().Info().Printfln("  Private IP: %s", joined.PrivateIP)
	}
	j.Term().Info().Printfln("  Pool:       %s", j.Pool)
	j.Term().Info().Printfln("  File:       %s", finalFile)

	j.result = &JoinResult{
		Hostname:  joined.Hostname,
		Platform:  j.Platform,
		Pool:      j.Pool,
		ServerID:  joined.ServerID,
		PublicIP:  joined.PublicIP,
		PrivateIP: joined.PrivateIP,
		Zones:     pool.Zones,
		File:      finalFile,
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
func scanExistingForJoin(nodesDir string, pools map[string]schema.Pool) []provisioner.ExistingNode {
	entries, err := os.ReadDir(nodesDir)
	if err != nil {
		return nil
	}

	poolNameSet := make(map[string]bool, len(pools))
	for name := range pools {
		poolNameSet[name] = true
	}

	var out []provisioner.ExistingNode
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") || entry.Name() == ".gitkeep" {
			continue
		}
		n, err := node.Load(filepath.Join(nodesDir, entry.Name()))
		if err != nil || n.ProviderMetadata.ServerID == "" {
			continue
		}
		poolName, index, ok := parseHostnamePool(n.Hostname, poolNameSet)
		if !ok {
			continue
		}
		out = append(out, provisioner.ExistingNode{
			Pool:     poolName,
			Index:    index,
			ImportID: n.ProviderMetadata.ServerID,
		})
	}
	return out
}

func parseHostnamePool(hostname string, validPools map[string]bool) (string, int, bool) {
	parts := strings.Split(hostname, "-")
	if len(parts) < 3 {
		return "", 0, false
	}
	idxStr := parts[len(parts)-1]
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 1 {
		return "", 0, false
	}
	for i := 1; i < len(parts)-1; i++ {
		candidate := strings.Join(parts[i:len(parts)-1], "-")
		if validPools[candidate] {
			return candidate, idx - 1, true
		}
	}
	return "", 0, false
}

func nextPoolIndex(nodesDir, platform, pool string) int {
	prefix := platform + "-" + pool + "-"
	entries, err := os.ReadDir(nodesDir)
	if err != nil {
		return 0
	}
	maxIdx := -1
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".yaml")
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		idxStr := strings.TrimPrefix(name, prefix)
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			continue
		}
		if idx-1 > maxIdx {
			maxIdx = idx - 1
		}
	}
	return maxIdx + 1
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
