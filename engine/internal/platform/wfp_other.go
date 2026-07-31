//go:build !windows

package platform

import "errors"

func InspectWFP(_ bool) (WFPStatus, error) {
	return WFPStatus{}, errors.New("Windows Filtering Platform is only available on Windows")
}
