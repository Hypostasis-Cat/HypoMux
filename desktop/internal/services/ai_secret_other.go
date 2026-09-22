//go:build !windows

package services

import "errors"

func protectAIData(data []byte, encrypt bool) ([]byte, error) {
	return nil, errors.New("AI 凭据存储需要 Windows DPAPI")
}
