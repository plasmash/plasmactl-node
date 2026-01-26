package action

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/pkg/types"
	"gopkg.in/yaml.v3"
)

// Allocate implements the node:allocate command
type Allocate struct {
	action.WithLogger
	action.WithTerm

	Hostname   string
	Operations []string
	Env        string
}

// ChassisOp represents a parsed chassis operation
type ChassisOp struct {
	Type    string // "add", "remove", "replace"
	Chassis string
	NewVal  string // only for replace
}

// Execute runs the node:allocate action
func (a *Allocate) Execute() error {
	// Find node file
	nodeFile, err := a.findNodeFile()
	if err != nil {
		return err
	}

	// Read node
	data, err := os.ReadFile(nodeFile)
	if err != nil {
		return fmt.Errorf("failed to read node file: %w", err)
	}

	var node types.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return fmt.Errorf("failed to parse node file: %w", err)
	}

	// No operations = show current allocations
	if len(a.Operations) == 0 {
		return a.showAllocations(&node)
	}

	// Parse and apply operations
	ops := a.parseOperations()
	modified := false

	for _, op := range ops {
		switch op.Type {
		case "add":
			if a.addChassis(&node, op.Chassis) {
				a.Term().Success().Printfln("Added: %s", op.Chassis)
				modified = true
			} else {
				a.Term().Warning().Printfln("Already allocated: %s", op.Chassis)
			}
		case "remove":
			if a.removeChassis(&node, op.Chassis) {
				a.Term().Success().Printfln("Removed: %s", op.Chassis)
				modified = true
			} else {
				a.Term().Warning().Printfln("Not allocated: %s", op.Chassis)
			}
		case "replace":
			removed := a.removeChassis(&node, op.Chassis)
			added := a.addChassis(&node, op.NewVal)
			if removed || added {
				a.Term().Success().Printfln("Replaced: %s → %s", op.Chassis, op.NewVal)
				modified = true
			} else {
				a.Term().Warning().Printfln("No change: %s → %s", op.Chassis, op.NewVal)
			}
		}
	}

	if !modified {
		return nil
	}

	// Update labels based on chassis
	node.AddChassisLabels()

	// Write back
	data, err = yaml.Marshal(&node)
	if err != nil {
		return fmt.Errorf("failed to marshal node: %w", err)
	}

	if err := os.WriteFile(nodeFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write node file: %w", err)
	}

	return nil
}

// showAllocations displays current chassis allocations
func (a *Allocate) showAllocations(node *types.Node) error {
	a.Term().Info().Printfln("Node: %s", node.Hostname)
	a.Term().Println()

	if len(node.Chassis) == 0 {
		a.Term().Warning().Println("No chassis allocations")
		a.Term().Println()
		a.Term().Info().Println("Tip: node:allocate HOSTNAME CHASSIS to add allocations")
		return nil
	}

	a.Term().Info().Println("Chassis allocations:")
	for _, c := range node.Chassis {
		a.Term().Printfln("  %s", c)
	}

	a.Term().Println()
	a.Term().Info().Println("Tip: CHASSIS (add), CHASSIS- (remove), OLD/NEW (replace)")

	return nil
}

// parseOperations parses the kubectl-style operations
func (a *Allocate) parseOperations() []ChassisOp {
	var ops []ChassisOp

	for _, arg := range a.Operations {
		switch {
		case strings.Contains(arg, "/"):
			// Replace: old/new
			parts := strings.SplitN(arg, "/", 2)
			ops = append(ops, ChassisOp{
				Type:    "replace",
				Chassis: parts[0],
				NewVal:  parts[1],
			})
		case strings.HasSuffix(arg, "-"):
			// Remove: chassis-
			ops = append(ops, ChassisOp{
				Type:    "remove",
				Chassis: strings.TrimSuffix(arg, "-"),
			})
		default:
			// Add: chassis
			ops = append(ops, ChassisOp{
				Type:    "add",
				Chassis: arg,
			})
		}
	}

	return ops
}

// addChassis adds a chassis to the node, returns true if added
func (a *Allocate) addChassis(node *types.Node, chassis string) bool {
	// Check if already exists
	for _, c := range node.Chassis {
		if c == chassis {
			return false
		}
	}

	node.Chassis = append(node.Chassis, chassis)
	sort.Strings(node.Chassis)
	return true
}

// removeChassis removes a chassis from the node, returns true if removed
func (a *Allocate) removeChassis(node *types.Node, chassis string) bool {
	for i, c := range node.Chassis {
		if c == chassis {
			node.Chassis = append(node.Chassis[:i], node.Chassis[i+1:]...)
			return true
		}
	}
	return false
}

// findNodeFile locates the node YAML file
func (a *Allocate) findNodeFile() (string, error) {
	// If env is specified, look in inst/<env>/nodes/
	if a.Env != "" {
		nodeFile := filepath.Join("inst", a.Env, "nodes", a.Hostname+".yaml")
		if _, err := os.Stat(nodeFile); err == nil {
			return nodeFile, nil
		}
		return "", fmt.Errorf("node %s not found in environment %s", a.Hostname, a.Env)
	}

	// Search all environments
	envDir := "inst"
	entries, err := os.ReadDir(envDir)
	if err != nil {
		return "", fmt.Errorf("failed to read inst directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		nodeFile := filepath.Join(envDir, entry.Name(), "nodes", a.Hostname+".yaml")
		if _, err := os.Stat(nodeFile); err == nil {
			return nodeFile, nil
		}
	}

	return "", fmt.Errorf("node %s not found in any environment", a.Hostname)
}
