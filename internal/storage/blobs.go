// Package storage stores raw message bodies content-addressed on disk under
// <dir>/msgs/ab/cd/<sha256> (CLAUDE.md storage layout). The DB holds the ref.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// Store writes and reads content-addressed message blobs.
type Store struct {
	dir string
}

// New creates a Store rooted at dir (created if missing).
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "msgs"), 0o750); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// refPath maps a sha256 hex digest to its on-disk path and containing dir.
func (s *Store) refPath(sum string) (dir, path string) {
	dir = filepath.Join(s.dir, "msgs", sum[0:2], sum[2:4])
	return dir, filepath.Join(dir, sum)
}

// Put stores data and returns its content-addressed reference
// ("msgs/ab/cd/<sha256>"). Writing is idempotent for identical content.
func (s *Store) Put(data []byte) (string, error) {
	sum := hex.EncodeToString(sha256Sum(data))
	dir, path := s.refPath(sum)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	// Atomic write via temp file + rename.
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return filepath.Join("msgs", sum[0:2], sum[2:4], sum), nil
}

// Get reads a blob by its reference.
func (s *Store) Get(ref string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.dir, ref))
}

// Delete removes a blob by reference (used by retention).
func (s *Store) Delete(ref string) error {
	err := os.Remove(filepath.Join(s.dir, ref))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func sha256Sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}
