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
	Node     string `json:"node"`
	Platform string `json:"platform"`
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
	// Load nodes by platform
	nodesByPlatform, err := node.LoadByPlatform(".")
	if err != nil {
		return fmt.Errorf("failed to load nodes: %w", err)
	}

	if len(nodesByPlatform) == 0 {
		l.Term().Warning().Println("No nodes found")
		return nil
	}

	// Build result - collect all nodes across platforms
	l.result = &ListResult{}
	var allNodes []node.Node

	// Sort platforms for consistent ordering
	var platforms []string
	for platform := range nodesByPlatform {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)

	for _, platform := range platforms {
		for _, n := range nodesByPlatform[platform] {
			l.result.Nodes = append(l.result.Nodes, NodeListItem{
				Node:     n.Hostname,
				Platform: n.Platform,
			})
			allNodes = append(allNodes, n)
		}
	}

	if l.Tree {
		return l.printTreeWithRelations()
	}

	// Flat output - one per line, scriptable
	for _, n := range allNodes {
		fmt.Println(n.DisplayName())
	}

	return nil
}

// printTreeWithRelations prints nodes as a tree with chassis paths (📍) and components (🧩)
func (l *List) printTreeWithRelations() error {
	// Load chassis for distribution
	c, err := chassis.Load(".")
	if err != nil {
		l.Log().Debug("Failed to load chassis", "error", err)
	}

	// Load nodes by platform
	nodesByPlatform, err := node.LoadByPlatform(".")
	if err != nil {
		return fmt.Errorf("failed to load nodes: %w", err)
	}

	// Load components
	components, _ := component.LoadFromPlaybooks(".")
	chassisToComponents := make(map[string][]string)
	for _, comp := range components {
		chassisToComponents[comp.Chassis] = append(chassisToComponents[comp.Chassis], comp.Name)
	}

	// Sort platforms
	var platforms []string
	for platform := range nodesByPlatform {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)

	for pi, platform := range platforms {
		nodes := nodesByPlatform[platform]
		allocations := nodes.Allocations(c)

		// Print platform header
		fmt.Println(platform)

		// Sort nodes by hostname
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].Hostname < nodes[j].Hostname
		})

		for ni, n := range nodes {
			isLastNode := ni == len(nodes)-1 && pi == len(platforms)-1

			var nodePrefix, nodeIndent string
			if isLastNode {
				nodePrefix = "└── "
				nodeIndent = "    "
			} else if ni == len(nodes)-1 {
				nodePrefix = "└── "
				nodeIndent = "    "
			} else {
				nodePrefix = "├── "
				nodeIndent = "│   "
			}

			fmt.Printf("%s🖥  %s\n", nodePrefix, n.DisplayName())

			// Get chassis paths for this node
			chassisPaths := allocations[n.Hostname]
			sort.Strings(chassisPaths)

			// Get components that run on this node's chassis paths
			var nodeComponents []string
			chassisSet := make(map[string]bool)
			for _, p := range chassisPaths {
				chassisSet[p] = true
			}
			for chassisPath, comps := range chassisToComponents {
				if chassisSet[chassisPath] {
					for _, c := range comps {
						nodeComponents = append(nodeComponents, c)
					}
				}
			}
			sort.Strings(nodeComponents)

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
				fmt.Printf("%s📍 %s\n", childPrefix, chassisPath)
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
				fmt.Printf("%s🧩 %s\n", childPrefix, comp)
			}
		}

		if pi < len(platforms)-1 {
			fmt.Println()
		}
	}

	return nil
}
