package show

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-node/pkg/node"
	"gopkg.in/yaml.v3"
)

// NetworkInfo represents node network configuration
type NetworkInfo struct {
	PublicIP   string `json:"public_ip,omitempty"`
	PrivateIP  string `json:"private_ip,omitempty"`
	FailoverIP string `json:"failover_ip,omitempty"`
}

// ProviderInfo represents provider metadata
type ProviderInfo struct {
	ServerID string `json:"server_id,omitempty"`
	Zone     string `json:"zone,omitempty"`
}

// NodeInfo represents detailed node information
type NodeInfo struct {
	Node     string       `json:"node"`
	Platform string       `json:"platform"`
	Chassis  []string     `json:"chassis,omitempty"`
	Network  NetworkInfo  `json:"network"`
	Provider ProviderInfo `json:"provider,omitempty"`
}

// ShowResult is the structured output for node:show
type ShowResult struct {
	Node *NodeInfo `json:"node"`
}

// Show implements the node:show command
type Show struct {
	action.WithLogger
	action.WithTerm

	Name string

	result *ShowResult
}

// Result returns the structured result for JSON output
func (s *Show) Result() any {
	return s.result
}

// Execute runs the node:show action
func (s *Show) Execute() error {
	s.result = &ShowResult{}

	// Find node by hostname across all platforms
	instDir := "inst"
	entries, err := os.ReadDir(instDir)
	if err != nil {
		return fmt.Errorf("failed to read inst directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		platform := entry.Name()
		nodesDir := filepath.Join(instDir, platform, "nodes")

		nodes, err := s.listNodes(nodesDir)
		if err != nil {
			continue
		}

		for _, node := range nodes {
			if node.Hostname == s.Name {
				return s.showNode(platform, node)
			}
		}
	}

	s.Term().Error().Printfln("Node %q not found in any platform", s.Name)
	return nil
}

// showNode displays details for a single node
func (s *Show) showNode(platform string, node node.Node) error {
	// Build result
	s.result = &ShowResult{
		Node: &NodeInfo{
			Node:     node.Hostname,
			Platform: platform,
			Chassis:  node.Chassis,
			Network: NetworkInfo{
				PublicIP:   node.Network.PublicIP,
				PrivateIP:  node.Network.PrivateIP,
				FailoverIP: node.Network.FailoverIP,
			},
			Provider: ProviderInfo{
				ServerID: node.ProviderMetadata.ServerID,
				Zone:     node.ProviderMetadata.Zone,
			},
		},
	}

	// Print human-readable output
	n := s.result.Node
	s.Term().Printfln("node\t%s", n.Node)
	s.Term().Printfln("platform\t%s", n.Platform)

	if len(n.Chassis) > 0 {
		s.Term().Println()
		s.Term().Info().Printfln("Chassis (%d)", len(n.Chassis))
		for _, ch := range n.Chassis {
			s.Term().Printfln("%s", ch)
		}
	}

	s.Term().Println()
	s.Term().Info().Println("Network")
	if n.Network.PublicIP != "" {
		s.Term().Printfln("public_ip\t%s", n.Network.PublicIP)
	}
	if n.Network.PrivateIP != "" {
		s.Term().Printfln("private_ip\t%s", n.Network.PrivateIP)
	}
	if n.Network.FailoverIP != "" {
		s.Term().Printfln("failover_ip\t%s", n.Network.FailoverIP)
	}

	if n.Provider.ServerID != "" || n.Provider.Zone != "" {
		s.Term().Println()
		s.Term().Info().Println("Provider")
		if n.Provider.ServerID != "" {
			s.Term().Printfln("server_id\t%s", n.Provider.ServerID)
		}
		if n.Provider.Zone != "" {
			s.Term().Printfln("zone\t%s", n.Provider.Zone)
		}
	}

	return nil
}

func (s *Show) listNodes(nodesDir string) ([]node.Node, error) {
	var nodes []node.Node

	if _, err := os.Stat(nodesDir); os.IsNotExist(err) {
		return nodes, nil
	}

	entries, err := os.ReadDir(nodesDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		if entry.Name() == ".gitkeep" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(nodesDir, entry.Name()))
		if err != nil {
			continue
		}

		var node node.Node
		if err := yaml.Unmarshal(data, &node); err != nil {
			continue
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}
