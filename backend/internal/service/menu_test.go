package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

type memoryRepository struct {
	mu        sync.Mutex
	menus     map[string]domain.Menu
	findErr   error
	saveErr   error
	saveCalls int
}

type writableRepository struct {
	*memoryRepository
	writableErr   error
	writableCalls int
}

func (r *writableRepository) CheckWritable(context.Context) error {
	r.writableCalls++
	return r.writableErr
}

func newMemoryRepository(menus ...domain.Menu) *memoryRepository {
	repository := &memoryRepository{menus: make(map[string]domain.Menu)}
	for _, menu := range menus {
		repository.menus[formatDate(menu.Date)] = menu
	}
	return repository
}

func (r *memoryRepository) FindAll(context.Context) ([]domain.Menu, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findErr != nil {
		return nil, r.findErr
	}
	menus := make([]domain.Menu, 0, len(r.menus))
	for _, menu := range r.menus {
		menus = append(menus, menu)
	}
	return menus, nil
}

func (r *memoryRepository) FindByDate(_ context.Context, date domain.LocalDate) (domain.Menu, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findErr != nil {
		return domain.Menu{}, false, r.findErr
	}
	menu, found := r.menus[formatDate(date)]
	return menu, found, nil
}

func (r *memoryRepository) SaveAll(_ context.Context, menus []domain.Menu) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saveCalls++
	if r.saveErr != nil {
		return r.saveErr
	}
	for _, menu := range menus {
		r.menus[formatDate(menu.Date)] = menu
	}
	return nil
}

type crawlerFunc func(context.Context) (string, error)

func (f crawlerFunc) Download(ctx context.Context) (string, error) { return f(ctx) }

type ocrFunc func(context.Context, []byte) ([]byte, error)

func (f ocrFunc) Recognize(ctx context.Context, image []byte) ([]byte, error) {
	return f(ctx, image)
}

type parserFunc func([]byte, io.Reader, domain.LocalDate) ([]domain.Menu, error)

func (f parserFunc) Parse(response []byte, image io.Reader, today domain.LocalDate) ([]domain.Menu, error) {
	return f(response, image, today)
}

type memoryHashStore struct {
	mu        sync.Mutex
	hash      string
	loadErr   error
	saveErr   error
	saveCalls int
}

func (s *memoryHashStore) Load(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hash, s.loadErr
}

func (s *memoryHashStore) Save(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveCalls++
	if s.saveErr == nil {
		s.hash = hash
	}
	return s.saveErr
}

func fixedDate(year, month, day int) func() domain.LocalDate {
	return func() domain.LocalDate { return domain.LocalDate{Year: year, Month: month, Day: day} }
}

func writeImage(t *testing.T, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "menu.jpg")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func successfulParser(menu domain.Menu) parserFunc {
	return func(response []byte, image io.Reader, today domain.LocalDate) ([]domain.Menu, error) {
		if string(response) != "ocr" {
			return nil, errors.New("wrong response")
		}
		if _, err := io.ReadAll(image); err != nil {
			return nil, err
		}
		return []domain.Menu{menu}, nil
	}
}

func TestTodayReturnsWeekendMessageWithoutDependencies(t *testing.T) {
	service := NewMenuService(nil, nil, nil, nil, nil)
	date := domain.LocalDate{Year: 2026, Month: 7, Day: 25}

	menu, err := service.Today(context.Background(), date)
	if err != nil {
		t.Fatal(err)
	}
	if menu.Date != date || menu.Valid || len(menu.Menus) != 1 || menu.Menus[0] != "주말에는 도시락이 없습니다." {
		t.Fatalf("unexpected weekend menu: %+v", menu)
	}
}

func TestReadyChecksOptionalRepositoryWriteCapability(t *testing.T) {
	writableError := errors.New("data directory is read-only")
	repository := &writableRepository{
		memoryRepository: newMemoryRepository(),
		writableErr:      writableError,
	}
	service := NewMenuService(repository, nil, nil, nil, nil)

	err := service.Ready(context.Background())
	if !errors.Is(err, writableError) {
		t.Fatalf("error = %v, want wrapped writable error", err)
	}
	if repository.writableCalls != 1 {
		t.Fatalf("writable checks = %d, want 1", repository.writableCalls)
	}
}

func TestReadySupportsRepositoriesWithoutWriteCapability(t *testing.T) {
	service := NewMenuService(newMemoryRepository(), nil, nil, nil, nil)
	if err := service.Ready(context.Background()); err != nil {
		t.Fatalf("ready = %v", err)
	}
}

func TestReadyChecksPersistedImageHashMetadata(t *testing.T) {
	metadataError := errors.New("corrupt metadata JSON")
	hashes := &memoryHashStore{loadErr: metadataError}
	service := NewMenuService(newMemoryRepository(), nil, nil, nil, hashes)

	err := service.Ready(context.Background())
	if !errors.Is(err, metadataError) {
		t.Fatalf("error = %v, want wrapped metadata error", err)
	}
}

func TestTodayReturnsExistingMenuWithoutCrawl(t *testing.T) {
	date := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	want := domain.NewMenu(date, []string{"one", "two", "three"})
	repository := newMemoryRepository(want)
	service := NewMenuService(repository, nil, nil, nil, nil)

	got, err := service.Today(context.Background(), date)
	if err != nil {
		t.Fatal(err)
	}
	if got.Date != want.Date || !got.Valid || len(got.Menus) != 3 {
		t.Fatalf("menu = %+v", got)
	}
}

func TestTodayCrawlsOnceAndRetriesRepository(t *testing.T) {
	date := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	want := domain.NewMenu(date, []string{"one", "two", "three"})
	repository := newMemoryRepository()
	hashes := &memoryHashStore{}
	path := writeImage(t, []byte("image"))
	service := NewMenuService(
		repository,
		crawlerFunc(func(context.Context) (string, error) { return path, nil }),
		ocrFunc(func(context.Context, []byte) ([]byte, error) { return []byte("ocr"), nil }),
		successfulParser(want),
		hashes,
		WithDateClock(fixedDate(2026, 7, 25)),
	)

	got, err := service.Today(context.Background(), date)
	if err != nil {
		t.Fatal(err)
	}
	if got.Date != want.Date || repository.saveCalls != 1 || hashes.saveCalls != 1 {
		t.Fatalf("menu=%+v saves=%d hash saves=%d", got, repository.saveCalls, hashes.saveCalls)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("crawler temp file was not removed: %v", err)
	}
}

func TestTodayReturnsTypedNotUploadedError(t *testing.T) {
	today := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	future := domain.NewMenu(
		domain.LocalDate{Year: 2026, Month: 7, Day: 28},
		[]string{"future", "menu", "coverage"},
	)
	repository := newMemoryRepository(future)
	hashes := &memoryHashStore{hash: "same"}
	path := writeImage(t, []byte("image"))
	hashes.hash = "78805a221a988e79ef3f42d7c5bfd418"
	ocrCalls := 0
	service := NewMenuService(repository, crawlerFunc(func(context.Context) (string, error) {
		return path, nil
	}), ocrFunc(func(context.Context, []byte) ([]byte, error) {
		ocrCalls++
		return []byte("ocr"), nil
	}), successfulParser(future), hashes, WithDateClock(func() domain.LocalDate { return today }))

	_, err := service.Today(context.Background(), today)
	if !errors.Is(err, ErrMenuNotUploaded) {
		t.Fatalf("error = %v", err)
	}
	if got := err.Error(); got != "Today Menu is not uploaded yet" {
		t.Fatalf("error message = %q", got)
	}
	if ocrCalls != 1 {
		t.Fatalf("OCR calls = %d, want 1", ocrCalls)
	}
	statusError, ok := ErrMenuNotUploaded.(interface {
		HTTPStatus() int
		ErrorCode() string
	})
	if !ok || statusError.HTTPStatus() != 404 || statusError.ErrorCode() != "MENU_NOT_UPLOADED" {
		t.Fatalf("missing HTTP error metadata: %#v", ErrMenuNotUploaded)
	}
}

func TestMatchingImageHashRetriesWhenRepositoryCoverageIsMissing(t *testing.T) {
	today := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	past := domain.NewMenu(domain.LocalDate{Year: 2026, Month: 7, Day: 24}, []string{"old", "menu", "only"})
	parsed := domain.NewMenu(today, []string{"new", "menu", "today"})
	future := domain.NewMenu(domain.LocalDate{Year: 2026, Month: 7, Day: 28}, []string{"future", "menu", "only"})
	tests := []struct {
		name  string
		menus []domain.Menu
	}{
		{name: "empty database"},
		{name: "stale database", menus: []domain.Menu{past}},
		{name: "today missing with future coverage", menus: []domain.Menu{future}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeImage(t, []byte("image"))
			repository := newMemoryRepository(test.menus...)
			hashes := &memoryHashStore{hash: "78805a221a988e79ef3f42d7c5bfd418"}
			var ocrCalls int
			service := NewMenuService(
				repository,
				crawlerFunc(func(context.Context) (string, error) { return path, nil }),
				ocrFunc(func(context.Context, []byte) ([]byte, error) {
					ocrCalls++
					return []byte("ocr"), nil
				}),
				successfulParser(parsed),
				hashes,
				WithDateClock(func() domain.LocalDate { return today }),
			)

			result, err := service.CrawlWithOptions(context.Background(), CrawlOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if result.Skipped || ocrCalls != 1 || repository.saveCalls != 1 || hashes.saveCalls != 1 {
				t.Fatalf("result=%+v OCR=%d repo saves=%d hash saves=%d", result, ocrCalls, repository.saveCalls, hashes.saveCalls)
			}
		})
	}
}

func TestTodayMissingDateRetriesMatchingImageAndReturnsRecoveredMenu(t *testing.T) {
	today := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	want := domain.NewMenu(today, []string{"new", "menu", "today"})
	repository := newMemoryRepository(domain.NewMenu(
		domain.LocalDate{Year: 2026, Month: 7, Day: 24},
		[]string{"old", "menu", "only"},
	))
	path := writeImage(t, []byte("image"))
	hashes := &memoryHashStore{hash: "78805a221a988e79ef3f42d7c5bfd418"}
	service := NewMenuService(
		repository,
		crawlerFunc(func(context.Context) (string, error) { return path, nil }),
		ocrFunc(func(context.Context, []byte) ([]byte, error) { return []byte("ocr"), nil }),
		successfulParser(want),
		hashes,
		WithDateClock(func() domain.LocalDate { return today }),
	)

	got, err := service.Today(context.Background(), today)
	if err != nil {
		t.Fatal(err)
	}
	if got.Date != want.Date || len(got.Menus) != len(want.Menus) || repository.saveCalls != 1 {
		t.Fatalf("menu=%+v repo saves=%d", got, repository.saveCalls)
	}
}

func TestConcurrentMissingTodayRequestsShareOneRefreshResult(t *testing.T) {
	today := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	want := domain.NewMenu(today, []string{"one", "two", "three"})
	repository := newMemoryRepository()
	hashes := &memoryHashStore{}
	path := writeImage(t, []byte("image"))
	started := make(chan struct{})
	release := make(chan struct{})
	var crawlCalls atomic.Int32
	var ocrCalls atomic.Int32
	service := NewMenuService(
		repository,
		crawlerFunc(func(context.Context) (string, error) {
			crawlCalls.Add(1)
			close(started)
			<-release
			return path, nil
		}),
		ocrFunc(func(context.Context, []byte) ([]byte, error) {
			ocrCalls.Add(1)
			return []byte("ocr"), nil
		}),
		successfulParser(want),
		hashes,
		WithDateClock(func() domain.LocalDate { return today }),
	)

	type result struct {
		menu domain.Menu
		err  error
	}
	results := make(chan result, 2)
	go func() {
		menu, err := service.Today(context.Background(), today)
		results <- result{menu: menu, err: err}
	}()
	<-started
	go func() {
		menu, err := service.Today(context.Background(), today)
		results <- result{menu: menu, err: err}
	}()

	waitForTodayWaiters(t, service, formatDate(today), 2)
	close(release)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.menu.Date != want.Date || len(result.menu.Menus) != len(want.Menus) {
			t.Fatalf("menu = %+v, want %+v", result.menu, want)
		}
	}
	if crawlCalls.Load() != 1 || ocrCalls.Load() != 1 || repository.saveCalls != 1 {
		t.Fatalf("crawl=%d OCR=%d saves=%d, want 1 each", crawlCalls.Load(), ocrCalls.Load(), repository.saveCalls)
	}
}

func TestMissingTodayRefreshOutlivesCanceledLeaderWhenFollowerRemains(t *testing.T) {
	today := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	want := domain.NewMenu(today, []string{"one", "two", "three"})
	path := writeImage(t, []byte("image"))
	started := make(chan struct{})
	release := make(chan struct{})
	service := NewMenuService(
		newMemoryRepository(),
		crawlerFunc(func(ctx context.Context) (string, error) {
			close(started)
			select {
			case <-release:
				return path, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}),
		ocrFunc(func(context.Context, []byte) ([]byte, error) { return []byte("ocr"), nil }),
		successfulParser(want),
		&memoryHashStore{},
		WithDateClock(func() domain.LocalDate { return today }),
	)

	leaderContext, cancelLeader := context.WithCancel(context.Background())
	leaderResult := make(chan error, 1)
	go func() {
		_, err := service.Today(leaderContext, today)
		leaderResult <- err
	}()
	<-started
	followerResult := make(chan resultMenu, 1)
	go func() {
		menu, err := service.Today(context.Background(), today)
		followerResult <- resultMenu{menu: menu, err: err}
	}()
	waitForTodayWaiters(t, service, formatDate(today), 2)

	cancelLeader()
	if err := <-leaderResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want context canceled", err)
	}
	close(release)
	follower := <-followerResult
	if follower.err != nil {
		t.Fatal(follower.err)
	}
	if follower.menu.Date != want.Date || len(follower.menu.Menus) != len(want.Menus) {
		t.Fatalf("follower menu = %+v, want %+v", follower.menu, want)
	}
}

func TestMissingTodayRefreshSurvivesZeroWaitersForLateRequest(t *testing.T) {
	today := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	want := domain.NewMenu(today, []string{"one", "two", "three"})
	path := writeImage(t, []byte("image"))
	started := make(chan struct{})
	release := make(chan struct{})
	var crawlCalls atomic.Int32
	service := NewMenuService(
		newMemoryRepository(),
		crawlerFunc(func(ctx context.Context) (string, error) {
			crawlCalls.Add(1)
			close(started)
			select {
			case <-release:
				return path, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}),
		ocrFunc(func(context.Context, []byte) ([]byte, error) { return []byte("ocr"), nil }),
		successfulParser(want),
		&memoryHashStore{},
		WithDateClock(func() domain.LocalDate { return today }),
	)

	requestContext, cancelRequest := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := service.Today(requestContext, today)
		result <- err
	}()
	<-started
	cancelRequest()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("request error = %v, want context canceled", err)
	}

	lateResult := make(chan resultMenu, 1)
	go func() {
		menu, err := service.Today(context.Background(), today)
		lateResult <- resultMenu{menu: menu, err: err}
	}()
	waitForTodayWaiters(t, service, formatDate(today), 1)
	close(release)
	late := <-lateResult
	if late.err != nil {
		t.Fatal(late.err)
	}
	if late.menu.Date != want.Date || len(late.menu.Menus) != len(want.Menus) {
		t.Fatalf("late menu = %+v, want %+v", late.menu, want)
	}
	if crawlCalls.Load() != 1 {
		t.Fatalf("crawl calls = %d, want 1", crawlCalls.Load())
	}
}

func TestMissingTodaySharedRefreshTimeoutEntersCooldown(t *testing.T) {
	today := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	clock := time.Date(2026, time.July, 27, 9, 0, 0, 0, time.UTC)
	var crawlCalls atomic.Int32
	service := NewMenuService(
		newMemoryRepository(),
		crawlerFunc(func(ctx context.Context) (string, error) {
			crawlCalls.Add(1)
			<-ctx.Done()
			return "", ctx.Err()
		}),
		ocrFunc(func(context.Context, []byte) ([]byte, error) { return []byte("ocr"), nil }),
		successfulParser(domain.NewMenu(today, []string{"one", "two", "three"})),
		&memoryHashStore{},
		WithDateClock(func() domain.LocalDate { return today }),
		WithTimeClock(func() time.Time { return clock }),
	)
	service.refreshTimeout = time.Millisecond

	for range 2 {
		if _, err := service.Today(context.Background(), today); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Today error = %v, want deadline exceeded", err)
		}
	}
	if crawlCalls.Load() != 1 {
		t.Fatalf("crawl calls = %d, want 1", crawlCalls.Load())
	}
}

func TestMissingTodayRechecksRepositoryBeforeExternalRefresh(t *testing.T) {
	today := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	want := domain.NewMenu(today, []string{"one", "two", "three"})
	var crawlCalls atomic.Int32
	service := NewMenuService(
		newMemoryRepository(want),
		crawlerFunc(func(context.Context) (string, error) {
			crawlCalls.Add(1)
			return "", errors.New("unexpected crawl")
		}),
		ocrFunc(func(context.Context, []byte) ([]byte, error) { return []byte("ocr"), nil }),
		successfulParser(want),
		&memoryHashStore{},
		WithDateClock(func() domain.LocalDate { return today }),
	)

	got, err := service.fetchMissingToday(context.Background(), today)
	if err != nil {
		t.Fatal(err)
	}
	if got.Date != want.Date || len(got.Menus) != len(want.Menus) {
		t.Fatalf("menu = %+v, want %+v", got, want)
	}
	if crawlCalls.Load() != 0 {
		t.Fatalf("crawl calls = %d, want 0", crawlCalls.Load())
	}
}

type resultMenu struct {
	menu domain.Menu
	err  error
}

func waitForTodayWaiters(t *testing.T, service *MenuService, key string, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		service.todayMu.Lock()
		call := service.todayCrawls[key]
		got := 0
		if call != nil {
			got = call.waiters
		}
		service.todayMu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("today crawl waiters = %d, want %d", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMissingTodayFailureCooldownPreventsHammeringAndThenRetries(t *testing.T) {
	today := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	clock := time.Date(2026, time.July, 27, 9, 0, 0, 0, time.UTC)
	downloadError := errors.New("vendor unavailable")
	var crawlCalls atomic.Int32
	crawler := crawlerFunc(func(context.Context) (string, error) {
		crawlCalls.Add(1)
		return "", downloadError
	})
	newService := func() *MenuService {
		return NewMenuService(
			newMemoryRepository(),
			crawler,
			ocrFunc(func(context.Context, []byte) ([]byte, error) { return []byte("ocr"), nil }),
			successfulParser(domain.NewMenu(today, []string{"one", "two", "three"})),
			&memoryHashStore{},
			WithDateClock(func() domain.LocalDate { return today }),
			WithTimeClock(func() time.Time { return clock }),
		)
	}
	service := newService()

	for range 2 {
		if _, err := service.Today(context.Background(), today); !errors.Is(err, downloadError) {
			t.Fatalf("Today error = %v, want %v", err, downloadError)
		}
	}
	if crawlCalls.Load() != 1 {
		t.Fatalf("crawl calls during cooldown = %d, want 1", crawlCalls.Load())
	}

	if err := service.Crawl(context.Background()); !errors.Is(err, downloadError) {
		t.Fatalf("explicit Crawl error = %v, want %v", err, downloadError)
	}
	if crawlCalls.Load() != 2 {
		t.Fatalf("crawl calls after explicit crawl = %d, want 2", crawlCalls.Load())
	}

	clock = clock.Add(todayFailureCooldown)
	if _, err := service.Today(context.Background(), today); !errors.Is(err, downloadError) {
		t.Fatalf("Today retry error = %v, want %v", err, downloadError)
	}
	if crawlCalls.Load() != 3 {
		t.Fatalf("crawl calls after cooldown = %d, want 3", crawlCalls.Load())
	}

	// The cooldown is intentionally in memory: a replacement process retries
	// immediately instead of inheriting stale suppression state.
	restarted := newService()
	if _, err := restarted.Today(context.Background(), today); !errors.Is(err, downloadError) {
		t.Fatalf("restarted Today error = %v, want %v", err, downloadError)
	}
	if crawlCalls.Load() != 4 {
		t.Fatalf("crawl calls after restart = %d, want 4", crawlCalls.Load())
	}
}

func TestMissingTodayNotUploadedResultUsesFailureCooldown(t *testing.T) {
	today := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	future := domain.NewMenu(domain.LocalDate{Year: 2026, Month: 7, Day: 28}, []string{"future", "menu", "only"})
	clock := time.Date(2026, time.July, 27, 9, 0, 0, 0, time.UTC)
	path := writeImage(t, []byte("image"))
	var crawlCalls atomic.Int32
	service := NewMenuService(
		newMemoryRepository(),
		crawlerFunc(func(context.Context) (string, error) {
			crawlCalls.Add(1)
			return path, nil
		}),
		ocrFunc(func(context.Context, []byte) ([]byte, error) { return []byte("ocr"), nil }),
		successfulParser(future),
		&memoryHashStore{},
		WithDateClock(func() domain.LocalDate { return today }),
		WithTimeClock(func() time.Time { return clock }),
	)

	for range 2 {
		if _, err := service.Today(context.Background(), today); !errors.Is(err, ErrMenuNotUploaded) {
			t.Fatalf("Today error = %v, want %v", err, ErrMenuNotUploaded)
		}
	}
	if crawlCalls.Load() != 1 {
		t.Fatalf("crawl calls = %d, want 1", crawlCalls.Load())
	}
}

func TestMatchingImageHashSkipsWhenRepositoryHasCurrentCoverage(t *testing.T) {
	today := domain.LocalDate{Year: 2026, Month: 7, Day: 27}
	tests := []struct {
		name string
		menu domain.Menu
	}{
		{name: "valid today is present", menu: domain.NewMenu(today, []string{"a", "b", "c"})},
		{name: "invalid today is present", menu: domain.NewMenu(today, []string{"holiday"})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeImage(t, []byte("image"))
			repository := newMemoryRepository(test.menu)
			hashes := &memoryHashStore{hash: "78805a221a988e79ef3f42d7c5bfd418"}
			var ocrCalls int
			service := NewMenuService(
				repository,
				crawlerFunc(func(context.Context) (string, error) { return path, nil }),
				ocrFunc(func(context.Context, []byte) ([]byte, error) {
					ocrCalls++
					return []byte("ocr"), nil
				}),
				successfulParser(test.menu),
				hashes,
				WithDateClock(func() domain.LocalDate { return today }),
			)

			result, err := service.CrawlWithOptions(context.Background(), CrawlOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !result.Skipped || ocrCalls != 0 || repository.saveCalls != 0 || hashes.saveCalls != 0 {
				t.Fatalf("result=%+v OCR=%d repo saves=%d hash saves=%d", result, ocrCalls, repository.saveCalls, hashes.saveCalls)
			}
		})
	}
}

func TestCrawlUpdatesHashOnlyAfterOCRAndRepositorySucceed(t *testing.T) {
	parseError := errors.New("parse failed")
	saveError := errors.New("save failed")
	tests := []struct {
		name          string
		parser        Parser
		repositoryErr error
		wantHashSaves int
		wantErr       error
	}{
		{
			name: "parser failure remains retryable",
			parser: parserFunc(func([]byte, io.Reader, domain.LocalDate) ([]domain.Menu, error) {
				return nil, parseError
			}),
			wantErr: parseError,
		},
		{
			name:          "repository failure remains retryable",
			parser:        successfulParser(domain.NewMenu(domain.LocalDate{Year: 2026, Month: 7, Day: 27}, []string{"a", "b", "c"})),
			repositoryErr: saveError,
			wantErr:       saveError,
		},
		{
			name:          "success commits hash",
			parser:        successfulParser(domain.NewMenu(domain.LocalDate{Year: 2026, Month: 7, Day: 27}, []string{"a", "b", "c"})),
			wantHashSaves: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeImage(t, []byte("image"))
			repository := newMemoryRepository()
			repository.saveErr = test.repositoryErr
			hashes := &memoryHashStore{}
			service := NewMenuService(
				repository,
				crawlerFunc(func(context.Context) (string, error) { return path, nil }),
				ocrFunc(func(context.Context, []byte) ([]byte, error) { return []byte("ocr"), nil }),
				test.parser,
				hashes,
			)

			_, err := service.CrawlWithOptions(context.Background(), CrawlOptions{})
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil && err != nil {
				t.Fatal(err)
			}
			if hashes.saveCalls != test.wantHashSaves {
				t.Fatalf("hash saves = %d, want %d", hashes.saveCalls, test.wantHashSaves)
			}
			if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("crawler temp file was not removed: %v", statErr)
			}
		})
	}
}

func TestCrawlRejectsEmptyParseWithoutUpdatingHash(t *testing.T) {
	path := writeImage(t, []byte("image"))
	hashes := &memoryHashStore{}
	service := NewMenuService(
		newMemoryRepository(),
		crawlerFunc(func(context.Context) (string, error) { return path, nil }),
		ocrFunc(func(context.Context, []byte) ([]byte, error) { return []byte("ocr"), nil }),
		parserFunc(func([]byte, io.Reader, domain.LocalDate) ([]domain.Menu, error) { return nil, nil }),
		hashes,
	)

	if _, err := service.CrawlWithOptions(context.Background(), CrawlOptions{}); !errors.Is(err, ErrNoMenusParsed) {
		t.Fatalf("error = %v", err)
	}
	if hashes.saveCalls != 0 {
		t.Fatalf("hash saves = %d", hashes.saveCalls)
	}
}

func TestCrawlDryRunParsesWithoutMutatingState(t *testing.T) {
	path := writeImage(t, []byte("image"))
	repository := newMemoryRepository()
	hashes := &memoryHashStore{}
	menu := domain.NewMenu(domain.LocalDate{Year: 2026, Month: 7, Day: 27}, []string{"a", "b", "c"})
	service := NewMenuService(
		repository,
		crawlerFunc(func(context.Context) (string, error) { return path, nil }),
		ocrFunc(func(context.Context, []byte) ([]byte, error) { return []byte("ocr"), nil }),
		successfulParser(menu),
		hashes,
		WithDateClock(fixedDate(2026, 7, 27)),
	)

	result, err := service.CrawlWithOptions(context.Background(), CrawlOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || len(result.Menus) != 1 || repository.saveCalls != 0 || hashes.saveCalls != 0 {
		t.Fatalf("result=%+v repo saves=%d hash saves=%d", result, repository.saveCalls, hashes.saveCalls)
	}
}

func TestConcurrentCrawlsUseOneOCRCallForIdenticalImages(t *testing.T) {
	var crawlCalls atomic.Int32
	var ocrCalls atomic.Int32
	repository := newMemoryRepository()
	hashes := &memoryHashStore{}
	menu := domain.NewMenu(domain.LocalDate{Year: 2026, Month: 7, Day: 27}, []string{"a", "b", "c"})
	crawler := crawlerFunc(func(context.Context) (string, error) {
		crawlCalls.Add(1)
		return writeConcurrentImage(t, []byte("same image")), nil
	})
	service := NewMenuService(
		repository,
		crawler,
		ocrFunc(func(context.Context, []byte) ([]byte, error) {
			ocrCalls.Add(1)
			time.Sleep(10 * time.Millisecond)
			return []byte("ocr"), nil
		}),
		successfulParser(menu),
		hashes,
		WithDateClock(fixedDate(2026, 7, 27)),
	)

	start := make(chan struct{})
	errorsChannel := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			errorsChannel <- service.Crawl(context.Background())
		}()
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-errorsChannel; err != nil {
			t.Fatal(err)
		}
	}
	if crawlCalls.Load() != 2 || ocrCalls.Load() != 1 || repository.saveCalls != 1 {
		t.Fatalf("crawl=%d OCR=%d saves=%d", crawlCalls.Load(), ocrCalls.Load(), repository.saveCalls)
	}
}

func writeConcurrentImage(t *testing.T, body []byte) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "menu-*.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return file.Name()
}

func TestParseAndSaveLeavesCallerOwnedFile(t *testing.T) {
	path := writeImage(t, []byte("image"))
	repository := newMemoryRepository()
	menu := domain.NewMenu(domain.LocalDate{Year: 2026, Month: 7, Day: 27}, []string{"a", "b", "c"})
	service := NewMenuService(
		repository,
		nil,
		ocrFunc(func(context.Context, []byte) ([]byte, error) { return []byte("ocr"), nil }),
		successfulParser(menu),
		nil,
	)

	if _, err := service.ParseAndSave(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("caller-owned file was removed: %v", err)
	}
}

func TestStartBackgroundRefreshDoesNotBlockReadiness(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repository := newMemoryRepository()
	started := make(chan struct{})
	service := NewMenuService(
		repository,
		crawlerFunc(func(ctx context.Context) (string, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		}),
		ocrFunc(func(context.Context, []byte) ([]byte, error) { return nil, nil }),
		parserFunc(func([]byte, io.Reader, domain.LocalDate) ([]domain.Menu, error) { return nil, nil }),
		&memoryHashStore{},
		WithDateClock(fixedDate(2026, 7, 25)),
	)

	result := service.StartBackgroundRefresh(ctx)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not start")
	}
	if err := service.Ready(context.Background()); err != nil {
		t.Fatalf("readiness depends on background crawl: %v", err)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("background error = %v", err)
	}
}

func TestRefreshIfStaleSkipsWhenFutureMenuExists(t *testing.T) {
	future := domain.NewMenu(domain.LocalDate{Year: 2026, Month: 7, Day: 27}, []string{"a", "b", "c"})
	var crawled atomic.Bool
	service := NewMenuService(
		newMemoryRepository(future),
		crawlerFunc(func(context.Context) (string, error) {
			crawled.Store(true)
			return "", nil
		}),
		nil,
		nil,
		&memoryHashStore{},
		WithDateClock(fixedDate(2026, 7, 25)),
	)

	if err := service.RefreshIfStale(context.Background()); err != nil {
		t.Fatal(err)
	}
	if crawled.Load() {
		t.Fatal("fresh repository triggered a crawl")
	}
}

func TestReadFileLimitedRejectsLargeImage(t *testing.T) {
	path := writeImage(t, []byte("12345"))
	service := NewMenuService(
		newMemoryRepository(),
		nil,
		ocrFunc(func(context.Context, []byte) ([]byte, error) { return nil, nil }),
		parserFunc(func([]byte, io.Reader, domain.LocalDate) ([]domain.Menu, error) { return nil, nil }),
		nil,
		WithMaxImageBytes(4),
	)
	if _, err := service.ParseAndSave(context.Background(), path); err == nil {
		t.Fatal("expected oversized image error")
	}
}
