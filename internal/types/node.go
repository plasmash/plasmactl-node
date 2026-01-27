// Package types defines YAML struct types for node configuration.
// Node types are owned by plasmactl-node.
// Platform types are imported from plasmactl-platform/pkg/schema.
package types

// Node represents a node configuration file in nodes/*.yaml
type Node struct {
	Hostname string   `yaml:"hostname"`
	Chassis  []string `yaml:"chassis"`
	Profile  string   `yaml:"profile,omitempty"` // Offer type that created this node

	Network          NodeNetwork          `yaml:"network"`
	Disks            []string             `yaml:"disks,omitempty"`
	ProviderMetadata NodeProviderMetadata `yaml:"provider_metadata,omitempty"`

	Resources NodeResources     `yaml:"resources,omitempty"`
	Labels    map[string]string `yaml:"labels,omitempty"`
	K8sLabels map[string]string `yaml:"k8s_labels,omitempty"`
}

// NodeResources defines resource specifications for a node
type NodeResources struct {
	CPU    int    `yaml:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty"`
	GPU    string `yaml:"gpu,omitempty"`
}

// NodeNetwork defines network configuration for a node
type NodeNetwork struct {
	PublicIP   string `yaml:"public_ip"`
	PrivateIP  string `yaml:"private_ip"`
	PublicMAC  string `yaml:"public_mac,omitempty"`
	PrivateMAC string `yaml:"private_mac,omitempty"`
}

// NodeProviderMetadata stores provider-specific metadata
type NodeProviderMetadata struct {
	ServerID   string `yaml:"server_id,omitempty"`
	Datacenter string `yaml:"datacenter,omitempty"`
	Zone       string `yaml:"zone,omitempty"`
	Region     string `yaml:"region,omitempty"`
	OfferID    string `yaml:"offer_id,omitempty"`
	OfferName  string `yaml:"offer_name,omitempty"`
}

// NewNode creates a new Node with the given configuration
func NewNode(hostname string, chassis []string, publicIP, privateIP string) *Node {
	return &Node{
		Hostname: hostname,
		Chassis:  chassis,
		Network: NodeNetwork{
			PublicIP:  publicIP,
			PrivateIP: privateIP,
		},
		Labels:    make(map[string]string),
		K8sLabels: make(map[string]string),
	}
}

// AddChassisLabels generates Kubernetes labels from chassis assignments
func (n *Node) AddChassisLabels() {
	if n.K8sLabels == nil {
		n.K8sLabels = make(map[string]string)
	}
	for _, c := range n.Chassis {
		n.K8sLabels[c] = "true"
	}
}

// ChassisSpec represents a parsed chassis specification for provisioning.
// Used to parse CLI input like "foundation.cluster.control:Start-1-S:3"
type ChassisSpec struct {
	Chassis   string
	OfferType string
	Count     int
}
