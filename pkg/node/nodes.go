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
//  1. Nodes are directly allocated to sections via their Chassis field
//  2. Downward: Nodes propagate from parent to empty child sections
//     (a section is "empty" if no node is directly allocated to it)
//  3. Upward: Nodes propagate to all ancestor sections
//
// Returns: hostname → []sections (effective allocations, sorted)
func (ns Nodes) Allocations(c *chassis.Chassis) map[string][]string {
	if c == nil || len(ns) == 0 {
		return nil
	}

	// Phase 0: Build tree relationships
	childrenMap := c.ChildrenMap()
	treeOrder := c.Flatten()

	// Phase 1: Initialize allocations and collect directly occupied sections
	allocs := make(map[string][]string)
	directlyOccupied := make(map[string]bool)

	for _, node := range ns {
		allocs[node.Hostname] = append([]string{}, node.Chassis...)
		for _, section := range node.Chassis {
			directlyOccupied[section] = true
		}
	}

	// Phase 2: Downward propagation (tree order - parent before children)
	// For each section in tree order, propagate nodes to empty children
	for _, section := range treeOrder {
		nodesInSection := ns.inSection(section, allocs)
		children := childrenMap[section]

		for _, child := range children {
			if !directlyOccupied[child] {
				// Empty child inherits parent's nodes
				for _, node := range nodesInSection {
					allocs[node.Hostname] = appendUnique(allocs[node.Hostname], child)
				}
			}
		}
	}

	// Phase 3: Upward propagation (add all ancestors)
	for hostname, sections := range allocs {
		var toAdd []string
		for _, section := range sections {
			for _, ancestor := range c.Ancestors(section) {
				toAdd = append(toAdd, ancestor)
			}
		}
		for _, ancestor := range toAdd {
			allocs[hostname] = appendUnique(allocs[hostname], ancestor)
		}
	}

	// Sort each node's sections for consistent output
	for hostname := range allocs {
		sort.Strings(allocs[hostname])
	}

	return allocs
}

// inSection returns nodes currently allocated to a section.
func (ns Nodes) inSection(section string, allocs map[string][]string) []Node {
	var result []Node
	for _, n := range ns {
		if contains(allocs[n.Hostname], section) {
			result = append(result, n)
		}
	}
	return result
}

// ForSection returns nodes directly allocated to a section or its descendants.
func (ns Nodes) ForSection(section string) Nodes {
	var result Nodes
	for _, n := range ns {
		for _, c := range n.Chassis {
			if c == section || chassis.IsDescendantOf(c, section) {
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
