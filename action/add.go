package action

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/pkg/types"
	"gopkg.in/yaml.v3"
)

// Add implements the node:add command
type Add struct {
	action.WithLogger
	action.WithTerm

	Name     string
	Provider string
	Domain   string
}

// Execute runs the node:add action
func (a *Add) Execute() error {
	envDir := filepath.Join("inst", a.Name)
	nodesDir := filepath.Join(envDir, "nodes")
	platformFile := filepath.Join(envDir, "platform.yaml")

	// Check if environment already exists
	if _, err := os.Stat(envDir); !os.IsNotExist(err) {
		return fmt.Errorf("environment %q already exists at %s", a.Name, envDir)
	}

	a.Term().Info().Printfln("Creating environment %q with provider %q", a.Name, a.Provider)

	// Create directories
	if err := os.MkdirAll(nodesDir, 0755); err != nil {
		return fmt.Errorf("failed to create nodes directory: %w", err)
	}

	// Create platform.yaml
	platform := types.NewPlatform(a.Name, a.Provider, a.Domain)

	// Set provider-specific defaults
	switch a.Provider {
	case "scaleway":
		platform.Infrastructure.API = types.APIConfig{
			URI:   "https://api.online.net/api/v1/",
			Token: "{{ .keyring.scaleway_api_token }}",
		}
	case "hetzner":
		platform.Infrastructure.API = types.APIConfig{
			Token: "{{ .keyring.hetzner_api_token }}",
		}
	case "aws":
		// AWS uses environment variables or ~/.aws/credentials
	case "ovh":
		platform.Infrastructure.API = types.APIConfig{
			Token: "{{ .keyring.ovh_api_token }}",
		}
	case "manual":
		// No API configuration needed
	}

	data, err := yaml.Marshal(platform)
	if err != nil {
		return fmt.Errorf("failed to marshal platform.yaml: %w", err)
	}

	if err := os.WriteFile(platformFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write platform.yaml: %w", err)
	}

	// Create .gitkeep in nodes directory to ensure it's tracked
	gitkeepFile := filepath.Join(nodesDir, ".gitkeep")
	if err := os.WriteFile(gitkeepFile, []byte{}, 0644); err != nil {
		return fmt.Errorf("failed to write .gitkeep: %w", err)
	}

	a.Term().Success().Printfln("Created environment at %s", envDir)
	a.Term().Info().Printfln("  - Platform config: %s", platformFile)
	a.Term().Info().Printfln("  - Nodes directory: %s", nodesDir)

	if a.Provider != "manual" {
		a.Term().Info().Println()
		a.Term().Info().Printfln("Next steps:")
		a.Term().Info().Printfln("  1. Configure API credentials in platform.yaml")
		a.Term().Info().Printfln("  2. Provision infrastructure: plasmactl env:provision %s -c <chassis>:<offer>:<count>", a.Name)
	} else {
		a.Term().Info().Println()
		a.Term().Info().Printfln("Next steps (manual provider):")
		a.Term().Info().Printfln("  1. Add nodes manually: plasmactl env:node %s --hostname <name> --public-ip <ip>", a.Name)
		a.Term().Info().Printfln("  2. Or create node YAML files directly in %s", nodesDir)
	}

	return nil
}
