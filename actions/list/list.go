package list

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// List implements the node:list command
type List struct {
	action.WithLogger
	action.WithTerm
}

// Execute runs the node:list action
func (l *List) Execute() error {
	envDir := "inst"

	// Check if env directory exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		l.Term().Warning().Println("No environments found (inst/ directory does not exist)")
		return nil
	}

	entries, err := os.ReadDir(envDir)
	if err != nil {
		return fmt.Errorf("failed to read env directory: %w", err)
	}

	if len(entries) == 0 {
		l.Term().Warning().Println("No environments found")
		return nil
	}

	l.Term().Info().Println("Environments:")
	l.Term().Println()

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
			var platform schema.Platform
			if err := yaml.Unmarshal(data, &platform); err == nil {
				provider = platform.Infrastructure.MetalProvider
				domain = platform.DNS.Domain
			}
		}

		// Format output
		l.Term().Printf("  %s", name)
		l.Term().Printf("  [%s]", provider)
		if domain != "" {
			l.Term().Printf("  %s", domain)
		}
		l.Term().Printf("  (%d nodes)", nodeCount)
		l.Term().Println()
	}

	return nil
}
