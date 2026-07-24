package store

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func ensureDataFile(directory, filename string) (string, error) {
	if directory == "" {
		return "", fmt.Errorf("data directory must not be empty")
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return "", fmt.Errorf("create data directory %q: %w", directory, err)
	}

	path := filepath.Join(directory, filename)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o640)
	if err != nil {
		return "", fmt.Errorf("open data file %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close data file %q: %w", path, err)
	}
	return path, nil
}

// replaceFileAtomically writes a unique temporary file in the destination
// directory, syncs its contents, renames it, and syncs the directory entry.
// The destination is never truncated and remains intact on pre-rename errors.
func replaceFileAtomically(path string, data []byte) (returnErr error) {
	directory := filepath.Dir(path)
	mode := fs.FileMode(0o640)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat destination file %q: %w", path, err)
	}

	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			if err := temporary.Close(); err != nil && returnErr == nil {
				returnErr = fmt.Errorf("close temporary file %q: %w", temporaryPath, err)
			}
		}
		if err := os.Remove(temporaryPath); err != nil && !os.IsNotExist(err) && returnErr == nil {
			returnErr = fmt.Errorf("remove temporary file %q: %w", temporaryPath, err)
		}
	}()

	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("set temporary file permissions: %w", err)
	}
	written, err := temporary.Write(data)
	if err != nil {
		return fmt.Errorf("write temporary file for %q: %w", path, err)
	}
	if written != len(data) {
		return fmt.Errorf("write temporary file for %q: %w", path, io.ErrShortWrite)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary file for %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file for %q: %w", path, err)
	}
	closed = true
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace data file %q: %w", path, err)
	}

	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open data directory %q for sync: %w", directory, err)
	}
	if err := directoryHandle.Sync(); err != nil {
		_ = directoryHandle.Close()
		return fmt.Errorf("sync data directory %q: %w", directory, err)
	}
	if err := directoryHandle.Close(); err != nil {
		return fmt.Errorf("close data directory %q: %w", directory, err)
	}
	return nil
}
