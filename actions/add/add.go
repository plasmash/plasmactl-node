package add

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/internal/allocator"
	"github.com/plasmash/plasmactl-node/pkg/node"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// AddResult is the structured output for node:add
type AddResult struct {
	Hostname  string   `json:"hostname"`
	Platform  string   `json:"platform"`
	PublicIP  string   `json:"public_ip"`
	PrivateIP string   `json:"private_ip"`
	Chassis   []string `json:"chassis,omitempty"`
	File      string   `json:"file"`
}

// Add implements the node:add command
type Add struct {
	action.WithLogger
	action.WithTerm

	Platform  string
	Hostname  string
	PublicIP  string
	PrivateIP string
	Chassis   []string

	result *AddResult
}

// Result returns the structured result for JSON output
func (a *Add) Result() any {
	return a.result
}

// Execute runs the node:add action
func (a *Add) Execute() error {
	envDir := filepath.Join("inst", a.Platform)
	nodesDir := filepath.Join(envDir, "nodes")
	platformFile := filepath.Join(envDir, "platform.yaml")

	// Check if platform exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		return fmt.Errorf("platform %q not found", a.Platform)
	}

	// Read platform.yaml to get network configuration
	platformData, err := os.ReadFile(platformFile)
	if err != nil {
		return fmt.Errorf("failed to read platform.yaml: %w", err)
	}

	var platform schema.Platform
	if err := yaml.Unmarshal(platformData, &platform); err != nil {
		return fmt.Errorf("failed to parse platform.yaml: %w", err)
	}

	// Allocate private IP if not provided
	privateIP := a.PrivateIP
	if privateIP == "" {
		ipAlloc, err := allocator.NewIPAllocator(platform.Networking.PrivateNetwork, nodesDir)
		if err != nil {
			return fmt.Errorf("failed to create IP allocator: %w", err)
		}

		privateIP, err = ipAlloc.Allocate()
		if err != nil {
			return fmt.Errorf("failed to allocate private IP: %w", err)
		}
		a.Term().Info().Printfln("Auto-assigned private IP: %s", privateIP)
	}

	// Check for hostname collision
	nodeFile := filepath.Join(nodesDir, a.Hostname+".yaml")
	if _, err := os.Stat(nodeFile); !os.IsNotExist(err) {
		return fmt.Errorf("node %q already exists", a.Hostname)
	}

	// Ensure nodes directory exists
	if err := os.MkdirAll(nodesDir, 0755); err != nil {
		return fmt.Errorf("failed to create nodes directory: %w", err)
	}

	// Create node configuration
	n := node.NewNode(a.Hostname, a.Chassis, a.PublicIP, privateIP)
	n.AddChassisLabels()

	data, err := yaml.Marshal(n)
	if err != nil {
		return fmt.Errorf("failed to marshal node: %w", err)
	}

	if err := os.WriteFile(nodeFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write node file: %w", err)
	}

	// Build result
	a.result = &AddResult{
		Hostname:  a.Hostname,
		Platform:  a.Platform,
		PublicIP:  a.PublicIP,
		PrivateIP: privateIP,
		Chassis:   a.Chassis,
		File:      nodeFile,
	}

	a.Term().Success().Printfln("Added node %s to platform %s", a.Hostname, a.Platform)
	a.Term().Info().Printfln("  Public IP:  %s", a.PublicIP)
	a.Term().Info().Printfln("  Private IP: %s", privateIP)
	if len(a.Chassis) > 0 {
		a.Term().Info().Printfln("  Chassis:    %v", a.Chassis)
	}
	a.Term().Info().Printfln("  File:       %s", nodeFile)

	return nil
}
