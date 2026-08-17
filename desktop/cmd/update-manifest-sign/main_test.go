package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestSignManifestWritesVerifiableRawSignature(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "latest.json")
	outputPath := filepath.Join(directory, "latest.json.sig")
	manifest := []byte("{\"schema_version\":1}\n")
	if err := os.WriteFile(inputPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	privateKey := ed25519.NewKeyFromSeed([]byte("0123456789abcdef0123456789abcdef"))
	encodedPrivateKey := base64.StdEncoding.EncodeToString(privateKey)
	if err := signManifest(inputPath, outputPath, encodedPrivateKey); err != nil {
		t.Fatal(err)
	}
	signature, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(signature) != ed25519.SignatureSize {
		t.Fatalf("signature length = %d", len(signature))
	}
	if !ed25519.Verify(privateKey.Public().(ed25519.PublicKey), manifest, signature) {
		t.Fatal("signature does not verify against exact manifest bytes")
	}
	publicKey := base64.StdEncoding.EncodeToString(privateKey.Public().(ed25519.PublicKey))
	if err := verifyManifestSignature(inputPath, outputPath, publicKey); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyManifestSignatureRejectsDifferentPublicKey(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "latest.json")
	signaturePath := filepath.Join(directory, "latest.json.sig")
	if err := os.WriteFile(inputPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	privateKey := ed25519.NewKeyFromSeed([]byte("0123456789abcdef0123456789abcdef"))
	if err := signManifest(inputPath, signaturePath, base64.StdEncoding.EncodeToString(privateKey)); err != nil {
		t.Fatal(err)
	}
	otherKey := ed25519.NewKeyFromSeed([]byte("abcdef0123456789abcdef0123456789"))
	otherPublicKey := base64.StdEncoding.EncodeToString(otherKey.Public().(ed25519.PublicKey))
	if err := verifyManifestSignature(inputPath, signaturePath, otherPublicKey); err == nil {
		t.Fatal("signature verified with a different public key")
	}
}

func TestSignManifestRejectsInvalidPrivateKey(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "latest.json")
	if err := os.WriteFile(inputPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signManifest(inputPath, inputPath+".sig", "not-a-private-key"); err == nil {
		t.Fatal("invalid private key was accepted")
	}
}
