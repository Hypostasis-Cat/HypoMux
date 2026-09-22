//go:build !windows

package services

import (
	"context"
	"errors"
)

func readMTU(context.Context, string) (MTUInfo, error) {
	return MTUInfo{}, errors.New("MTU 检测仅支持 Windows")
}
func probeMTU(context.Context, string, string, int) (bool, error) {
	return false, errors.New("MTU 检测仅支持 Windows")
}
