package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

const privateKeyEnvironment = "UPDATE_MANIFEST_ED25519_PRIVATE_KEY"

func main() {
	inputPath := flag.String("input", "", "path to the exact manifest bytes to sign")
	outputPath := flag.String("output", "", "path for the raw Ed25519 signature")
	verificationKey := flag.String("verify-public-key", "", "optional base64 Ed25519 public key that must verify the signature")
	flag.Parse()
	if err := signManifest(*inputPath, *outputPath, os.Getenv(privateKeyEnvironment)); err != nil {
		fmt.Fprintln(os.Stderr, "sign update manifest:", err)
		os.Exit(1)
	}
	if *verificationKey != "" {
		if err := verifyManifestSignature(*inputPath, *outputPath, *verificationKey); err != nil {
			fmt.Fprintln(os.Stderr, "verify update manifest signature:", err)
			os.Exit(1)
		}
	}
}

func signManifest(inputPath string, outputPath string, encodedPrivateKey string) error {
	if inputPath == "" || outputPath == "" {
		return errors.New("both -input and -output are required")
	}
	privateKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedPrivateKey))
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return errors.New("private key secret must be base64-encoded raw Ed25519 private key bytes")
	}
	manifest, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	signature := ed25519.Sign(ed25519.PrivateKey(privateKey), manifest)
	if err := os.WriteFile(outputPath, signature, 0o600); err != nil {
		return fmt.Errorf("write signature: %w", err)
	}
	return nil
}

func verifyManifestSignature(inputPath string, signaturePath string, encodedPublicKey string) error {
	publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedPublicKey))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("verification key must be base64-encoded raw Ed25519 public key bytes")
	}
	manifest, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	signature, err := os.ReadFile(signaturePath)
	if err != nil {
		return fmt.Errorf("read signature: %w", err)
	}
	if len(signature) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(publicKey), manifest, signature) {
		return errors.New("signature does not match the supplied public key and manifest")
	}
	return nil
}
