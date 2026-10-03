package dns

import (
	"context"
	"fmt"
	"net"
	"net/netip"
)

// RFC 6052 puts the IPv4 bytes around the reserved octet at bit 64.
// Reject unaligned prefixes and non-standard lengths rather than guessing.
func SynthesizeNAT64(prefix netip.Prefix, address netip.Addr) (netip.Addr, error) {
	if !prefix.IsValid() || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || prefix.Addr().IsMulticast() || prefix.Addr().IsLinkLocalUnicast() || prefix.Addr().IsUnspecified() || prefix.Addr().Zone() != "" || prefix != prefix.Masked() || !address.Is4() {
		return netip.Addr{}, fmt.Errorf("invalid NAT64 prefix or IPv4 address")
	}
	switch prefix.Bits() {
	case 32, 40, 48, 56, 64, 96:
	default:
		return netip.Addr{}, fmt.Errorf("unsupported NAT64 prefix length")
	}
	out := prefix.Addr().As16()
	v4 := address.As4()
	position := prefix.Bits() / 8
	for _, b := range v4 {
		if position == 8 {
			position++
		}
		out[position] = b
		position++
	}
	return netip.AddrFrom16(out), nil
}

// RFC 7050 discovery accepts only answers embedding an ipv4only.arpa address
// with zero reserved/suffix bits. Never assume the well-known prefix exists.
func DiscoverNAT64Prefix(answers []string) (netip.Prefix, error) {
	for _, text := range answers {
		answer, err := netip.ParseAddr(text)
		if err != nil || !answer.Is6() || answer.Is4In6() || answer.Zone() != "" {
			continue
		}
		for _, bits := range []int{96, 64, 56, 48, 40, 32} {
			prefix := netip.PrefixFrom(answer, bits).Masked()
			for _, v4 := range []string{"192.0.0.170", "192.0.0.171"} {
				synthesized, _ := SynthesizeNAT64(prefix, netip.MustParseAddr(v4))
				if synthesized == answer {
					return prefix, nil
				}
			}
		}
	}
	return netip.Prefix{}, fmt.Errorf("selected network DNS did not advertise a NAT64 prefix")
}

func (r *Resolver) TranslateIPv4(ctx context.Context, binding Binding, ip net.IP) (net.IP, error) {
	if r == nil || ip.To4() == nil || binding.SourceIPv6 == "" {
		return nil, fmt.Errorf("NAT64 requires an IPv6 source and IPv4 target")
	}
	answer, err := r.Resolve(ctx, Query{Domain: "ipv4only.arpa", RecordType: RecordAAAA, Binding: binding, NetworkDNS: true})
	if err != nil {
		return nil, fmt.Errorf("NAT64 prefix discovery: %w", err)
	}
	addresses := answer.Addresses
	if len(addresses) == 0 {
		addresses = []string{answer.Address}
	}
	prefix, err := DiscoverNAT64Prefix(addresses)
	if err != nil {
		return nil, err
	}
	v4, _ := netip.AddrFromSlice(ip.To4())
	translated, err := SynthesizeNAT64(prefix, v4)
	if err != nil {
		return nil, err
	}
	return net.IP(translated.AsSlice()), nil
}
