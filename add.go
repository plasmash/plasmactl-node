package plasmactlnode

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-node/pkg/types"
	"gopkg.in/yaml.v3"
)

// addAction implements the env:add command
type addAction struct {
	log  *launchr.Logger
	term *launchr.Terminal

	name     string
	provider string
	domain   string
}

// SetLogger sets the logger for the action
func (a *addAction) SetLogger(log *launchr.Logger) {
	a.log = log
}

// SetTerm sets the terminal for the action
func (a *addAction) SetTerm(term *launchr.Terminal) {
	a.term = term
}

// Execute runs the env:add action
func (a *addAction) Execute() error {
	envDir := filepath.Join("env", a.name)
	nodesDir := filepath.Join(envDir, "nodes")
	platformFile := filepath.Join(envDir, "platform.yaml")

	// Check if environment already exists
	if _, err := os.Stat(envDir); !os.IsNotExist(err) {
		return fmt.Errorf("environment %q already exists at %s", a.name, envDir)
	}

	a.term.Info().Printfln("Creating environment %q with provider %q", a.name, a.provider)

	// Create directories
	if err := os.MkdirAll(nodesDir, 0755); err != nil {
		return fmt.Errorf("failed to create nodes directory: %w", err)
	}

	// Create platform.yaml
	platform := types.NewPlatform(a.name, a.provider, a.domain)

	// Set provider-specific defaults
	switch a.provider {
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

	a.term.Success().Printfln("Created environment at %s", envDir)
	a.term.Info().Printfln("  - Platform config: %s", platformFile)
	a.term.Info().Printfln("  - Nodes directory: %s", nodesDir)

	if a.provider != "manual" {
		a.term.Info().Println()
		a.term.Info().Printfln("Next steps:")
		a.term.Info().Printfln("  1. Configure API credentials in platform.yaml")
		a.term.Info().Printfln("  2. Provision infrastructure: plasmactl env:provision %s -c <chassis>:<offer>:<count>", a.name)
	} else {
		a.term.Info().Println()
		a.term.Info().Printfln("Next steps (manual provider):")
		a.term.Info().Printfln("  1. Add nodes manually: plasmactl env:node %s --hostname <name> --public-ip <ip>", a.name)
		a.term.Info().Printfln("  2. Or create node YAML files directly in %s", nodesDir)
	}

	return nil
}
