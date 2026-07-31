//go:build !windows

package services

func detectCompatibilityPlan() compatibilityPlan {
	return normalizedCompatibilityPlan(nil, nil)
}
