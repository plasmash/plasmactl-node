# plasmactl-node

A [Launchr](https://github.com/launchrctl/launchr) plugin for [Plasmactl](https://github.com/plasmash/plasmactl) that manages node provisioning and infrastructure for Plasma platforms.

## Overview

`plasmactl-node` handles the provisioning and management of physical/virtual machines (nodes) that form the infrastructure for Plasma platforms. It integrates with cloud providers via Terraform/OpenTofu to automate infrastructure provisioning.

## Features

- **Infrastructure Provisioning**: Provision nodes via Terraform/OpenTofu
- **Multi-Provider Support**: Scaleway, Hetzner, OVH, AWS, and more
- **Zone-Driven**: Allocate nodes to zones for proper resource mapping
- **Manual Registration**: Register existing nodes manually
- **IP Management**: Automatic private IP allocation from configurable pools

## Commands

### node:add

Create a platform scaffold with nodes directory:

```bash
plasmactl node:add myplatform
plasmactl node:add myplatform --provider scaleway --domain example.com
```

Options:
- `-p, --provider`: Infrastructure provider (default: manual)
- `-d, --domain`: Platform domain

### node:provision

Provision infrastructure for a platform:

```bash
# Provision nodes with zone specifications
plasmactl node:provision myplatform \
  -z foundation.cluster.control:GP1-L:3 \
  -z cognition.data:GPU-3090:2

# Dry run (preview only)
plasmactl node:provision myplatform -z foundation.cluster.control:GP1-L:3 --dry-run

# Auto-approve without confirmation
plasmactl node:provision myplatform -z foundation.cluster.control:GP1-L:3 --auto-approve
```

Options:
- `-z, --zone`: Zone specification (format: `zone:type:count`)
- `--dry-run`: Preview infrastructure changes without applying
- `--auto-approve`: Skip confirmation prompts

### node:register

Manually register a node:

```bash
plasmactl node:register myplatform \
  --hostname server1 \
  --public-ip 51.159.x.x \
  --private-ip 192.168.1.10 \
  --zone foundation.cluster.control
```

Options:
- `--hostname`: Node hostname (required)
- `--public-ip`: Public IP address
- `--private-ip`: Private IP address
- `-z, --zone`: Zone assignments (can be specified multiple times)

### node:allocate

Allocate a node to zones using kubectl-style operations:

```bash
# Show current allocations (no operations = show mode)
plasmactl node:allocate node001

# Add zone allocations
plasmactl node:allocate node001 platform.foundation.cluster.control
plasmactl node:allocate node001 platform.foundation.cluster.control platform.cognition.data

# Remove allocation (trailing dash)
plasmactl node:allocate node001 platform.cognition.data-

# Replace allocation (slash separator)
plasmactl node:allocate node001 platform.foundation.cluster.control/platform.foundation.cluster.nodes

# Combined operations
plasmactl node:allocate node001 newsection oldsection- old/new
```

Options:
- `-e, --env`: Environment name (looks in `inst/<env>/nodes/`)

### node:list

List platforms and their nodes:

```bash
plasmactl node:list
```

### node:show

Show details of a platform or node:

```bash
plasmactl node:show myplatform
```

### node:destroy

Destroy infrastructure:

```bash
plasmactl node:destroy myplatform
plasmactl node:destroy myplatform --force
plasmactl node:destroy myplatform --keep-nodes
```

Options:
- `--force`: Force destruction without confirmation
- `--keep-nodes`: Keep node files after destroying infrastructure

## Project Structure

```
plasmactl-node/
├── plugin.go                        # Plugin registration
├── actions/
│   ├── add/
│   │   ├── add.yaml                 # Action definition
│   │   └── add.go                   # Implementation
│   ├── allocate/
│   │   ├── allocate.yaml
│   │   └── allocate.go
│   ├── destroy/
│   │   ├── destroy.yaml
│   │   └── destroy.go
│   ├── list/
│   │   ├── list.yaml
│   │   └── list.go
│   ├── provision/
│   │   ├── provision.yaml
│   │   └── provision.go
│   ├── register/
│   │   ├── register.yaml
│   │   └── register.go
│   └── show/
│       ├── show.yaml
│       └── show.go
└── internal/
    ├── allocator/                   # IP allocation
    │   └── ip_allocator.go
    ├── terraform/                   # Terraform/OpenTofu integration
    │   └── terraform.go
    └── types/                       # YAML types
        ├── node.go
        └── platform.go
```

## Directory Structure

Nodes are stored in the `inst/` directory:

```
inst/
└── myplatform/
    ├── platform.yaml          # Platform configuration
    └── nodes/
        ├── node001.yaml
        ├── node002.yaml
        └── node003.yaml
```

## Node File Format

```yaml
hostname: node001
zones:
  - platform.foundation.cluster.control
  - platform.foundation.network.ingress
network:
  public_ip: 51.159.x.x
  private_ip: 192.168.1.10
provider_metadata:
  server_id: "12345"
labels:
  foundation: "true"
```

## Zone-Driven Provisioning

The zone specification maps logical architecture to physical infrastructure:

```bash
# Format: zone:instance_type:count
plasmactl node:provision myplatform \
  -z foundation.cluster.control:GP1-L:3 \      # 3 control plane nodes
  -z cognition.data:GPU-3090:2 \               # 2 GPU nodes for AI/ML
  -z cognition.data:HIGH-MEM:3                 # 3 high-memory nodes
```

## Workflow Example

```bash
# 1. Create platform scaffold
plasmactl node:add myplatform --provider scaleway

# 2. Provision infrastructure
plasmactl node:provision myplatform -z foundation.cluster.control:GP1-L:3

# 3. Verify nodes
plasmactl node:list

# 4. Allocate additional zones to nodes
plasmactl node:allocate node001 platform.interaction.observability

# 5. Deploy platform
plasmactl platform:deploy myplatform
```

## Related Commands

| Plugin | Command | Purpose |
|--------|---------|---------|
| plasmactl-topology | `topology:list` | List available zones |
| plasmactl-topology | `topology:show` | Show nodes allocated to a zone |
| plasmactl-platform | `platform:deploy` | Deploy to provisioned nodes |

## Documentation

- [Plasmactl](https://github.com/plasmash/plasmactl) - Main CLI tool
- [plasmactl-topology](https://github.com/plasmash/plasmactl-topology) - Topology management
- [Plasma Platform](https://plasma.sh) - Platform documentation

## License

[European Union Public License 1.2 (EUPL-1.2)](LICENSE)
