package fileintegrity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySHA256RejectsMutationAndInvalidDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.exe")
	if err := os.WriteFile(path, []byte("trusted"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest, err := SHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	if digest != SHA256Bytes([]byte("trusted")) {
		t.Fatalf("file and byte digests differ: file=%s bytes=%s", digest, SHA256Bytes([]byte("trusted")))
	}
	if err := VerifySHA256(path, digest); err != nil {
		t.Fatalf("verify trusted file: %v", err)
	}
	if err := os.WriteFile(path, []byte("mutated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifySHA256(path, digest); err == nil {
		t.Fatal("mutated file matched its pinned digest")
	}
	if err := VerifySHA256(path, "not-a-digest"); err == nil {
		t.Fatal("invalid pinned digest was accepted")
	}
}
