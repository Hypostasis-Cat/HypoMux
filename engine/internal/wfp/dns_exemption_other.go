//go:build !windows

package wfp

import "errors"

func OpenDNSExemption(_ string, _ []Adapter) (DNSExemption, error) {
	return nil, errors.New("WFP DNS exemption is only available on Windows")
}
