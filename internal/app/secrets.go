package app

import (
	"errors"
	"path/filepath"
	"strings"
)

func ResolveSecret(ref string, lookup func(string) (string, bool), readFile func(string) ([]byte, error)) ([]byte, error) {
	kind, key, ok := strings.Cut(ref, ":")
	if !ok || key == "" || strings.ContainsRune(key, 0) {
		return nil, errors.New("invalid credential reference")
	}
	var b []byte
	switch kind {
	case "env":
		v, ok := lookup(key)
		if !ok {
			return nil, errors.New("credential reference is unavailable")
		}
		b = []byte(v)
	case "file":
		if !filepath.IsAbs(key) {
			return nil, errors.New("credential file must be absolute")
		}
		var e error
		b, e = readFile(key)
		if e != nil {
			return nil, errors.New("credential reference is unavailable")
		}
		b = []byte(strings.TrimRight(string(b), "\r\n"))
	default:
		return nil, errors.New("unsupported credential reference")
	}
	if len(b) == 0 || len(b) > 65536 || strings.ContainsAny(string(b), "\r\n\x00") {
		return nil, errors.New("invalid credential value")
	}
	return b, nil
}
