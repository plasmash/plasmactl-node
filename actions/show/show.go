package show

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/internal/types"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// Show implements the node:show command
type Show struct {
	action.WithLogger
	action.WithTerm

	Name string
}

// Execute runs the node:show action
func (s *Show) Execute() error {
	envDir := filepath.Join("inst", s.Name)
	platformFile := filepath.Join(envDir, "platform.yaml")
	nodesDir := filepath.Join(envDir, "nodes")

	// Check if environment exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		return fmt.Errorf("environment %q not found", s.Name)
	}

	// Read platform.yaml
	data, err := os.ReadFile(platformFile)
	if err != nil {
		return fmt.Errorf("failed to read platform.yaml: %w", err)
	}

	var platform schema.Platform
	if err := yaml.Unmarshal(data, &platform); err != nil {
		return fmt.Errorf("failed to parse platform.yaml: %w", err)
	}

	// Print environment details
	s.Term().Info().Printfln("Environment: %s", s.Name)
	s.Term().Println()

	s.Term().Printf("  Provider:    %s\n", platform.Infrastructure.MetalProvider)
	if platform.DNS.Domain != "" {
		s.Term().Printf("  Domain:      %s\n", platform.DNS.Domain)
	}
	if platform.Cluster != "" {
		s.Term().Printf("  Cluster:     %s\n", platform.Cluster)
	}
	if platform.Description != "" {
		s.Term().Printf("  Description: %s\n", platform.Description)
	}

	// Print networking
	if platform.Networking.PrivateNetwork != "" {
		s.Term().Println()
		s.Term().Info().Println("Networking:")
		s.Term().Printf("  Private Network: %s\n", platform.Networking.PrivateNetwork)
		if platform.Networking.Bus.IP != "" {
			s.Term().Printf("  Bus IP:          %s\n", platform.Networking.Bus.IP)
		}
	}

	// Print chassis configuration
	if len(platform.Chassis) > 0 {
		s.Term().Println()
		s.Term().Info().Println("Chassis Configuration:")
		for chassis, profiles := range platform.Chassis {
			for _, profile := range profiles {
				s.Term().Printf("  %s: %s x%d\n", chassis, profile.Type, profile.Count)
			}
		}
	}

	// List nodes
	nodes, err := s.listNodes(nodesDir)
	if err != nil {
		s.Log().Warn("Failed to list nodes", "error", err)
	}

	s.Term().Println()
	s.Term().Info().Printfln("Nodes: %d", len(nodes))

	for _, node := range nodes {
		chassis := ""
		if len(node.Chassis) > 0 {
			chassis = node.Chassis[0]
			if len(node.Chassis) > 1 {
				chassis += fmt.Sprintf(" +%d more", len(node.Chassis)-1)
			}
		}
		s.Term().Printf("  %-30s  %-15s  %-15s  %s\n",
			node.Hostname,
			node.Network.PublicIP,
			node.Network.PrivateIP,
			chassis,
		)
	}

	return nil
}

func (s *Show) listNodes(nodesDir string) ([]types.Node, error) {
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
