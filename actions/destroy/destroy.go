package destroy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/internal/provisioner"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// DestroyResult is the structured output for node:destroy
type DestroyResult struct {
	Name         string   `json:"name"`
	NodesRemoved []string `json:"nodes_removed,omitempty"`
	Success      bool     `json:"success"`
}

// Destroy implements the node:destroy command
type Destroy struct {
	action.WithLogger
	action.WithTerm

	Keyring   keyring.Keyring
	Name      string
	Force     bool
	KeepNodes bool

	result *DestroyResult
}

// Result returns the structured result for JSON output
func (d *Destroy) Result() any {
	return d.result
}

// Execute runs the node:destroy action
func (d *Destroy) Execute() error {
	d.result = &DestroyResult{Name: d.Name}

	envDir := filepath.Join("inst", d.Name)
	platformFile := filepath.Join(envDir, "platform.yaml")
	nodesDir := filepath.Join(envDir, "nodes")

	// Check if environment exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		return fmt.Errorf("environment %q not found", d.Name)
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

	// Check if provider is manual
	if platform.Infrastructure.MetalProvider == "manual" {
		d.Term().Warning().Println("Cannot destroy infrastructure for manual provider")
		d.Term().Info().Println("Remove node files manually if needed")
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

	// Create provisioner manager
	mgr, err := provisioner.NewManager(d.Name, false, true)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Initialize
	d.Term().Info().Println("Initializing provisioner...")
	if err := mgr.Init(ctx); err != nil {
		return fmt.Errorf("provisioner init failed: %w", err)
	}

	// Destroy
	d.Term().Info().Println("Destroying infrastructure...")
	if err := mgr.Destroy(ctx); err != nil {
		return fmt.Errorf("provisioner destroy failed: %w", err)
	}

	d.Term().Success().Println("Infrastructure destroyed")
	d.result.Success = true

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
					d.result.NodesRemoved = append(d.result.NodesRemoved, entry.Name())
					d.Term().Info().Printfln("Removed: %s", nodePath)
				}
			}
		}
		d.Term().Info().Println("Node files removed")
	} else {
		d.Term().Info().Println("Node files kept (--keep-nodes)")
	}

	return nil
}
