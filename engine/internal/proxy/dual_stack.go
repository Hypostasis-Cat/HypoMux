package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/Hypostasis-Cat/HypoMux/engine/internal/dns"
)

const connectionAttemptDelay = 250 * time.Millisecond
const nat64AttemptDelay = 2 * time.Second
const maxTargetAddressesPerFamily = 8

func adapterConnectionBudget(config Config, adapter Adapter, literal net.IP) time.Duration {
	budget := config.ConnectTimeout
	if literal == nil || (literal.To4() != nil && adapter.SourceIP == "") {
		budget += config.DNS.QueryTimeout
	}
	if literal == nil && adapter.SourceIP == "" && len(adapter.DNSServers) > 0 {
		budget += config.DNS.QueryTimeout + nat64AttemptDelay
	}
	return budget
}

type targetBatch struct {
	addresses []net.IP
	err       error
}
type targetDialResult struct {
	connection net.Conn
	ip         net.IP
	err        error
}

// DNS and dials overlap: a slow A/AAAA lookup cannot hold up the other family.
// Two in-flight sockets and eight addresses per family bound each attempt.
func (s *Server) dialAdapterTargets(ctx context.Context, adapter Adapter, host, port string, literal net.IP, scheduler *scheduler, tune bool) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, adapterConnectionBudget(s.config, adapter, literal))
	defer cancel()
	batches := make(chan targetBatch, 2)
	resolving := 0
	var queue []net.IP
	var requestNAT64 chan struct{}
	if literal != nil {
		if literal.To4() != nil && adapter.SourceIP == "" {
			translated, err := s.resolver.TranslateIPv4(ctx, adapterDNSBinding(adapter), literal)
			if err != nil {
				return nil, err
			}
			literal = translated
		}
		queue = append(queue, literal)
	} else {
		if s.resolver == nil {
			return nil, errors.New("DNS resolver is unavailable")
		}
		ready4 := adapter.SourceIP != "" && scheduler.health.familyAvailable(adapter, "tcp4")
		ready6 := adapter.SourceIPv6 != "" && scheduler.health.familyAvailable(adapter, "tcp6")
		if adapter.SourceIP == "" && adapter.SourceIPv6 != "" && len(adapter.DNSServers) > 0 {
			// RFC 8305: let native IPv6 win, then synthesize A records using
			// this network's prefix when a public resolver does not do DNS64.
			requestNAT64 = make(chan struct{}, 1)
			resolving++
			go func() {
				timer := time.NewTimer(nat64AttemptDelay)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-requestNAT64:
				case <-ctx.Done():
					return
				}
				answer, err := s.resolver.Resolve(ctx, dns.Query{Domain: host, RecordType: dns.RecordA, Binding: adapterDNSBinding(adapter)})
				batch := targetBatch{err: err}
				if err == nil {
					addresses := answer.Addresses
					if len(addresses) == 0 {
						addresses = []string{answer.Address}
					}
					for _, address := range addresses {
						ip := net.ParseIP(address)
						if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
							continue
						}
						translated, err := s.resolver.TranslateIPv4(ctx, adapterDNSBinding(adapter), ip)
						if err != nil {
							batch.err = err
							break
						}
						batch.addresses = append(batch.addresses, translated)
						if len(batch.addresses) == maxTargetAddressesPerFamily {
							break
						}
					}
					if len(batch.addresses) == 0 && batch.err == nil {
						batch.err = errors.New("no IPv4 target available for NAT64")
					}
				}
				select {
				case batches <- batch:
				case <-ctx.Done():
				}
			}()
		}
		for _, record := range []dns.RecordType{dns.RecordA, dns.RecordAAAA} {
			if (record == dns.RecordA && adapter.SourceIP == "") || (record == dns.RecordAAAA && adapter.SourceIPv6 == "") {
				continue
			}
			if (ready4 || ready6) && ((record == dns.RecordA && !ready4) || (record == dns.RecordAAAA && !ready6)) {
				continue
			}
			resolving++
			go func(record dns.RecordType) {
				answer, err := s.resolver.Resolve(ctx, dns.Query{Domain: host, RecordType: record, Binding: adapterDNSBinding(adapter)})
				batch := targetBatch{err: err}
				if err == nil {
					addresses := answer.Addresses
					if len(addresses) == 0 {
						addresses = []string{answer.Address}
					}
					seen := make(map[string]bool)
					for _, text := range addresses {
						ip := net.ParseIP(text)
						if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || seen[ip.String()] || (record == dns.RecordA) != (ip.To4() != nil) {
							continue
						}
						seen[ip.String()] = true
						batch.addresses = append(batch.addresses, ip)
						if len(batch.addresses) == maxTargetAddressesPerFamily {
							break
						}
					}
					if len(batch.addresses) == 0 {
						batch.err = fmt.Errorf("no usable %s target address", record)
					}
				}
				select {
				case batches <- batch:
				case <-ctx.Done():
				}
			}(record)
		}
	}
	results := make(chan targetDialResult)
	active := 0
	lastAttempt := time.Time{}
	timer := time.NewTimer(connectionAttemptDelay)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var failures []error
	launch := func() {
		ip := queue[0]
		queue = queue[1:]
		active++
		lastAttempt = time.Now()
		go func() {
			network := networkForIP("tcp", ip)
			dialer, err := boundNetworkDialer(adapter, s.config.ConnectTimeout, network)
			var connection net.Conn
			if err == nil {
				if tune {
					enableTCPDialerTuning(dialer)
				}
				dialCtx, stop := context.WithTimeout(ctx, s.config.ConnectTimeout)
				connection, err = s.dialTCP(dialCtx, dialer, net.JoinHostPort(ip.String(), port))
				stop()
			}
			outcome := targetDialResult{connection, ip, err}
			select {
			case results <- outcome:
			case <-ctx.Done():
				if connection != nil {
					connection.Close()
				}
			}
		}()
	}
	for resolving > 0 || active > 0 || len(queue) > 0 {
		var tick <-chan time.Time
		// Reserve the second socket while another DNS family or NAT64 is
		// pending; two blackholed same-family addresses must not starve it.
		if len(queue) > 0 && active < 2 && (active == 0 || resolving == 0) {
			wait := time.Until(lastAttempt.Add(connectionAttemptDelay))
			if active == 0 || wait <= 0 {
				launch()
				continue
			}
			timer.Reset(wait)
			tick = timer.C
		}
		select {
		case batch := <-batches:
			resolving--
			if batch.err != nil {
				failures = append(failures, batch.err)
			}
			// Put the newly available family first instead of exhausting one
			// family's list before giving the other family a chance.
			queue = append(batch.addresses, queue...)
		case result := <-results:
			active--
			if result.err == nil && result.connection != nil {
				scheduler.health.recordFamily(adapter, networkForIP("tcp", result.ip), true)
				if tune {
					tuneTCPConnection(result.connection)
				}
				scheduler.watchLatency(result.ip.String())
				return result.connection, nil
			}
			if result.connection != nil {
				result.connection.Close()
			}
			if ctx.Err() == nil && isLocalConnectFailure(result.err) {
				scheduler.health.recordFamily(adapter, networkForIP("tcp", result.ip), false)
			}
			failures = append(failures, fmt.Errorf("%s: %w", result.ip, result.err))
		case <-tick:
			launch()
		case <-ctx.Done():
			return nil, errors.Join(append(failures, ctx.Err())...)
		}
		if requestNAT64 != nil && active == 0 && len(queue) == 0 && resolving == 1 {
			select {
			case requestNAT64 <- struct{}{}:
			default:
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
	if len(failures) == 0 {
		return nil, errors.New("no target address matches the selected adapter")
	}
	return nil, errors.Join(failures...)
}
