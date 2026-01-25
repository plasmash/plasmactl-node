// Package types defines YAML struct types for environment configuration
package types

// Platform represents the platform.yaml configuration for an environment
type Platform struct {
	Name        string                `yaml:"name"`
	Cluster     string                `yaml:"cluster,omitempty"`
	Description string                `yaml:"description,omitempty"`

	Infrastructure Infrastructure      `yaml:"infrastructure"`
	Networking     Networking          `yaml:"networking,omitempty"`
	Chassis        map[string][]ChassisProfile `yaml:"chassis,omitempty"`

	Defaults   PlatformDefaults `yaml:"defaults,omitempty"`
	Features   PlatformFeatures `yaml:"features,omitempty"`
	Environment EnvironmentConfig `yaml:"environment,omitempty"`
}

// Infrastructure defines the infrastructure provider configuration
type Infrastructure struct {
	Provider string `yaml:"provider"` // scaleway, hetzner, aws, ovh, manual
	API      APIConfig `yaml:"api,omitempty"`
}

// APIConfig defines API connection settings
type APIConfig struct {
	URI   string `yaml:"uri,omitempty"`
	Token string `yaml:"token,omitempty"`
}

// Networking defines network configuration
type Networking struct {
	Domain            string    `yaml:"domain,omitempty"`
	PrivateNetwork    string    `yaml:"private_network,omitempty"`
	PrivateVIPNetwork string    `yaml:"private_vip_network,omitempty"`
	Bus               BusConfig `yaml:"bus,omitempty"`
}

// BusConfig defines message bus configuration
type BusConfig struct {
	IP    string         `yaml:"ip,omitempty"`
	Event EventBusConfig `yaml:"event,omitempty"`
	Data  DataBusConfig  `yaml:"data,omitempty"`
}

// EventBusConfig defines event bus (NATS) configuration
type EventBusConfig struct {
	Application string `yaml:"application,omitempty"`
	Port        int    `yaml:"port,omitempty"`
}

// DataBusConfig defines data bus (Kafka) configuration
type DataBusConfig struct {
	Application string `yaml:"application,omitempty"`
	Port        int    `yaml:"port,omitempty"`
	Service     string `yaml:"service,omitempty"`
	BrokerCount int    `yaml:"broker_count,omitempty"`
}

// ChassisProfile defines a hardware profile for a chassis attachment
type ChassisProfile struct {
	Type  string `yaml:"type"`  // Offer type (e.g., GP1-L, GPU-3090)
	Count int    `yaml:"count"` // Number of nodes
}

// PlatformDefaults defines default values for nodes
type PlatformDefaults struct {
	Chassis      string   `yaml:"chassis,omitempty"`
	Capabilities []string `yaml:"capabilities,omitempty"`
	Resources    Resources `yaml:"resources,omitempty"`
}

// Resources defines resource specifications
type Resources struct {
	CPU    int    `yaml:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty"`
	GPU    string `yaml:"gpu,omitempty"`
}

// PlatformFeatures defines feature flags
type PlatformFeatures struct {
	DisplayOSRebuildConfirmation   bool `yaml:"display_os_rebuild_confirmation,omitempty"`
	DisplayDataWipeConfirmation    bool `yaml:"display_data_wipe_confirmation,omitempty"`
	OSWipeData                     bool `yaml:"os_wipe_data,omitempty"`
}

// EnvironmentConfig defines environment-level settings
type EnvironmentConfig struct {
	Type            string `yaml:"type,omitempty"` // development, staging, production
	AutoDeploy      bool   `yaml:"auto_deploy,omitempty"`
	MonitoringLevel string `yaml:"monitoring_level,omitempty"`
}

// NewPlatform creates a new Platform with default values
func NewPlatform(name, provider, domain string) *Platform {
	return &Platform{
		Name: name,
		Infrastructure: Infrastructure{
			Provider: provider,
		},
		Networking: Networking{
			Domain:         domain,
			PrivateNetwork: "192.168.0.0/16",
		},
		Chassis: make(map[string][]ChassisProfile),
	}
}
