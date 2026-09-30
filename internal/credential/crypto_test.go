package credential

import (
	"strings"
	"testing"
)

func TestCipherRoundTripAndRandomization(t *testing.T) {
	c, err := New(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.Seal("p", "r", []byte("test-secret-marker"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Seal("p", "r", []byte("test-secret-marker"))
	if err != nil {
		t.Fatal(err)
	}
	if a == b || strings.Contains(a, "test-secret-marker") {
		t.Fatal("not randomized ciphertext")
	}
	plain, err := c.Open("p", "r", a)
	if err != nil || string(plain) != "test-secret-marker" {
		t.Fatal("roundtrip failed", err)
	}
}
func TestCipherRejectsInvalidKey(t *testing.T) {
	for _, s := range []string{"", strings.Repeat("ab", 16), strings.Repeat("ab", 24), strings.Repeat("x", 64)} {
		if _, err := New(s); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
}
func TestCipherRejectsCorruptionAndIdentitySwap(t *testing.T) {
	c, _ := New(strings.Repeat("ab", 32))
	wrong, _ := New(strings.Repeat("cd", 32))
	enc, _ := c.Seal("p", "r", []byte("secret-marker"))
	for _, tc := range []struct {
		c       *Cipher
		p, r, e string
	}{{wrong, "p", "r", enc}, {c, "q", "r", enc}, {c, "p", "s", enc}, {c, "p", "r", "v2:" + enc[3:]}, {c, "p", "r", "v1:AA=="}, {c, "p", "r", enc[:len(enc)-4] + "AAAA"}, {c, "p", "r", "secret-marker"}} {
		if _, err := tc.c.Open(tc.p, tc.r, tc.e); err == nil || strings.Contains(err.Error(), "secret-marker") {
			t.Fatal("invalid ciphertext accepted or exposed")
		}
	}
}
func TestDigestBindsPayload(t *testing.T) {
	c, _ := New(strings.Repeat("ab", 32))
	d, _ := New(strings.Repeat("cd", 32))
	if c.Digest([]byte("a")) != c.Digest([]byte("a")) || c.Digest([]byte("a")) == c.Digest([]byte("b")) || c.Digest([]byte("a")) == d.Digest([]byte("a")) {
		t.Fatal("digest does not bind key and input")
	}
}
