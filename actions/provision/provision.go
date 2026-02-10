package provision

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/internal/allocator"
	"github.com/plasmash/plasmactl-node/internal/provisioner"
	"github.com/plasmash/plasmactl-node/pkg/node"
	"github.com/plasmash/plasmactl-platform/pkg/dns"
	"github.com/plasmash/plasmactl-platform/pkg/graph"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// ProvisionedNode represents a provisioned node in the result
type ProvisionedNode struct {
	Hostname  string `json:"hostname"`
	PublicIP  string `json:"public_ip"`
	PrivateIP string `json:"private_ip"`
	Pool      string `json:"pool"`
}

// ProvisionResult is the structured output for node:provision
type ProvisionResult struct {
	Name     string            `json:"name"`
	Provider string            `json:"provider"`
	Nodes    []ProvisionedNode `json:"nodes"`
	DryRun   bool              `json:"dry_run"`
}

// Provision implements the node:provision command
type Provision struct {
	action.WithLogger
	action.WithTerm

	Keyring     keyring.Keyring
	Name        string
	DryRun      bool
	AutoApprove bool

	result *ProvisionResult
}

// Result returns the structured result for JSON output
func (p *Provision) Result() any {
	return p.result
}

// Execute runs the node:provision action
func (p *Provision) Execute() error {
	envDir := filepath.Join("inst", p.Name)
	platformFile := filepath.Join(envDir, "platform.yaml")
	nodesDir := filepath.Join(envDir, "nodes")

	// Check if environment exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		return fmt.Errorf("environment %q not found", p.Name)
	}

	// Read platform.yaml
	platformData, err := os.ReadFile(platformFile)
	if err != nil {
		return fmt.Errorf("failed to read platform.yaml: %w", err)
	}

	var platform schema.Platform
	if err := yaml.Unmarshal(platformData, &platform); err != nil {
		return fmt.Errorf("failed to parse platform.yaml: %w", err)
	}

	// Read pools from platform.yaml
	if len(platform.Pools) == 0 {
		return fmt.Errorf("no pools configured (run: plasmactl platform:size %s --suggest)", p.Name)
	}

	pools := poolsToSpecs(platform.Pools)

	// Initialize result
	p.result = &ProvisionResult{
		Name:     p.Name,
		Provider: platform.Infrastructure.MetalProvider,
		DryRun:   p.DryRun,
	}

	p.Term().Info().Printfln("Provisioning environment %q with provider %q", p.Name, platform.Infrastructure.MetalProvider)

	// Provider-specific provisioning
	switch platform.Infrastructure.MetalProvider {
	case "manual":
		return fmt.Errorf("cannot provision with manual provider - use node:add to add nodes manually")
	default:
		return p.provisionInfra(nodesDir, platform, pools)
	}
}

// poolsToSpecs converts platform.yaml pools to PoolSpecs.
func poolsToSpecs(pools map[string]schema.Pool) []node.PoolSpec {
	var specs []node.PoolSpec
	for name, pool := range pools {
		specs = append(specs, node.PoolSpec{
			Name:    name,
			Chassis: pool.Chassis,
			Machine: pool.Machine,
			Count:   pool.Count,
		})
	}
	return specs
}

// resolveAPIToken resolves the API token from platform config, using keyring if templated
func (p *Provision) resolveAPIToken(platform schema.Platform) (string, error) {
	apiToken := platform.Infrastructure.API.Token
	if apiToken == "" {
		return "", nil
	}

	if strings.HasPrefix(apiToken, "{{ .keyring.") {
		provider := platform.Infrastructure.MetalProvider
		token, err := p.Keyring.GetForURL(provider)
		if err != nil {
			return "", fmt.Errorf("failed to get API token from keyring for %s: %w", provider, err)
		}
		return token.Password, nil
	}

	return apiToken, nil
}

// provisionInfra provisions infrastructure using OpenTofu with the registered provider template
func (p *Provision) provisionInfra(nodesDir string, platform schema.Platform, pools []node.PoolSpec) error {
	ctx := context.Background()

	// Resolve API token
	apiToken, err := p.resolveAPIToken(platform)
	if err != nil {
		return err
	}

	// Scan existing node files to build import list
	existingNodes := p.scanExistingNodes(nodesDir, pools)
	if len(existingNodes) > 0 {
		p.Term().Info().Printfln("Found %d existing nodes to import", len(existingNodes))
	}

	// Build provider config from platform infrastructure fields
	config := provisioner.ProviderConfig{
		EnvName:       p.Name,
		Pools:         pools,
		Provider:      platform.Infrastructure.MetalProvider,
		APIToken:      apiToken,
		Zone:          platform.Infrastructure.Zone,
		Region:        platform.Infrastructure.Region,
		ProjectID:     platform.Infrastructure.ProjectID,
		Image:         platform.Infrastructure.Image,
		SSHKeyID:      platform.Infrastructure.SSHKeyID,
		ExistingNodes: existingNodes,
	}

	// Create provisioner manager (workdir: .plasma/provision/<envName>/)
	mgr, err := provisioner.NewManager(p.Name, p.DryRun, p.AutoApprove)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Clean stale state — import blocks in HCL are the sole source of truth
	if err := mgr.CleanState(); err != nil {
		return fmt.Errorf("failed to clean provisioning state: %w", err)
	}

	p.Term().Info().Printfln("Generating HCL configuration...")

	// Generate HCL
	if err := mgr.GenerateHCL(config); err != nil {
		return fmt.Errorf("failed to generate HCL: %w", err)
	}

	p.Term().Info().Printfln("Generated: %s/main.tf", mgr.GetWorkDir())

	// Initialize
	p.Term().Info().Println("Initializing provisioner...")
	if err := mgr.Init(ctx); err != nil {
		return fmt.Errorf("provisioner init failed: %w", err)
	}

	// Plan
	p.Term().Info().Println("Planning changes...")
	hasChanges, err := mgr.Plan(ctx)
	if err != nil {
		return fmt.Errorf("provisioner plan failed: %w", err)
	}

	if !hasChanges {
		p.Term().Info().Println("No changes needed")
		return nil
	}

	if p.DryRun {
		p.Term().Warning().Println("Dry run - skipping apply")
		p.Term().Info().Printfln("Review the plan at: %s", mgr.GetWorkDir())
		return nil
	}

	// Confirm if not auto-approve
	if !p.AutoApprove {
		p.Term().Warning().Println("This will provision infrastructure (incurring costs)")
		p.Term().Warning().Print("Do you want to apply these changes? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			p.Term().Info().Println("Aborted.")
			return nil
		}
	}

	// Apply
	p.Term().Info().Println("Applying changes...")
	if err := mgr.Apply(ctx); err != nil {
		return fmt.Errorf("provisioner apply failed: %w", err)
	}

	// Get outputs and generate node files
	p.Term().Info().Println("Generating node files...")
	servers, err := mgr.GetOutputs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get provisioning outputs: %w", err)
	}

	// Build pool lookup by name for chassis resolution
	poolsByName := make(map[string]node.PoolSpec)
	for _, pool := range pools {
		poolsByName[pool.Name] = pool
	}

	// Create IP allocator for private IPs (used when provider doesn't supply them)
	ipAlloc, err := allocator.NewIPAllocator(platform.Networking.PrivateNetwork, nodesDir)
	if err != nil {
		return fmt.Errorf("failed to create IP allocator: %w", err)
	}

	// Ensure nodes directory exists
	if err := os.MkdirAll(nodesDir, 0755); err != nil {
		return fmt.Errorf("failed to create nodes directory: %w", err)
	}

	// Generate node files
	for _, server := range servers {
		// Use provider-supplied private IP if available, otherwise allocate from CIDR
		privateIP := server.PrivateIP
		if privateIP == "" {
			privateIP, err = ipAlloc.Allocate()
			if err != nil {
				return fmt.Errorf("failed to allocate private IP: %w", err)
			}
		}

		// Resolve chassis list from pool
		pool := poolsByName[server.Pool]

		n := &node.Node{
			Hostname: server.Hostname,
			Chassis:  pool.Chassis,
			Machine:  server.Machine,
			Network: node.Network{
				PublicIP:   server.PublicIP,
				FailoverIP: server.FailoverIP,
				PrivateIP:  privateIP,
				PrivateMAC: server.PrivateMAC,
			},
			ProviderMetadata: node.ProviderMetadata{
				ServerID: server.ServerID,
				Zone:     server.Zone,
				Region:   server.Region,
				Machine:  server.Machine,
			},
		}
		n.AddChassisLabels()

		nodeFile := filepath.Join(nodesDir, server.Hostname+".yaml")
		data, err := yaml.Marshal(n)
		if err != nil {
			return fmt.Errorf("failed to marshal node %s: %w", server.Hostname, err)
		}

		if err := os.WriteFile(nodeFile, data, 0644); err != nil {
			return fmt.Errorf("failed to write node file %s: %w", nodeFile, err)
		}

		// Add to result
		p.result.Nodes = append(p.result.Nodes, ProvisionedNode{
			Hostname:  server.Hostname,
			PublicIP:  server.PublicIP,
			PrivateIP: privateIP,
			Pool:      server.Pool,
		})

		p.Term().Success().Printfln("Created node: %s (%s) [pool: %s]", server.Hostname, server.PublicIP, server.Pool)
	}

	p.Term().Success().Printfln("Provisioned %d nodes across %d pools", len(servers), len(pools))

	// Show what components will be deployed
	p.showProvisionImpact(pools)

	// Configure DNS records using provisioned IPs
	if platform.DNS.Provider != "" && platform.DNS.Provider != "manual" {
		p.configureDNS(ctx, platform, servers)
	}

	return nil
}

// configureDNS sets up DNS records using provisioned server IPs.
// Failure is non-fatal - infrastructure is already provisioned.
func (p *Provision) configureDNS(ctx context.Context, platform schema.Platform, servers []provisioner.ServerOutput) {
	p.Term().Println()
	p.Term().Info().Println("Configuring DNS records...")

	// Find ingress IPs from provisioned servers
	var webIP, mailIP string
	for _, s := range servers {
		if strings.Contains(s.Pool, "ingress") {
			if s.FailoverIP != "" {
				webIP = s.FailoverIP
				mailIP = s.FailoverIP
			} else {
				webIP = s.PublicIP
				mailIP = s.PublicIP
			}
		}
	}

	if webIP == "" && mailIP == "" {
		p.Term().Info().Println("  No ingress nodes provisioned, skipping DNS")
		return
	}

	// Resolve DNS credentials from keyring
	username, token, err := p.resolveDNSCredentials(platform)
	if err != nil {
		p.Term().Warning().Printfln("  DNS skipped: %v", err)
		return
	}

	zone := platform.DNS.Zone
	if zone == "" {
		zone = dns.DeriveZone(platform.DNS.Domain)
	}

	config := dns.Config{
		Provider:     platform.DNS.Provider,
		APIToken:     token,
		APIUsername:   username,
		Domain:       platform.DNS.Domain,
		Zone:         zone,
		Subdomain:    dns.DeriveSubdomain(platform.DNS.Domain, zone),
		Region:       platform.DNS.Region,
		WebIP:        webIP,
		MailIP:       mailIP,
		DKIMSelector: "default",
		DKIMKey:      platform.DNS.DKIMKey,
	}

	dnsManager, err := dns.NewManager(p.Name)
	if err != nil {
		p.Term().Warning().Printfln("  DNS failed: %v", err)
		return
	}

	if err := dnsManager.GenerateHCL(config); err != nil {
		p.Term().Warning().Printfln("  DNS generation failed: %v", err)
		return
	}

	p.Term().Info().Printfln("  Generated: %s/main.tf", dnsManager.GetWorkDir())

	if p.DryRun {
		p.Term().Info().Println("  DNS dry run - skipping apply")
		return
	}

	if err := dnsManager.Apply(ctx); err != nil {
		p.Term().Warning().Printfln("  DNS apply failed: %v", err)
		p.Term().Warning().Println("  You can re-run provisioning to retry DNS setup")
		return
	}

	p.Term().Success().Println("DNS records configured")
	if webIP != "" {
		p.Term().Info().Printfln("  A / *.A → %s", webIP)
	}
	if mailIP != "" {
		p.Term().Info().Printfln("  MX / SPF → %s", mailIP)
	}
}

// resolveDNSCredentials resolves DNS credentials (username + token/password) from keyring
func (p *Provision) resolveDNSCredentials(platform schema.Platform) (username, token string, err error) {
	if p.Keyring == nil {
		return "", "", fmt.Errorf("keyring not available")
	}

	cred, err := p.Keyring.GetForURL(platform.DNS.Provider)
	if err != nil {
		if platform.DNS.Provider == platform.Infrastructure.MetalProvider {
			cred, err = p.Keyring.GetForURL(platform.Infrastructure.MetalProvider)
			if err != nil {
				return "", "", fmt.Errorf("no credentials for %s (run: plasmactl keyring:login %s)", platform.DNS.Provider, platform.DNS.Provider)
			}
			return cred.Username, cred.Password, nil
		}
		return "", "", fmt.Errorf("no DNS credentials for %s (run: plasmactl keyring:login %s)", platform.DNS.Provider, platform.DNS.Provider)
	}
	return cred.Username, cred.Password, nil
}

// hostnameIndexRe matches the trailing numeric index from hostnames like "myenv-control-001"
var hostnameIndexRe = regexp.MustCompile(`-(\d+)$`)

// scanExistingNodes reads node YAML files from nodesDir and builds ExistingNode
// entries for nodes that have a provider_metadata.server_id. This enables TF
// import blocks to adopt existing resources without persistent state.
func (p *Provision) scanExistingNodes(nodesDir string, pools []node.PoolSpec) []provisioner.ExistingNode {
	entries, err := os.ReadDir(nodesDir)
	if err != nil {
		return nil
	}

	// Build pool name lookup from hostname prefix pattern: <envName>-<pool>-NNN
	poolNames := make(map[string]bool)
	for _, pool := range pools {
		poolNames[pool.Name] = true
	}

	var existing []provisioner.ExistingNode
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") || entry.Name() == ".gitkeep" {
			continue
		}

		n, err := node.Load(filepath.Join(nodesDir, entry.Name()))
		if err != nil {
			p.Log().Debug("Skipping unreadable node file", "file", entry.Name(), "error", err)
			continue
		}

		// Only import nodes that have a provider server ID
		if n.ProviderMetadata.ServerID == "" {
			continue
		}

		// Derive pool name and index from hostname: <envName>-<pool>-NNN
		pool, index, ok := parseHostnamePool(n.Hostname, p.Name, poolNames)
		if !ok {
			p.Log().Debug("Cannot match node to pool", "hostname", n.Hostname)
			continue
		}

		existing = append(existing, provisioner.ExistingNode{
			Pool:     pool,
			Index:    index,
			ImportID: n.ProviderMetadata.ServerID,
		})
	}

	return existing
}

// parseHostnamePool extracts pool name and 0-based index from a hostname.
// Expected format: <envName>-<poolName>-NNN (e.g., "ski-dev-control-001")
// Returns pool name, 0-based index, and whether parsing succeeded.
func parseHostnamePool(hostname, envName string, validPools map[string]bool) (string, int, bool) {
	prefix := envName + "-"
	if !strings.HasPrefix(hostname, prefix) {
		return "", 0, false
	}

	// Strip env prefix: "control-001"
	rest := strings.TrimPrefix(hostname, prefix)

	// Extract trailing index
	match := hostnameIndexRe.FindStringSubmatch(rest)
	if match == nil {
		return "", 0, false
	}

	// Pool name is everything before the index
	poolName := rest[:len(rest)-len(match[0])]
	if !validPools[poolName] {
		return "", 0, false
	}

	// Parse 1-based index → 0-based
	idx, err := strconv.Atoi(match[1])
	if err != nil || idx < 1 {
		return "", 0, false
	}

	return poolName, idx - 1, true
}

// showProvisionImpact loads the platform graph and shows what components
// will be deployed on the provisioned chassis paths.
func (p *Provision) showProvisionImpact(pools []node.PoolSpec) {
	g, err := graph.Load()
	if err != nil {
		p.Log().Debug("Platform graph not available for impact display", "error", err)
		return
	}

	p.Term().Println()
	p.Term().Info().Println("Deployment impact:")

	for _, pool := range pools {
		p.Term().Info().Printfln("  Pool %q (%s x%d):", pool.Name, pool.Machine, pool.Count)
		for _, chassis := range pool.Chassis {
			gNode := g.Node(chassis)
			if gNode == nil {
				p.Term().Warning().Printfln("    %s: not found in platform graph", chassis)
				continue
			}

			attached := g.EdgesFrom(chassis, "distributes")
			if len(attached) == 0 {
				p.Term().Info().Printfln("    %s: no components attached", chassis)
				continue
			}

			p.Term().Info().Printfln("    %s:", chassis)
			for _, e := range attached {
				p.Term().Printfln("      %s (%s)", e.To().Name, e.To().Kind)
			}
		}
	}
}
