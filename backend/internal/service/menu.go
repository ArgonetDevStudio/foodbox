package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

const (
	defaultMaxImageBytes int64 = 10 << 20
	todayFailureCooldown       = 30 * time.Second
	// Download can make three 15-second HTTP requests before the 15-second OCR
	// request. Keep a small margin for parsing and persistence.
	todayRefreshTimeout = 75 * time.Second
)

var (
	ErrNotConfigured         = errors.New("service dependency not configured")
	ErrNoMenusParsed         = errors.New("OCR response contained no menus")
	ErrMenuNotUploaded error = menuNotUploadedError{}
)

type menuNotUploadedError struct{}

func (menuNotUploadedError) Error() string     { return "Today Menu is not uploaded yet" }
func (menuNotUploadedError) HTTPStatus() int   { return 404 }
func (menuNotUploadedError) ErrorCode() string { return "MENU_NOT_UPLOADED" }

type Repository interface {
	FindAll(ctx context.Context) ([]domain.Menu, error)
	FindByDate(ctx context.Context, date domain.LocalDate) (domain.Menu, bool, error)
	SaveAll(ctx context.Context, menus []domain.Menu) error
}

// WritableChecker is an optional repository capability used by readiness.
// File-backed repositories should implement it to verify that their data
// directory can still create and atomically replace files.
type WritableChecker interface {
	CheckWritable(ctx context.Context) error
}

type Crawler interface {
	Download(ctx context.Context) (path string, err error)
}

type OCRClient interface {
	Recognize(ctx context.Context, image []byte) (response []byte, err error)
}

type OCRClientFunc func(context.Context, []byte) ([]byte, error)

func (f OCRClientFunc) Recognize(ctx context.Context, image []byte) ([]byte, error) {
	return f(ctx, image)
}

type Parser interface {
	Parse(response []byte, image io.Reader, today domain.LocalDate) ([]domain.Menu, error)
}

type ParserFunc func([]byte, io.Reader, domain.LocalDate) ([]domain.Menu, error)

func (f ParserFunc) Parse(response []byte, image io.Reader, today domain.LocalDate) ([]domain.Menu, error) {
	return f(response, image, today)
}

// HashStore persists the last successfully processed image hash. Implementations
// should keep this metadata next to the menu database so it survives restarts.
type HashStore interface {
	Load(ctx context.Context) (hash string, err error)
	Save(ctx context.Context, hash string) error
}

type MenuServiceOption func(*MenuService)

func WithDateClock(now func() domain.LocalDate) MenuServiceOption {
	return func(service *MenuService) {
		if now != nil {
			service.now = now
		}
	}
}

func WithMaxImageBytes(max int64) MenuServiceOption {
	return func(service *MenuService) {
		if max > 0 {
			service.maxImageBytes = max
		}
	}
}

func WithTimeClock(now func() time.Time) MenuServiceOption {
	return func(service *MenuService) {
		if now != nil {
			service.timeNow = now
		}
	}
}

// WithRemoveFile is intended for tests that need to observe temporary-file
// cleanup. Production callers should use the default os.Remove implementation.
func WithRemoveFile(remove func(string) error) MenuServiceOption {
	return func(service *MenuService) {
		if remove != nil {
			service.removeFile = remove
		}
	}
}

type MenuService struct {
	repository Repository
	crawler    Crawler
	ocr        OCRClient
	parser     Parser
	hashes     HashStore

	now            func() domain.LocalDate
	timeNow        func() time.Time
	maxImageBytes  int64
	refreshTimeout time.Duration
	removeFile     func(string) error
	crawlGate      chan struct{}
	startupOnce    sync.Once

	todayMu       sync.Mutex
	todayCrawls   map[string]*todayCrawlCall
	todayFailures map[string]todayCrawlFailure
}

type todayCrawlCall struct {
	done    chan struct{}
	waiters int
	menu    domain.Menu
	err     error
}

type todayCrawlFailure struct {
	err        error
	retryAfter time.Time
}

func NewMenuService(
	repository Repository,
	crawler Crawler,
	ocr OCRClient,
	parser Parser,
	hashes HashStore,
	options ...MenuServiceOption,
) *MenuService {
	service := &MenuService{
		repository:     repository,
		crawler:        crawler,
		ocr:            ocr,
		parser:         parser,
		hashes:         hashes,
		now:            todayInSeoul,
		timeNow:        time.Now,
		maxImageBytes:  defaultMaxImageBytes,
		refreshTimeout: todayRefreshTimeout,
		removeFile:     os.Remove,
		crawlGate:      make(chan struct{}, 1),
		todayCrawls:    make(map[string]*todayCrawlCall),
		todayFailures:  make(map[string]todayCrawlFailure),
	}
	for _, option := range options {
		option(service)
	}
	return service
}

// Ready validates that the file repository is readable without waiting for any
// external crawling or OCR dependency.
func (s *MenuService) Ready(ctx context.Context) error {
	if s.repository == nil {
		return fmt.Errorf("repository: %w", ErrNotConfigured)
	}
	_, err := s.repository.FindAll(ctx)
	if err != nil {
		return fmt.Errorf("read menu repository: %w", err)
	}
	if checker, ok := s.repository.(WritableChecker); ok {
		if err := checker.CheckWritable(ctx); err != nil {
			return fmt.Errorf("menu repository is not writable: %w", err)
		}
	}
	if s.hashes != nil {
		if _, err := s.hashes.Load(ctx); err != nil {
			return fmt.Errorf("read image hash metadata: %w", err)
		}
	}
	return nil
}

func (s *MenuService) FindAll(ctx context.Context) ([]domain.Menu, error) {
	if s.repository == nil {
		return nil, fmt.Errorf("repository: %w", ErrNotConfigured)
	}
	menus, err := s.repository.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("find all menus: %w", err)
	}
	return menus, nil
}

func (s *MenuService) Today(ctx context.Context, date domain.LocalDate) (domain.Menu, error) {
	if isWeekend(date) {
		return domain.NewMenu(date, []string{"주말에는 도시락이 없습니다."}), nil
	}
	if s.repository == nil {
		return domain.Menu{}, fmt.Errorf("repository: %w", ErrNotConfigured)
	}

	menu, found, err := s.repository.FindByDate(ctx, date)
	if err != nil {
		return domain.Menu{}, fmt.Errorf("find menu for %s: %w", formatDate(date), err)
	}
	if found {
		return menu, nil
	}
	return s.fetchMissingToday(ctx, date)
}

// fetchMissingToday coalesces automatic missing-menu refreshes for one date.
// The bounded refresh outlives individual requests, preventing disconnects
// from triggering replacement work. Its failure cooldown is deliberately
// process-local, so a restart retries immediately. Explicit Crawl bypasses it.
func (s *MenuService) fetchMissingToday(ctx context.Context, date domain.LocalDate) (domain.Menu, error) {
	if err := ctx.Err(); err != nil {
		return domain.Menu{}, err
	}
	key := formatDate(date)
	s.todayMu.Lock()
	now := s.timeNow()
	for failedDate, failure := range s.todayFailures {
		if !now.Before(failure.retryAfter) {
			delete(s.todayFailures, failedDate)
		}
	}
	if failure, found := s.todayFailures[key]; found {
		s.todayMu.Unlock()
		return domain.Menu{}, failure.err
	}
	if call, found := s.todayCrawls[key]; found {
		call.waiters++
		s.todayMu.Unlock()
		return s.waitForTodayCrawl(ctx, key, call)
	}
	crawlContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.refreshTimeout)
	call := &todayCrawlCall{done: make(chan struct{}), waiters: 1}
	s.todayCrawls[key] = call
	s.todayMu.Unlock()
	go s.runTodayCrawl(crawlContext, cancel, key, date, call)
	return s.waitForTodayCrawl(ctx, key, call)
}

func (s *MenuService) waitForTodayCrawl(ctx context.Context, key string, call *todayCrawlCall) (domain.Menu, error) {
	select {
	case <-call.done:
		return call.menu, call.err
	case <-ctx.Done():
		s.todayMu.Lock()
		call.waiters--
		s.todayMu.Unlock()
		return domain.Menu{}, ctx.Err()
	}
}

func (s *MenuService) runTodayCrawl(
	ctx context.Context,
	cancel context.CancelFunc,
	key string,
	date domain.LocalDate,
	call *todayCrawlCall,
) {
	defer cancel()
	menu, err := s.crawlAndFind(ctx, date)

	s.todayMu.Lock()
	call.menu, call.err = menu, err
	if current, found := s.todayCrawls[key]; found && current == call {
		delete(s.todayCrawls, key)
		if err == nil {
			delete(s.todayFailures, key)
		} else {
			s.todayFailures[key] = todayCrawlFailure{
				err:        err,
				retryAfter: s.timeNow().Add(todayFailureCooldown),
			}
		}
	}
	close(call.done)
	s.todayMu.Unlock()
}

func (s *MenuService) crawlAndFind(ctx context.Context, date domain.LocalDate) (domain.Menu, error) {
	menu, found, err := s.repository.FindByDate(ctx, date)
	if err != nil {
		return domain.Menu{}, fmt.Errorf("recheck menu for %s: %w", formatDate(date), err)
	}
	if found {
		return menu, nil
	}
	if err := s.Crawl(ctx); err != nil {
		return domain.Menu{}, fmt.Errorf("crawl missing menu for %s: %w", formatDate(date), err)
	}
	menu, found, err = s.repository.FindByDate(ctx, date)
	if err != nil {
		return domain.Menu{}, fmt.Errorf("find crawled menu for %s: %w", formatDate(date), err)
	}
	if !found {
		return domain.Menu{}, ErrMenuNotUploaded
	}
	return menu, nil
}

type CrawlOptions struct {
	// DryRun downloads and parses the current image but does not update menus or
	// the persisted image hash.
	DryRun bool
}

type CrawlResult struct {
	Menus   []domain.Menu
	Hash    string
	Skipped bool
	DryRun  bool
}

func (s *MenuService) Crawl(ctx context.Context) error {
	_, err := s.CrawlWithOptions(ctx, CrawlOptions{})
	return err
}

func (s *MenuService) CrawlWithOptions(ctx context.Context, options CrawlOptions) (CrawlResult, error) {
	if err := s.acquireCrawl(ctx); err != nil {
		return CrawlResult{}, err
	}
	defer s.releaseCrawl()

	if s.crawler == nil || s.ocr == nil || s.parser == nil || s.hashes == nil || s.repository == nil {
		return CrawlResult{}, fmt.Errorf("crawl: %w", ErrNotConfigured)
	}

	path, err := s.crawler.Download(ctx)
	if err != nil {
		return CrawlResult{}, fmt.Errorf("download menu image: %w", err)
	}
	if path == "" {
		return CrawlResult{}, errors.New("download menu image: empty path")
	}
	defer func() { _ = s.removeFile(path) }()

	image, err := readFileLimited(path, s.maxImageBytes)
	if err != nil {
		return CrawlResult{}, fmt.Errorf("read menu image: %w", err)
	}
	hash := fmt.Sprintf("%x", md5.Sum(image))
	lastHash, err := s.hashes.Load(ctx)
	if err != nil {
		return CrawlResult{}, fmt.Errorf("load image hash: %w", err)
	}
	if hash == lastHash && !options.DryRun {
		covered, err := s.hasCurrentCoverage(ctx)
		if err != nil {
			return CrawlResult{}, err
		}
		if covered {
			return CrawlResult{Hash: hash, Skipped: true}, nil
		}
	}

	menus, err := s.parse(ctx, image)
	if err != nil {
		return CrawlResult{}, err
	}
	result := CrawlResult{Menus: menus, Hash: hash, DryRun: options.DryRun}
	if options.DryRun {
		return result, nil
	}
	if err := s.repository.SaveAll(ctx, menus); err != nil {
		return CrawlResult{}, fmt.Errorf("save parsed menus: %w", err)
	}
	// The hash is deliberately committed last. A failed OCR or DB write must be
	// retried on the next crawl instead of suppressing the same image forever.
	if err := s.hashes.Save(ctx, hash); err != nil {
		return CrawlResult{}, fmt.Errorf("save image hash: %w", err)
	}
	return result, nil
}

func (s *MenuService) ParseAndSave(ctx context.Context, path string) ([]domain.Menu, error) {
	if s.repository == nil || s.ocr == nil || s.parser == nil {
		return nil, fmt.Errorf("parse and save: %w", ErrNotConfigured)
	}
	image, err := readFileLimited(path, s.maxImageBytes)
	if err != nil {
		return nil, fmt.Errorf("read uploaded image: %w", err)
	}
	menus, err := s.parse(ctx, image)
	if err != nil {
		return nil, err
	}
	if err := s.repository.SaveAll(ctx, menus); err != nil {
		return nil, fmt.Errorf("save parsed menus: %w", err)
	}
	return menus, nil
}

// SaveManual validates and persists one operator-supplied menu. It shares the
// crawl gate so an OCR refresh cannot overwrite a manual correction while the
// correction is being written.
func (s *MenuService) SaveManual(ctx context.Context, date domain.LocalDate, menus []string) (domain.Menu, error) {
	if s.repository == nil {
		return domain.Menu{}, fmt.Errorf("save manual menu: %w", ErrNotConfigured)
	}
	if err := date.Validate(); err != nil {
		return domain.Menu{}, fmt.Errorf("save manual menu date: %w", err)
	}
	if len(menus) < 3 {
		return domain.Menu{}, errors.New("manual menu must contain at least three items")
	}
	for index, menu := range menus {
		if strings.TrimSpace(menu) == "" {
			return domain.Menu{}, fmt.Errorf("manual menu item %d must not be empty", index)
		}
	}

	if err := s.acquireCrawl(ctx); err != nil {
		return domain.Menu{}, fmt.Errorf("save manual menu: %w", err)
	}
	defer s.releaseCrawl()

	menu := domain.NewMenu(date, menus)
	if err := s.repository.SaveAll(ctx, []domain.Menu{menu}); err != nil {
		return domain.Menu{}, fmt.Errorf("save manual menu: %w", err)
	}
	return menu, nil
}

func (s *MenuService) parse(ctx context.Context, image []byte) ([]domain.Menu, error) {
	response, err := s.ocr.Recognize(ctx, image)
	if err != nil {
		return nil, fmt.Errorf("recognize menu image: %w", err)
	}
	menus, err := s.parser.Parse(response, bytes.NewReader(image), s.now())
	if err != nil {
		return nil, fmt.Errorf("parse OCR response: %w", err)
	}
	if len(menus) == 0 {
		return nil, ErrNoMenusParsed
	}
	return menus, nil
}

// hasCurrentCoverage prevents a persisted image hash from suppressing recovery
// of a missing current-day record. Future records alone are not enough because
// Today must still be able to repair a gap in an otherwise fresh database.
func (s *MenuService) hasCurrentCoverage(ctx context.Context) (bool, error) {
	_, found, err := s.repository.FindByDate(ctx, s.now())
	if err != nil {
		return false, fmt.Errorf("check menu coverage: %w", err)
	}
	return found, nil
}

// RefreshIfStale refreshes the database when it has no menu beyond today. It is
// synchronous so a scheduler can own error handling; StartBackgroundRefresh is
// the non-blocking startup convenience.
func (s *MenuService) RefreshIfStale(ctx context.Context) error {
	menus, err := s.FindAll(ctx)
	if err != nil {
		return err
	}
	today := s.now()
	for _, menu := range menus {
		if compareDate(menu.Date, today) > 0 {
			return nil
		}
	}
	return s.Crawl(ctx)
}

// StartBackgroundRefresh starts at most one startup refresh and returns
// immediately. The buffered result channel receives the eventual error and is
// then closed, so readiness never depends on the vendor or OCR APIs.
func (s *MenuService) StartBackgroundRefresh(ctx context.Context) <-chan error {
	result := make(chan error, 1)
	started := false
	s.startupOnce.Do(func() {
		started = true
		go func() {
			defer close(result)
			result <- s.RefreshIfStale(ctx)
		}()
	})
	if !started {
		close(result)
	}
	return result
}

func (s *MenuService) acquireCrawl(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	select {
	case s.crawlGate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *MenuService) releaseCrawl() {
	<-s.crawlGate
}

func readFileLimited(path string, max int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("image exceeds %d bytes", max)
	}
	return data, nil
}
