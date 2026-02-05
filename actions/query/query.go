package query

import (
	"fmt"
	"sort"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-chassis/pkg/chassis"
	"github.com/plasmash/plasmactl-component/pkg/component"
	"github.com/plasmash/plasmactl-node/pkg/node"
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
	// Load chassis for distribution computation
	c, err := chassis.Load(".")
	if err != nil {
		return err
	}

	var matchingNodes []nodeMatch

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

	// Search based on kind or auto-detect
	searchChassis := q.Kind == "" || q.Kind == "chassis"
	searchComponent := q.Kind == "" || q.Kind == "component"

	// Try 1: Query by chassis path
	if searchChassis && c.Exists(q.Identifier) {
		for _, nodes := range nodesByPlatform {
			allocations := nodes.Allocations(c)
			for _, n := range nodes {
				effectiveChassis := allocations[n.Hostname]
				for _, chassisPath := range effectiveChassis {
					if chassisPath == q.Identifier || chassis.IsDescendantOf(chassisPath, q.Identifier) {
						matchingNodes = append(matchingNodes, nodeMatch{
							n:      n,
							reason: "chassis",
						})
						break
					}
				}
			}
		}
	}

	// Try 2: Query by component name (find nodes that run the component's chassis path)
	if searchComponent && len(matchingNodes) == 0 {
		components, err := component.LoadFromPlaybooks(".")
		if err == nil {
			comp := components.Find(q.Identifier)
			if comp != nil && comp.Chassis != "" {
				for _, nodes := range nodesByPlatform {
					allocations := nodes.Allocations(c)
					for _, n := range nodes {
						effectiveChassis := allocations[n.Hostname]
						for _, chassisPath := range effectiveChassis {
							if chassisPath == comp.Chassis || chassis.IsDescendantOf(chassisPath, comp.Chassis) {
								matchingNodes = append(matchingNodes, nodeMatch{
									n:      n,
									reason: fmt.Sprintf("component:%s", comp.Chassis),
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
		if matchingNodes[i].n.Platform != matchingNodes[j].n.Platform {
			return matchingNodes[i].n.Platform < matchingNodes[j].n.Platform
		}
		return matchingNodes[i].n.Hostname < matchingNodes[j].n.Hostname
	})

	// Build result - output is handled by launchr
	for _, m := range matchingNodes {
		q.result.Nodes = append(q.result.Nodes, NodeMatch{
			Node:     m.n.Hostname,
			Platform: m.n.Platform,
		})
	}

	return nil
}

// Result returns the structured result for JSON output
func (q *Query) Result() any {
	return q.result
}

type nodeMatch struct {
	n      node.Node
	reason string
}
