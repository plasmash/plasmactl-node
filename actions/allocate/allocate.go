package allocate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/pkg/node"
	"github.com/plasmash/plasmactl-platform/pkg/graph"
	"gopkg.in/yaml.v3"
)

// AllocateResult is the structured output for node:allocate.
type AllocateResult struct {
	Hostname string   `json:"hostname"`
	Zones    []string `json:"zones"`
	Added    []string `json:"added,omitempty"`
	Modified bool     `json:"modified"`
}

// Allocate implements the node:allocate command.
type Allocate struct {
	action.WithLogger
	action.WithTerm

	Hostname string
	Zones    []string
	Platform string

	result *AllocateResult
}

// Result returns the structured result for JSON output.
func (a *Allocate) Result() any {
	return a.result
}

// Execute runs the node:allocate action.
func (a *Allocate) Execute() error {
	nodeFile, err := FindNodeFile(a.Platform, a.Hostname)
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

	// Load graph (optional — only used for zone validation warnings).
	var g *graph.PlatformGraph
	if pg, err := graph.Load(); err == nil {
		g = pg
	}

	var added []string
	existing := make(map[string]bool, len(n.Zones))
	for _, z := range n.Zones {
		existing[z] = true
	}

	for _, z := range a.Zones {
		if g != nil {
			if gNode := g.Node(z); gNode == nil {
				a.Term().Warning().Printfln("Warning: %q not found in platform graph", z)
			} else if gNode.Kind != "zone" {
				a.Term().Warning().Printfln("Warning: %q is a %s, not a zone", z, gNode.Kind)
			}
		}
		if existing[z] {
			a.Term().Warning().Printfln("Already allocated: %s", z)
			continue
		}
		n.Zones = append(n.Zones, z)
		existing[z] = true
		added = append(added, z)
		a.Term().Success().Printfln("Allocated: %s", z)
	}

	modified := len(added) > 0
	if !modified {
		a.result = &AllocateResult{Hostname: n.Hostname, Zones: n.Zones, Modified: false}
		return nil
	}

	sort.Strings(n.Zones)
	n.AddZoneLabels()

	out, err := yaml.Marshal(&n)
	if err != nil {
		return fmt.Errorf("failed to marshal node: %w", err)
	}
	if err := os.WriteFile(nodeFile, out, 0644); err != nil {
		return fmt.Errorf("failed to write node file: %w", err)
	}

	a.result = &AllocateResult{
		Hostname: n.Hostname,
		Zones:    n.Zones,
		Added:    added,
		Modified: true,
	}
	return nil
}

// FindNodeFile locates a node YAML either in a specific platform (if supplied)
// or across all platforms under platforms/. Exported so `deallocate` can reuse it.
func FindNodeFile(platform, hostname string) (string, error) {
	if platform != "" {
		nodeFile := filepath.Join("platforms", platform, "nodes", hostname+".yaml")
		if _, err := os.Stat(nodeFile); err == nil {
			return nodeFile, nil
		}
		return "", fmt.Errorf("node %s not found in platform %s", hostname, platform)
	}

	platformsDir := "platforms"
	entries, err := os.ReadDir(platformsDir)
	if err != nil {
		return "", fmt.Errorf("failed to read platforms directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		nodeFile := filepath.Join(platformsDir, entry.Name(), "nodes", hostname+".yaml")
		if _, err := os.Stat(nodeFile); err == nil {
			return nodeFile, nil
		}
	}
	return "", fmt.Errorf("node %s not found in any platform", hostname)
}
