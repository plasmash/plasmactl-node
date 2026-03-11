package allocate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/pkg/node"
	"github.com/plasmash/plasmactl-platform/pkg/graph"
	"gopkg.in/yaml.v3"
)

// AllocateResult is the structured output for node:allocate
type AllocateResult struct {
	Hostname string   `json:"hostname"`
	Zones    []string `json:"zones"`
	Modified bool     `json:"modified"`
}

// Allocate implements the node:allocate command
type Allocate struct {
	action.WithLogger
	action.WithTerm

	Hostname   string
	Operations []string
	Env        string

	result *AllocateResult
}

// Result returns the structured result for JSON output
func (a *Allocate) Result() any {
	return a.result
}

// ZoneOp represents a parsed zone operation
type ZoneOp struct {
	Type   string // "add", "remove", "replace"
	Zone   string
	NewVal string // only for replace
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

	var node node.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return fmt.Errorf("failed to parse node file: %w", err)
	}

	// No operations = show current allocations
	if len(a.Operations) == 0 {
		// Build result for show mode
		a.result = &AllocateResult{
			Hostname: node.Hostname,
			Zones:    node.Zones,
			Modified: false,
		}
		return a.showAllocations(&node)
	}

	// Load platform graph for zone validation
	var g *graph.PlatformGraph
	if pg, err := graph.Load(); err == nil {
		g = pg
	}

	// Parse and apply operations
	ops := a.parseOperations()
	modified := false

	for _, op := range ops {
		// Validate zones against graph for add/replace operations
		if g != nil && (op.Type == "add" || op.Type == "replace") {
			target := op.Zone
			if op.Type == "replace" {
				target = op.NewVal
			}
			if gNode := g.Node(target); gNode == nil {
				a.Term().Warning().Printfln("Warning: %q not found in platform graph", target)
			} else if gNode.Kind != "zone" {
				a.Term().Warning().Printfln("Warning: %q is a %s, not a zone", target, gNode.Kind)
			}
		}

		switch op.Type {
		case "add":
			if a.addZone(&node, op.Zone) {
				a.Term().Success().Printfln("Added: %s", op.Zone)
				modified = true
			} else {
				a.Term().Warning().Printfln("Already allocated: %s", op.Zone)
			}
		case "remove":
			if a.removeZone(&node, op.Zone) {
				a.Term().Success().Printfln("Removed: %s", op.Zone)
				modified = true
			} else {
				a.Term().Warning().Printfln("Not allocated: %s", op.Zone)
			}
		case "replace":
			removed := a.removeZone(&node, op.Zone)
			added := a.addZone(&node, op.NewVal)
			if removed || added {
				a.Term().Success().Printfln("Replaced: %s → %s", op.Zone, op.NewVal)
				modified = true
			} else {
				a.Term().Warning().Printfln("No change: %s → %s", op.Zone, op.NewVal)
			}
		}
	}

	if !modified {
		a.result = &AllocateResult{
			Hostname: node.Hostname,
			Zones:    node.Zones,
			Modified: false,
		}
		return nil
	}

	// Update labels based on zones
	node.AddZoneLabels()

	// Write back
	data, err = yaml.Marshal(&node)
	if err != nil {
		return fmt.Errorf("failed to marshal node: %w", err)
	}

	if err := os.WriteFile(nodeFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write node file: %w", err)
	}

	// Build result
	a.result = &AllocateResult{
		Hostname: node.Hostname,
		Zones:    node.Zones,
		Modified: modified,
	}

	return nil
}

// showAllocations displays current zone allocations with component info from graph
func (a *Allocate) showAllocations(n *node.Node) error {
	a.Term().Info().Printfln("Node: %s", n.Hostname)
	a.Term().Println()

	if len(n.Zones) == 0 {
		a.Term().Warning().Println("No zone allocations")
		a.Term().Println()
		a.Term().Info().Println("Tip: node:allocate HOSTNAME ZONE to add allocations")
		return nil
	}

	// Load graph for component info
	var g *graph.PlatformGraph
	if pg, err := graph.Load(); err == nil {
		g = pg
	}

	a.Term().Info().Println("Zone allocations:")
	for _, c := range n.Zones {
		a.Term().Printfln("  %s", c)
		// Show attached components from graph
		if g != nil {
			attached := g.EdgesFrom(c, "distributes")
			for _, e := range attached {
				a.Term().Printfln("    → %s (%s)", e.To().Name, e.To().Kind)
			}
		}
	}

	a.Term().Println()
	a.Term().Info().Println("Tip: ZONE (add), ZONE- (remove), OLD/NEW (replace)")

	return nil
}

// parseOperations parses the kubectl-style operations
func (a *Allocate) parseOperations() []ZoneOp {
	var ops []ZoneOp

	for _, arg := range a.Operations {
		switch {
		case strings.Contains(arg, "/"):
			// Replace: old/new
			parts := strings.SplitN(arg, "/", 2)
			ops = append(ops, ZoneOp{
				Type: "replace",
				Zone: parts[0],
				NewVal:  parts[1],
			})
		case strings.HasSuffix(arg, "-"):
			// Remove: zone-
			ops = append(ops, ZoneOp{
				Type: "remove",
				Zone: strings.TrimSuffix(arg, "-"),
			})
		default:
			// Add: zone
			ops = append(ops, ZoneOp{
				Type: "add",
				Zone: arg,
			})
		}
	}

	return ops
}

// addZone adds a zone to the node, returns true if added
func (a *Allocate) addZone(node *node.Node, zone string) bool {
	// Check if already exists
	for _, z := range node.Zones {
		if z == zone {
			return false
		}
	}

	node.Zones = append(node.Zones, zone)
	sort.Strings(node.Zones)
	return true
}

// removeZone removes a zone from the node, returns true if removed
func (a *Allocate) removeZone(node *node.Node, zone string) bool {
	for i, z := range node.Zones {
		if z == zone {
			node.Zones = append(node.Zones[:i], node.Zones[i+1:]...)
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
