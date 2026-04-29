package infra

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

func TestSealActionsSecret_RoundtripsViaOpenAnonymous(t *testing.T) {
	// Generate a key pair the way the GitHub server would.
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub[:])

	plaintext := "fw_secret_value_12345"

	sealedB64, err := sealActionsSecret(plaintext, pubB64)
	if err != nil {
		t.Fatalf("sealActionsSecret: %v", err)
	}

	sealed, err := base64.StdEncoding.DecodeString(sealedB64)
	if err != nil {
		t.Fatalf("decode sealed: %v", err)
	}

	opened, ok := box.OpenAnonymous(nil, sealed, pub, priv)
	if !ok {
		t.Fatal("OpenAnonymous failed; sealed payload not decryptable with the matching key pair")
	}
	if string(opened) != plaintext {
		t.Errorf("opened = %q, want %q", string(opened), plaintext)
	}
}

func TestSealActionsSecret_RejectsMalformedPublicKey(t *testing.T) {
	tests := []struct {
		name      string
		publicKey string
	}{
		{"empty", ""},
		{"not base64", "this is not base64!@#$"},
		{"wrong length", base64.StdEncoding.EncodeToString([]byte("only-21-bytes-long-xx"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sealActionsSecret("anything", tt.publicKey)
			if err == nil {
				t.Errorf("expected error for malformed public key %q", tt.publicKey)
			}
		})
	}
}
