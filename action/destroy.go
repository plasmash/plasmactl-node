package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/pkg/types"
	"gopkg.in/yaml.v3"
)

// Destroy implements the node:destroy command
type Destroy struct {
	action.WithLogger
	action.WithTerm

	Keyring   keyring.Keyring
	Name      string
	Force     bool
	KeepNodes bool
}

// Execute runs the node:destroy action
func (d *Destroy) Execute() error {
	envDir := filepath.Join("inst", d.Name)
	platformFile := filepath.Join(envDir, "platform.yaml")
	nodesDir := filepath.Join(envDir, "nodes")
	terraformDir := filepath.Join(envDir, ".terraform")

	// Check if environment exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		return fmt.Errorf("environment %q not found", d.Name)
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
		d.Term().Warning().Println("Cannot destroy infrastructure for manual provider")
		d.Term().Info().Println("Remove node files manually if needed")
		return nil
	}

	// Check if terraform state exists
	if _, err := os.Stat(filepath.Join(terraformDir, "terraform.tfstate")); os.IsNotExist(err) {
		d.Term().Warning().Println("No Terraform state found - nothing to destroy")
		return nil
	}

	// Confirm destruction
	if !d.Force {
		d.Term().Warning().Printfln("This will DESTROY all infrastructure for environment %q", d.Name)
		d.Term().Warning().Println("This action cannot be undone!")
		// TODO: Add interactive confirmation
		d.Term().Info().Println("Use --force to skip this confirmation")
		return fmt.Errorf("destruction cancelled - use --force to proceed")
	}

	d.Term().Warning().Printfln("Destroying infrastructure for environment %q", d.Name)

	ctx := context.Background()

	// Create Terraform manager
	tfManager, err := NewTerraformManager(envDir, false, true)
	if err != nil {
		return fmt.Errorf("failed to create terraform manager: %w", err)
	}

	// Initialize Terraform (in case state is missing)
	d.Term().Info().Println("Initializing Terraform...")
	if err := tfManager.Init(ctx); err != nil {
		return fmt.Errorf("terraform init failed: %w", err)
	}

	// Destroy
	d.Term().Info().Println("Destroying infrastructure...")
	if err := tfManager.Destroy(ctx); err != nil {
		return fmt.Errorf("terraform destroy failed: %w", err)
	}

	d.Term().Success().Println("Infrastructure destroyed")

	// Remove node files unless --keep-nodes
	if !d.KeepNodes {
		entries, err := os.ReadDir(nodesDir)
		if err == nil {
			for _, entry := range entries {
				if entry.Name() == ".gitkeep" {
					continue
				}
				nodePath := filepath.Join(nodesDir, entry.Name())
				if err := os.Remove(nodePath); err != nil {
					d.Log().Warn("Failed to remove node file", "file", nodePath, "error", err)
				} else {
					d.Term().Info().Printfln("Removed: %s", nodePath)
				}
			}
		}
		d.Term().Info().Println("Node files removed")
	} else {
		d.Term().Info().Println("Node files kept (--keep-nodes)")
	}

	// Clean up terraform directory
	if err := os.RemoveAll(terraformDir); err != nil {
		d.Log().Warn("Failed to remove terraform directory", "error", err)
	} else {
		d.Term().Info().Println("Terraform state cleaned up")
	}

	return nil
}
