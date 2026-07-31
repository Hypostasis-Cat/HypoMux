//go:build !windows

package diagnostic

import "errors"

func newProber() (prober, error) {
	return nil, errors.New("source-bound ICMP diagnostics require Windows")
}
