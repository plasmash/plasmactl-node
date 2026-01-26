package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/pkg/types"
	"gopkg.in/yaml.v3"
)

// Provision implements the node:provision command
type Provision struct {
	action.WithLogger
	action.WithTerm

	Keyring     keyring.Keyring
	Name        string
	ChassisSpec []string
	DryRun      bool
	AutoApprove bool
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

	var platform types.Platform
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

	p.Term().Info().Printfln("Provisioning environment %q with provider %q", p.Name, platform.Infrastructure.Provider)

	// Provider-specific provisioning
	switch platform.Infrastructure.Provider {
	case "scaleway":
		return p.provisionScaleway(envDir, nodesDir, platform, specs)
	case "hetzner":
		return fmt.Errorf("hetzner provider not yet implemented")
	case "aws":
		return fmt.Errorf("aws provider not yet implemented")
	case "ovh":
		return fmt.Errorf("ovh provider not yet implemented")
	case "manual":
		return fmt.Errorf("cannot provision with manual provider - use env:node to add nodes manually")
	default:
		return fmt.Errorf("unknown provider: %s", platform.Infrastructure.Provider)
	}
}

// parseChassisSpecs parses chassis specifications from CLI or platform.yaml
func (p *Provision) parseChassisSpecs(platform types.Platform) ([]ChassisSpec, error) {
	var specs []ChassisSpec

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

		specs = append(specs, ChassisSpec{
			Chassis:   parts[0],
			OfferType: parts[1],
			Count:     count,
		})
	}

	// If no CLI specs, use platform.yaml chassis configuration
	if len(specs) == 0 && platform.Chassis != nil {
		for chassis, profiles := range platform.Chassis {
			for _, profile := range profiles {
				specs = append(specs, ChassisSpec{
					Chassis:   chassis,
					OfferType: profile.Type,
					Count:     profile.Count,
				})
			}
		}
	}

	return specs, nil
}

// provisionScaleway provisions infrastructure using Scaleway Dedibox
func (p *Provision) provisionScaleway(envDir, nodesDir string, platform types.Platform, specs []ChassisSpec) error {
	ctx := context.Background()

	// Get API token
	apiToken := platform.Infrastructure.API.Token
	if strings.HasPrefix(apiToken, "{{ .keyring.") {
		// Extract key name and fetch from keyring
		keyName := strings.TrimPrefix(apiToken, "{{ .keyring.")
		keyName = strings.TrimSuffix(keyName, " }}")

		token, err := p.Keyring.GetForURL("scaleway")
		if err != nil {
			return fmt.Errorf("failed to get API token from keyring: %w", err)
		}
		apiToken = token.Password
	}

	// Create Terraform manager
	tfManager, err := NewTerraformManager(envDir, p.DryRun, p.AutoApprove)
	if err != nil {
		return fmt.Errorf("failed to create terraform manager: %w", err)
	}

	p.Term().Info().Printfln("Generating Terraform configuration...")

	// Generate HCL
	if err := tfManager.GenerateHCL(p.Name, specs, apiToken); err != nil {
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

	// Create IP allocator for private IPs
	allocator, err := NewIPAllocator(platform.Networking.PrivateNetwork, nodesDir)
	if err != nil {
		return fmt.Errorf("failed to create IP allocator: %w", err)
	}

	// Ensure nodes directory exists
	if err := os.MkdirAll(nodesDir, 0755); err != nil {
		return fmt.Errorf("failed to create nodes directory: %w", err)
	}

	// Generate node files
	for _, server := range servers {
		// Allocate private IP
		privateIP, err := allocator.Allocate()
		if err != nil {
			return fmt.Errorf("failed to allocate private IP: %w", err)
		}

		node := &types.Node{
			Hostname: server.Hostname,
			Chassis:  []string{server.Chassis},
			Profile:  server.OfferName,
			Network: types.NodeNetwork{
				PublicIP:   server.PublicIP,
				PrivateIP:  privateIP,
				PrivateMAC: server.PrivateMAC,
			},
			ProviderMetadata: types.NodeProviderMetadata{
				ServerID:  server.ServerID,
				Zone:      server.Zone,
				OfferName: server.OfferName,
			},
		}
		node.AddChassisLabels()

		nodeFile := filepath.Join(nodesDir, server.Hostname+".yaml")
		data, err := yaml.Marshal(node)
		if err != nil {
			return fmt.Errorf("failed to marshal node %s: %w", server.Hostname, err)
		}

		if err := os.WriteFile(nodeFile, data, 0644); err != nil {
			return fmt.Errorf("failed to write node file %s: %w", nodeFile, err)
		}

		p.Term().Success().Printfln("Created node: %s (%s)", server.Hostname, server.PublicIP)
	}

	p.Term().Success().Printfln("Provisioned %d nodes", len(servers))

	// Update platform.yaml with chassis configuration if it was provided via CLI
	if len(p.ChassisSpec) > 0 {
		platform.Chassis = make(map[string][]types.ChassisProfile)
		for _, spec := range specs {
			platform.Chassis[spec.Chassis] = append(platform.Chassis[spec.Chassis], types.ChassisProfile{
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
