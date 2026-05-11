// Package ovh contains OVH-specific implementations of plasmactl-node's
// provider abstractions: NodeMetadataFetcher (server MACs + disks) and
// VRackReconciler (private-network membership management).
package ovh

import (
	"fmt"
	"strings"
)

// SynthesizeDiskPaths returns kernel-conventional /dev paths for the given
// disk type and count. Mirrors the legacy logic from pla-plasma's
// platform_nodes.py:
//
//	NVMe          → /dev/nvme0n1, /dev/nvme1n1, ...
//	SATA/SAS/SSD/HDD/unknown  → /dev/sda, /dev/sdb, ...
//
// OVH's DiskTypeEnum is documented as NVMe/SAS/SATA/SSD/Unknown but the
// `/dedicated/server/{name}/specifications/hardware` endpoint returns
// uppercase values in practice (e.g. "NVME" for advance-1 BHS hardware).
// Comparison is case-insensitive to absorb that drift. "HDD" is not an OVH
// enum value but is accepted here for forward-compatibility with callers
// that may use it.
//
// Empty for count<=0.
func SynthesizeDiskPaths(diskType string, count int) []string {
	if count <= 0 {
		return []string{}
	}
	if strings.EqualFold(diskType, "NVMe") {
		paths := make([]string, count)
		for i := 0; i < count; i++ {
			paths[i] = fmt.Sprintf("/dev/nvme%dn1", i)
		}
		return paths
	}
	// SATA / SAS / SSD / HDD / Unknown / anything else → /dev/sd<a..>
	paths := make([]string, count)
	for i := 0; i < count; i++ {
		paths[i] = "/dev/sd" + string(rune('a'+i))
	}
	return paths
}
