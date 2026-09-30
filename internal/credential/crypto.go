// Package credential encrypts managed provider credentials, independently of storage.
package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// Cipher holds an instance master key. Its methods are safe for concurrent use.
type Cipher struct {
	aead      cipher.AEAD
	digestKey []byte
}

func New(hexKey string) (*Cipher, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("encryption key must be 32 hex-encoded bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	derive := hmac.New(sha256.New, key)
	derive.Write([]byte("jev-router/managed-credentials/idempotency/v1"))
	return &Cipher{aead: aead, digestKey: derive.Sum(nil)}, nil
}
func associatedData(providerID, revisionID string) []byte {
	b, _ := json.Marshal([]string{"jev-router/provider-credential", "v1", providerID, revisionID})
	return b
}
func (c *Cipher) Seal(providerID, revisionID string, plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", errors.New("credential encryption failed")
	}
	sealed := c.aead.Seal(nonce, nonce, plaintext, associatedData(providerID, revisionID))
	return "v1:" + base64.StdEncoding.EncodeToString(sealed), nil
}
func (c *Cipher) Open(providerID, revisionID, envelope string) ([]byte, error) {
	fail := errors.New("managed credential unavailable")
	if !strings.HasPrefix(envelope, "v1:") {
		return nil, fail
	}
	data, err := base64.StdEncoding.DecodeString(envelope[3:])
	if err != nil || len(data) < c.aead.NonceSize()+c.aead.Overhead() {
		return nil, fail
	}
	plain, err := c.aead.Open(nil, data[:c.aead.NonceSize()], data[c.aead.NonceSize():], associatedData(providerID, revisionID))
	if err != nil {
		return nil, fail
	}
	return plain, nil
}
func (c *Cipher) Digest(canonical []byte) string {
	h := hmac.New(sha256.New, c.digestKey)
	h.Write(canonical)
	return "hmac-v1:" + hex.EncodeToString(h.Sum(nil))
}
