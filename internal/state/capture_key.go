package state

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/zerone-agents/jev-model-router/internal/credential"
)

// The capture key lives beside the database and is never stored in SQLite.
func loadCaptureCipher(path string, create bool) (*credential.Cipher, error) {
	key, err := os.ReadFile(path)
	if create && errors.Is(err, os.ErrNotExist) {
		var raw [32]byte
		if _, err = rand.Read(raw[:]); err != nil {
			return nil, err
		}
		var tmp *os.File
		tmp, err = os.CreateTemp(filepath.Dir(path), ".capture-key-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmp.Name())
		_, err = tmp.WriteString(hex.EncodeToString(raw[:]))
		if err == nil {
			err = tmp.Sync()
		}
		closeErr := tmp.Close()
		if err != nil || closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		if err := os.Link(tmp.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		key, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("capture key must be an owner-only regular file")
	}
	return credential.New(string(key))
}
