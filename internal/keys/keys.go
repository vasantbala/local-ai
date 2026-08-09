// Package keys manages local-ai's own gateway API keys, independent of
// llama-server's internal secret. Keys are stored hashed (bcrypt); the raw
// value is shown to the user exactly once, at creation time.
package keys

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Key is one issued gateway API key (never holds the raw secret).
type Key struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"created_at"`
}

// Store is a hashed key store backed by a JSON file.
type Store struct {
	path string
	keys []Key
}

// Load reads path, or starts an empty store if it doesn't exist yet.
func Load(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &s.keys); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return s, nil
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.keys, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// Create generates a new key named name, persists its hash, and returns the
// raw key value. The raw value is never stored or recoverable afterward.
func (s *Store) Create(name string) (raw string, key Key, err error) {
	idBytes := make([]byte, 4)
	if _, err = rand.Read(idBytes); err != nil {
		return "", Key{}, err
	}
	secretBytes := make([]byte, 20)
	if _, err = rand.Read(secretBytes); err != nil {
		return "", Key{}, err
	}

	raw = "la_" + hex.EncodeToString(secretBytes)
	hash, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return "", Key{}, err
	}

	key = Key{
		ID:        hex.EncodeToString(idBytes),
		Name:      name,
		Prefix:    raw[:9], // "la_" + 6 hex chars, enough to eyeball in `list`
		Hash:      string(hash),
		CreatedAt: time.Now().UTC(),
	}
	s.keys = append(s.keys, key)
	if err := s.save(); err != nil {
		return "", Key{}, err
	}
	return raw, key, nil
}

// List returns all issued keys (hashes only, never raw values).
func (s *Store) List() []Key {
	return append([]Key(nil), s.keys...)
}

// Revoke removes the key matching id or name. Reports whether one was found.
func (s *Store) Revoke(idOrName string) bool {
	for i, k := range s.keys {
		if k.ID == idOrName || k.Name == idOrName {
			s.keys = append(s.keys[:i], s.keys[i+1:]...)
			_ = s.save()
			return true
		}
	}
	return false
}

// Verify reports whether raw matches any non-revoked stored key.
func (s *Store) Verify(raw string) bool {
	for _, k := range s.keys {
		if bcrypt.CompareHashAndPassword([]byte(k.Hash), []byte(raw)) == nil {
			return true
		}
	}
	return false
}
