package plasmactlnode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-node/pkg/types"
	"gopkg.in/yaml.v3"
)

// destroyAction implements the env:destroy command
type destroyAction struct {
	log  *launchr.Logger
	term *launchr.Terminal

	keyring   keyring.Keyring
	name      string
	force     bool
	keepNodes bool
}

// SetLogger sets the logger for the action
func (a *destroyAction) SetLogger(log *launchr.Logger) {
	a.log = log
}

// SetTerm sets the terminal for the action
func (a *destroyAction) SetTerm(term *launchr.Terminal) {
	a.term = term
}

// Execute runs the env:destroy action
func (a *destroyAction) Execute() error {
	envDir := filepath.Join("inst", a.name)
	platformFile := filepath.Join(envDir, "platform.yaml")
	nodesDir := filepath.Join(envDir, "nodes")
	terraformDir := filepath.Join(envDir, ".terraform")

	// Check if environment exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		return fmt.Errorf("environment %q not found", a.name)
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

	// Check if provider is manual
	if platform.Infrastructure.Provider == "manual" {
		a.term.Warning().Println("Cannot destroy infrastructure for manual provider")
		a.term.Info().Println("Remove node files manually if needed")
		return nil
	}

	// Check if terraform state exists
	if _, err := os.Stat(filepath.Join(terraformDir, "terraform.tfstate")); os.IsNotExist(err) {
		a.term.Warning().Println("No Terraform state found - nothing to destroy")
		return nil
	}

	// Confirm destruction
	if !a.force {
		a.term.Warning().Printfln("This will DESTROY all infrastructure for environment %q", a.name)
		a.term.Warning().Println("This action cannot be undone!")
		// TODO: Add interactive confirmation
		a.term.Info().Println("Use --force to skip this confirmation")
		return fmt.Errorf("destruction cancelled - use --force to proceed")
	}

	a.term.Warning().Printfln("Destroying infrastructure for environment %q", a.name)

	ctx := context.Background()

	// Create Terraform manager
	tfManager, err := NewTerraformManager(envDir, false, true)
	if err != nil {
		return fmt.Errorf("failed to create terraform manager: %w", err)
	}

	// Initialize Terraform (in case state is missing)
	a.term.Info().Println("Initializing Terraform...")
	if err := tfManager.Init(ctx); err != nil {
		return fmt.Errorf("terraform init failed: %w", err)
	}

	// Destroy
	a.term.Info().Println("Destroying infrastructure...")
	if err := tfManager.Destroy(ctx); err != nil {
		return fmt.Errorf("terraform destroy failed: %w", err)
	}

	a.term.Success().Println("Infrastructure destroyed")

	// Remove node files unless --keep-nodes
	if !a.keepNodes {
		entries, err := os.ReadDir(nodesDir)
		if err == nil {
			for _, entry := range entries {
				if entry.Name() == ".gitkeep" {
					continue
				}
				nodePath := filepath.Join(nodesDir, entry.Name())
				if err := os.Remove(nodePath); err != nil {
					a.log.Warn("Failed to remove node file", "file", nodePath, "error", err)
				} else {
					a.term.Info().Printfln("Removed: %s", nodePath)
				}
			}
		}
		a.term.Info().Println("Node files removed")
	} else {
		a.term.Info().Println("Node files kept (--keep-nodes)")
	}

	// Clean up terraform directory
	if err := os.RemoveAll(terraformDir); err != nil {
		a.log.Warn("Failed to remove terraform directory", "error", err)
	} else {
		a.term.Info().Println("Terraform state cleaned up")
	}

	return nil
}
