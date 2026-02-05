package node

import (
	"sort"

	"github.com/plasmash/plasmactl-chassis/pkg/chassis"
)

// Nodes is a collection of Node.
type Nodes []Node

// Find returns the node with the given hostname, or nil if not found.
func (ns Nodes) Find(hostname string) *Node {
	for i := range ns {
		if ns[i].Hostname == hostname {
			return &ns[i]
		}
	}
	return nil
}

// Hostnames returns a list of all hostnames.
func (ns Nodes) Hostnames() []string {
	names := make([]string, len(ns))
	for i, n := range ns {
		names[i] = n.Hostname
	}
	return names
}

// Allocations computes effective chassis allocations for all nodes
// by distributing them over the chassis structure.
//
// Distribution rules (matching platform_nodes.py behavior):
//  1. Nodes are directly allocated to chassis paths via their Chassis field
//  2. Downward: Nodes propagate from parent to empty child paths
//     (a path is "empty" if no node is directly allocated to it)
//  3. Upward: Nodes propagate to all ancestor paths
//
// Returns: hostname → []chassisPaths (effective allocations, sorted)
func (ns Nodes) Allocations(c *chassis.Chassis) map[string][]string {
	if c == nil || len(ns) == 0 {
		return nil
	}

	// Phase 0: Build tree relationships
	childrenMap := c.ChildrenMap()
	treeOrder := c.Flatten()

	// Phase 1: Initialize allocations and collect directly occupied chassis paths
	allocs := make(map[string][]string)
	directlyOccupied := make(map[string]bool)

	for _, node := range ns {
		allocs[node.Hostname] = append([]string{}, node.Chassis...)
		for _, chassisPath := range node.Chassis {
			directlyOccupied[chassisPath] = true
		}
	}

	// Phase 2: Downward propagation (tree order - parent before children)
	// For each chassis path in tree order, propagate nodes to empty children
	for _, chassisPath := range treeOrder {
		nodesAtChassis := ns.atChassis(chassisPath, allocs)
		children := childrenMap[chassisPath]

		for _, child := range children {
			if !directlyOccupied[child] {
				// Empty child inherits parent's nodes
				for _, node := range nodesAtChassis {
					allocs[node.Hostname] = appendUnique(allocs[node.Hostname], child)
				}
			}
		}
	}

	// Phase 3: Upward propagation (add all ancestors)
	for hostname, chassisPaths := range allocs {
		var toAdd []string
		for _, chassisPath := range chassisPaths {
			for _, ancestor := range c.Ancestors(chassisPath) {
				toAdd = append(toAdd, ancestor)
			}
		}
		for _, ancestor := range toAdd {
			allocs[hostname] = appendUnique(allocs[hostname], ancestor)
		}
	}

	// Sort each node's chassis paths for consistent output
	for hostname := range allocs {
		sort.Strings(allocs[hostname])
	}

	return allocs
}

// atChassis returns nodes currently allocated to a chassis path.
func (ns Nodes) atChassis(chassisPath string, allocs map[string][]string) []Node {
	var result []Node
	for _, n := range ns {
		if contains(allocs[n.Hostname], chassisPath) {
			result = append(result, n)
		}
	}
	return result
}

// ForChassis returns nodes directly allocated to a chassis path or its descendants.
func (ns Nodes) ForChassis(chassisPath string) Nodes {
	var result Nodes
	for _, n := range ns {
		for _, c := range n.Chassis {
			if c == chassisPath || chassis.IsDescendantOf(c, chassisPath) {
				result = append(result, n)
				break
			}
		}
	}
	return result
}

// appendUnique appends value to slice if not already present.
func appendUnique(slice []string, value string) []string {
	for _, v := range slice {
		if v == value {
			return slice
		}
	}
	return append(slice, value)
}

// contains checks if slice contains value.
func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}
