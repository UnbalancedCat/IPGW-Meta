package ipgw

import (
	"context"
	"net"
	"net/netip"
	"sort"
)

func (c *Client) ListInterfaces(ctx context.Context) ([]Interface, error) {
	if err := validatePublicCall(c, ctx); err != nil {
		return nil, err
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, wrapError(CodeNetwork, "could not enumerate network interfaces", true, err)
	}
	var result []Interface
	for _, iface := range interfaces {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err != nil || !prefix.Addr().Is4() || prefix.Addr().IsLoopback() || prefix.Addr().IsUnspecified() {
				continue
			}
			ip := prefix.Addr().Unmap()
			result = append(result, Interface{Name: iface.Name, Index: iface.Index, IP: ip})
		}
	}
	return normalizeInterfaces(result), nil
}

type interfaceAddressKey struct {
	index int
	ip    netip.Addr
}

func normalizeInterfaces(candidates []Interface) []Interface {
	result := make([]Interface, 0, len(candidates))
	seen := make(map[interfaceAddressKey]struct{}, len(candidates))
	for _, candidate := range candidates {
		key := interfaceAddressKey{index: candidate.Index, ip: candidate.IP}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Index == result[j].Index {
			return result[i].IP.Less(result[j].IP)
		}
		return result[i].Index < result[j].Index
	})
	return result
}
