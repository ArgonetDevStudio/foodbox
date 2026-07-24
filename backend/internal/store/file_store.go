package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

const menuDatabaseFilename = "db.json"

type FileStore struct {
	mu   sync.RWMutex
	path string
}

func NewFileStore(directory string) (*FileStore, error) {
	path, err := ensureDataFile(directory, menuDatabaseFilename)
	if err != nil {
		return nil, err
	}
	return &FileStore{path: path}, nil
}

// CheckWritable verifies the permissions required by atomic database writes
// without touching the database. It creates a uniquely named file in the same
// directory, writes and syncs it, closes it, and removes it before returning.
func (s *FileStore) CheckWritable(ctx context.Context) (returnErr error) {
	if s == nil || s.path == "" {
		return errors.New("check database writability: store is not initialized")
	}
	if err := contextError(ctx); err != nil {
		return err
	}

	directory := filepath.Dir(s.path)
	temporary, err := os.CreateTemp(directory, ".foodbox-writable-check-*")
	if err != nil {
		return fmt.Errorf("database directory %q is not writable: create check file: %w", directory, err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			if err := temporary.Close(); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("close writability check file %q: %w", temporaryPath, err))
			}
		}
		if err := os.Remove(temporaryPath); err != nil && !os.IsNotExist(err) {
			returnErr = errors.Join(returnErr, fmt.Errorf("remove writability check file %q: %w", temporaryPath, err))
		}
	}()

	checkData := []byte("foodbox-writable-check\n")
	written, err := temporary.Write(checkData)
	if err != nil {
		return fmt.Errorf("database directory %q is not writable: write check file: %w", directory, err)
	}
	if written != len(checkData) {
		return fmt.Errorf("database directory %q is not writable: write check file: %w", directory, io.ErrShortWrite)
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("database directory %q is not writable: sync check file: %w", directory, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("database directory %q is not writable: close check file: %w", directory, err)
	}
	closed = true
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("database directory %q is not writable: remove check file: %w", directory, err)
	}
	return nil
}

func (s *FileStore) FindByDate(ctx context.Context, date domain.LocalDate) (domain.Menu, bool, error) {
	if err := date.Validate(); err != nil {
		return domain.Menu{}, false, fmt.Errorf("find menu: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return domain.Menu{}, false, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	menus, err := s.loadLocked()
	if err != nil {
		return domain.Menu{}, false, err
	}
	for _, menu := range menus {
		if menu.Date == date {
			return menu.Clone(), true, nil
		}
	}
	return domain.Menu{}, false, nil
}

// FindAll returns menus in reverse chronological order, matching the existing
// API repository behavior. Database files themselves are written oldest first.
func (s *FileStore) FindAll(ctx context.Context) ([]domain.Menu, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	menus, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(menus, func(left, right int) bool {
		return menus[left].Date.After(menus[right].Date)
	})
	return cloneMenus(menus), nil
}

// SaveAll upserts by date. If input contains a duplicate date, its last value
// wins. A failed encode or write leaves the existing database file untouched.
func (s *FileStore) SaveAll(ctx context.Context, incoming []domain.Menu) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	for index, menu := range incoming {
		if err := menu.Date.Validate(); err != nil {
			return fmt.Errorf("save menu at index %d: %w", index, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	menus, err := s.loadLocked()
	if err != nil {
		return err
	}

	byDate := make(map[domain.LocalDate]domain.Menu, len(menus)+len(incoming))
	for _, menu := range menus {
		byDate[menu.Date] = menu.Clone()
	}
	for _, menu := range incoming {
		byDate[menu.Date] = menu.Clone()
	}

	merged := make([]domain.Menu, 0, len(byDate))
	for _, menu := range byDate {
		merged = append(merged, menu)
	}
	sort.Slice(merged, func(left, right int) bool {
		return merged[left].Date.Before(merged[right].Date)
	})

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("encode menu database: %w", err)
	}
	data = append(data, '\n')
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := replaceFileAtomically(s.path, data); err != nil {
		return fmt.Errorf("save menu database: %w", err)
	}
	return nil
}

func (s *FileStore) loadLocked() ([]domain.Menu, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read menu database %q: %w", s.path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return []domain.Menu{}, nil
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, fmt.Errorf("decode menu database %q: top-level null is not a menu array", s.path)
	}

	var menus []domain.Menu
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&menus); err != nil {
		return nil, fmt.Errorf("decode menu database %q: %w", s.path, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode menu database %q: %w", s.path, err)
	}

	byDate := make(map[domain.LocalDate]domain.Menu, len(menus))
	for index, menu := range menus {
		if err := menu.Date.Validate(); err != nil {
			return nil, fmt.Errorf("decode menu database %q item %d: %w", s.path, index, err)
		}
		byDate[menu.Date] = menu.Clone()
	}
	result := make([]domain.Menu, 0, len(byDate))
	for _, menu := range byDate {
		result = append(result, menu)
	}
	return result, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("unexpected data after JSON value")
}

func cloneMenus(menus []domain.Menu) []domain.Menu {
	cloned := make([]domain.Menu, len(menus))
	for index, menu := range menus {
		cloned[index] = menu.Clone()
	}
	return cloned
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("store operation canceled: %w", err)
	}
	return nil
}
