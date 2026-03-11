package validate

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/pkg/node"
	"github.com/plasmash/plasmactl-platform/pkg/graph"
)

// ValidationCheck represents a single validation check result
type ValidationCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "pass", "fail", "warning"
	Value  string `json:"value,omitempty"`
}

// NodeValidation represents validation results for a single node
type NodeValidation struct {
	Hostname  string            `json:"hostname"`
	Platform  string            `json:"platform"`
	Valid     bool              `json:"valid"`
	Checks    []ValidationCheck `json:"checks"`
	HasErrors bool              `json:"has_errors"`
}

// ValidateResult is the structured output for node:validate
type ValidateResult struct {
	Nodes       []NodeValidation `json:"nodes"`
	TotalNodes  int              `json:"total_nodes"`
	ValidNodes  int              `json:"valid_nodes"`
	HasErrors   bool             `json:"has_errors"`
	HasWarnings bool             `json:"has_warnings"`
}

// Validate implements the node:validate command
type Validate struct {
	action.WithLogger
	action.WithTerm

	Identifier string
	Env        string

	graph  *graph.PlatformGraph
	result *ValidateResult
}

// Result returns the structured result for JSON output
func (v *Validate) Result() any {
	return v.result
}

// Execute runs the node:validate action
func (v *Validate) Execute() error {
	v.result = &ValidateResult{}

	// Load platform graph for zone validation
	if g, err := graph.Load(); err == nil {
		v.graph = g
		v.Log().Debug("Platform graph loaded", "nodes", g.NodeCount(), "edges", g.EdgeCount())
	} else {
		v.Log().Debug("Platform graph not available, zone validation will be limited", "error", err)
	}

	// Determine what to validate
	if v.Identifier == "" {
		// Validate all nodes in all platforms (or specific env)
		return v.validateAllNodes()
	}

	// Check if identifier is a platform name or node hostname
	if v.isPlatform(v.Identifier) {
		return v.validatePlatformNodes(v.Identifier)
	}

	// Single node validation
	return v.validateSingleNode(v.Identifier)
}

// isPlatform checks if the identifier is a platform directory
func (v *Validate) isPlatform(name string) bool {
	platformDir := filepath.Join("inst", name)
	platformFile := filepath.Join(platformDir, "platform.yaml")
	_, err := os.Stat(platformFile)
	return err == nil
}

// validateAllNodes validates all nodes across all platforms
func (v *Validate) validateAllNodes() error {
	instDir := "inst"

	if v.Env != "" {
		return v.validatePlatformNodes(v.Env)
	}

	entries, err := os.ReadDir(instDir)
	if err != nil {
		if os.IsNotExist(err) {
			v.Term().Warning().Println("No platforms found (inst/ directory does not exist)")
			return nil
		}
		return fmt.Errorf("failed to read inst directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		platformFile := filepath.Join(instDir, entry.Name(), "platform.yaml")
		if _, err := os.Stat(platformFile); os.IsNotExist(err) {
			continue
		}
		if err := v.validatePlatformNodes(entry.Name()); err != nil {
			v.Log().Warn("Failed to validate platform", "platform", entry.Name(), "error", err)
		}
	}

	return v.printSummary()
}

// validatePlatformNodes validates all nodes in a specific platform
func (v *Validate) validatePlatformNodes(platform string) error {
	nodesDir := filepath.Join("inst", platform, "nodes")
	nodes, err := node.LoadAll(".", platform)
	if err != nil {
		return fmt.Errorf("failed to load nodes: %w", err)
	}

	if len(nodes) == 0 {
		v.Term().Warning().Printfln("No nodes found in platform %s", platform)
		return nil
	}

	v.Term().Info().Printfln("Validating %d nodes in platform %s...", len(nodes), platform)

	// Track IPs for uniqueness check
	publicIPs := make(map[string]string)  // IP -> hostname
	privateIPs := make(map[string]string) // IP -> hostname

	for _, n := range nodes {
		validation := v.validateNode(n, nodesDir, publicIPs, privateIPs)
		v.result.Nodes = append(v.result.Nodes, validation)
		v.result.TotalNodes++
		if validation.Valid {
			v.result.ValidNodes++
		}
		if validation.HasErrors {
			v.result.HasErrors = true
		}
		for _, check := range validation.Checks {
			if check.Status == "warning" {
				v.result.HasWarnings = true
				break
			}
		}
	}

	return nil
}

// validateSingleNode validates a single node by hostname
func (v *Validate) validateSingleNode(hostname string) error {
	// Find the node
	nodes, err := node.LoadAll(".", v.Env)
	if err != nil {
		return fmt.Errorf("failed to load nodes: %w", err)
	}

	found := nodes.Find(hostname)
	if found == nil {
		return fmt.Errorf("node %q not found", hostname)
	}

	nodesDir := filepath.Join("inst", found.Platform, "nodes")
	publicIPs := make(map[string]string)
	privateIPs := make(map[string]string)

	validation := v.validateNode(*found, nodesDir, publicIPs, privateIPs)
	v.result.Nodes = append(v.result.Nodes, validation)
	v.result.TotalNodes = 1
	if validation.Valid {
		v.result.ValidNodes = 1
	}
	v.result.HasErrors = validation.HasErrors
	for _, check := range validation.Checks {
		if check.Status == "warning" {
			v.result.HasWarnings = true
			break
		}
	}

	return v.printSummary()
}

// validateNode performs all validation checks on a single node
func (v *Validate) validateNode(n node.Node, nodesDir string, publicIPs, privateIPs map[string]string) NodeValidation {
	validation := NodeValidation{
		Hostname: n.Hostname,
		Platform: n.Platform,
		Checks:   []ValidationCheck{},
	}

	hasErrors := false

	// Check hostname
	if n.Hostname == "" {
		validation.Checks = append(validation.Checks, ValidationCheck{
			Name:   "hostname",
			Status: "fail",
		})
		v.Term().Error().Printfln("  ✗ %s: hostname is empty", n.DisplayName())
		hasErrors = true
	} else {
		validation.Checks = append(validation.Checks, ValidationCheck{
			Name:   "hostname",
			Status: "pass",
			Value:  n.Hostname,
		})
	}

	// Check public IP
	if n.Network.PublicIP == "" {
		validation.Checks = append(validation.Checks, ValidationCheck{
			Name:   "public_ip",
			Status: "fail",
		})
		v.Term().Error().Printfln("  ✗ %s: public IP is missing", n.DisplayName())
		hasErrors = true
	} else if !isValidIP(n.Network.PublicIP) {
		validation.Checks = append(validation.Checks, ValidationCheck{
			Name:   "public_ip",
			Status: "fail",
			Value:  n.Network.PublicIP,
		})
		v.Term().Error().Printfln("  ✗ %s: invalid public IP %q", n.DisplayName(), n.Network.PublicIP)
		hasErrors = true
	} else if existing, ok := publicIPs[n.Network.PublicIP]; ok {
		validation.Checks = append(validation.Checks, ValidationCheck{
			Name:   "public_ip_unique",
			Status: "fail",
			Value:  fmt.Sprintf("duplicate with %s", existing),
		})
		v.Term().Error().Printfln("  ✗ %s: public IP %s already used by %s", n.DisplayName(), n.Network.PublicIP, existing)
		hasErrors = true
	} else {
		validation.Checks = append(validation.Checks, ValidationCheck{
			Name:   "public_ip",
			Status: "pass",
			Value:  n.Network.PublicIP,
		})
		publicIPs[n.Network.PublicIP] = n.Hostname
	}

	// Check private IP
	if n.Network.PrivateIP == "" {
		validation.Checks = append(validation.Checks, ValidationCheck{
			Name:   "private_ip",
			Status: "warning",
		})
		v.Term().Warning().Printfln("  ! %s: private IP is missing", n.DisplayName())
	} else if !isValidIP(n.Network.PrivateIP) {
		validation.Checks = append(validation.Checks, ValidationCheck{
			Name:   "private_ip",
			Status: "fail",
			Value:  n.Network.PrivateIP,
		})
		v.Term().Error().Printfln("  ✗ %s: invalid private IP %q", n.DisplayName(), n.Network.PrivateIP)
		hasErrors = true
	} else if existing, ok := privateIPs[n.Network.PrivateIP]; ok {
		validation.Checks = append(validation.Checks, ValidationCheck{
			Name:   "private_ip_unique",
			Status: "fail",
			Value:  fmt.Sprintf("duplicate with %s", existing),
		})
		v.Term().Error().Printfln("  ✗ %s: private IP %s already used by %s", n.DisplayName(), n.Network.PrivateIP, existing)
		hasErrors = true
	} else {
		validation.Checks = append(validation.Checks, ValidationCheck{
			Name:   "private_ip",
			Status: "pass",
			Value:  n.Network.PrivateIP,
		})
		privateIPs[n.Network.PrivateIP] = n.Hostname
	}

	// Check zone allocations
	if len(n.Zones) == 0 {
		validation.Checks = append(validation.Checks, ValidationCheck{
			Name:   "zones",
			Status: "warning",
		})
		v.Term().Warning().Printfln("  ! %s: no zone allocations", n.DisplayName())
	} else {
		validation.Checks = append(validation.Checks, ValidationCheck{
			Name:   "zones",
			Status: "pass",
			Value:  fmt.Sprintf("%d allocations", len(n.Zones)),
		})

		// Validate zones against the platform graph
		if v.graph != nil {
			for _, zonePath := range n.Zones {
				gNode := v.graph.Node(zonePath)
				if gNode == nil {
					validation.Checks = append(validation.Checks, ValidationCheck{
						Name:   "zone_exists",
						Status: "fail",
						Value:  zonePath,
					})
					v.Term().Error().Printfln("  ✗ %s: zone %q not found in platform graph", n.DisplayName(), zonePath)
					hasErrors = true
				} else if gNode.Kind != "zone" {
					validation.Checks = append(validation.Checks, ValidationCheck{
						Name:   "zone_type",
						Status: "fail",
						Value:  fmt.Sprintf("%s is %s, not zone", zonePath, gNode.Kind),
					})
					v.Term().Error().Printfln("  ✗ %s: %q is a %s, not a zone", n.DisplayName(), zonePath, gNode.Kind)
					hasErrors = true
				} else {
					// Show what's attached to this zone
					attached := v.graph.EdgesFrom(zonePath, "distributes")
					if len(attached) > 0 {
						names := make([]string, len(attached))
						for i, e := range attached {
							names[i] = e.To().Name
						}
						validation.Checks = append(validation.Checks, ValidationCheck{
							Name:   "zone_components",
							Status: "pass",
							Value:  fmt.Sprintf("%s: %d components attached", zonePath, len(attached)),
						})
					}
				}
			}
		}
	}

	validation.HasErrors = hasErrors
	validation.Valid = !hasErrors

	if !hasErrors {
		v.Term().Success().Printfln("  ✓ %s", n.DisplayName())
	}

	return validation
}

// printSummary prints the validation summary
func (v *Validate) printSummary() error {
	v.Term().Println()

	if v.result.TotalNodes == 0 {
		v.Term().Warning().Println("No nodes to validate")
		return nil
	}

	if v.result.HasErrors {
		v.Term().Error().Printfln("Validation failed: %d/%d nodes valid", v.result.ValidNodes, v.result.TotalNodes)
		return fmt.Errorf("validation failed")
	}

	v.Term().Success().Printfln("Validation passed: %d/%d nodes valid", v.result.ValidNodes, v.result.TotalNodes)
	return nil
}

// isValidIP checks if a string is a valid IP address
func isValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}
