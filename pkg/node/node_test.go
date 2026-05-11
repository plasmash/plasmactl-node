package node

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNodeYAML_ParsesProviderMetadataProvider(t *testing.T) {
	yamlInput := []byte(`hostname: ns1.example.net
zones: [platform.foundation.cluster.control]
network:
  public_ip: 1.2.3.4
  private_ip: 192.168.0.1
provider_metadata:
  provider: ovh
  server_id: ns1.example.net
  zone: bhs
`)
	var n Node
	if err := yaml.Unmarshal(yamlInput, &n); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if n.ProviderMetadata.Provider != "ovh" {
		t.Fatalf("Provider = %q, want %q", n.ProviderMetadata.Provider, "ovh")
	}
}

func TestNodeYAML_LegacyYAMLWithoutProvider_ParsesCleanly(t *testing.T) {
	// Backward-compat: existing node yaml files predating the Provider
	// field must continue to parse with Provider == "" (omitempty zero value).
	yamlInput := []byte(`hostname: ns1.example.net
zones: [platform.foundation.cluster.control]
network:
  public_ip: 1.2.3.4
  private_ip: 192.168.0.1
provider_metadata:
  server_id: ns1.example.net
  zone: bhs
`)
	var n Node
	if err := yaml.Unmarshal(yamlInput, &n); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if n.ProviderMetadata.Provider != "" {
		t.Fatalf("Provider = %q, want empty string for legacy yaml", n.ProviderMetadata.Provider)
	}
	if n.ProviderMetadata.ServerID != "ns1.example.net" {
		t.Fatalf("ServerID = %q, want ns1.example.net", n.ProviderMetadata.ServerID)
	}
}
