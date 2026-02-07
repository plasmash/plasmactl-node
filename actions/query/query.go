package query

import (
	"fmt"
	"sort"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/pkg/node"
	"github.com/plasmash/plasmactl-platform/pkg/graph"
)

// NodeMatch represents a node found by query
type NodeMatch struct {
	Node     string `json:"node"`
	Platform string `json:"platform"`
}

// QueryResult is the structured output for node:query
type QueryResult struct {
	Nodes []NodeMatch `json:"nodes"`
}

// Query implements the node:query command
type Query struct {
	action.WithLogger
	action.WithTerm

	Identifier string
	Env        string
	Kind       string // "chassis" or "component" to skip auto-detection

	result QueryResult
}

// Execute runs the query action
func (q *Query) Execute() error {
	// Load platform graph
	g, err := graph.Load()
	if err != nil {
		return fmt.Errorf("failed to load platform graph: %w", err)
	}

	// Load nodes by platform
	nodesByPlatform, err := node.LoadByPlatform(".")
	if err != nil {
		q.Log().Debug("Failed to load nodes", "error", err)
	}

	// Filter by environment if specified
	if q.Env != "" {
		filtered := make(map[string]node.Nodes)
		if nodes, ok := nodesByPlatform[q.Env]; ok {
			filtered[q.Env] = nodes
		}
		nodesByPlatform = filtered
	}

	var matchingNodes []nodeMatchEntry

	searchChassis := q.Kind == "" || q.Kind == "chassis"
	searchComponent := q.Kind == "" || q.Kind == "component"

	// Try 1: Query by chassis path using graph
	if searchChassis {
		gNode := g.Node(q.Identifier)
		if gNode != nil && gNode.Kind == "chassis" {
			// Find all chassis paths that are this chassis or its descendants
			chassisPaths := map[string]bool{q.Identifier: true}
			descendants := g.Descendants(q.Identifier, -1, "contains")
			for _, d := range descendants {
				if d.Kind == "chassis" {
					chassisPaths[d.Name] = true
				}
			}

			for platform, nodes := range nodesByPlatform {
				for _, n := range nodes {
					for _, c := range n.Chassis {
						if chassisPaths[c] {
							matchingNodes = append(matchingNodes, nodeMatchEntry{
								n:        n,
								platform: platform,
							})
							break
						}
					}
				}
			}
		}
	}

	// Try 2: Query by component name - find its chassis attachment via graph
	if searchComponent && len(matchingNodes) == 0 {
		gNode := g.Node(q.Identifier)
		if gNode != nil && gNode.Kind != "chassis" && gNode.Kind != "node" {
			// Find chassis that attaches this component (reverse: chassis --attaches--> component)
			ancestors := g.Ancestors(q.Identifier, 1, "attaches")
			chassisPaths := make(map[string]bool)
			for _, a := range ancestors {
				if a.Kind == "chassis" {
					chassisPaths[a.Name] = true
					// Also include descendant chassis
					descendants := g.Descendants(a.Name, -1, "contains")
					for _, d := range descendants {
						if d.Kind == "chassis" {
							chassisPaths[d.Name] = true
						}
					}
				}
			}

			if len(chassisPaths) > 0 {
				for platform, nodes := range nodesByPlatform {
					for _, n := range nodes {
						for _, c := range n.Chassis {
							if chassisPaths[c] {
								matchingNodes = append(matchingNodes, nodeMatchEntry{
									n:        n,
									platform: platform,
								})
								break
							}
						}
					}
				}
			}
		}
	}

	if len(matchingNodes) == 0 {
		q.Term().Warning().Printfln("No nodes found for %q", q.Identifier)
		return nil
	}

	// Sort by platform, then hostname
	sort.Slice(matchingNodes, func(i, j int) bool {
		if matchingNodes[i].platform != matchingNodes[j].platform {
			return matchingNodes[i].platform < matchingNodes[j].platform
		}
		return matchingNodes[i].n.Hostname < matchingNodes[j].n.Hostname
	})

	// Build result
	for _, m := range matchingNodes {
		q.result.Nodes = append(q.result.Nodes, NodeMatch{
			Node:     m.n.Hostname,
			Platform: m.platform,
		})
	}

	return nil
}

// Result returns the structured result for JSON output
func (q *Query) Result() any {
	return q.result
}

type nodeMatchEntry struct {
	n        node.Node
	platform string
}
