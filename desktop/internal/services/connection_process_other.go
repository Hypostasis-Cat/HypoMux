//go:build !windows

package services

func resolveConnectionProcesses(_ []connectionTelemetry) map[uint64]string {
	return map[uint64]string{}
}
