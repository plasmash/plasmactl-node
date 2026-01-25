package plasmactlnode

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-node/pkg/types"
	"gopkg.in/yaml.v3"
)

// listAction implements the env:list command
type listAction struct {
	log  *launchr.Logger
	term *launchr.Terminal
}

// SetLogger sets the logger for the action
func (a *listAction) SetLogger(log *launchr.Logger) {
	a.log = log
}

// SetTerm sets the terminal for the action
func (a *listAction) SetTerm(term *launchr.Terminal) {
	a.term = term
}

// Execute runs the env:list action
func (a *listAction) Execute() error {
	envDir := "inst"

	// Check if env directory exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		a.term.Warning().Println("No environments found (inst/ directory does not exist)")
		return nil
	}

	entries, err := os.ReadDir(envDir)
	if err != nil {
		return fmt.Errorf("failed to read env directory: %w", err)
	}

	if len(entries) == 0 {
		a.term.Warning().Println("No environments found")
		return nil
	}

	a.term.Info().Println("Environments:")
	a.term.Println()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		platformFile := filepath.Join(envDir, name, "platform.yaml")
		nodesDir := filepath.Join(envDir, name, "nodes")

		// Count nodes
		nodeCount := 0
		if files, err := os.ReadDir(nodesDir); err == nil {
			for _, f := range files {
				if !f.IsDir() && filepath.Ext(f.Name()) == ".yaml" {
					nodeCount++
				}
			}
		}

		// Read platform.yaml for provider info
		provider := "unknown"
		domain := ""
		if data, err := os.ReadFile(platformFile); err == nil {
			var platform types.Platform
			if err := yaml.Unmarshal(data, &platform); err == nil {
				provider = platform.Infrastructure.Provider
				domain = platform.Networking.Domain
			}
		}

		// Format output
		a.term.Printf("  %s", name)
		a.term.Printf("  [%s]", provider)
		if domain != "" {
			a.term.Printf("  %s", domain)
		}
		a.term.Printf("  (%d nodes)", nodeCount)
		a.term.Println()
	}

	return nil
}
