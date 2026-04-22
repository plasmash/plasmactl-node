package node

import (
	"sort"

	"github.com/plasmash/plasmactl-zone/pkg/topology"
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

// Allocations computes effective zone allocations for all nodes
// by distributing them over the topology structure.
//
// Distribution rules (matching platform_nodes.py behavior):
//  1. Nodes are directly allocated to zones via their Zones field
//  2. Downward: Nodes propagate from parent to empty child zones
//     (a zone is "empty" if no node is directly allocated to it)
//  3. Upward: Nodes propagate to all ancestor zones
//
// Returns: hostname → []zones (effective allocations, sorted)
func (ns Nodes) Allocations(t *topology.Topology) map[string][]string {
	if t == nil || len(ns) == 0 {
		return nil
	}

	// Phase 0: Build tree relationships
	childrenMap := t.ChildrenMap()
	treeOrder := t.Flatten()

	// Phase 1: Initialize allocations and collect directly occupied zones
	allocs := make(map[string][]string)
	directlyOccupied := make(map[string]bool)

	for _, node := range ns {
		allocs[node.Hostname] = append([]string{}, node.Zones...)
		for _, zonePath := range node.Zones {
			directlyOccupied[zonePath] = true
		}
	}

	// Phase 2: Downward propagation (tree order - parent before children)
	// For each zone in tree order, propagate nodes to empty children
	for _, zonePath := range treeOrder {
		nodesAtZone := ns.atZone(zonePath, allocs)
		children := childrenMap[zonePath]

		for _, child := range children {
			if !directlyOccupied[child] {
				// Empty child inherits parent's nodes
				for _, node := range nodesAtZone {
					allocs[node.Hostname] = appendUnique(allocs[node.Hostname], child)
				}
			}
		}
	}

	// Phase 3: Upward propagation (add all ancestors)
	for hostname, zonePaths := range allocs {
		var toAdd []string
		for _, zonePath := range zonePaths {
			for _, ancestor := range t.Ancestors(zonePath) {
				toAdd = append(toAdd, ancestor)
			}
		}
		for _, ancestor := range toAdd {
			allocs[hostname] = appendUnique(allocs[hostname], ancestor)
		}
	}

	// Sort each node's zones for consistent output
	for hostname := range allocs {
		sort.Strings(allocs[hostname])
	}

	return allocs
}

// atZone returns nodes currently allocated to a zone.
func (ns Nodes) atZone(zonePath string, allocs map[string][]string) []Node {
	var result []Node
	for _, n := range ns {
		if contains(allocs[n.Hostname], zonePath) {
			result = append(result, n)
		}
	}
	return result
}

// ForZone returns nodes directly allocated to a zone or its descendants.
func (ns Nodes) ForZone(zonePath string) Nodes {
	var result Nodes
	for _, n := range ns {
		for _, z := range n.Zones {
			if z == zonePath || topology.IsDescendantOf(z, zonePath) {
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
