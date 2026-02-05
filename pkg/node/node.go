// Package node provides types and operations for managing platform nodes.
// Nodes are physical or virtual machines that get allocated to chassis paths.
package node

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Node represents a node configuration from inst/<platform>/nodes/<hostname>.yaml
type Node struct {
	Hostname string   `yaml:"hostname"`
	Platform string   `yaml:"-"` // Platform name (derived from directory structure)
	Chassis  []string `yaml:"chassis"` // Direct chassis allocations
	Profile  string   `yaml:"profile,omitempty"`

	Network          Network          `yaml:"network"`
	Disks            []string         `yaml:"disks,omitempty"`
	ProviderMetadata ProviderMetadata `yaml:"provider_metadata,omitempty"`

	Resources Resources         `yaml:"resources,omitempty"`
	Labels    map[string]string `yaml:"labels,omitempty"`
	K8sLabels map[string]string `yaml:"k8s_labels,omitempty"`
}

// DisplayName returns the node formatted as "hostname@platform".
func (n Node) DisplayName() string {
	return n.Hostname + "@" + n.Platform
}

// Resources defines resource specifications for a node
type Resources struct {
	CPU    int    `yaml:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty"`
	GPU    string `yaml:"gpu,omitempty"`
}

// Network defines network configuration for a node
type Network struct {
	PublicIP   string `yaml:"public_ip"`
	PrivateIP  string `yaml:"private_ip"`
	PublicMAC  string `yaml:"public_mac,omitempty"`
	PrivateMAC string `yaml:"private_mac,omitempty"`
}

// FormatDisplayName formats a node display name as "hostname@platform".
func FormatDisplayName(hostname, platform string) string {
	return hostname + "@" + platform
}

// ProviderMetadata stores provider-specific metadata
type ProviderMetadata struct {
	ServerID   string `yaml:"server_id,omitempty"`
	Datacenter string `yaml:"datacenter,omitempty"`
	Zone       string `yaml:"zone,omitempty"`
	Region     string `yaml:"region,omitempty"`
	OfferID    string `yaml:"offer_id,omitempty"`
	OfferName  string `yaml:"offer_name,omitempty"`
}

// Load loads a single node from a YAML file.
func Load(path string) (*Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var node Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}

	// Use filename as hostname if not set
	if node.Hostname == "" {
		base := filepath.Base(path)
		node.Hostname = strings.TrimSuffix(base, ".yaml")
	}

	return &node, nil
}

// LoadAll loads all nodes from inst/<platform>/nodes/ directories.
// If platform is empty, loads from all platforms.
func LoadAll(dir string, platform string) (Nodes, error) {
	var nodes Nodes

	instDir := filepath.Join(dir, "inst")
	if platform != "" {
		return loadNodesFromPlatform(instDir, platform)
	}

	// Load from all platforms
	entries, err := os.ReadDir(instDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		platformNodes, err := loadNodesFromPlatform(instDir, entry.Name())
		if err != nil {
			continue
		}
		nodes = append(nodes, platformNodes...)
	}

	return nodes, nil
}

// LoadByPlatform loads nodes grouped by platform.
func LoadByPlatform(dir string) (map[string]Nodes, error) {
	result := make(map[string]Nodes)

	instDir := filepath.Join(dir, "inst")
	entries, err := os.ReadDir(instDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		nodes, err := loadNodesFromPlatform(instDir, entry.Name())
		if err != nil {
			continue
		}
		if len(nodes) > 0 {
			result[entry.Name()] = nodes
		}
	}

	return result, nil
}

func loadNodesFromPlatform(instDir, platform string) (Nodes, error) {
	nodesDir := filepath.Join(instDir, platform, "nodes")
	entries, err := os.ReadDir(nodesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var nodes Nodes
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		node, err := Load(filepath.Join(nodesDir, entry.Name()))
		if err != nil {
			continue
		}
		node.Platform = platform
		nodes = append(nodes, *node)
	}

	// Sort by hostname for consistent ordering
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Hostname < nodes[j].Hostname
	})

	return nodes, nil
}
