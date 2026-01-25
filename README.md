# plasmactl-node

A [Launchr](https://github.com/launchrctl/launchr) plugin for [Plasmactl](https://github.com/plasmash/plasmactl) that manages node provisioning and infrastructure for Plasma platforms.

## Overview

`plasmactl-node` handles the provisioning and management of physical/virtual machines (nodes) that form the infrastructure for Plasma platforms. It integrates with cloud providers via Terraform/OpenTofu to automate infrastructure provisioning.

## Features

- **Infrastructure Provisioning**: Provision nodes via Terraform/OpenTofu
- **Multi-Provider Support**: Scaleway Dedibox, Hetzner, OVH, AWS, GCP, Azure
- **Chassis-Driven Allocation**: Map logical architecture to physical resources
- **Manual Node Registration**: Support for on-premise/manual infrastructure
- **Node Lifecycle Management**: Create, list, show, destroy nodes

## Commands

### node:provision

Provision infrastructure for a platform:

```bash
# Provision nodes for a platform
plasmactl node:provision ski-dev \
  -c foundation.cluster.control:GP1-L:3 \
  -c cognition.data:GPU-3090:2

# Dry run (preview only)
plasmactl node:provision ski-dev -c foundation.cluster.control:GP1-L:3 --dry-run
```

Options:
- `-c, --chassis`: Chassis specification (format: `chassis:type:count`)
- `--dry-run`: Preview infrastructure changes without applying
- `--auto-approve`: Skip confirmation prompts

### node:register

Manually register a node (for manual/on-premise providers):

```bash
plasmactl node:register ski-dev \
  --hostname server1 \
  --public-ip 51.159.x.x \
  --private-ip 192.168.1.10 \
  --chassis foundation.cluster.control
```

Options:
- `-h, --hostname`: Node hostname (required)
- `--public-ip`: Public IP address (required)
- `--private-ip`: Private IP address (auto-assigned if not provided)
- `-c, --chassis`: Chassis assignments (can be specified multiple times)

### node:add

Create a platform scaffold with nodes directory:

```bash
plasmactl node:add ski-dev
```

### node:list

List nodes for a platform:

```bash
# List all platforms and their nodes
plasmactl node:list

# List nodes for a specific platform
plasmactl node:list ski-dev
```

### node:show

Show details of a specific node:

```bash
plasmactl node:show ski-dev server1
```

### node:destroy

Destroy a node (requires confirmation):

```bash
plasmactl node:destroy ski-dev server1

# With confirmation bypass (for automation)
plasmactl node:destroy ski-dev server1 --yes-i-am-sure
```

## Directory Structure

Nodes are stored in the `inst/` directory:

```
inst/
└── ski-dev/
    ├── platform.yaml          # Platform configuration
    └── nodes/
        ├── ski-dev-control-001.yaml
        ├── ski-dev-control-002.yaml
        └── ski-dev-cognition-001.yaml
```

## Node File Format

```yaml
hostname: ski-dev-control-001
chassis:
  - platform.foundation.cluster.control
provider_metadata:
  server_id: "12345"
network:
  public_ip: 51.159.x.x
  private_ip: 192.168.1.10
```

## Chassis-Driven Provisioning

The chassis specification maps logical architecture to physical infrastructure:

```bash
# Format: chassis:instance_type:count
plasmactl node:provision ski-dev \
  -c foundation.cluster.control:GP1-L:3 \      # 3 control plane nodes
  -c cognition.data:GPU-3090:2 \               # 2 GPU nodes for AI/ML
  -c cognition.data:HIGH-MEM:3                 # 3 high-memory nodes
```

Same chassis can have multiple hardware profiles for different workloads.

## Provider Configuration

Configure providers via `platform.yaml`:

```yaml
name: ski-dev
infrastructure:
  metal_provider: scaleway
  api:
    token: {{ .keyring.scaleway_api_token }}

networking:
  private_network: 192.168.0.0/16
```

### Supported Providers

| Provider | Type | Status |
|----------|------|--------|
| Scaleway Dedibox | Dedicated servers | Supported |
| Hetzner | Cloud/Dedicated | Planned |
| OVH | Cloud/Dedicated | Planned |
| AWS | Cloud | Planned |
| GCP | Cloud | Planned |
| Azure | Cloud | Planned |
| Manual | On-premise | Supported |

## Workflow Example

```bash
# 1. Create platform (handled by plasmactl-platform)
plasmactl platform:create ski-dev \
  --metal-provider scaleway \
  --dns-provider ovh \
  --domain dev.skilld.cloud

# 2. Provision infrastructure
plasmactl node:provision ski-dev \
  -c foundation.cluster.control:GP1-L:3

# 3. Verify nodes
plasmactl node:list ski-dev

# 4. Deploy platform
plasmactl platform:deploy ski-dev
```

## Documentation

- [Plasmactl](https://github.com/plasmash/plasmactl) - Main CLI tool
- [plasmactl-platform](https://github.com/plasmash/plasmactl-platform) - Platform management
- [Plasma Platform](https://plasma.sh) - Platform documentation

## License

[European Union Public License 1.2 (EUPL-1.2)](LICENSE)
