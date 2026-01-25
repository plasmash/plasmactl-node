package plasmactlnode

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-node/pkg/types"
	"gopkg.in/yaml.v3"
)

// showAction implements the env:show command
type showAction struct {
	log  *launchr.Logger
	term *launchr.Terminal

	name string
}

// SetLogger sets the logger for the action
func (a *showAction) SetLogger(log *launchr.Logger) {
	a.log = log
}

// SetTerm sets the terminal for the action
func (a *showAction) SetTerm(term *launchr.Terminal) {
	a.term = term
}

// Execute runs the env:show action
func (a *showAction) Execute() error {
	envDir := filepath.Join("env", a.name)
	platformFile := filepath.Join(envDir, "platform.yaml")
	nodesDir := filepath.Join(envDir, "nodes")

	// Check if environment exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		return fmt.Errorf("environment %q not found", a.name)
	}

	// Read platform.yaml
	data, err := os.ReadFile(platformFile)
	if err != nil {
		return fmt.Errorf("failed to read platform.yaml: %w", err)
	}

	var platform types.Platform
	if err := yaml.Unmarshal(data, &platform); err != nil {
		return fmt.Errorf("failed to parse platform.yaml: %w", err)
	}

	// Print environment details
	a.term.Info().Printfln("Environment: %s", a.name)
	a.term.Println()

	a.term.Printf("  Provider:    %s\n", platform.Infrastructure.Provider)
	if platform.Networking.Domain != "" {
		a.term.Printf("  Domain:      %s\n", platform.Networking.Domain)
	}
	if platform.Cluster != "" {
		a.term.Printf("  Cluster:     %s\n", platform.Cluster)
	}
	if platform.Description != "" {
		a.term.Printf("  Description: %s\n", platform.Description)
	}

	// Print networking
	if platform.Networking.PrivateNetwork != "" {
		a.term.Println()
		a.term.Info().Println("Networking:")
		a.term.Printf("  Private Network: %s\n", platform.Networking.PrivateNetwork)
		if platform.Networking.Bus.IP != "" {
			a.term.Printf("  Bus IP:          %s\n", platform.Networking.Bus.IP)
		}
	}

	// Print chassis configuration
	if len(platform.Chassis) > 0 {
		a.term.Println()
		a.term.Info().Println("Chassis Configuration:")
		for chassis, profiles := range platform.Chassis {
			for _, profile := range profiles {
				a.term.Printf("  %s: %s x%d\n", chassis, profile.Type, profile.Count)
			}
		}
	}

	// List nodes
	nodes, err := a.listNodes(nodesDir)
	if err != nil {
		a.log.Warn("Failed to list nodes", "error", err)
	}

	a.term.Println()
	a.term.Info().Printfln("Nodes: %d", len(nodes))

	for _, node := range nodes {
		chassis := ""
		if len(node.Chassis) > 0 {
			chassis = node.Chassis[0]
			if len(node.Chassis) > 1 {
				chassis += fmt.Sprintf(" +%d more", len(node.Chassis)-1)
			}
		}
		a.term.Printf("  %-30s  %-15s  %-15s  %s\n",
			node.Hostname,
			node.Network.PublicIP,
			node.Network.PrivateIP,
			chassis,
		)
	}

	return nil
}

func (a *showAction) listNodes(nodesDir string) ([]types.Node, error) {
	var nodes []types.Node

	if _, err := os.Stat(nodesDir); os.IsNotExist(err) {
		return nodes, nil
	}

	entries, err := os.ReadDir(nodesDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		if entry.Name() == ".gitkeep" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(nodesDir, entry.Name()))
		if err != nil {
			continue
		}

		var node types.Node
		if err := yaml.Unmarshal(data, &node); err != nil {
			continue
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}
