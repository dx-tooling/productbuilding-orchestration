package infra

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/nacl/box"
)

// sealActionsSecret encrypts a plaintext value with a repository's GitHub
// Actions public key using libsodium's crypto_box_seal (anonymous sealed box).
// The returned string is the base64-encoded sealed payload that can be PUT
// to /repos/{owner}/{repo}/actions/secrets/{name} as `encrypted_value`.
//
// publicKeyB64 is the base64-encoded 32-byte X25519 public key returned by
// GET /repos/{owner}/{repo}/actions/secrets/public-key.
func sealActionsSecret(plaintext, publicKeyB64 string) (string, error) {
	pkBytes, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return "", fmt.Errorf("decode public key: %w", err)
	}
	if len(pkBytes) != 32 {
		return "", fmt.Errorf("public key length = %d, want 32", len(pkBytes))
	}
	var pk [32]byte
	copy(pk[:], pkBytes)

	sealed, err := box.SealAnonymous(nil, []byte(plaintext), &pk, rand.Reader)
	if err != nil {
		return "", fmt.Errorf("seal: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}
