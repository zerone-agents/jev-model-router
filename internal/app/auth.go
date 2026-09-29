package app

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/management"
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"strings"
)

type Credentials struct{ settings, inference [32]byte }

func LoadCredentials(c Config, lookup func(string) (string, bool), readFile func(string) ([]byte, error)) (Credentials, error) {
	a, e := ResolveSecret(c.SettingsRef, lookup, readFile)
	if e != nil {
		return Credentials{}, e
	}
	b, e := ResolveSecret(c.InferenceRef, lookup, readFile)
	if e != nil {
		return Credentials{}, e
	}
	if subtle.ConstantTimeCompare(a, b) == 1 {
		return Credentials{}, errors.New("role credentials must differ")
	}
	return Credentials{sha256.Sum256(a), sha256.Sum256(b)}, nil
}
func Authenticate(header string, c Credentials) (management.Principal, error) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return management.Principal{}, routing.Fail("unauthorized", "valid bearer credential required")
	}
	h := sha256.Sum256([]byte(token))
	settings := subtle.ConstantTimeCompare(h[:], c.settings[:])
	inference := subtle.ConstantTimeCompare(h[:], c.inference[:])
	if settings == 1 {
		return management.Principal{ID: "settings", Role: "settings"}, nil
	}
	if inference == 1 {
		return management.Principal{ID: "inference", Role: "inference"}, nil
	}
	return management.Principal{}, routing.Fail("unauthorized", "valid bearer credential required")
}
