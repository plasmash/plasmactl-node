package deallocate

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/actions/allocate"
	"github.com/plasmash/plasmactl-node/pkg/node"
	"github.com/plasmash/plasmactl-platform/pkg/graph"
	"gopkg.in/yaml.v3"
)

// DeallocateResult is the structured output for node:deallocate.
type DeallocateResult struct {
	Hostname string   `json:"hostname"`
	Zones    []string `json:"zones"`
	Removed  []string `json:"removed,omitempty"`
	Modified bool     `json:"modified"`
}

// Deallocate implements the node:deallocate command.
type Deallocate struct {
	action.WithLogger
	action.WithTerm

	Hostname  string
	Zones     []string
	Platform  string
	Recursive bool

	result *DeallocateResult
}

// Result returns the structured result for JSON output.
func (d *Deallocate) Result() any {
	return d.result
}

// Execute runs the node:deallocate action. Matching is exact by default:
// - declared zone matches: remove it
// - ancestor declared, descendant requested: error (covered implicitly)
// - descendants declared, ancestor requested: error unless --recursive
func (d *Deallocate) Execute() error {
	nodeFile, err := allocate.FindNodeFile(d.Platform, d.Hostname)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(nodeFile)
	if err != nil {
		return fmt.Errorf("failed to read node file: %w", err)
	}

	var n node.Node
	if err := yaml.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("failed to parse node file: %w", err)
	}

	declared := make(map[string]bool, len(n.Zones))
	for _, z := range n.Zones {
		declared[z] = true
	}

	// Load graph for inheritance checks (optional — missing graph falls back
	// to pure prefix comparison on zone paths).
	var g *graph.PlatformGraph
	if pg, err := graph.Load(); err == nil {
		g = pg
	}

	var removeSet = make(map[string]bool)
	for _, target := range d.Zones {
		if declared[target] {
			removeSet[target] = true
			continue
		}
		// Not directly declared — check inheritance cases.
		ancestors := declaredAncestors(declared, target, g)
		descendants := declaredDescendants(declared, target, g)

		switch {
		case len(ancestors) > 0:
			return fmt.Errorf("zone %q is not directly allocated to %s; coverage is inherited from parent(s) %s — remove the parent(s) instead, or use a narrower allocation",
				target, n.Hostname, strings.Join(ancestors, ", "))
		case len(descendants) > 0 && !d.Recursive:
			return fmt.Errorf("zone %q is not directly allocated to %s; descendant(s) %s are directly allocated — pass --recursive to cascade-remove them",
				target, n.Hostname, strings.Join(descendants, ", "))
		case len(descendants) > 0 && d.Recursive:
			for _, desc := range descendants {
				removeSet[desc] = true
			}
		default:
			return fmt.Errorf("zone %q is not allocated to %s", target, n.Hostname)
		}
	}

	var kept []string
	var removed []string
	for _, z := range n.Zones {
		if removeSet[z] {
			removed = append(removed, z)
			continue
		}
		kept = append(kept, z)
	}

	for _, r := range removed {
		d.Term().Success().Printfln("Deallocated: %s", r)
	}

	if len(removed) == 0 {
		d.result = &DeallocateResult{Hostname: n.Hostname, Zones: n.Zones, Modified: false}
		return nil
	}

	sort.Strings(kept)
	n.Zones = kept
	n.AddZoneLabels()

	out, err := yaml.Marshal(&n)
	if err != nil {
		return fmt.Errorf("failed to marshal node: %w", err)
	}
	if err := os.WriteFile(nodeFile, out, 0644); err != nil {
		return fmt.Errorf("failed to write node file: %w", err)
	}

	sort.Strings(removed)
	d.result = &DeallocateResult{
		Hostname: n.Hostname,
		Zones:    kept,
		Removed:  removed,
		Modified: true,
	}
	return nil
}

// declaredAncestors returns every declared zone that is a strict ancestor of
// target. Falls back to dotted-path prefix comparison if the graph is missing.
func declaredAncestors(declared map[string]bool, target string, g *graph.PlatformGraph) []string {
	var out []string
	if g != nil {
		for _, a := range g.Ancestors(target, 0, "contains") {
			if a.Kind == "zone" && a.Name != target && declared[a.Name] {
				out = append(out, a.Name)
			}
		}
		if len(out) > 0 {
			sort.Strings(out)
			return out
		}
	}
	// Fallback: declared zone that is a prefix of target (dotted-path).
	for z := range declared {
		if z != target && strings.HasPrefix(target, z+".") {
			out = append(out, z)
		}
	}
	sort.Strings(out)
	return out
}

// declaredDescendants returns every declared zone that is a strict descendant
// of target. Falls back to dotted-path suffix comparison if no graph.
func declaredDescendants(declared map[string]bool, target string, g *graph.PlatformGraph) []string {
	var out []string
	if g != nil {
		for _, d := range g.Descendants(target, -1, "contains") {
			if d.Kind == "zone" && d.Name != target && declared[d.Name] {
				out = append(out, d.Name)
			}
		}
		if len(out) > 0 {
			sort.Strings(out)
			return out
		}
	}
	for z := range declared {
		if z != target && strings.HasPrefix(z, target+".") {
			out = append(out, z)
		}
	}
	sort.Strings(out)
	return out
}
