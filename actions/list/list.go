package list

import (
	"fmt"
	"sort"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-component/pkg/component"
	"github.com/plasmash/plasmactl-zone/pkg/topology"
	"github.com/plasmash/plasmactl-node/pkg/node"
)

// NodeListItem represents a node in the list output
type NodeListItem struct {
	Node       string   `json:"node"                  yaml:"node"`
	Platform   string   `json:"platform"              yaml:"platform"`
	Zones      []string `json:"zones,omitempty"       yaml:"zones,omitempty"`
	Components []string `json:"components,omitempty"  yaml:"components,omitempty"`
}

// ListResult is the structured output for node:list
type ListResult struct {
	Nodes []NodeListItem `json:"nodes"`
}

// List implements the node:list command
type List struct {
	action.WithLogger
	action.WithTerm

	Tree bool

	result *ListResult
}

// Result returns the structured result for JSON output
func (l *List) Result() any {
	return l.result
}

// Execute runs the node:list action
func (l *List) Execute() error {
	l.result = &ListResult{Nodes: []NodeListItem{}}

	// Load nodes by platform
	nodesByPlatform, err := node.LoadByPlatform(".")
	if err != nil {
		return fmt.Errorf("failed to load nodes: %w", err)
	}

	if len(nodesByPlatform) == 0 {
		l.Term().Warning().Println("No nodes found")
		return nil
	}

	// Sort platforms for consistent ordering
	var platforms []string
	for platform := range nodesByPlatform {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)

	// Load topology and components once (only needed for tree mode)
	var topo *topology.Topology
	zoneToComponents := make(map[string][]string)
	if l.Tree {
		topo, err = topology.Load(".")
		if err != nil {
			l.Log().Debug("Failed to load topology", "error", err)
		}
		components, _ := component.LoadFromPlaybooks(".")
		for _, comp := range components {
			zoneToComponents[comp.Zone] = append(zoneToComponents[comp.Zone], comp.Name)
		}
	}

	for _, platform := range platforms {
		nodes := nodesByPlatform[platform]
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].Hostname < nodes[j].Hostname
		})

		var allocations map[string][]string
		if l.Tree && topo != nil {
			allocations = nodes.Allocations(topo)
		}

		for _, n := range nodes {
			item := NodeListItem{
				Node:     n.Hostname,
				Platform: n.Platform,
			}

			if l.Tree {
				zonePaths := allocations[n.Hostname]
				sort.Strings(zonePaths)
				if len(zonePaths) > 0 {
					item.Zones = zonePaths
				}

				// Collect components across all zones for this node
				var nodeComps []string
				seen := make(map[string]bool)
				for _, p := range zonePaths {
					for _, comp := range zoneToComponents[p] {
						if !seen[comp] {
							seen[comp] = true
							nodeComps = append(nodeComps, comp)
						}
					}
				}
				sort.Strings(nodeComps)
				if len(nodeComps) > 0 {
					item.Components = nodeComps
				}
			}

			l.result.Nodes = append(l.result.Nodes, item)
		}
	}

	if l.Tree {
		return l.printTree(platforms, nodesByPlatform)
	}

	// Flat output - one per line, scriptable
	for _, item := range l.result.Nodes {
		l.Term().Printfln("%s@%s", item.Node, item.Platform)
	}

	return nil
}

// printTree prints nodes as a tree with zones (📍) and components (🧩)
// It reads enriched data from l.result.Nodes which was populated in Execute.
func (l *List) printTree(platforms []string, nodesByPlatform map[string]node.Nodes) error {
	term := l.Term()
	resultIdx := 0

	for pi, platform := range platforms {
		nodes := nodesByPlatform[platform]
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].Hostname < nodes[j].Hostname
		})

		// Print platform header
		term.Printfln("%s", platform)

		for ni, n := range nodes {
			var nodePrefix, nodeIndent string
			if ni == len(nodes)-1 {
				nodePrefix = "└── "
				nodeIndent = "    "
			} else {
				nodePrefix = "├── "
				nodeIndent = "│   "
			}

			term.Printfln("%s🖥  %s", nodePrefix, n.DisplayName())

			// Enriched data already computed in Execute
			item := l.result.Nodes[resultIdx]
			resultIdx++

			zonePaths := item.Zones
			nodeComponents := item.Components
			totalChildren := len(zonePaths) + len(nodeComponents)
			childIdx := 0

			// Print zones
			for _, zonePath := range zonePaths {
				childIdx++
				isLast := childIdx == totalChildren
				var childPrefix string
				if isLast {
					childPrefix = nodeIndent + "└── "
				} else {
					childPrefix = nodeIndent + "├── "
				}
				term.Printfln("%s📍 %s", childPrefix, zonePath)
			}

			// Print components
			for _, comp := range nodeComponents {
				childIdx++
				isLast := childIdx == totalChildren
				var childPrefix string
				if isLast {
					childPrefix = nodeIndent + "└── "
				} else {
					childPrefix = nodeIndent + "├── "
				}
				term.Printfln("%s🧩 %s", childPrefix, comp)
			}
		}

		if pi < len(platforms)-1 {
			term.Println()
		}
	}

	return nil
}
