package plasmactlnode

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-node/pkg/types"
	"gopkg.in/yaml.v3"
)

// nodeAction implements the env:node command
type nodeAction struct {
	log  *launchr.Logger
	term *launchr.Terminal

	envName   string
	hostname  string
	publicIP  string
	privateIP string
	chassis   []string
}

// SetLogger sets the logger for the action
func (a *nodeAction) SetLogger(log *launchr.Logger) {
	a.log = log
}

// SetTerm sets the terminal for the action
func (a *nodeAction) SetTerm(term *launchr.Terminal) {
	a.term = term
}

// Execute runs the env:node action
func (a *nodeAction) Execute() error {
	envDir := filepath.Join("env", a.envName)
	nodesDir := filepath.Join(envDir, "nodes")
	platformFile := filepath.Join(envDir, "platform.yaml")

	// Check if environment exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		return fmt.Errorf("environment %q not found", a.envName)
	}

	// Read platform.yaml to get network configuration
	platformData, err := os.ReadFile(platformFile)
	if err != nil {
		return fmt.Errorf("failed to read platform.yaml: %w", err)
	}

	var platform types.Platform
	if err := yaml.Unmarshal(platformData, &platform); err != nil {
		return fmt.Errorf("failed to parse platform.yaml: %w", err)
	}

	// Allocate private IP if not provided
	privateIP := a.privateIP
	if privateIP == "" {
		allocator, err := NewIPAllocator(platform.Networking.PrivateNetwork, nodesDir)
		if err != nil {
			return fmt.Errorf("failed to create IP allocator: %w", err)
		}

		privateIP, err = allocator.Allocate()
		if err != nil {
			return fmt.Errorf("failed to allocate private IP: %w", err)
		}
		a.term.Info().Printfln("Auto-assigned private IP: %s", privateIP)
	}

	// Check for hostname collision
	nodeFile := filepath.Join(nodesDir, a.hostname+".yaml")
	if _, err := os.Stat(nodeFile); !os.IsNotExist(err) {
		return fmt.Errorf("node %q already exists", a.hostname)
	}

	// Ensure nodes directory exists
	if err := os.MkdirAll(nodesDir, 0755); err != nil {
		return fmt.Errorf("failed to create nodes directory: %w", err)
	}

	// Create node configuration
	node := types.NewNode(a.hostname, a.chassis, a.publicIP, privateIP)
	node.AddChassisLabels()

	data, err := yaml.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to marshal node: %w", err)
	}

	if err := os.WriteFile(nodeFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write node file: %w", err)
	}

	a.term.Success().Printfln("Created node %s", a.hostname)
	a.term.Info().Printfln("  Public IP:  %s", a.publicIP)
	a.term.Info().Printfln("  Private IP: %s", privateIP)
	if len(a.chassis) > 0 {
		a.term.Info().Printfln("  Chassis:    %v", a.chassis)
	}
	a.term.Info().Printfln("  File:       %s", nodeFile)

	return nil
}
