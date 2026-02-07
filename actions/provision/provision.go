package provision

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/internal/allocator"
	"github.com/plasmash/plasmactl-node/internal/terraform"
	"github.com/plasmash/plasmactl-node/pkg/node"
	"github.com/plasmash/plasmactl-platform/pkg/graph"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// ProvisionedNode represents a provisioned node in the result
type ProvisionedNode struct {
	Hostname  string `json:"hostname"`
	PublicIP  string `json:"public_ip"`
	PrivateIP string `json:"private_ip"`
	Chassis   string `json:"chassis"`
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
	ChassisSpec []string
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

	// Parse chassis specifications from command line or platform.yaml
	specs, err := p.parseChassisSpecs(platform)
	if err != nil {
		return fmt.Errorf("failed to parse chassis specs: %w", err)
	}

	if len(specs) == 0 {
		return fmt.Errorf("no chassis specifications provided (use -c flag or configure chassis in platform.yaml)")
	}

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
		return fmt.Errorf("cannot provision with manual provider - use env:node to add nodes manually")
	default:
		return p.provisionWithTerraform(envDir, nodesDir, platform, specs)
	}
}

// parseChassisSpecs parses chassis specifications from CLI or platform.yaml
func (p *Provision) parseChassisSpecs(platform schema.Platform) ([]node.ChassisSpec, error) {
	var specs []node.ChassisSpec

	// First, use CLI specifications if provided
	for _, spec := range p.ChassisSpec {
		parts := strings.Split(spec, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid chassis spec %q - expected format chassis:offer:count", spec)
		}

		count, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("invalid count in chassis spec %q: %w", spec, err)
		}

		specs = append(specs, node.ChassisSpec{
			Chassis:   parts[0],
			OfferType: parts[1],
			Count:     count,
		})
	}

	// If no CLI specs, use platform.yaml chassis configuration
	if len(specs) == 0 && platform.Chassis != nil {
		for chassis, profiles := range platform.Chassis {
			for _, profile := range profiles {
				specs = append(specs, node.ChassisSpec{
					Chassis:   chassis,
					OfferType: profile.Type,
					Count:     profile.Count,
				})
			}
		}
	}

	return specs, nil
}

// resolveAPIToken resolves the API token from platform config, using keyring if templated
func (p *Provision) resolveAPIToken(platform schema.Platform) (string, error) {
	apiToken := platform.Infrastructure.API.Token
	if apiToken == "" {
		return "", nil
	}

	if strings.HasPrefix(apiToken, "{{ .keyring.") {
		// Resolve keyring URL from provider name
		provider := platform.Infrastructure.MetalProvider
		token, err := p.Keyring.GetForURL(provider)
		if err != nil {
			return "", fmt.Errorf("failed to get API token from keyring for %s: %w", provider, err)
		}
		return token.Password, nil
	}

	return apiToken, nil
}

// provisionWithTerraform provisions infrastructure using Terraform with the registered provider template
func (p *Provision) provisionWithTerraform(envDir, nodesDir string, platform schema.Platform, specs []node.ChassisSpec) error {
	ctx := context.Background()

	// Resolve API token
	apiToken, err := p.resolveAPIToken(platform)
	if err != nil {
		return err
	}

	// Build provider config from platform infrastructure fields
	config := terraform.ProviderConfig{
		EnvName:   p.Name,
		Specs:     specs,
		Provider:  platform.Infrastructure.MetalProvider,
		APIToken:  apiToken,
		Zone:      platform.Infrastructure.Zone,
		Region:    platform.Infrastructure.Region,
		ProjectID: platform.Infrastructure.ProjectID,
		Image:     platform.Infrastructure.Image,
		SSHKeyID:  platform.Infrastructure.SSHKeyID,
	}

	// Create Terraform manager
	tfManager, err := terraform.NewTerraformManager(envDir, p.DryRun, p.AutoApprove)
	if err != nil {
		return fmt.Errorf("failed to create terraform manager: %w", err)
	}

	p.Term().Info().Printfln("Generating Terraform configuration...")

	// Generate HCL
	if err := tfManager.GenerateHCL(config); err != nil {
		return fmt.Errorf("failed to generate terraform HCL: %w", err)
	}

	p.Term().Info().Printfln("Generated: %s/main.tf", tfManager.GetWorkDir())

	// Initialize Terraform
	p.Term().Info().Println("Initializing Terraform...")
	if err := tfManager.Init(ctx); err != nil {
		return fmt.Errorf("terraform init failed: %w", err)
	}

	// Plan
	p.Term().Info().Println("Planning changes...")
	hasChanges, err := tfManager.Plan(ctx)
	if err != nil {
		return fmt.Errorf("terraform plan failed: %w", err)
	}

	if !hasChanges {
		p.Term().Info().Println("No changes needed")
		return nil
	}

	if p.DryRun {
		p.Term().Warning().Println("Dry run - skipping apply")
		p.Term().Info().Printfln("Review the plan at: %s", tfManager.GetWorkDir())
		return nil
	}

	// Confirm if not auto-approve
	if !p.AutoApprove {
		p.Term().Warning().Println("This will provision infrastructure (incurring costs)")
		// TODO: Add interactive confirmation
	}

	// Apply
	p.Term().Info().Println("Applying changes...")
	if err := tfManager.Apply(ctx); err != nil {
		return fmt.Errorf("terraform apply failed: %w", err)
	}

	// Get outputs and generate node files
	p.Term().Info().Println("Generating node files...")
	servers, err := tfManager.GetOutputs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get terraform outputs: %w", err)
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

		n := &node.Node{
			Hostname: server.Hostname,
			Chassis:  []string{server.Chassis},
			Profile:  server.OfferName,
			Network: node.Network{
				PublicIP:   server.PublicIP,
				PrivateIP:  privateIP,
				PrivateMAC: server.PrivateMAC,
			},
			ProviderMetadata: node.ProviderMetadata{
				ServerID:  server.ServerID,
				Zone:      server.Zone,
				Region:    server.Region,
				OfferName: server.OfferName,
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
			Chassis:   server.Chassis,
		})

		p.Term().Success().Printfln("Created node: %s (%s)", server.Hostname, server.PublicIP)
	}

	p.Term().Success().Printfln("Provisioned %d nodes", len(servers))

	// Show what components will be deployed on the new nodes
	p.showProvisionImpact(specs)

	// Update platform.yaml with chassis configuration if it was provided via CLI
	if len(p.ChassisSpec) > 0 {
		platform.Chassis = make(map[string][]schema.ChassisProfile)
		for _, spec := range specs {
			platform.Chassis[spec.Chassis] = append(platform.Chassis[spec.Chassis], schema.ChassisProfile{
				Type:  spec.OfferType,
				Count: spec.Count,
			})
		}

		data, err := yaml.Marshal(platform)
		if err != nil {
			p.Log().Warn("Failed to update platform.yaml", "error", err)
		} else {
			platformFile := filepath.Join(envDir, "platform.yaml")
			if err := os.WriteFile(platformFile, data, 0644); err != nil {
				p.Log().Warn("Failed to write platform.yaml", "error", err)
			} else {
				p.Term().Info().Println("Updated platform.yaml with chassis configuration")
			}
		}
	}

	return nil
}

// showProvisionImpact loads the platform graph and shows what components
// will be deployed on the provisioned chassis paths.
func (p *Provision) showProvisionImpact(specs []node.ChassisSpec) {
	g, err := graph.Load()
	if err != nil {
		p.Log().Debug("Platform graph not available for impact display", "error", err)
		return
	}

	p.Term().Println()
	p.Term().Info().Println("Deployment impact:")

	seen := make(map[string]bool)
	for _, spec := range specs {
		if seen[spec.Chassis] {
			continue
		}
		seen[spec.Chassis] = true

		gNode := g.Node(spec.Chassis)
		if gNode == nil {
			p.Term().Warning().Printfln("  %s: not found in platform graph", spec.Chassis)
			continue
		}

		attached := g.EdgesFrom(spec.Chassis, "attaches")
		if len(attached) == 0 {
			p.Term().Info().Printfln("  %s: no components attached", spec.Chassis)
			continue
		}

		p.Term().Info().Printfln("  %s:", spec.Chassis)
		for _, e := range attached {
			p.Term().Printfln("    %s (%s)", e.To().Name, e.To().Kind)
		}
	}
}
