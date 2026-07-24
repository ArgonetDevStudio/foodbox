package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"testing"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

// legacyMenuFile is intentionally independent of domain.Menu. It models the
// exact JSON shape consumed by the previous Java MenuRepository.
type legacyMenuFile struct {
	Date  [3]int   `json:"date"`
	Menus []string `json:"menus"`
	Valid bool     `json:"valid"`
}

func TestLegacyDatabaseContractPreservesDateValidityAndMenuOrder(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, menuDatabaseFilename)
	legacy := []byte(`[
  {"date":[2026,7,27],"menus":["last","두 번째",""],"valid":false},
  {"date":[2026,7,25],"menus":["first","second","third"],"valid":false},
  {"date":[2026,7,25],"menus":["replacement","side","kimchi"],"valid":true},
  {"date":[2026,7,26],"menus":["only"],"valid":true}
]`)
	mustWriteCompatFile(t, path, legacy)

	repository := mustOpenCompatStore(t, directory)
	menus, err := repository.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll() error = %v", err)
	}
	want := []domain.Menu{
		{Date: compatDate(2026, 7, 27), Menus: []string{"last", "두 번째", ""}, Valid: false},
		{Date: compatDate(2026, 7, 26), Menus: []string{"only"}, Valid: true},
		{Date: compatDate(2026, 7, 25), Menus: []string{"replacement", "side", "kimchi"}, Valid: true},
	}
	if !reflect.DeepEqual(menus, want) {
		t.Fatalf("FindAll() = %#v, want %#v", menus, want)
	}
}

func TestLegacyDatabaseUpsertIsLastWriteWinsAndDiskIsAscending(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, menuDatabaseFilename)
	mustWriteCompatFile(t, path, []byte(`[
  {"date":[2026,7,27],"menus":["old future"],"valid":false},
  {"date":[2026,7,25],"menus":["old first","old second"],"valid":false}
]`))
	repository := mustOpenCompatStore(t, directory)

	date := compatDate(2026, 7, 25)
	err := repository.SaveAll(context.Background(), []domain.Menu{
		{Date: compatDate(2026, 7, 26), Menus: []string{"middle", "side", "kimchi"}, Valid: true},
		{Date: date, Menus: []string{"first replacement"}, Valid: false},
		{Date: date, Menus: []string{"last replacement", "keeps", "this order"}, Valid: true},
	})
	if err != nil {
		t.Fatalf("SaveAll() error = %v", err)
	}

	disk := decodeLegacyJavaDatabase(t, mustReadCompatFile(t, path))
	want := []legacyMenuFile{
		{Date: [3]int{2026, 7, 25}, Menus: []string{"last replacement", "keeps", "this order"}, Valid: true},
		{Date: [3]int{2026, 7, 26}, Menus: []string{"middle", "side", "kimchi"}, Valid: true},
		{Date: [3]int{2026, 7, 27}, Menus: []string{"old future"}, Valid: false},
	}
	if !reflect.DeepEqual(disk, want) {
		t.Fatalf("legacy database = %#v, want oldest-first %#v", disk, want)
	}
	menu, found, err := repository.FindByDate(context.Background(), date)
	if err != nil || !found || !reflect.DeepEqual(menu.Menus, want[0].Menus) || menu.Valid != want[0].Valid {
		t.Fatalf("FindByDate() = (%#v, %v, %v), want last duplicate %#v", menu, found, err, want[0])
	}
}

func TestRepositoryDatabaseFixtureSurvivesGoRoundTripAndJavaRollback(t *testing.T) {
	t.Parallel()
	originalData := []byte(legacyDatabaseRoundTripFixture)
	original := decodeLegacyJavaDatabase(t, originalData)
	if len(original) == 0 {
		t.Fatal("repository db fixture is unexpectedly empty")
	}

	directory := t.TempDir()
	path := filepath.Join(directory, menuDatabaseFilename)
	mustWriteCompatFile(t, path, originalData)
	repository := mustOpenCompatStore(t, directory)
	menus, err := repository.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll(repository fixture) error = %v", err)
	}
	if len(menus) != len(original) {
		t.Fatalf("FindAll(repository fixture) count = %d, want %d", len(menus), len(original))
	}
	for index, menu := range menus {
		legacy := original[len(original)-1-index]
		if menu.Date != compatDate(legacy.Date[0], legacy.Date[1], legacy.Date[2]) ||
			!reflect.DeepEqual(menu.Menus, legacy.Menus) || menu.Valid != legacy.Valid {
			t.Fatalf("FindAll()[%d] = %#v, want reverse of fixture %#v", index, menu, legacy)
		}
	}

	// Rewriting with Go must leave a file that the old Java runtime can read
	// unchanged after a rollback.
	if err := repository.SaveAll(context.Background(), menus); err != nil {
		t.Fatalf("SaveAll(repository fixture) error = %v", err)
	}
	rewritten := decodeLegacyJavaDatabase(t, mustReadCompatFile(t, path))
	if !reflect.DeepEqual(rewritten, original) {
		t.Fatalf("Go round trip changed Java-readable fixture\ngot:  %#v\nwant: %#v", rewritten, original)
	}
}

func TestLegacyDatabaseEmptyAndMalformedInputs(t *testing.T) {
	t.Parallel()
	for name, data := range map[string][]byte{
		"zero bytes":  nil,
		"whitespace":  []byte(" \n\t"),
		"empty array": []byte("[]"),
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			mustWriteCompatFile(t, filepath.Join(directory, menuDatabaseFilename), data)
			menus, err := mustOpenCompatStore(t, directory).FindAll(context.Background())
			if err != nil {
				t.Fatalf("FindAll() error = %v", err)
			}
			if menus == nil || len(menus) != 0 {
				t.Fatalf("FindAll() = %#v, want non-nil empty slice", menus)
			}
		})
	}

	malformed := map[string][]byte{
		"top-level null":        []byte("null"),
		"top-level object":      []byte(`{"date":[2026,7,25]}`),
		"truncated JSON":        []byte(`[{"date":[2026,7,25]}`),
		"trailing JSON":         []byte(`[] []`),
		"short date array":      []byte(`[{"date":[2026,7],"menus":[],"valid":false}]`),
		"invalid calendar date": []byte(`[{"date":[2026,2,30],"menus":[],"valid":false}]`),
		"ISO typo":              []byte(`[{"date":"2026-7-25","menus":[],"valid":false}]`),
	}
	for name, data := range malformed {
		data := append([]byte(nil), data...)
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, menuDatabaseFilename)
			mustWriteCompatFile(t, path, data)
			repository := mustOpenCompatStore(t, directory)
			if _, err := repository.FindAll(context.Background()); err == nil {
				t.Fatal("FindAll() unexpectedly accepted malformed legacy database")
			}
			if err := repository.SaveAll(context.Background(), []domain.Menu{{
				Date: compatDate(2026, 7, 28), Menus: []string{"must not replace malformed input"}, Valid: false,
			}}); err == nil {
				t.Fatal("SaveAll() unexpectedly accepted malformed legacy database")
			}
			if after := mustReadCompatFile(t, path); !bytes.Equal(after, data) {
				t.Fatalf("failed operation changed malformed database: got %q, want %q", after, data)
			}
		})
	}
}

func TestLegacyDatabaseConcurrentAtomicUpsertsRemainReadable(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, menuDatabaseFilename)
	mustWriteCompatFile(t, path, []byte(`[{"date":[2026,7,1],"menus":["seed"],"valid":false}]`))
	repository := mustOpenCompatStore(t, directory)

	const writers = 16
	start := make(chan struct{})
	stopReader := make(chan struct{})
	readerErrors := make(chan error, 1)
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		for {
			select {
			case <-stopReader:
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err == nil {
				_, err = strictLegacyJavaDatabase(data)
			}
			if err != nil {
				select {
				case readerErrors <- fmt.Errorf("observed partial/non-legacy database: %w", err):
				default:
				}
				return
			}
			runtime.Gosched()
		}
	}()

	writerErrors := make(chan error, writers)
	for day := 2; day <= writers+1; day++ {
		day := day
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			writerErrors <- repository.SaveAll(context.Background(), []domain.Menu{{
				Date:  compatDate(2026, 7, day),
				Menus: []string{fmt.Sprintf("main-%02d", day), "side", "kimchi"}, Valid: true,
			}})
		}()
	}
	close(start)
	for range writers {
		if err := <-writerErrors; err != nil {
			t.Errorf("concurrent SaveAll() error = %v", err)
		}
	}
	close(stopReader)
	wait.Wait()
	select {
	case err := <-readerErrors:
		t.Fatal(err)
	default:
	}

	disk := decodeLegacyJavaDatabase(t, mustReadCompatFile(t, path))
	if len(disk) != writers+1 {
		t.Fatalf("database count = %d, want %d (no lost concurrent updates)", len(disk), writers+1)
	}
	if !sort.SliceIsSorted(disk, func(i, j int) bool { return compatDateArrayLess(disk[i].Date, disk[j].Date) }) {
		t.Fatalf("concurrent database is not oldest-first: %#v", disk)
	}
	leftovers, err := filepath.Glob(filepath.Join(directory, ".db.json.tmp-*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("atomic upserts left temporary files behind: %v", leftovers)
	}
}

func decodeLegacyJavaDatabase(t *testing.T, data []byte) []legacyMenuFile {
	t.Helper()
	menus, err := strictLegacyJavaDatabase(data)
	if err != nil {
		t.Fatalf("database is not readable by the legacy Java shape: %v\n%s", err, data)
	}
	return menus
}

func strictLegacyJavaDatabase(data []byte) ([]legacyMenuFile, error) {
	var values []map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&values); err != nil {
		return nil, err
	}
	if values == nil {
		return nil, fmt.Errorf("top-level value must be a non-null array")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	menus := make([]legacyMenuFile, len(values))
	for index, value := range values {
		if len(value) != 3 || value["date"] == nil || value["menus"] == nil || value["valid"] == nil {
			return nil, fmt.Errorf("item %d keys = %v, want exactly date, menus, valid", index, compatKeys(value))
		}
		var dateParts []int
		if err := json.Unmarshal(value["date"], &dateParts); err != nil {
			return nil, fmt.Errorf("item %d legacy date: %w", index, err)
		}
		if len(dateParts) != 3 {
			return nil, fmt.Errorf("item %d legacy date must contain exactly 3 integers, got %d", index, len(dateParts))
		}
		menus[index].Date = [3]int{dateParts[0], dateParts[1], dateParts[2]}
		if _, err := domain.NewLocalDate(menus[index].Date[0], menus[index].Date[1], menus[index].Date[2]); err != nil {
			return nil, fmt.Errorf("item %d legacy date: %w", index, err)
		}
		if err := json.Unmarshal(value["menus"], &menus[index].Menus); err != nil || menus[index].Menus == nil {
			return nil, fmt.Errorf("item %d menus must be a non-null string array: %w", index, err)
		}
		if err := json.Unmarshal(value["valid"], &menus[index].Valid); err != nil {
			return nil, fmt.Errorf("item %d valid: %w", index, err)
		}
	}
	return menus, nil
}

func compatKeys(value map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compatDateArrayLess(left, right [3]int) bool {
	for index := range left {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	return false
}

func compatDate(year, month, day int) domain.LocalDate {
	return domain.LocalDate{Year: year, Month: month, Day: day}
}

const legacyDatabaseRoundTripFixture = `[
  {"date":[2025,8,1],"menus":["치킨마요","참치김치찌개","햄맛살볶음","사과해초무침","마늘쫑무침","포기김치"],"valid":true},
  {"date":[2025,8,4],"menus":["돈육간장불고기","맑은콩나물국","견과류멸치볶음","계란찜","오복채고추지무침","배추김치"],"valid":true},
  {"date":[2025,8,5],"menus":["고구마치즈돈가스","쫄데기찌개","간장어묵볶음","매운목이버섯무침","단무지무침","배추김치"],"valid":true},
  {"date":[2025,8,6],"menus":["카레라이스","소고기무국","도라지오이무침","명엽채볶음","브로콜리/초장","배추김치"],"valid":true},
  {"date":[2025,8,7],"menus":["안동찜닭","야채된장국","건새우마늘쫑볶음","오이피클","볼어묵조림","배추김치"],"valid":true},
  {"date":[2025,8,8],"menus":["탕수육","짬뽕국","두부조림","오이무침","맛살유부겨자채","배추김치"],"valid":true},
  {"date":[2025,8,11],"menus":["돼지양념구이","우렁된장찌개","오이도라지무침","양념깻잎지","실곤약국수무침","배추김치"],"valid":true},
  {"date":[2025,8,12],"menus":["불고기궁중떡볶이","육개장","후랑크소시지볶음","감자조림","닭가슴살샐러드","배추김치"],"valid":true},
  {"date":[2025,8,13],"menus":["오리훈제/부추무침","된장미역국","치킨너겟/케찹","우엉어묵볶음","동부묵무침","배추김치"],"valid":true},
  {"date":[2025,8,14],"menus":["돈육떡갈비/샐러드","우거지해장국","계란찜","과일샐러드","골뱅이야채무침","배추김치"],"valid":true},
  {"date":[2025,8,15],"menus":["광복절"],"valid":false}
]`

func mustOpenCompatStore(t *testing.T, directory string) *FileStore {
	t.Helper()
	repository, err := NewFileStore(directory)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	return repository
}

func mustWriteCompatFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func mustReadCompatFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return data
}
