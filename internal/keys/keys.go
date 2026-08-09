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
	"sync"
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

// Store is a hashed key store backed by a JSON file. It's shared by two
// very different callers: short-lived CLI invocations (`keys create`,
// `keys revoke`, ...) and the long-running gateway process's Verify calls.
// Since those are normally separate OS processes, Store transparently
// reloads from disk when the file's mtime moves forward, so keys created or
// revoked while `serve`/the service is already running take effect
// immediately rather than requiring a restart.
type Store struct {
	path string

	mu    sync.Mutex
	keys  []Key
	mtime time.Time
}

// Load reads path, or starts an empty store if it doesn't exist yet.
func Load(path string) (*Store, error) {
	s := &Store{path: path}
	if err := s.reloadLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// reloadLocked (re)reads the key file if it changed since the last load.
// Caller must hold s.mu.
func (s *Store) reloadLocked() error {
	fi, err := os.Stat(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", s.path, err)
	}
	if !fi.ModTime().After(s.mtime) {
		return nil
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", s.path, err)
	}
	var keys []Key
	if err := json.Unmarshal(data, &keys); err != nil {
		return fmt.Errorf("parsing %s: %w", s.path, err)
	}
	s.keys = keys
	s.mtime = fi.ModTime()
	return nil
}

// save persists s.keys and updates mtime so this same process doesn't
// immediately (and redundantly) reload what it just wrote.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.keys, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return err
	}
	if fi, err := os.Stat(s.path); err == nil {
		s.mtime = fi.ModTime()
	}
	return nil
}

// Create generates a new key named name, persists its hash, and returns the
// raw key value. The raw value is never stored or recoverable afterward.
func (s *Store) Create(name string) (raw string, key Key, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.reloadLocked(); err != nil {
		return "", Key{}, err
	}

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
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadLocked()
	return append([]Key(nil), s.keys...)
}

// Revoke removes the key matching id or name. Reports whether one was found.
func (s *Store) Revoke(idOrName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadLocked()

	for i, k := range s.keys {
		if k.ID == idOrName || k.Name == idOrName {
			s.keys = append(s.keys[:i], s.keys[i+1:]...)
			_ = s.save()
			return true
		}
	}
	return false
}

// Verify reports whether raw matches any non-revoked stored key. It reloads
// the on-disk store first so keys created or revoked by a separate `local-ai
// keys ...` invocation take effect without restarting the gateway.
func (s *Store) Verify(raw string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.reloadLocked()

	for _, k := range s.keys {
		if bcrypt.CompareHashAndPassword([]byte(k.Hash), []byte(raw)) == nil {
			return true
		}
	}
	return false
}
