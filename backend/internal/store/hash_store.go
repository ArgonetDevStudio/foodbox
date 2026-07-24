package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

const hashMetadataFilename = "metadata.json"

type hashMetadata struct {
	LastImageHash string `json:"lastImageHash"`
}

type HashStore struct {
	mu   sync.RWMutex
	path string
}

func NewHashStore(directory string) (*HashStore, error) {
	path, err := ensureDataFile(directory, hashMetadataFilename)
	if err != nil {
		return nil, err
	}
	return &HashStore{path: path}, nil
}

func (s *HashStore) Load(ctx context.Context) (string, error) {
	return s.LoadImageHash(ctx)
}

func (s *HashStore) Save(ctx context.Context, hash string) error {
	return s.SaveImageHash(ctx, hash)
}

func (s *HashStore) LoadImageHash(ctx context.Context) (string, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadLocked()
}

func (s *HashStore) loadLocked() (string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return "", fmt.Errorf("read image hash metadata %q: %w", s.path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return "", nil
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return "", fmt.Errorf("decode image hash metadata %q: top-level null is not metadata", s.path)
	}
	var metadata hashMetadata
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&metadata); err != nil {
		return "", fmt.Errorf("decode image hash metadata %q: %w", s.path, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return "", fmt.Errorf("decode image hash metadata %q: %w", s.path, err)
	}
	return metadata.LastImageHash, nil
}

func (s *HashStore) SaveImageHash(ctx context.Context, hash string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	metadata := hashMetadata{LastImageHash: hash}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode image hash metadata: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return err
	}
	// Refuse to silently discard corrupt metadata. An operator can inspect or
	// restore the existing file before a new hash is persisted.
	if _, err := s.loadLocked(); err != nil {
		return err
	}
	if err := replaceFileAtomically(s.path, data); err != nil {
		return fmt.Errorf("save image hash metadata: %w", err)
	}
	return nil
}
