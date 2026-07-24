package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

const defaultMaxImageBytes int64 = 10 << 20

var (
	ErrNotConfigured         = errors.New("service dependency not configured")
	ErrNoMenusParsed         = errors.New("OCR response contained no menus")
	ErrMenuNotUploaded error = menuNotUploadedError{}
)

type menuNotUploadedError struct{}

func (menuNotUploadedError) Error() string     { return "menu not uploaded" }
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

	now           func() domain.LocalDate
	maxImageBytes int64
	removeFile    func(string) error
	crawlGate     chan struct{}
	startupOnce   sync.Once
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
		repository:    repository,
		crawler:       crawler,
		ocr:           ocr,
		parser:        parser,
		hashes:        hashes,
		now:           todayInSeoul,
		maxImageBytes: defaultMaxImageBytes,
		removeFile:    os.Remove,
		crawlGate:     make(chan struct{}, 1),
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
		return CrawlResult{Hash: hash, Skipped: true}, nil
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
