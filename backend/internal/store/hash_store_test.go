package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHashStoreEmptySaveAndLoad(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	store, err := NewHashStore(directory)
	if err != nil {
		t.Fatalf("NewHashStore() error = %v", err)
	}
	hash, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if hash != "" {
		t.Fatalf("initial hash = %q, want empty", hash)
	}

	const want = "315f5bdb76d078c43b8ac0064e4a0164612b1fce"
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.LoadImageHash(context.Background())
	if err != nil {
		t.Fatalf("LoadImageHash() error = %v", err)
	}
	if got != want {
		t.Fatalf("LoadImageHash() = %q, want %q", got, want)
	}

	leftovers, err := filepath.Glob(filepath.Join(directory, ".metadata.json.tmp-*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

func TestHashStoreDoesNotReplaceMalformedMetadata(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	store, err := NewHashStore(directory)
	if err != nil {
		t.Fatalf("NewHashStore() error = %v", err)
	}
	path := filepath.Join(directory, hashMetadataFilename)
	original := []byte(`{"lastImageHash":`)
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := store.Load(context.Background()); err == nil {
		t.Fatal("Load() unexpectedly accepted malformed metadata")
	}
	if err := store.Save(context.Background(), "new-hash"); err == nil {
		t.Fatal("Save() unexpectedly replaced malformed metadata")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("metadata changed after failed load: %q", got)
	}
}
