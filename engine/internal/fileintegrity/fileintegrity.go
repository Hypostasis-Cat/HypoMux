package fileintegrity

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// SHA256 returns the lowercase SHA-256 digest of the file at path.
func SHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// SHA256Bytes returns the lowercase SHA-256 digest of data.
func SHA256Bytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// VerifySHA256 compares a file with a pinned SHA-256 digest. The comparison is
// constant-time so callers can use this at a local privilege boundary.
func VerifySHA256(path string, expected string) error {
	expectedBytes, err := hex.DecodeString(strings.TrimSpace(expected))
	if err != nil || len(expectedBytes) != sha256.Size {
		return errors.New("invalid pinned SHA-256 digest")
	}
	actual, err := SHA256(path)
	if err != nil {
		return fmt.Errorf("hash file: %w", err)
	}
	actualBytes, err := hex.DecodeString(actual)
	if err != nil {
		return fmt.Errorf("decode calculated SHA-256 digest: %w", err)
	}
	if subtle.ConstantTimeCompare(actualBytes, expectedBytes) != 1 {
		return errors.New("SHA-256 digest mismatch")
	}
	return nil
}
