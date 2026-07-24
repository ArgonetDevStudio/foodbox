package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

func TestFileStoreReadsLegacyAndISOAndWritesLegacySorted(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, menuDatabaseFilename)
	original := `[
  {"date":"2026-07-26","menus":["new"],"valid":false},
  {"date":[2026,7,25],"menus":["old","side","kimchi"],"valid":true}
]`
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	repository, err := NewFileStore(directory)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	menus, err := repository.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll() error = %v", err)
	}
	if len(menus) != 2 || menus[0].Date.String() != "2026-07-26" || menus[1].Date.String() != "2026-07-25" {
		t.Fatalf("FindAll() = %+v, want reverse chronological order", menus)
	}

	replacement := domain.Menu{
		Date:  domain.LocalDate{Year: 2026, Month: 7, Day: 25},
		Menus: []string{"replacement"},
		Valid: false,
	}
	inserted := domain.NewMenu(
		domain.LocalDate{Year: 2026, Month: 7, Day: 27},
		[]string{"main", "side", "kimchi"},
	)
	if err := repository.SaveAll(context.Background(), []domain.Menu{replacement, inserted}); err != nil {
		t.Fatalf("SaveAll() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var raw []struct {
		Date  []int    `json:"date"`
		Menus []string `json:"menus"`
		Valid bool     `json:"valid"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("written database is invalid JSON: %v\n%s", err, data)
	}
	if len(raw) != 3 {
		t.Fatalf("written menu count = %d, want 3", len(raw))
	}
	wantDates := [][]int{{2026, 7, 25}, {2026, 7, 26}, {2026, 7, 27}}
	for index, want := range wantDates {
		if !equalInts(raw[index].Date, want) {
			t.Fatalf("date[%d] = %v, want %v", index, raw[index].Date, want)
		}
	}
	if len(raw[0].Menus) != 1 || raw[0].Menus[0] != "replacement" || raw[0].Valid {
		t.Fatalf("upsert did not preserve replacement value: %+v", raw[0])
	}
}

func TestFileStoreHandlesEmptyFile(t *testing.T) {
	t.Parallel()

	repository, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	menus, err := repository.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll() error = %v", err)
	}
	if menus == nil || len(menus) != 0 {
		t.Fatalf("FindAll() = %#v, want non-nil empty slice", menus)
	}
}

func TestFileStoreCheckWritableIsNonDestructiveAndCleansUp(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	repository, err := NewFileStore(directory)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	databasePath := filepath.Join(directory, menuDatabaseFilename)
	original := []byte(`[{"date":[2026,7,25],"menus":["one"],"valid":false}]`)
	if err := os.WriteFile(databasePath, original, 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := repository.CheckWritable(context.Background()); err != nil {
		t.Fatalf("CheckWritable() error = %v", err)
	}
	got, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("CheckWritable() changed database: %q", got)
	}
	assertNoWritableCheckFiles(t, directory)
}

func TestFileStoreCheckWritableHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	repository, err := NewFileStore(directory)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := repository.CheckWritable(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckWritable() error = %v, want context.Canceled", err)
	}
	assertNoWritableCheckFiles(t, directory)
}

func TestFileStoreCheckWritableFailsForReadOnlyDirectory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	repository, err := NewFileStore(directory)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if err := os.Chmod(directory, 0o555); err != nil {
		t.Fatalf("Chmod(read-only) error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(directory, 0o755); err != nil {
			t.Errorf("restore directory permissions: %v", err)
		}
	})

	err = repository.CheckWritable(context.Background())
	if err == nil {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory write permission bits")
		}
		t.Fatal("CheckWritable() unexpectedly accepted a read-only directory")
	}
	assertNoWritableCheckFiles(t, directory)
}

func TestFileStoreLastDuplicateWins(t *testing.T) {
	t.Parallel()

	repository, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	date := domain.LocalDate{Year: 2026, Month: 7, Day: 25}
	if err := repository.SaveAll(context.Background(), []domain.Menu{
		domain.NewMenu(date, []string{"first"}),
		domain.NewMenu(date, []string{"last"}),
	}); err != nil {
		t.Fatalf("SaveAll() error = %v", err)
	}
	menu, found, err := repository.FindByDate(context.Background(), date)
	if err != nil || !found {
		t.Fatalf("FindByDate() = (%+v, %v, %v)", menu, found, err)
	}
	if len(menu.Menus) != 1 || menu.Menus[0] != "last" {
		t.Fatalf("FindByDate() = %+v, want last duplicate", menu)
	}
}

func TestFileStorePreservesExistingFileOnInvalidData(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, menuDatabaseFilename)
	original := []byte(`{"not":"an array"}`)
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	repository, err := NewFileStore(directory)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	err = repository.SaveAll(context.Background(), []domain.Menu{
		domain.NewMenu(domain.LocalDate{Year: 2026, Month: 7, Day: 25}, []string{"one"}),
	})
	if err == nil {
		t.Fatal("SaveAll() unexpectedly replaced malformed existing data")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("database was changed on failure: %q", got)
	}
}

func TestFileStorePropagatesCancellation(t *testing.T) {
	t.Parallel()

	repository, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.FindAll(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("FindAll() error = %v, want context.Canceled", err)
	}
	if err := repository.SaveAll(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("SaveAll() error = %v, want context.Canceled", err)
	}
}

func TestFileStoreConcurrentUpsertsDoNotLoseData(t *testing.T) {
	t.Parallel()

	repository, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	const workers = 20
	var wait sync.WaitGroup
	errorsByWorker := make(chan error, workers)
	for day := 1; day <= workers; day++ {
		day := day
		wait.Add(1)
		go func() {
			defer wait.Done()
			menu := domain.NewMenu(
				domain.LocalDate{Year: 2026, Month: 7, Day: day},
				[]string{"main", "side", "kimchi"},
			)
			errorsByWorker <- repository.SaveAll(context.Background(), []domain.Menu{menu})
		}()
	}
	wait.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		if err != nil {
			t.Fatalf("concurrent SaveAll() error = %v", err)
		}
	}

	menus, err := repository.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll() error = %v", err)
	}
	if len(menus) != workers {
		t.Fatalf("FindAll() count = %d, want %d", len(menus), workers)
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(repository.path), ".db.json.tmp-*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

func TestNewFileStorePropagatesFilesystemError(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := NewFileStore(path); err == nil {
		t.Fatal("NewFileStore() unexpectedly accepted a file as directory")
	}
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func assertNoWritableCheckFiles(t *testing.T, directory string) {
	t.Helper()
	leftovers, err := filepath.Glob(filepath.Join(directory, ".foodbox-writable-check-*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("writability check files left behind: %v", leftovers)
	}
}
