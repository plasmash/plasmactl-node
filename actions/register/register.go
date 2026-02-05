package register

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/internal/allocator"
	"github.com/plasmash/plasmactl-node/internal/types"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// RegisterResult is the structured output for node:register
type RegisterResult struct {
	Hostname  string   `json:"hostname"`
	Platform  string   `json:"platform"`
	PublicIP  string   `json:"public_ip"`
	PrivateIP string   `json:"private_ip"`
	Chassis   []string `json:"chassis,omitempty"`
	File      string   `json:"file"`
}

// Register implements the node:register command
type Register struct {
	action.WithLogger
	action.WithTerm

	EnvName   string
	Hostname  string
	PublicIP  string
	PrivateIP string
	Chassis   []string

	result *RegisterResult
}

// Result returns the structured result for JSON output
func (r *Register) Result() any {
	return r.result
}

// Execute runs the node:register action
func (r *Register) Execute() error {
	envDir := filepath.Join("inst", r.EnvName)
	nodesDir := filepath.Join(envDir, "nodes")
	platformFile := filepath.Join(envDir, "platform.yaml")

	// Check if environment exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		return fmt.Errorf("environment %q not found", r.EnvName)
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
	privateIP := r.PrivateIP
	if privateIP == "" {
		ipAlloc, err := allocator.NewIPAllocator(platform.Networking.PrivateNetwork, nodesDir)
		if err != nil {
			return fmt.Errorf("failed to create IP allocator: %w", err)
		}

		privateIP, err = ipAlloc.Allocate()
		if err != nil {
			return fmt.Errorf("failed to allocate private IP: %w", err)
		}
		r.Term().Info().Printfln("Auto-assigned private IP: %s", privateIP)
	}

	// Check for hostname collision
	nodeFile := filepath.Join(nodesDir, r.Hostname+".yaml")
	if _, err := os.Stat(nodeFile); !os.IsNotExist(err) {
		return fmt.Errorf("node %q already exists", r.Hostname)
	}

	// Ensure nodes directory exists
	if err := os.MkdirAll(nodesDir, 0755); err != nil {
		return fmt.Errorf("failed to create nodes directory: %w", err)
	}

	// Create node configuration
	node := types.NewNode(r.Hostname, r.Chassis, r.PublicIP, privateIP)
	node.AddChassisLabels()

	data, err := yaml.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to marshal node: %w", err)
	}

	if err := os.WriteFile(nodeFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write node file: %w", err)
	}

	// Build result
	r.result = &RegisterResult{
		Hostname:  r.Hostname,
		Platform:  r.EnvName,
		PublicIP:  r.PublicIP,
		PrivateIP: privateIP,
		Chassis:   r.Chassis,
		File:      nodeFile,
	}

	r.Term().Success().Printfln("Created node %s", r.Hostname)
	r.Term().Info().Printfln("  Public IP:  %s", r.PublicIP)
	r.Term().Info().Printfln("  Private IP: %s", privateIP)
	if len(r.Chassis) > 0 {
		r.Term().Info().Printfln("  Chassis:    %v", r.Chassis)
	}
	r.Term().Info().Printfln("  File:       %s", nodeFile)

	return nil
}
