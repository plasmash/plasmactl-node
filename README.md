# plasmactl-node

A [Launchr](https://github.com/launchrctl/launchr) plugin for [Plasmactl](https://github.com/plasmash/plasmactl) that manages node provisioning and infrastructure for Plasma platforms.

## Overview

`plasmactl-node` handles the provisioning and management of physical/virtual machines (nodes) that form the infrastructure for Plasma platforms. It integrates with cloud providers via Terraform/OpenTofu to automate infrastructure provisioning.

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
# Provision nodes
plasmactl node:provision myplatform \
  -c foundation.cluster.control:GP1-L:3 \
  -c cognition.data:GPU-3090:2

# Dry run (preview only)
plasmactl node:provision myplatform -c foundation.cluster.control:GP1-L:3 --dry-run

# Auto-approve without confirmation
plasmactl node:provision myplatform -c foundation.cluster.control:GP1-L:3 --auto-approve
```

Options:
- `-c, --chassis`: Chassis specification (format: `section:type:count`)
- `--dry-run`: Preview infrastructure changes without applying
- `--auto-approve`: Skip confirmation prompts

### node:register

Manually register a node:

```bash
plasmactl node:register myplatform \
  --hostname server1 \
  --public-ip 51.159.x.x \
  --private-ip 192.168.1.10 \
  --chassis foundation.cluster.control
```

Options:
- `--hostname`: Node hostname (required)
- `--public-ip`: Public IP address
- `--private-ip`: Private IP address
- `-c, --chassis`: Chassis assignments

### node:allocate

Allocate a node to chassis sections using kubectl-style operations:

```bash
# Show current allocations (no operations = show mode)
plasmactl node:allocate node001

# Add chassis allocations
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
chassis:
  - platform.foundation.cluster.control
  - platform.foundation.network.ingress
network:
  public_ip: 51.159.x.x
  private_ip: 192.168.1.10
labels:
  foundation: "true"
```

## Chassis-Driven Provisioning

The chassis specification maps logical architecture to physical infrastructure:

```bash
# Format: section:instance_type:count
plasmactl node:provision myplatform \
  -c foundation.cluster.control:GP1-L:3 \      # 3 control plane nodes
  -c cognition.data:GPU-3090:2 \               # 2 GPU nodes for AI/ML
  -c cognition.data:HIGH-MEM:3                 # 3 high-memory nodes
```

## Workflow Example

```bash
# 1. Create platform scaffold
plasmactl node:add myplatform --provider scaleway

# 2. Provision infrastructure
plasmactl node:provision myplatform -c foundation.cluster.control:GP1-L:3

# 3. Verify nodes
plasmactl node:list

# 4. Allocate additional chassis sections to nodes
plasmactl node:allocate node001 platform.interaction.observability

# 5. Deploy platform
plasmactl platform:deploy myplatform
```

## Documentation

- [Plasmactl](https://github.com/plasmash/plasmactl) - Main CLI tool
- [plasmactl-chassis](https://github.com/plasmash/plasmactl-chassis) - Chassis management
- [Plasma Platform](https://plasma.sh) - Platform documentation

## License

[European Union Public License 1.2 (EUPL-1.2)](LICENSE)
