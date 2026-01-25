package plasmactlnode

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"

	"github.com/plasmash/plasmactl-node/pkg/types"
	"gopkg.in/yaml.v3"
)

// IPAllocator manages private IP allocation from a CIDR pool
type IPAllocator struct {
	network   *net.IPNet
	nodesDir  string
	allocated map[string]bool
}

// NewIPAllocator creates a new IP allocator for the given CIDR and nodes directory
func NewIPAllocator(cidr string, nodesDir string) (*IPAllocator, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}

	allocator := &IPAllocator{
		network:   network,
		nodesDir:  nodesDir,
		allocated: make(map[string]bool),
	}

	// Load existing allocations from node files
	if err := allocator.loadExisting(); err != nil {
		return nil, fmt.Errorf("failed to load existing allocations: %w", err)
	}

	return allocator, nil
}

// loadExisting reads all node files and marks their private IPs as allocated
func (a *IPAllocator) loadExisting() error {
	if _, err := os.Stat(a.nodesDir); os.IsNotExist(err) {
		return nil // No nodes directory yet
	}

	entries, err := os.ReadDir(a.nodesDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(a.nodesDir, entry.Name()))
		if err != nil {
			continue
		}

		var node types.Node
		if err := yaml.Unmarshal(data, &node); err != nil {
			continue
		}

		if node.Network.PrivateIP != "" {
			a.allocated[node.Network.PrivateIP] = true
		}
	}

	return nil
}

// Allocate returns the next available IP address from the pool
func (a *IPAllocator) Allocate() (string, error) {
	// Get all IPs in the range
	ips := a.getIPsInRange()

	// Find first available
	for _, ip := range ips {
		ipStr := ip.String()
		if !a.allocated[ipStr] {
			a.allocated[ipStr] = true
			return ipStr, nil
		}
	}

	return "", fmt.Errorf("no available IPs in pool %s", a.network.String())
}

// AllocateN returns N available IP addresses from the pool
func (a *IPAllocator) AllocateN(n int) ([]string, error) {
	var result []string

	for i := 0; i < n; i++ {
		ip, err := a.Allocate()
		if err != nil {
			return nil, err
		}
		result = append(result, ip)
	}

	return result, nil
}

// getIPsInRange returns all usable IPs in the network range
func (a *IPAllocator) getIPsInRange() []net.IP {
	var ips []net.IP

	ip := a.network.IP.Mask(a.network.Mask)
	for ip := ip.To4(); a.network.Contains(ip); inc(ip) {
		// Skip network address (first) and broadcast address (last)
		if !a.isNetworkOrBroadcast(ip) {
			// Also skip .0 and .255 addresses within subnets for safety
			lastOctet := ip[3]
			if lastOctet != 0 && lastOctet != 255 {
				ipCopy := make(net.IP, len(ip))
				copy(ipCopy, ip)
				ips = append(ips, ipCopy)
			}
		}
	}

	// Sort IPs for deterministic allocation
	sort.Slice(ips, func(i, j int) bool {
		return ipToInt(ips[i]) < ipToInt(ips[j])
	})

	return ips
}

// isNetworkOrBroadcast checks if an IP is the network or broadcast address
func (a *IPAllocator) isNetworkOrBroadcast(ip net.IP) bool {
	// Network address: all host bits are 0
	networkIP := a.network.IP.Mask(a.network.Mask)
	if ip.Equal(networkIP) {
		return true
	}

	// Broadcast address: all host bits are 1
	broadcast := make(net.IP, len(networkIP))
	for i := range broadcast {
		broadcast[i] = networkIP[i] | ^a.network.Mask[i]
	}
	return ip.Equal(broadcast)
}

// inc increments an IP address by 1
func inc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

// ipToInt converts an IP to an integer for sorting
func ipToInt(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}
