package list

import (
	"fmt"
	"sort"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-chassis/pkg/chassis"
	"github.com/plasmash/plasmactl-component/pkg/component"
	"github.com/plasmash/plasmactl-node/pkg/node"
)

// NodeListItem represents a node in the list output
type NodeListItem struct {
	Node       string   `json:"node"`
	Platform   string   `json:"platform"`
	Chassis    []string `json:"chassis,omitempty"`
	Components []string `json:"components,omitempty"`
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

	// Load chassis and components once (only needed for tree mode)
	var c *chassis.Chassis
	chassisToComponents := make(map[string][]string)
	if l.Tree {
		c, err = chassis.Load(".")
		if err != nil {
			l.Log().Debug("Failed to load chassis", "error", err)
		}
		components, _ := component.LoadFromPlaybooks(".")
		for _, comp := range components {
			chassisToComponents[comp.Chassis] = append(chassisToComponents[comp.Chassis], comp.Name)
		}
	}

	for _, platform := range platforms {
		nodes := nodesByPlatform[platform]
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].Hostname < nodes[j].Hostname
		})

		var allocations map[string][]string
		if l.Tree && c != nil {
			allocations = nodes.Allocations(c)
		}

		for _, n := range nodes {
			item := NodeListItem{
				Node:     n.Hostname,
				Platform: n.Platform,
			}

			if l.Tree {
				chassisPaths := allocations[n.Hostname]
				sort.Strings(chassisPaths)
				if len(chassisPaths) > 0 {
					item.Chassis = chassisPaths
				}

				// Collect components across all chassis paths for this node
				var nodeComps []string
				seen := make(map[string]bool)
				for _, p := range chassisPaths {
					for _, comp := range chassisToComponents[p] {
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
		l.Term().Printfln("%s", item.Node)
	}

	return nil
}

// printTree prints nodes as a tree with chassis paths (📍) and components (🧩)
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

			chassisPaths := item.Chassis
			nodeComponents := item.Components
			totalChildren := len(chassisPaths) + len(nodeComponents)
			childIdx := 0

			// Print chassis paths
			for _, chassisPath := range chassisPaths {
				childIdx++
				isLast := childIdx == totalChildren
				var childPrefix string
				if isLast {
					childPrefix = nodeIndent + "└── "
				} else {
					childPrefix = nodeIndent + "├── "
				}
				term.Printfln("%s📍 %s", childPrefix, chassisPath)
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
